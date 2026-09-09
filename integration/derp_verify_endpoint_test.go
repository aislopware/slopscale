package integration

import (
	"fmt"
	"net"
	"strconv"
	"testing"

	"github.com/aislopware/slopscale/integration/dsic"
	"github.com/aislopware/slopscale/integration/hsic"
	"github.com/aislopware/slopscale/integration/integrationutil"
	"github.com/aislopware/slopscale/integration/tsic"
	"github.com/stretchr/testify/require"
	"tailscale.com/derp"
	"tailscale.com/derp/derphttp"
	"tailscale.com/net/netmon"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
	"tailscale.com/util/rands"
)

func TestDERPVerifyEndpoint(t *testing.T) {
	IntegrationSkip(t)

	// Generate random hostname for the slopscale instance
	hash := rands.HexString(6)

	testName := "derpverify"
	hostname := fmt.Sprintf("hs-%s-%s", testName, hash)

	slopscalePort := 8080

	// Create cert for slopscale
	certsSlopscale, err := integrationutil.CreateCertificate(hostname)
	require.NoError(t, err)

	spec := ScenarioSpec{
		NodesPerUser: len(MustTestVersions),
		Users:        []string{"user1"},
	}

	scenario, err := NewScenario(spec)

	require.NoError(t, err)
	defer scenario.ShutdownAssertNoPanics(t)

	derper, err := scenario.CreateDERPServer(
		"head",
		dsic.WithCACert(certsSlopscale.CACertPEM),
		dsic.WithVerifyClientURL(
			fmt.Sprintf("https://%s/verify", net.JoinHostPort(hostname, strconv.Itoa(slopscalePort))),
		),
	)
	require.NoError(t, err)

	derpRegion := tailcfg.DERPRegion{
		RegionCode: "test-derpverify",
		RegionName: "TestDerpVerify",
		Nodes: []*tailcfg.DERPNode{
			{
				Name:             "TestDerpVerify",
				RegionID:         900,
				HostName:         derper.GetHostname(),
				STUNPort:         derper.GetSTUNPort(),
				STUNOnly:         false,
				DERPPort:         derper.GetDERPPort(),
				InsecureForTests: true,
			},
		},
	}
	derpMap := tailcfg.DERPMap{
		Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{
			900: &derpRegion,
		},
	}

	// [hsic.WithHostname] is used instead of [hsic.WithTestName] because the hostname
	// must match the pre-generated TLS certificate created above.
	// The test name "derpverify" is embedded in the hostname variable.
	//
	// [tsic.WithCACert] passes the external DERP server's certificate so
	// tailscale clients trust it. [hsic.WithCustomTLS] and [hsic.WithDERPConfig]
	// configure slopscale to use the external DERP server created
	// above instead of the default embedded one.
	err = scenario.CreateSlopscaleEnv([]tsic.Option{tsic.WithCACert(derper.GetCert())},
		hsic.WithHostname(hostname),
		hsic.WithPort(slopscalePort),
		hsic.WithCustomTLS(certsSlopscale.CACertPEM, certsSlopscale.CertPEM, certsSlopscale.KeyPEM),
		hsic.WithDERPConfig(derpMap))
	requireNoErrSlopscaleEnv(t, err)

	allClients, err := scenario.ListTailscaleClients()
	requireNoErrListClients(t, err)

	fakeKey := key.NewNode()
	DERPVerify(t, fakeKey, derpRegion, false)

	for _, client := range allClients {
		nodeKey, err := client.GetNodePrivateKey()
		require.NoError(t, err)
		DERPVerify(t, *nodeKey, derpRegion, true)
	}
}

func DERPVerify(
	t *testing.T,
	nodeKey key.NodePrivate,
	region tailcfg.DERPRegion,
	expectSuccess bool,
) {
	t.Helper()

	c := derphttp.NewRegionClient(nodeKey, t.Logf, netmon.NewStatic(), func() *tailcfg.DERPRegion {
		return &region
	})
	defer c.Close()

	var result error

	err := c.Connect(t.Context())
	if err != nil {
		result = fmt.Errorf("client Connect: %w", err)
	}

	m, recvErr := c.Recv()
	if recvErr != nil {
		result = fmt.Errorf("client first Recv: %w", recvErr)
	} else if v, ok := m.(derp.ServerInfoMessage); !ok {
		result = fmt.Errorf("client first Recv was unexpected type %T", v)
	}

	if expectSuccess && result != nil {
		t.Fatalf(
			"DERP verify failed unexpectedly for client %s. Expected success but got error: %v",
			nodeKey.Public(),
			result,
		)
	} else if !expectSuccess && result == nil {
		t.Fatalf(
			"DERP verify succeeded unexpectedly for client %s. Expected failure but it succeeded.",
			nodeKey.Public(),
		)
	}
}
