package servertest_test

import (
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tsclient "tailscale.com/client/tailscale/v2"
	"tailscale.com/types/netmap"
)

// operatorPolicy is what an operator deployment needs: the operator's own
// tag, and tag:k8s owned by it so the operator can mint keys for the
// proxies it creates.
const operatorPolicy = `{
  "tagOwners": {"tag:k8s-operator": ["k8s@"], "tag:k8s": ["tag:k8s-operator"]},
  "acls": [{"action": "accept", "src": ["*"], "dst": ["*:*"]}]
}`

// operatorWait bounds how long the joined proxy is given to appear.
const operatorWait = 15 * time.Second

// TestAPIv2KubernetesOperatorPath walks the exact sequence the Tailscale
// Kubernetes operator runs at startup and while it reconciles: it is
// OAuth-only, so every call goes through a client-credentials token, and a
// single missing endpoint stops the operator dead. The steps are the ones
// the operator makes, in its order — the permission probe, the auth key it
// mints for a proxy, the proxy joining with that key, the service it
// registers for an ingress, and the device deletion when the proxy goes
// away.
//
// It drives the official Go SDK, which is what the operator is built on,
// and needs neither tscli nor tofu.
func TestAPIv2KubernetesOperatorPath(t *testing.T) {
	srv := servertest.NewServer(t, servertest.WithRealListener())
	owner := srv.CreateUser(t, "k8s")
	apiKey := srv.CreateAPIKey(t, owner)

	setStatePolicy(t, srv, operatorPolicy)

	ctx := t.Context()

	client, err := goClient(t, srv.URL, apiKey).Keys().CreateOAuthClient(ctx,
		tsclient.CreateOAuthClientRequest{
			Scopes:      []string{"auth_keys", "devices:core", "services"},
			Tags:        []string{"tag:k8s-operator"},
			Description: "k8s-operator",
		})
	require.NoError(t, err)
	require.NotEmpty(t, client.Key, "the client secret is shown once, on create")

	// The operator authenticates only as an OAuth client; the SDK mints the
	// token at /api/v2/oauth/token on the first call and reuses it.
	op := &tsclient.Client{
		BaseURL: mustParseURL(t, srv.URL),
		Tailnet: "-",
		Auth:    &tsclient.OAuth{ClientID: client.ID, ClientSecret: client.Key},
	}

	// The operator's startup probe: it reads devices, keys and services to
	// find out what its credential may do, and refuses to start if any of
	// the three is not there.
	_, err = op.Devices().List(ctx)
	require.NoError(t, err, "devices list is the operator's first call")

	_, err = op.Keys().List(ctx, true)
	require.NoError(t, err, "keys list")

	_, err = op.Services().List(ctx)
	require.NoError(t, err, "services list is the operator's permission probe")

	// The auth key the operator mints for a proxy pod: no expiry, no
	// description, only the device-create capabilities.
	var keyReq tsclient.CreateKeyRequest

	keyReq.Capabilities.Devices.Create.Reusable = false
	keyReq.Capabilities.Devices.Create.Preauthorized = true
	keyReq.Capabilities.Devices.Create.Tags = []string{"tag:k8s"}

	proxyKey, err := op.Keys().CreateAuthKey(ctx, keyReq)
	require.NoError(t, err, "the operator token mints a tag:k8s key through tag ownership")
	require.NotEmpty(t, proxyKey.Key)

	proxy := servertest.NewClient(t, srv, "k8s-proxy", servertest.WithAuthKey(proxyKey.Key))
	proxy.WaitForCondition(t, "the proxy is registered", operatorWait, func(nm *netmap.NetworkMap) bool {
		return nm != nil && nm.SelfNode.Valid()
	})

	// The operator addresses a device by the stable id its client reads out
	// of the netmap, which slopscale derives from the node id.
	stableID := string(proxy.Netmap().SelfNode.StableID())
	require.NotEmpty(t, stableID)

	device, err := op.Devices().Get(ctx, stableID)
	require.NoError(t, err, "the device getter takes the stable id the client sees")
	assert.Equal(t, stableID, device.NodeID)
	assert.Equal(t, []string{"tag:k8s"}, device.Tags)

	t.Run("Services", func(t *testing.T) {
		// An ingress the operator has not yet resolved ports for: it sends
		// the do-not-validate sentinel rather than a port list.
		require.NoError(t, op.Services().CreateOrUpdate(ctx, tsclient.VIPService{
			Name:  "svc:foo",
			Ports: []string{"do-not-validate"},
		}))

		svc, err := op.Services().Get(ctx, "svc:foo")
		require.NoError(t, err)
		assert.Equal(t, "svc:foo", svc.Name)
		assert.NotEmpty(t, svc.Addrs, "a service gets a pair of tailnet addresses")

		// Once the ingress is resolved the operator replaces the sentinel
		// with the real ports.
		require.NoError(t, op.Services().CreateOrUpdate(ctx, tsclient.VIPService{
			Name:  "svc:foo",
			Ports: []string{"tcp:443"},
		}))

		svc, err = op.Services().Get(ctx, "svc:foo")
		require.NoError(t, err)
		assert.Equal(t, []string{"tcp:443"}, svc.Ports)

		require.NoError(t, op.Services().Delete(ctx, "svc:foo"))

		_, err = op.Services().Get(ctx, "svc:foo")
		require.Error(t, err)
		assert.True(t, tsclient.IsNotFound(err), "a deleted service is gone: %v", err)
	})

	t.Run("DeleteDevice", func(t *testing.T) {
		require.NoError(t, op.Devices().Delete(ctx, stableID))

		// The operator deletes on every reconcile of a removed proxy, so the
		// second delete has to be a clean 404 in Tailscale's error body
		// rather than a 500 it would retry forever.
		err := op.Devices().Delete(ctx, stableID)
		require.Error(t, err)
		assert.True(t, tsclient.IsNotFound(err), "a repeated delete is a 404: %v", err)
	})
}
