package servertest_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/servertest"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/derp"
	"tailscale.com/derp/derphttp"
	"tailscale.com/net/netmon"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
	"tailscale.com/types/netmap"
)

const derpWait = 10 * time.Second

// derpMapServer serves a DERP map JSON and counts the fetches.
func derpMapServer(t *testing.T, regions ...tailcfg.DERPRegion) (*httptest.Server, *atomic.Int32) {
	t.Helper()

	var fetches atomic.Int32

	dm := tailcfg.DERPMap{Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{}}
	for _, r := range regions {
		dm.Regions[r.RegionID] = &r
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)

		_ = json.NewEncoder(w).Encode(dm)
	}))
	t.Cleanup(srv.Close)

	return srv, &fetches
}

func derpRegion(id tailcfg.DERPRegionID, code string) tailcfg.DERPRegion {
	return tailcfg.DERPRegion{
		RegionID:   id,
		RegionCode: code,
		RegionName: code,
		Nodes: []*tailcfg.DERPNode{{
			Name:     code + "a",
			RegionID: id,
			HostName: code + ".derp.example",
		}},
	}
}

// connectRelay connects a DERP client with the key to the server's
// embedded relay and proves it is admitted with a ping.
func connectRelay(ctx context.Context, t *testing.T, url string, k key.NodePrivate) *derphttp.Client {
	t.Helper()

	relay, err := derphttp.NewClient(k, url, t.Logf, netmon.NewStatic())
	require.NoError(t, err)
	t.Cleanup(func() { _ = relay.Close() })
	require.NoError(t, relay.Connect(ctx))
	require.NoError(t, relay.SendPing([8]byte{1}))

	msg, err := relay.Recv()
	for err == nil {
		if _, ok := msg.(derp.PongMessage); ok {
			break
		}

		msg, err = relay.Recv()
	}

	require.NoError(t, err)

	return relay
}

// relayDropped reports whether the relay ends the client's connection:
// a read fails, rather than blocking until the deadline.
func relayDropped(relay *derphttp.Client) bool {
	done := make(chan bool, 1)

	go func() {
		_, err := relay.Recv()
		done <- err != nil
	}()

	select {
	case dropped := <-done:
		return dropped
	case <-time.After(derpWait):
		return false
	}
}

func regionIDs(body map[string]any) []int {
	regions, _ := body["regions"].([]any)
	ids := make([]int, 0, len(regions))

	for _, r := range regions {
		region, _ := r.(map[string]any)
		id, _ := region["id"].(float64)
		ids = append(ids, int(id))
	}

	return ids
}

func regionSource(body map[string]any, id int) string {
	regions, _ := body["regions"].([]any)
	for _, r := range regions {
		region, _ := r.(map[string]any)
		if got, _ := region["id"].(float64); int(got) == id {
			source, _ := region["source"].(string)

			return source
		}
	}

	return ""
}

// TestDERPSettingsEndToEnd proves the runtime relay settings: the file's
// map reaches the client first, a PUT fetches a new map URL, adds a custom
// region and turns the embedded relay on, all of which reach the client in
// the next netmap; a relay client then connects to the embedded relay; a
// refresh refetches; a bad URL or an empty map is refused without a
// change; and a reset returns to the file. The subtests build on one
// another, so they run in order.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestDERPSettingsEndToEnd(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t, servertest.WithRealListener())
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1/derp"

	owner := srv.CreateUser(t, "derp-owner")
	ownerKey := srv.CreateAPIKey(t, owner)
	member := srv.CreateUser(t, "derp-member")
	memberKey := srv.CreateAPIKey(t, member)

	node := servertest.NewClient(t, srv, "derp-1", servertest.WithUser(owner))

	node.WaitForCondition(t, "netmap with the file's DERP map", derpWait, func(nm *netmap.NetworkMap) bool {
		return nm.DERPMap != nil && len(nm.DERPMap.Regions) == 1
	})

	mapSrv, fetches := derpMapServer(t, derpRegion(10, "nyc"), derpRegion(11, "sfo"))

	t.Run("get reports the file", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1, nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, false, body["overridden"])
		assert.Equal(t, true, body["relayAvailable"])
		assert.Equal(t, false, body["relayRunning"])
		assert.Equal(t, []int{900}, regionIDs(body))
		assert.Equal(t, "config", regionSource(body, 900))
	})

	t.Run("member cannot write", func(t *testing.T) {
		status, body := apiCall(t, client, memberKey, http.MethodPut, v1, map[string]any{
			"server": map[string]any{"enabled": false},
		})
		assert.Equal(t, http.StatusForbidden, status, body)
	})

	t.Run("set fetches, adds a region and starts the relay", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPut, v1, map[string]any{
			"urls":            []string{mapSrv.URL},
			"autoUpdate":      true,
			"updateFrequency": "1h",
			"regions": []map[string]any{{
				"id":   20,
				"code": "sgp",
				"nodes": []map[string]any{{
					"hostName": "sgp.derp.example",
					"ipv4":     "203.0.113.5",
				}},
			}},
			"server": map[string]any{
				"enabled":       true,
				"regionId":      999,
				"regionCode":    "headscale",
				"verifyClients": true,
				"stunAddr":      "127.0.0.1:0",
			},
		})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, body["overridden"])
		assert.Equal(t, true, body["relayRunning"])
		assert.NotEmpty(t, body["stunAddr"])
		assert.Equal(t, []int{10, 11, 20, 900, 999}, regionIDs(body))
		assert.Equal(t, "url", regionSource(body, 10))
		assert.Equal(t, "custom", regionSource(body, 20))
		assert.Equal(t, "embedded", regionSource(body, 999))
		assert.Equal(t, int32(1), fetches.Load())

		effective, _ := body["effective"].(map[string]any)
		assert.Equal(t, "1h0m0s", effective["updateFrequency"])

		node.WaitForCondition(t, "netmap with the new DERP map", derpWait, func(nm *netmap.NetworkMap) bool {
			if nm.DERPMap == nil || len(nm.DERPMap.Regions) != 5 {
				return false
			}

			embedded := nm.DERPMap.Regions[999]

			return embedded != nil && len(embedded.Nodes) == 1 && embedded.Nodes[0].InsecureForTests
		})

		nm := node.Netmap()
		assert.Equal(t, "sgp.derp.example", nm.DERPMap.Regions[20].Nodes[0].HostName)
		assert.Equal(t, "sgp", nm.DERPMap.Regions[20].RegionName, "an empty name takes the code")
	})

	t.Run("a tailnet node reaches the embedded relay, a stranger does not", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), derpWait)
		defer cancel()

		connectRelay(ctx, t, srv.URL+"/derp", node.NodePrivateKey())

		stranger, err := derphttp.NewClient(key.NewNode(), srv.URL+"/derp", t.Logf, netmon.NewStatic())
		require.NoError(t, err)
		t.Cleanup(func() { _ = stranger.Close() })

		err = stranger.Connect(ctx)
		if err == nil {
			_, err = stranger.Recv()
		}

		assert.Error(t, err, "verify_clients refuses a key the tailnet does not know")
	})

	t.Run("turning verification on drops the clients it no longer admits", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), derpWait)
		defer cancel()

		open := map[string]any{
			"urls": []string{mapSrv.URL},
			"regions": []map[string]any{{
				"id": 20, "code": "sgp",
				"nodes": []map[string]any{{"hostName": "sgp.derp.example", "ipv4": "203.0.113.5"}},
			}},
			"server": map[string]any{
				"enabled": true, "regionId": 999, "regionCode": "headscale",
				"verifyClients": false, "stunAddr": "127.0.0.1:0",
			},
		}
		status, body := apiCall(t, client, ownerKey, http.MethodPut, v1, open)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, int32(1), fetches.Load(), "an unchanged URL list is not fetched again")

		stranger := connectRelay(ctx, t, srv.URL+"/derp", key.NewNode())
		member := connectRelay(ctx, t, srv.URL+"/derp", node.NodePrivateKey())

		verified := map[string]any{
			"urls": []string{mapSrv.URL},
			"regions": []map[string]any{{
				"id": 20, "code": "sgp",
				"nodes": []map[string]any{{"hostName": "sgp.derp.example", "ipv4": "203.0.113.5"}},
			}},
			"server": map[string]any{
				"enabled": true, "regionId": 999, "regionCode": "headscale", "stunAddr": "127.0.0.1:0",
			},
		}
		status, body = apiCall(t, client, ownerKey, http.MethodPut, v1, verified)
		require.Equal(t, http.StatusOK, status, body)

		effective, _ := body["effective"].(map[string]any)
		server, _ := effective["server"].(map[string]any)
		assert.Equal(t, true, server["verifyClients"], "a request that says nothing about verification gets it")

		assert.True(t, relayDropped(stranger), "the stranger is dropped")
		assert.True(t, relayDropped(member), "every client reconnects under the new rule")
		connectRelay(ctx, t, srv.URL+"/derp", node.NodePrivateKey())
	})

	t.Run("refresh refetches", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/refresh", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, int32(2), fetches.Load())
		assert.Empty(t, body["fetchError"])
	})

	t.Run("an unreachable map is refused and nothing changes", func(t *testing.T) {
		dead := httptest.NewServer(http.NotFoundHandler())
		dead.Close()

		status, body := apiCall(t, client, ownerKey, http.MethodPut, v1, map[string]any{
			"urls":   []string{dead.URL},
			"server": map[string]any{"enabled": false},
		})
		assert.Equal(t, http.StatusBadGateway, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1, nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, body["relayRunning"], "a failed write keeps the relay as it was")
		assert.Equal(t, []int{10, 11, 20, 900, 999}, regionIDs(body))
		assert.NotEmpty(t, body["fetchError"])
	})

	t.Run("invalid settings are refused", func(t *testing.T) {
		for name, body := range map[string]map[string]any{
			"bad url": {"urls": []string{"ftp://x"}, "server": map[string]any{"enabled": false}},
			"region without relays": {
				"urls":    []string{mapSrv.URL},
				"regions": []map[string]any{{"id": 30, "code": "x"}},
				"server":  map[string]any{"enabled": false},
			},
			"duplicate region id": {
				"urls": []string{mapSrv.URL},
				"regions": []map[string]any{
					{"id": 30, "code": "x", "nodes": []map[string]any{{"hostName": "a"}}},
					{"id": 30, "code": "y", "nodes": []map[string]any{{"hostName": "b"}}},
				},
				"server": map[string]any{"enabled": false},
			},
			"relay without stun": {
				"urls":   []string{mapSrv.URL},
				"server": map[string]any{"enabled": true, "regionId": 999, "regionCode": "hs"},
			},
			"too frequent": {
				"urls": []string{mapSrv.URL}, "autoUpdate": true, "updateFrequency": "10s",
				"server": map[string]any{"enabled": false},
			},
		} {
			status, resp := apiCall(t, client, ownerKey, http.MethodPut, v1, body)
			assert.Equal(t, http.StatusBadRequest, status, "%s: %v", name, resp)
		}
	})

	t.Run("turning the relay off stops it and drops its clients", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), derpWait)
		defer cancel()

		connected := connectRelay(ctx, t, srv.URL+"/derp", node.NodePrivateKey())

		status, body := apiCall(t, client, ownerKey, http.MethodPut, v1, map[string]any{
			"urls":   []string{mapSrv.URL},
			"server": map[string]any{"enabled": false},
		})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, false, body["relayRunning"])
		assert.Empty(t, body["stunAddr"])
		assert.Equal(t, []int{10, 11, 900}, regionIDs(body))
		assert.True(t, relayDropped(connected), "a connected client is dropped with the relay")

		resp, err := client.Get(srv.URL + "/derp") //nolint:noctx // test client
		require.NoError(t, err)

		_ = resp.Body.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)

		node.WaitForCondition(t, "netmap without the embedded relay", derpWait, func(nm *netmap.NetworkMap) bool {
			return nm.DERPMap != nil && len(nm.DERPMap.Regions) == 3
		})
	})

	t.Run("reset returns to the file", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodDelete, v1, nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, false, body["overridden"])
		assert.Equal(t, []int{900}, regionIDs(body))

		node.WaitForCondition(t, "netmap with the file's map again", derpWait, func(nm *netmap.NetworkMap) bool {
			return nm.DERPMap != nil && len(nm.DERPMap.Regions) == 1 && nm.DERPMap.Regions[900] != nil
		})

		status, body = apiCall(t, client, ownerKey, http.MethodGet, srv.URL+"/api/v1/audit", nil)
		require.Equal(t, http.StatusOK, status, body)

		actions := map[string]bool{}

		events, _ := body["events"].([]any)
		for _, e := range events {
			event, _ := e.(map[string]any)
			action, _ := event["action"].(string)
			actions[action] = true
		}

		assert.True(t, actions["derp.set"], "audit: %v", actions)
		assert.True(t, actions["derp.refresh"], "audit: %v", actions)
		assert.True(t, actions["derp.reset"], "audit: %v", actions)
	})
}

// TestDERPFetchedMapIsSanitized proves a fetched map with a null relay
// and relays under the wrong region id is served without them rather
// than crashing the refresh.
func TestDERPFetchedMapIsSanitized(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)

	dirty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Regions":{
			"10":{"RegionID":10,"RegionCode":"nyc","Nodes":[
				null,
				{"Name":"a","RegionID":99,"HostName":"a.example"},
				{"Name":"a","HostName":"dup.example"}
			]},
			"11":{"RegionID":11,"RegionCode":"empty","Nodes":[null]}
		}}`))
	}))
	t.Cleanup(dirty.Close)

	_, _, err := srv.State().SetDERP(t.Context(), types.DERPSettings{
		URLs:   []string{dirty.URL},
		Server: types.DERPServerSettings{Enabled: false},
	})
	require.NoError(t, err)

	_, err = srv.State().RefreshDERPMap(t.Context())
	require.NoError(t, err, "a refresh of the same map survives the null relay")

	regions := srv.State().DERPMap().AsStruct().Regions
	require.Contains(t, regions, tailcfg.DERPRegionID(10))
	assert.Empty(t, regions[11].Nodes, "a region whose only relay was null is left without relays")
	require.Len(t, regions[10].Nodes, 1, "the null and the duplicate relay are dropped")
	assert.Equal(t, tailcfg.DERPRegionID(10), regions[10].Nodes[0].RegionID, "the relay carries the map's region id")
	assert.Equal(t, "a.example", regions[10].Nodes[0].HostName)
}

// TestDERPSetThroughState proves the state entry point the API and the
// CLI share.
func TestDERPSetThroughState(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	mapSrv, _ := derpMapServer(t, derpRegion(10, "nyc"))

	_, _, err := srv.State().SetDERP(t.Context(), types.DERPSettings{
		URLs:   []string{mapSrv.URL},
		Server: types.DERPServerSettings{Enabled: false},
	})
	require.NoError(t, err)

	st := srv.DERP()
	assert.True(t, st.Overridden)
	require.Len(t, st.Regions, 2)
	assert.Equal(t, tailcfg.DERPRegionID(10), st.Regions[0].ID)
	assert.Equal(t, "url", string(st.Regions[0].Source))
}

// TestDERPFileEmbeddedRelay proves derp.server.enabled in the file starts
// the relay at boot and publishes its region.
func TestDERPFileEmbeddedRelay(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t, servertest.WithEmbeddedDERP())

	st := srv.DERP()
	assert.False(t, st.Overridden)
	assert.True(t, st.RelayRunning)
	assert.NotEmpty(t, st.STUNAddr)
	require.Len(t, st.Regions, 2)
	assert.Equal(t, tailcfg.DERPRegionID(999), st.Regions[1].ID)
	assert.Equal(t, "embedded", string(st.Regions[1].Source))
}
