package servertest_test

import (
	"io"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/health"
	"tailscale.com/ipn"
	"tailscale.com/tailcfg"
)

const c2nOpsWait = 10 * time.Second

// TestNodeClientOperations proves the control-to-node operations end to
// end: the server asks a connected client over its map stream and answers
// the operator with what the client said. A client that refuses is
// reported as refusing rather than as a server failure, a node that is
// not connected is a conflict, and preferences may only be changed on a
// machine that opted into remote configuration.
//
//nolint:tparallel // later steps read the state earlier ones leave behind
func TestNodeClientOperations(t *testing.T) {
	t.Parallel()

	// A base domain gives every node a MagicDNS name, which is the name
	// the certificate question is asked about.
	srv := servertest.NewServer(t,
		servertest.WithDNS(types.DNSConfig{MagicDNS: true, BaseDomain: "ops.test"}))
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "ops-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	laptop := servertest.NewClient(t, srv, "ops-laptop", servertest.WithUser(owner),
		servertest.WithClientUpdate(tailcfg.C2NUpdateResponse{Enabled: true, Supported: true}),
		servertest.WithClientHealth(health.State{
			Warnings: map[health.WarnableCode]health.UnhealthyState{
				"network-status": {
					WarnableCode:        "network-status",
					Severity:            health.SeverityHigh,
					Title:               "Network is down",
					Text:                "no route to the internet",
					ImpactsConnectivity: true,
				},
				"dns-slow": {
					WarnableCode: "dns-slow",
					Severity:     health.SeverityLow,
					Title:        "DNS is slow",
					Text:         "the resolver took a while",
				},
			},
		}),
		servertest.WithSSHUsernames("alice", "root"),
		servertest.WithAppConnectorRoutes(map[string][]netip.Addr{
			"example.com": {netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("1.1.1.1")},
		}),
		servertest.WithTLSCert("ops-laptop.ops.test", tailcfg.C2NTLSCertInfo{Valid: true}),
		servertest.WithClientPrefs(ipn.Prefs{
			CorpDNS:         true,
			Hostname:        "ops-laptop",
			AdvertiseRoutes: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/24")},
			AutoUpdate:      ipn.AutoUpdatePrefs{Check: true},
			PostureChecking: true,
		}, true))

	// A machine whose owner never opted into remote updates or remote
	// configuration: it answers, and refuses.
	desktop := servertest.NewClient(t, srv, "ops-desktop", servertest.WithUser(owner),
		servertest.WithClientUpdate(tailcfg.C2NUpdateResponse{Supported: true}),
		servertest.WithClientPrefs(ipn.Prefs{Hostname: "ops-desktop"}, false))

	laptop.WaitForPeerCount(t, 1, c2nOpsWait)
	desktop.WaitForPeerCount(t, 1, c2nOpsWait)

	offline := srv.CreateRegisteredNode(t, owner, "ops-offline")

	t.Run("client update status and start", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodGet,
			v1+"/node/"+laptop.NodeIDString()+"/client-update", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, body["enabled"])
		assert.Equal(t, true, body["supported"])
		assert.Equal(t, false, body["started"])

		status, body = apiCall(t, client, ownerKey, http.MethodPost,
			v1+"/node/"+laptop.NodeIDString()+"/client-update", map[string]any{"force": true})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, body["started"])

		// The client remembers it started, so a later read says so.
		status, body = apiCall(t, client, ownerKey, http.MethodGet,
			v1+"/node/"+laptop.NodeIDString()+"/client-update", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, body["started"])
	})

	t.Run("a client that refuses is reported as refusing", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost,
			v1+"/node/"+desktop.NodeIDString()+"/client-update", nil)
		assert.Equal(t, http.StatusBadGateway, status, body)
		assert.Contains(t, body["detail"], "not enabled")
	})

	t.Run("an offline node is a conflict", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodGet,
			v1+"/node/"+offline.StringID()+"/client-update", nil)
		assert.Equal(t, http.StatusConflict, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodGet,
			v1+"/node/"+offline.StringID()+"/health", nil)
		assert.Equal(t, http.StatusConflict, status, body)
	})

	t.Run("client health comes back ordered by code", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodGet,
			v1+"/node/"+laptop.NodeIDString()+"/health", nil)
		require.Equal(t, http.StatusOK, status, body)

		assert.Equal(t, "dns-slow", field(t, body, "warnings", "0", "code"))
		assert.Equal(t, "network-status", field(t, body, "warnings", "1", "code"))
		assert.Equal(t, "high", field(t, body, "warnings", "1", "severity"))
		assert.Equal(t, "no route to the internet", field(t, body, "warnings", "1", "text"))
		assert.Equal(t, true, field(t, body, "warnings", "1", "impactsConnectivity"))
	})

	t.Run("every diagnostic comes back as the client wrote it", func(t *testing.T) {
		for kind, wantType := range map[string]string{
			"prefs":      "application/json",
			"netmap":     "application/json",
			"tka-log":    "application/json",
			"metrics":    "text/plain",
			"goroutines": "text/plain",
			"sockstats":  "text/plain",
		} {
			status, header, raw := rawCall(t, client, ownerKey,
				v1+"/node/"+laptop.NodeIDString()+"/diagnostics/"+kind)
			require.Equal(t, http.StatusOK, status, string(raw))
			assert.Equal(t, wantType, header.Get("Content-Type"))
			assert.NotEmpty(t, raw)

			ext := "json"
			if wantType == "text/plain" {
				ext = "txt"
			}

			assert.Equal(t, `attachment; filename="ops-laptop-`+kind+"."+ext+`"`,
				header.Get("Content-Disposition"))
		}
	})

	t.Run("app connector routes and certificate status", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodGet,
			v1+"/node/"+laptop.NodeIDString()+"/app-connector-routes", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, []any{"1.1.1.1", "93.184.216.34"},
			field(t, body, "domains", "example.com"))

		// The client answers for its MagicDNS name and no other, so a
		// valid answer proves the server asked about the right name.
		status, body = apiCall(t, client, ownerKey, http.MethodGet,
			v1+"/node/"+laptop.NodeIDString()+"/tls-cert", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, body["valid"])
		assert.Equal(t, false, body["missing"])

		// A machine that fetched no certificate reports it as missing
		// rather than as a failure.
		status, body = apiCall(t, client, ownerKey, http.MethodGet,
			v1+"/node/"+desktop.NodeIDString()+"/tls-cert", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, false, body["valid"])
		assert.Equal(t, true, body["missing"])
	})

	t.Run("ssh username hints follow the session visibility rule", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodGet,
			v1+"/node/"+laptop.NodeIDString()+"/ssh-usernames", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, []any{"alice", "root"}, body["usernames"])

		// A member with no machines is not told the node exists.
		member := srv.CreateUser(t, "ops-member")
		memberKey := srv.CreateAPIKey(t, member)

		status, body = apiCall(t, client, memberKey, http.MethodGet,
			v1+"/node/"+laptop.NodeIDString()+"/ssh-usernames", nil)
		assert.Equal(t, http.StatusNotFound, status, body)
	})

	t.Run("preferences are read from any client and changed on one that opted in", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodGet,
			v1+"/node/"+laptop.NodeIDString()+"/preferences", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, []any{"10.0.0.0/24"}, body["advertiseRoutes"])
		assert.Equal(t, false, body["acceptRoutes"])
		assert.Equal(t, true, body["acceptDns"])
		assert.Equal(t, false, body["advertiseExitNode"])
		assert.Equal(t, "ops-laptop", body["hostname"])

		status, body = apiCall(t, client, ownerKey, http.MethodPatch,
			v1+"/node/"+laptop.NodeIDString()+"/preferences", map[string]any{
				"acceptRoutes":    true,
				"advertiseRoutes": []string{"10.1.0.0/24", "192.168.5.0/24"},
			})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, body["acceptRoutes"])
		assert.Equal(t, []any{"10.1.0.0/24", "192.168.5.0/24"}, body["advertiseRoutes"])

		// The change stuck on the client, and offering to be an exit node
		// leaves the other routes alone.
		status, body = apiCall(t, client, ownerKey, http.MethodPatch,
			v1+"/node/"+laptop.NodeIDString()+"/preferences", map[string]any{"advertiseExitNode": true})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, body["advertiseExitNode"])
		assert.Equal(t, true, body["acceptRoutes"])
		assert.Equal(t, []any{"10.1.0.0/24", "192.168.5.0/24"}, body["advertiseRoutes"])
	})

	t.Run("a machine without the remote config opt-in refuses the change", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodGet,
			v1+"/node/"+desktop.NodeIDString()+"/preferences", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "ops-desktop", body["hostname"])

		status, body = apiCall(t, client, ownerKey, http.MethodPatch,
			v1+"/node/"+desktop.NodeIDString()+"/preferences", map[string]any{"shieldsUp": true})
		assert.Equal(t, http.StatusConflict, status, body)
		assert.Contains(t, body["detail"], "--remote-config")
	})

	t.Run("a bulk update reports every node on its own", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/nodes/client-update",
			map[string]any{
				"nodeIds": []string{laptop.NodeIDString(), desktop.NodeIDString(), offline.StringID()},
				"force":   true,
			})
		require.Equal(t, http.StatusOK, status, body)

		assert.Equal(t, laptop.NodeIDString(), field(t, body, "results", "0", "nodeId"))
		assert.Equal(t, true, field(t, body, "results", "0", "started"))

		assert.Equal(t, desktop.NodeIDString(), field(t, body, "results", "1", "nodeId"))
		assert.Equal(t, false, field(t, body, "results", "1", "started"))
		assert.Contains(t, field(t, body, "results", "1", "error"), "not enabled")

		assert.Contains(t, field(t, body, "results", "2", "error"), "not connected")
	})
}

// rawCall performs one authenticated GET and returns the status, the
// response headers and the body as sent, for the endpoints that answer
// with a file rather than JSON.
func rawCall(t *testing.T, client *http.Client, key, url string) (int, http.Header, []byte) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return resp.StatusCode, resp.Header, raw
}

// TestTLSCertStatusWithoutBaseDomain proves a tailnet with no base domain
// reports the certificate as missing without asking: the node has no
// MagicDNS name to hold one for, and the client refuses the question
// without a name.
func TestTLSCertStatusWithoutBaseDomain(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)

	owner := srv.CreateUser(t, "nocert-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	laptop := servertest.NewClient(t, srv, "nocert-laptop", servertest.WithUser(owner),
		servertest.WithTLSCert("nocert-laptop", tailcfg.C2NTLSCertInfo{Valid: true}))
	laptop.WaitForPeerCount(t, 0, c2nOpsWait)

	status, body := apiCall(t, client, ownerKey, http.MethodGet,
		srv.URL+"/api/v1/node/"+laptop.NodeIDString()+"/tls-cert", nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, false, body["valid"])
	assert.Equal(t, true, body["missing"])
}
