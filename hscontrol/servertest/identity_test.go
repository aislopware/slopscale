package servertest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/netmap"
)

// machineDo sends one request over the node's noise connection, for the
// endpoints that are not POSTs.
func machineDo(
	t *testing.T,
	srv *servertest.TestServer,
	node *servertest.TestClient,
	method, path string,
	body any,
) (int, []byte) {
	t.Helper()

	var raw []byte

	if body != nil {
		var err error

		raw, err = json.Marshal(body)
		require.NoError(t, err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	target := strings.Replace(srv.URL+"/machine/"+path, "http://", "https://", 1)

	req, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(raw))
	require.NoError(t, err)

	resp, err := node.Direct().DoNoiseRequest(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	var out bytes.Buffer

	_, err = out.ReadFrom(resp.Body)
	require.NoError(t, err)

	return resp.StatusCode, out.Bytes()
}

// TestIDToken proves `tailscale id-token` gets a token a verifier accepts
// through OpenID discovery at the server URL, carrying the node's
// identity; and that a node outside the tailnet gets none.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestIDToken(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t, servertest.WithDNS(types.DNSConfig{MagicDNS: true, BaseDomain: "id.test"}))
	owner := srv.CreateUser(t, "alice")
	alice := servertest.NewClient(t, srv, "laptop", servertest.WithUser(owner))
	server := servertest.NewClient(t, srv, "srv", servertest.WithTags("tag:server"))
	alice.WaitForPeerCount(t, 1, 10*time.Second)

	const audience = "https://vault.example.com"

	request := func(node *servertest.TestClient, aud string) (int, []byte) {
		return machineDo(t, srv, node, http.MethodPost, "id-token", tailcfg.TokenRequest{
			CapVersion: tailcfg.CurrentCapabilityVersion,
			NodeKey:    node.NodePrivateKey().Public(),
			Audience:   aud,
		})
	}

	// A verifier that knows only the server URL finds the key by itself.
	ctx := oidc.ClientContext(t.Context(), srv.HTTPClient(t))

	provider, err := oidc.NewProvider(ctx, srv.URL)
	require.NoError(t, err)

	verifier := provider.Verifier(&oidc.Config{ClientID: audience, SupportedSigningAlgs: []string{oidc.ES256}})

	t.Run("a user-owned node", func(t *testing.T) {
		status, body := request(alice, audience)
		require.Equal(t, http.StatusOK, status, string(body))

		var response tailcfg.TokenResponse

		require.NoError(t, json.Unmarshal(body, &response))

		token, err := verifier.Verify(ctx, response.IDToken)
		require.NoError(t, err)
		assert.Equal(t, srv.URL, token.Issuer)
		assert.Equal(t, "laptop.id.test", token.Subject)
		assert.WithinDuration(t, time.Now().Add(5*time.Minute), token.Expiry, time.Minute)

		var claims struct {
			Key       string   `json:"key"`
			Addresses []string `json:"addresses"`
			NodeID    uint64   `json:"nid"`
			Node      string   `json:"node"`
			Domain    string   `json:"domain"`
			Tags      []string `json:"tags"`
			User      string   `json:"user"`
			UserID    uint64   `json:"uid"`
			JTI       string   `json:"jti"`
		}

		require.NoError(t, token.Claims(&claims))
		assert.Equal(t, alice.NodePrivateKey().Public().String(), claims.Key)
		assert.Len(t, claims.Addresses, 2)
		assert.Equal(t, alice.NodeIDString(), strconv.FormatUint(claims.NodeID, 10))
		assert.Equal(t, "laptop", claims.Node)
		assert.NotEmpty(t, claims.Domain)
		assert.Empty(t, claims.Tags)
		assert.Equal(t, claims.Domain+":alice", claims.User)
		assert.Equal(t, uint64(owner.ID), claims.UserID)
		assert.NotEmpty(t, claims.JTI)
	})

	t.Run("a tagged node", func(t *testing.T) {
		status, body := request(server, audience)
		require.Equal(t, http.StatusOK, status, string(body))

		var response tailcfg.TokenResponse

		require.NoError(t, json.Unmarshal(body, &response))

		token, err := verifier.Verify(ctx, response.IDToken)
		require.NoError(t, err)

		var claims struct {
			Domain string   `json:"domain"`
			Tags   []string `json:"tags"`
			User   string   `json:"user"`
			UserID uint64   `json:"uid"`
		}

		require.NoError(t, token.Claims(&claims))
		assert.Equal(t, []string{claims.Domain + ":tag:server"}, claims.Tags)
		assert.Empty(t, claims.User)
		assert.Zero(t, claims.UserID)
	})

	t.Run("the audience is checked", func(t *testing.T) {
		status, body := request(alice, "https://other.example.com")
		require.Equal(t, http.StatusOK, status, string(body))

		var response tailcfg.TokenResponse

		require.NoError(t, json.Unmarshal(body, &response))

		_, err := verifier.Verify(ctx, response.IDToken)
		require.Error(t, err, "a token for another audience is refused")

		status, _ = request(alice, "")
		assert.Equal(t, http.StatusBadRequest, status)
	})

	t.Run("a suspended node gets none", func(t *testing.T) {
		client := srv.HTTPClient(t)
		ownerKey := srv.CreateAPIKey(t, owner)

		status, body := apiCall(t, client, ownerKey, http.MethodPost,
			srv.URL+"/api/v1/node/"+alice.NodeIDString()+"/suspend", nil)
		require.Equal(t, http.StatusOK, status, body)

		require.EventuallyWithT(t, func(c *assert.CollectT) {
			status, _ := request(alice, audience)
			assert.Equal(c, http.StatusForbidden, status)
		}, 10*time.Second, 100*time.Millisecond)
	})

	t.Run("whoami names the machine's node", func(t *testing.T) {
		status, body := machineDo(t, srv, server, http.MethodGet, "whoami", nil)
		require.Equal(t, http.StatusOK, status, string(body))

		var response struct {
			MachineKey string `json:"machineKey"`
			Nodes      []struct {
				Name string   `json:"name"`
				Tags []string `json:"tags"`
			} `json:"nodes"`
		}

		require.NoError(t, json.Unmarshal(body, &response))
		assert.NotEmpty(t, response.MachineKey)
		require.Len(t, response.Nodes, 1)
		assert.Equal(t, "srv", response.Nodes[0].Name)
		assert.Equal(t, []string{"tag:server"}, response.Nodes[0].Tags)
	})
}

// TestSetDeviceAttributes proves a machine can set its own custom
// attributes once the setting allows it, that they show as such, and
// that a bad patch changes nothing.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestSetDeviceAttributes(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	owner := srv.CreateUser(t, "owner")
	ownerKey := srv.CreateAPIKey(t, owner)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	alice := servertest.NewClient(t, srv, "alice", servertest.WithUser(owner))
	alice.WaitForUpdate(t, 10*time.Second)

	patch := func(update tailcfg.AttrUpdate) (int, []byte) {
		return machineDo(t, srv, alice, http.MethodPatch, "set-device-attr", tailcfg.SetDeviceAttributesRequest{
			Version: tailcfg.CurrentCapabilityVersion,
			NodeKey: alice.NodePrivateKey().Public(),
			Update:  update,
		})
	}

	custom := func() map[string]map[string]any {
		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/node/"+alice.NodeIDString()+"/posture", nil)
		require.Equal(t, http.StatusOK, status, body)

		out := map[string]map[string]any{}

		list, _ := body["custom"].([]any)
		for _, item := range list {
			attr, _ := item.(map[string]any)
			key, _ := attr["key"].(string)
			out[key] = attr
		}

		return out
	}

	t.Run("refused while the setting is off", func(t *testing.T) {
		status, body := patch(tailcfg.AttrUpdate{"custom:agent": "1.4"})
		assert.Equal(t, http.StatusForbidden, status)
		assert.Contains(t, string(body), "deviceAttributesOn")
		assert.Empty(t, custom())
	})

	status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/settings",
		map[string]any{"deviceAttributesOn": true})
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, true, body["deviceAttributesOn"])

	t.Run("sets and deletes", func(t *testing.T) {
		status, body := patch(
			tailcfg.AttrUpdate{"custom:agent": "1.4", "custom:disk-encrypted": true, "custom:score": 7},
		)
		require.Equal(t, http.StatusOK, status, string(body))

		attrs := custom()
		require.Len(t, attrs, 3)
		assert.Equal(t, "1.4", attrs["custom:agent"]["value"])
		assert.Equal(t, true, attrs["custom:disk-encrypted"]["value"])
		assert.InDelta(t, 7, attrs["custom:score"]["value"], 0)
		assert.Equal(t, "Set by the machine", attrs["custom:agent"]["comment"])

		status, body = patch(tailcfg.AttrUpdate{"custom:score": nil, "custom:missing": nil, "custom:agent": "1.5"})
		require.Equal(t, http.StatusOK, status, string(body))

		attrs = custom()
		require.Len(t, attrs, 2)
		assert.Equal(t, "1.5", attrs["custom:agent"]["value"])
		assert.NotContains(t, attrs, "custom:score")
	})

	t.Run("a bad key changes nothing", func(t *testing.T) {
		status, body := patch(tailcfg.AttrUpdate{"custom:ok": "yes", "node:os": "linux"})
		assert.Equal(t, http.StatusBadRequest, status, string(body))
		assert.NotContains(t, custom(), "custom:ok")

		status, _ = patch(tailcfg.AttrUpdate{"custom:list": []any{"a"}})
		assert.Equal(t, http.StatusBadRequest, status)
	})

	t.Run("the audit log names the machine", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/audit?action=node.attribute.set", nil)
		require.Equal(t, http.StatusOK, status, body)

		events, _ := body["events"].([]any)
		require.NotEmpty(t, events)

		first, _ := events[0].(map[string]any)
		assert.Equal(t, "node", first["actorKind"])
		assert.Equal(t, "alice", first["actorName"])
	})
}

// TestApprovalMessage proves a node waiting for approval is told so in a
// health message that clears once it is approved.
func TestApprovalMessage(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	owner := srv.CreateUser(t, "owner")
	ownerKey := srv.CreateAPIKey(t, owner)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	status, body := apiCall(
		t,
		client,
		ownerKey,
		http.MethodPost,
		v1+"/settings",
		map[string]any{"devicesApprovalOn": true},
	)
	require.Equal(t, http.StatusOK, status, body)

	plainKey := srv.CreatePreAuthKeyFromSpec(t, types.PreAuthKeySpec{
		UserID: owner.TypedID(), Reusable: true, Preauthorized: false,
	})
	carol := servertest.NewClient(t, srv, "carol", servertest.WithUser(owner), servertest.WithAuthKey(plainKey))

	carol.WaitForCondition(t, "told to wait for approval", 10*time.Second, func(nm *netmap.NetworkMap) bool {
		msg, ok := nm.DisplayMessages["slopscale-approval"]

		return !authorized(nm) && ok && strings.Contains(msg.Title, "waiting for approval") &&
			msg.PrimaryAction != nil && strings.HasSuffix(msg.PrimaryAction.URL, "/console/machines")
	})

	status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/node/"+carol.NodeIDString()+"/approve", nil)
	require.Equal(t, http.StatusOK, status, body)

	carol.WaitForCondition(t, "approved and no longer told", 10*time.Second, func(nm *netmap.NetworkMap) bool {
		_, told := nm.DisplayMessages["slopscale-approval"]

		return authorized(nm) && !told
	})
}
