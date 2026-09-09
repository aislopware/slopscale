package state

import (
	"fmt"
	"net/netip"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/dnstype"
	"tailscale.com/types/opt"
)

func TestPeerAPIDNS(t *testing.T) {
	t.Parallel()

	nodeV4 := netip.MustParseAddr("100.64.0.1")
	nodeV6 := netip.MustParseAddr("fd7a:115c:a1e0::1")
	peerV4 := netip.MustParseAddr("100.64.0.2")
	peerV6 := netip.MustParseAddr("fd7a:115c:a1e0::2")

	tests := []struct {
		name string
		node types.NodeView
		peer types.NodeView
		want string
	}{
		{
			name: "node with v4 and v6 and peer with peerapi4 and peerapi6 ports",
			node: (&types.Node{
				IPv4: &nodeV4,
				IPv6: &nodeV6,
			}).View(),
			peer: (&types.Node{
				IPv4: &peerV4,
				IPv6: &peerV6,
				Hostinfo: &tailcfg.Hostinfo{
					Services: []tailcfg.Service{
						{Proto: tailcfg.PeerAPI4, Port: 1234},
						{Proto: tailcfg.PeerAPI6, Port: 5678},
					},
				},
			}).View(),
			want: "http://100.64.0.2:1234/dns-query",
		},
		{
			name: "node v6-only",
			node: (&types.Node{
				IPv6: &nodeV6,
			}).View(),
			peer: (&types.Node{
				IPv4: &peerV4,
				IPv6: &peerV6,
				Hostinfo: &tailcfg.Hostinfo{
					Services: []tailcfg.Service{
						{Proto: tailcfg.PeerAPI4, Port: 1234},
						{Proto: tailcfg.PeerAPI6, Port: 5678},
					},
				},
			}).View(),
			want: "http://[fd7a:115c:a1e0::2]:5678/dns-query",
		},
		{
			name: "peer without services",
			node: (&types.Node{
				IPv4: &nodeV4,
				IPv6: &nodeV6,
			}).View(),
			peer: (&types.Node{
				IPv4:     &peerV4,
				IPv6:     &peerV6,
				Hostinfo: &tailcfg.Hostinfo{},
			}).View(),
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := peerAPIDNS(tt.node, tt.peer)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestAppDNSRoutes(t *testing.T) {
	t.Parallel()

	s := newRoleTestState(t)
	user := createUserWithRole(t, s, "test-user", "")

	userNode := s.CreateRegisteredNodeForTest(user, "user-laptop")
	s.PutNodeInStoreForTest(*userNode)

	connector := s.CreateRegisteredNodeForTest(user, "connector-node")
	connector.Tags = []string{"tag:connector"}
	connector.UserID = nil
	connector.User = nil
	connector.Hostinfo = &tailcfg.Hostinfo{
		AppConnector: opt.NewBool(true),
		Services: []tailcfg.Service{
			{Proto: tailcfg.PeerAPI4, Port: 1234},
		},
	}
	s.PutNodeInStoreForTest(*connector)

	pol := fmt.Sprintf(`{
		"tagOwners": {"tag:connector": ["%s@"]},
		"acls": [{"action": "accept", "src": ["*"], "dst": ["*:*"]}]
	}`, user.Name)
	_, err := s.SetPolicy([]byte(pol))
	require.NoError(t, err)

	_, err = s.updatePolicyManagerNodes()
	require.NoError(t, err)

	// No apps -> nil
	assert.Nil(t, s.AppDNSRoutes(userNode.View()), "no apps should yield nil routes")
	assert.Nil(t, s.AppDNSRoutes(connector.View()), "no apps should yield nil routes")

	// Create an app with Connectors: ["tag:connector"], Domains: ["example.com", "*.wild.example"]
	app := types.AppConnector{
		Name:       "my-app",
		Connectors: []string{"tag:connector"},
		Domains:    []string{"example.com", "*.wild.example"},
	}
	_, _, err = s.CreateAppConnector(app)
	require.NoError(t, err)

	// The user node gets routes example.com and wild.example to http://<connector v4>:1234/dns-query
	expectedURL := "http://" + netip.AddrPortFrom(*connector.IPv4, 1234).String() + "/dns-query"
	wantResolvers := []*dnstype.Resolver{{Addr: expectedURL}}

	userRoutes := s.AppDNSRoutes(userNode.View())
	require.NotNil(t, userRoutes)
	assert.Equal(t, wantResolvers, userRoutes["example.com"])
	assert.Equal(t, wantResolvers, userRoutes["wild.example"])

	// The connector node gets none
	assert.Nil(t, s.AppDNSRoutes(connector.View()), "the connector node gets no routes for its own apps")
}
