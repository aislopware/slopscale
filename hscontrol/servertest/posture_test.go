package servertest_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

const postureWait = 10 * time.Second

// TestPostureCollection proves the c2n round trip: with the setting on,
// the server asks a connected client for its serial numbers through its
// map stream, the client posts the answer back over noise, and the
// result shows up as node:serialNumber next to the attributes derived
// from Hostinfo. A client with posture checking off reports so. Custom
// attributes are set through v1 and v2, expire on their own and reach
// the attribute map. The subtests build on one another.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestPostureCollection(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"
	v2 := srv.URL + "/api/v2"

	owner := srv.CreateUser(t, "posture-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	mac := servertest.NewClient(t, srv, "mac", servertest.WithUser(owner),
		servertest.WithSerialNumbers("C02XYZ123", "FVFAB456"),
		servertest.WithHostinfo(func(hi *tailcfg.Hostinfo) {
			hi.OS = "macOS"
			hi.OSVersion = "15.6"
			hi.IPNVersion = "1.86.2-t1234abcd"
			hi.Machine = "arm64"
			hi.AllowsUpdate = true
		}))
	linux := servertest.NewClient(t, srv, "linux", servertest.WithUser(owner),
		servertest.WithHostinfo(func(hi *tailcfg.Hostinfo) {
			hi.OS = "linux"
			hi.IPNVersion = "1.87.0-t5678"
			hi.Distro = "debian"
		}))
	mac.WaitForPeerCount(t, 1, postureWait)
	linux.WaitForPeerCount(t, 1, postureWait)

	postureOf := func(t *testing.T, id string) map[string]any {
		t.Helper()

		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/node/"+id+"/posture", nil)
		require.Equal(t, http.StatusOK, status, body)

		return body
	}

	t.Run("hostinfo attributes need no collection", func(t *testing.T) {
		body := postureOf(t, mac.NodeIDString())
		assert.Equal(t, "macos", field(t, body, "attributes", types.AttrOS))
		assert.Equal(t, "15.6", field(t, body, "attributes", types.AttrOSVersion))
		assert.Equal(t, "1.86.2", field(t, body, "attributes", types.AttrTSVersion))
		assert.Equal(t, "stable", field(t, body, "attributes", types.AttrTSReleaseTrack))
		assert.Equal(t, true, field(t, body, "attributes", types.AttrTSAutoUpdate))
		assert.Equal(t, "arm64", field(t, body, "attributes", types.AttrMachine))
		assert.Nil(t, field(t, body, "attributes", types.AttrSerialNumber))
		assert.Nil(t, body["identity"])
		assert.Equal(t, false, body["identityCollectionOn"])

		body = postureOf(t, linux.NodeIDString())
		assert.Equal(t, "unstable", field(t, body, "attributes", types.AttrTSReleaseTrack))
		assert.Equal(t, "debian", field(t, body, "attributes", types.AttrDistro))
	})

	t.Run("collection is refused while the setting is off", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost,
			v1+"/node/"+mac.NodeIDString()+"/posture/collect", nil)
		assert.Equal(t, http.StatusConflict, status, body)
	})

	status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/settings",
		map[string]bool{"postureIdentityOn": true})
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, true, body["postureIdentityOn"])

	t.Run("the setting shows on v2 as Tailscale names it", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodGet, v2+"/tailnet/-/settings", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, body["postureIdentityCollectionOn"])
	})

	t.Run("collecting asks the client over c2n and records the serials", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost,
			v1+"/node/"+mac.NodeIDString()+"/posture/collect", nil)
		require.Equal(t, http.StatusOK, status, body)

		assert.Equal(t, []any{"C02XYZ123", "FVFAB456"}, field(t, body, "identity", "serialNumbers"))
		assert.Equal(t, false, field(t, body, "identity", "disabled"))
		assert.Equal(t, []any{"C02XYZ123", "FVFAB456"}, field(t, body, "attributes", types.AttrSerialNumber))
		assert.Equal(t, true, body["identityCollectionOn"])

		// The report survives a re-read from the store and the database.
		node, ok := srv.State().GetNodeByID(types.MustParseNodeID(mac.NodeIDString()))
		require.True(t, ok)
		require.True(t, node.Posture().Valid())
		assert.Equal(t, []string{"C02XYZ123", "FVFAB456"}, node.Posture().SerialNumbers().AsSlice())
	})

	t.Run("a client with posture checking off says so", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost,
			v1+"/node/"+linux.NodeIDString()+"/posture/collect", nil)
		require.Equal(t, http.StatusOK, status, body)

		assert.Equal(t, true, field(t, body, "identity", "disabled"))
		assert.Equal(t, []any{}, field(t, body, "identity", "serialNumbers"))
		assert.Nil(t, field(t, body, "attributes", types.AttrSerialNumber))
	})

	t.Run("custom attributes are set through v1 and read through v2", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPut,
			v1+"/node/"+mac.NodeIDString()+"/attributes/custom:team",
			map[string]any{"value": "platform", "comment": "owned by platform"})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "platform", field(t, body, "attributes", "custom:team"))

		status, body = apiCall(t, client, ownerKey, http.MethodPost,
			v2+"/device/"+mac.NodeIDString()+"/attributes/custom:level", map[string]any{"value": 3})
		require.Equal(t, http.StatusOK, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v2+"/device/"+mac.NodeIDString()+"/attributes", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "platform", field(t, body, "attributes", "custom:team"))
		assert.InDelta(t, 3, field(t, body, "attributes", "custom:level"), 0)
		assert.Equal(t, "macos", field(t, body, "attributes", types.AttrOS))

		body = postureOf(t, mac.NodeIDString())
		custom, _ := body["custom"].([]any)
		require.Len(t, custom, 2)
	})

	t.Run("bad keys and values are refused", func(t *testing.T) {
		status, _ := apiCall(t, client, ownerKey, http.MethodPut,
			v1+"/node/"+mac.NodeIDString()+"/attributes/team", map[string]any{"value": "x"})
		assert.Equal(t, http.StatusUnprocessableEntity, status)

		status, _ = apiCall(t, client, ownerKey, http.MethodPut,
			v1+"/node/"+mac.NodeIDString()+"/attributes/custom:list", map[string]any{"value": []string{"a"}})
		assert.Equal(t, http.StatusUnprocessableEntity, status)

		status, _ = apiCall(t, client, ownerKey, http.MethodPost,
			v2+"/device/"+mac.NodeIDString()+"/attributes/custom:past",
			map[string]any{"value": true, "expiry": "2020-01-01T00:00:00Z"})
		assert.Equal(t, http.StatusBadRequest, status)
	})

	t.Run("an expiring attribute disappears on its own", func(t *testing.T) {
		// A grant has to read the attribute for its expiry to be a policy
		// change; posture inputs of a policy that names no posture are
		// stored without a recompute.
		changed, err := srv.State().SetPolicy([]byte(`{
			"postures": {"posture:oncall": ["custom:oncall == true"]},
			"grants": [{"src": ["*"], "dst": ["*"], "ip": ["*"], "srcPosture": ["posture:oncall"]}]
		}`))
		require.NoError(t, err)
		require.True(t, changed)

		changes, err := srv.State().ReloadPolicy()
		require.NoError(t, err)
		srv.App.Change(changes...)

		expiry := time.Now().Add(1500 * time.Millisecond).UTC().Format(time.RFC3339Nano)

		status, body := apiCall(t, client, ownerKey, http.MethodPut,
			v1+"/node/"+mac.NodeIDString()+"/attributes/custom:oncall",
			map[string]any{"value": true, "expiry": expiry})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, field(t, body, "attributes", "custom:oncall"))

		// The attribute map drops it at expiry before the sweeper runs.
		require.Eventually(t, func() bool {
			current := postureOf(t, mac.NodeIDString())

			return field(t, current, "attributes", "custom:oncall") == nil
		}, postureWait, 100*time.Millisecond)

		c, err := srv.State().ExpireNodeAttributes(time.Now())
		require.NoError(t, err)
		assert.False(t, c.IsEmpty(), "the sweep drops it and recomputes, since a grant reads it")

		body = postureOf(t, mac.NodeIDString())
		custom, _ := body["custom"].([]any)
		assert.Len(t, custom, 2, "the expired attribute is gone from the list")
	})

	t.Run("deleting an attribute", func(t *testing.T) {
		status, _ := apiCall(t, client, ownerKey, http.MethodDelete,
			v2+"/device/"+mac.NodeIDString()+"/attributes/custom:level", nil)
		assert.Equal(t, http.StatusOK, status)

		status, _ = apiCall(t, client, ownerKey, http.MethodDelete,
			v1+"/node/"+mac.NodeIDString()+"/attributes/custom:level", nil)
		assert.Equal(t, http.StatusNotFound, status)

		body := postureOf(t, mac.NodeIDString())
		assert.Nil(t, field(t, body, "attributes", "custom:level"))
		assert.Equal(t, "platform", field(t, body, "attributes", "custom:team"))
	})
}
