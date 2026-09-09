package servertest_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tsclient "tailscale.com/client/tailscale/v2"
	"tailscale.com/tailcfg"
	"tailscale.com/types/netmap"
	"tailscale.com/types/opt"
)

// compatPolicy is the policy the validate subtests start from; it declares
// the tag the device fixture carries. It is written as HuJSON, with a
// comment and a trailing comma, so the raw read has something to lose.
const compatPolicy = `// the tailnet policy, as an operator wrote it
{
  "tagOwners": {"tag:compat": ["compat@"]},
  "acls": [
    {"action": "accept", "src": ["*"], "dst": ["*:*"]}, // everything, for now
  ],
}
`

const compatWait = 15 * time.Second

// TestAPIv2Compat covers the endpoints the Terraform provider and the
// Kubernetes operator need beyond the ones the older suites already prove,
// through the official Go SDK: policy validation (the tailscale_acl plan
// modifier calls it on every plan), the raw HuJSON read, the OAuth client
// update, the whole-tailnet DNS configuration, posture integrations, log
// streaming and the fields=all device response.
//
// It needs neither tscli nor tofu, so it runs on a bare checkout.
func TestAPIv2Compat(t *testing.T) {
	srv := servertest.NewServer(t, servertest.WithRealListener())
	owner := srv.CreateUser(t, "compat")
	apiKey := srv.CreateAPIKey(t, owner)

	setStatePolicy(t, srv, compatPolicy)

	// Posture identity collection is a tailnet switch and is off on a
	// fresh server, so turn it on before the device connects: the c2n
	// request follows a couple of seconds behind the map poll.
	_, err := srv.State().SetSetting(types.SettingPostureIdentityOn, true)
	require.NoError(t, err)

	// The device is created here rather than in the subtest that reads it:
	// a client is torn down with the TB it was made for, and the policy
	// tests need an address to resolve compat@ to.
	node := servertest.NewClient(t, srv, "compat-device",
		servertest.WithUser(owner),
		servertest.WithSerialNumbers("SERIAL-1"),
		servertest.WithHostinfo(func(hi *tailcfg.Hostinfo) {
			hi.ShieldsUp = true
			hi.Distro = "ubuntu"
			hi.DistroVersion = "24.04"
			hi.DistroCodeName = "noble"
			hi.SSH_HostKeys = []string{"ssh-ed25519 AAAA"}
			hi.NetInfo = &tailcfg.NetInfo{
				MappingVariesByDestIP: opt.Bool("true"),
				WorkingUDP:            opt.Bool("true"),
				WorkingIPv6:           opt.Bool("false"),
				UPnP:                  opt.Bool("true"),
				PreferredDERP:         1,
			}
		}),
	)
	node.WaitForCondition(t, "the device is registered", compatWait, func(nm *netmap.NetworkMap) bool {
		return nm != nil && nm.SelfNode.Valid()
	})

	client := goClient(t, srv.URL, apiKey)
	ctx := t.Context()

	t.Run("PolicyValidate", func(t *testing.T) {
		require.NoError(t, client.PolicyFile().Validate(ctx, compatPolicy),
			"a policy the server accepts validates")

		err := client.PolicyFile().Validate(ctx,
			`{"acls": [{"action": "accept", "src": ["group:nope"], "dst": ["*:*"]}]}`)
		require.Error(t, err, "a policy naming an undefined group fails validation")

		// A test list runs against the policy in force, which is allow-all,
		// so an accept assertion passes and a deny assertion fails.
		require.NoError(t, client.PolicyFile().Validate(ctx, []tsclient.ACLTest{
			{Source: "compat@", Accept: []string{"100.64.0.1:80"}},
		}))

		err = client.PolicyFile().Validate(ctx, []tsclient.ACLTest{
			{Source: "compat@", Deny: []string{"100.64.0.1:80"}},
		})
		require.Error(t, err, "a deny assertion the allow-all policy breaks fails validation")
	})

	t.Run("PolicyRaw", func(t *testing.T) {
		raw, err := client.PolicyFile().Raw(ctx)
		require.NoError(t, err)
		assert.Equal(t, compatPolicy, raw.HuJSON,
			"the stored bytes come back untouched, comments and trailing commas included")
		assert.NotEmpty(t, raw.ETag, "the raw read carries the ETag a conditional write needs")
	})

	t.Run("SetOAuthClient", func(t *testing.T) {
		created, err := client.Keys().CreateOAuthClient(ctx, tsclient.CreateOAuthClientRequest{
			Scopes:      []string{"devices:core:read"},
			Description: "before",
		})
		require.NoError(t, err)

		updated, err := client.Keys().SetOAuthClient(ctx, created.ID, tsclient.SetOAuthClientRequest{
			Scopes:      []string{"dns:read"},
			Description: "after",
		})
		require.NoError(t, err)
		assert.Equal(t, created.ID, updated.ID, "the update keeps the client id")
		assert.Equal(t, []string{"dns:read"}, updated.Scopes)
		assert.Equal(t, "after", updated.Description)
		assert.Empty(t, updated.Key, "the secret is not re-exposed by an update")
	})

	t.Run("DNSConfiguration", func(t *testing.T) {
		before, err := client.DNS().Configuration(ctx)
		require.NoError(t, err)

		require.NoError(t, client.DNS().SetConfiguration(ctx, tsclient.DNSConfiguration{
			Nameservers: []tsclient.DNSConfigurationResolver{
				{Address: "1.1.1.1", UseWithExitNode: true},
				{Address: "9.9.9.9"},
			},
			SplitDNS: map[string][]tsclient.DNSConfigurationResolver{
				"internal.example.com": {{Address: "10.0.0.53"}},
			},
			SearchPaths: []string{"example.com"},
			Preferences: tsclient.DNSConfigurationPreferences{
				OverrideLocalDNS: true,
				MagicDNS:         before.Preferences.MagicDNS,
			},
		}))

		after, err := client.DNS().Configuration(ctx)
		require.NoError(t, err)
		assert.Equal(t, []tsclient.DNSConfigurationResolver{
			{Address: "1.1.1.1", UseWithExitNode: true},
			{Address: "9.9.9.9"},
		}, after.Nameservers)
		assert.Equal(t, []string{"example.com"}, after.SearchPaths)
		assert.True(t, after.Preferences.OverrideLocalDNS)
		require.Len(t, after.SplitDNS["internal.example.com"], 1)
		assert.Equal(t, "10.0.0.53", after.SplitDNS["internal.example.com"][0].Address)

		// The single-aspect endpoints read the same settings back.
		nameservers, err := client.DNS().Nameservers(ctx)
		require.NoError(t, err)
		assert.Equal(t, []string{"1.1.1.1", "9.9.9.9"}, nameservers)
	})

	t.Run("PostureIntegrations", func(t *testing.T) {
		provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		t.Cleanup(provider.Close)

		created, err := client.DevicePosture().CreateIntegration(ctx,
			tsclient.CreatePostureIntegrationRequest{
				Provider:     tsclient.PostureIntegrationProviderKolide,
				CloudID:      provider.URL,
				ClientSecret: "secret",
			})
		require.NoError(t, err)
		require.NotEmpty(t, created.ID)
		assert.Equal(t, tsclient.PostureIntegrationProviderKolide, created.Provider)
		assert.Equal(t, provider.URL, created.CloudID)

		got, err := client.DevicePosture().GetIntegration(ctx, created.ID)
		require.NoError(t, err)
		assert.Equal(t, created.ID, got.ID)

		list, err := client.DevicePosture().ListIntegrations(ctx)
		require.NoError(t, err)
		assert.Len(t, list, 1)

		clientID := "kolide-tenant"

		updated, err := client.DevicePosture().UpdateIntegration(ctx, created.ID,
			tsclient.UpdatePostureIntegrationRequest{ClientID: clientID})
		require.NoError(t, err)
		assert.Equal(t, clientID, updated.ClientID)
		assert.Equal(t, provider.URL, updated.CloudID, "an omitted field keeps its stored value")

		require.NoError(t, client.DevicePosture().DeleteIntegration(ctx, created.ID))

		_, err = client.DevicePosture().GetIntegration(ctx, created.ID)
		require.Error(t, err)
		assert.True(t, tsclient.IsNotFound(err), "a deleted integration is gone: %v", err)

		_, err = client.DevicePosture().CreateIntegration(ctx,
			tsclient.CreatePostureIntegrationRequest{
				Provider: tsclient.PostureIntegrationProviderFleet,
				CloudID:  provider.URL,
			})
		require.Error(t, err, "a provider slopscale has no integration for is refused")
		assert.Contains(t, err.Error(), "fleet")
	})

	t.Run("LogStreaming", func(t *testing.T) {
		sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		t.Cleanup(sink.Close)

		logging := client.Logging()

		_, err := logging.LogstreamConfiguration(ctx, tsclient.LogTypeConfig)
		require.Error(t, err, "there is no stream before one is set")
		assert.True(t, tsclient.IsNotFound(err), "%v", err)

		require.NoError(t, logging.SetLogstreamConfiguration(ctx, tsclient.LogTypeConfig,
			tsclient.SetLogstreamConfigurationRequest{
				DestinationType: "http",
				URL:             sink.URL,
				Token:           "sink-token",
			}))

		stream, err := logging.LogstreamConfiguration(ctx, tsclient.LogTypeConfig)
		require.NoError(t, err)
		assert.Equal(t, tsclient.LogTypeConfig, stream.LogType)
		assert.Equal(t, tsclient.LogstreamEndpointType("http"), stream.DestinationType)
		assert.Equal(t, sink.URL, stream.URL)

		// A second PUT replaces the same stream rather than adding one.
		require.NoError(t, logging.SetLogstreamConfiguration(ctx, tsclient.LogTypeConfig,
			tsclient.SetLogstreamConfigurationRequest{
				DestinationType: tsclient.LogstreamSplunkEndpoint,
				URL:             sink.URL + "/splunk",
				Token:           "sink-token",
			}))

		streams, err := srv.State().ListLogStreams()
		require.NoError(t, err)
		assert.Len(t, streams, 1, "the API owns exactly one stream")

		err = logging.SetLogstreamConfiguration(ctx, tsclient.LogTypeConfig,
			tsclient.SetLogstreamConfigurationRequest{
				DestinationType: tsclient.LogstreamS3Endpoint,
				S3Bucket:        "logs",
			})
		require.Error(t, err, "a destination slopscale cannot encode for is refused")

		_, err = logging.LogstreamConfiguration(ctx, tsclient.LogTypeNetwork)
		require.Error(t, err, "slopscale collects no network flow logs")
		assert.Contains(t, err.Error(), "network flow logs are not available")

		require.NoError(t, logging.DeleteLogstreamConfiguration(ctx, tsclient.LogTypeConfig))

		_, err = logging.LogstreamConfiguration(ctx, tsclient.LogTypeConfig)
		require.Error(t, err)
	})

	t.Run("DeviceAllFields", func(t *testing.T) {
		id := node.NodeIDString()

		device, err := client.Devices().GetWithAllFields(ctx, id)
		require.NoError(t, err)
		assert.True(t, device.BlocksIncomingConnections, "shields up is reported")
		assert.False(t, device.IsExternal, "slopscale has no shared-in devices")
		assert.True(t, device.ConnectedToControl, "the device holds a map poll")
		assert.Empty(t, device.TailnetLockError, "the lock is off, so nothing is unsigned")
		assert.True(t, device.SSHEnabled, "the client reported an SSH host key")

		require.NotNil(t, device.Distro)
		assert.Equal(t, "ubuntu", device.Distro.Name)
		assert.Equal(t, "24.04", device.Distro.Version)
		assert.Equal(t, "noble", device.Distro.CodeName)

		require.NotNil(t, device.ClientConnectivity)
		assert.True(t, device.ClientConnectivity.MappingVariesByDestIP)
		assert.True(t, device.ClientConnectivity.ClientSupports.UDP)
		assert.False(t, device.ClientConnectivity.ClientSupports.IPV6)
		assert.True(t, device.ClientConnectivity.ClientSupports.UPNP)

		// The posture identity arrives over c2n after registration, so give
		// the serial numbers a moment to land.
		require.EventuallyWithT(t, func(c *assert.CollectT) {
			d, getErr := client.Devices().GetWithAllFields(ctx, id)
			assert.NoError(c, getErr)

			if !assert.NotNil(c, d.PostureIdentity) {
				return
			}

			assert.Equal(c, []string{"SERIAL-1"}, d.PostureIdentity.SerialNumbers)
			assert.False(c, d.PostureIdentity.Disabled)
		}, compatWait, 100*time.Millisecond)

		// The default field set stays Tailscale's: no route slices, no
		// nested detail objects.
		plain, err := client.Devices().Get(ctx, id)
		require.NoError(t, err)
		assert.Nil(t, plain.ClientConnectivity)
		assert.Nil(t, plain.Distro)
		assert.Empty(t, plain.EnabledRoutes)
		assert.True(t, plain.BlocksIncomingConnections, "a default field is still there")
	})
}
