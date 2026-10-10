package policyutil_test

import (
	"net/netip"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/policy/policyutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go4.org/netipx"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/peercap"
	"tailscale.com/types/logger"
	"tailscale.com/wgengine/filter"
)

// clientAnswersPeerDNS is the client's side of
// [policyutil.AnswersPeerDNS], run on Tailscale's own packet filter: the
// check in offersExitNodeOrAppConnectorAndPeerHasAutogroupInternet
// (ipn/ipnlocal/peerapi.go) against the local addresses
// updateFilterLocked gives a connector (0.0.0.0 and ::) or an exit node
// (every address).
func clientAnswersPeerDNS(t *testing.T, rules []tailcfg.FilterRule, src netip.Addr, exitNode bool) bool {
	t.Helper()

	var local netipx.IPSetBuilder

	local.Add(netip.IPv4Unspecified())
	local.Add(netip.IPv6Unspecified())

	if exitNode {
		local.AddPrefix(netip.MustParsePrefix("0.0.0.0/0"))
		local.AddPrefix(netip.MustParsePrefix("::/0"))
	}

	localNets, err := local.IPSet()
	require.NoError(t, err)

	matches, err := filter.MatchesFromFilterRules(rules)
	require.NoError(t, err)

	f := filter.New(matches, nil, localNets, &netipx.IPSet{}, nil, logger.Discard)

	probe := netip.IPv4Unspecified()
	if src.Is6() {
		probe = netip.MustParseAddr("2000::")
	}

	return f.CheckTCP(src, probe, 53) == filter.Accept
}

// TestAnswersPeerDNS pins the control plane's reading of a node's packet
// filter to the verdict Tailscale's filter gives the same rules, so a
// DNS route is never offered toward a resolver that answers 403.
func TestAnswersPeerDNS(t *testing.T) {
	t.Parallel()

	const (
		tcp = 6
		udp = 17
	)

	v4 := netip.MustParseAddr("100.64.0.9")
	v6 := netip.MustParseAddr("fd7a:115c:a1e0::9")
	dns := tailcfg.PortRange{First: 53, Last: 53}

	// What ReduceFilterRules hands a connector for a rule that reaches
	// the internet.
	synthesised := []tailcfg.NetPortRange{{IP: "0.0.0.0/32", Ports: dns}, {IP: "::/128", Ports: dns}}

	tests := []struct {
		name     string
		rules    []tailcfg.FilterRule
		src      netip.Addr
		exitNode bool
		want     bool
	}{
		{
			name:  "no rules",
			rules: nil,
			src:   v4,
			want:  false,
		},
		{
			name:  "synthesised rule for the peer",
			rules: []tailcfg.FilterRule{{SrcIPs: []string{"100.64.0.9/32"}, DstPorts: synthesised}},
			src:   v4,
			want:  true,
		},
		{
			name:  "synthesised rule for another peer",
			rules: []tailcfg.FilterRule{{SrcIPs: []string{"100.64.0.10/32"}, DstPorts: synthesised}},
			src:   v4,
			want:  false,
		},
		{
			name: "node-to-node rule only",
			rules: []tailcfg.FilterRule{{
				SrcIPs:   []string{"100.64.0.9"},
				DstPorts: []tailcfg.NetPortRange{{IP: "100.64.0.1/32", Ports: tailcfg.PortRangeAny}},
			}},
			src:  v4,
			want: false,
		},
		{
			name:  "udp only",
			rules: []tailcfg.FilterRule{{SrcIPs: []string{"100.64.0.9"}, DstPorts: synthesised, IPProto: []int{udp}}},
			src:   v4,
			want:  false,
		},
		{
			name: "tcp and udp",
			rules: []tailcfg.FilterRule{
				{SrcIPs: []string{"100.64.0.9"}, DstPorts: synthesised, IPProto: []int{udp, tcp}},
			},
			src:  v4,
			want: true,
		},
		{
			name: "the internet on another port",
			rules: []tailcfg.FilterRule{{
				SrcIPs:   []string{"100.64.0.9"},
				DstPorts: []tailcfg.NetPortRange{{IP: "0.0.0.0/5", Ports: tailcfg.PortRange{First: 443, Last: 443}}},
			}},
			src:      v4,
			exitNode: true,
			want:     false,
		},
		{
			name: "everything",
			rules: []tailcfg.FilterRule{{
				SrcIPs:   []string{"*"},
				DstPorts: []tailcfg.NetPortRange{{IP: "*", Ports: tailcfg.PortRangeAny}},
			}},
			src:  v4,
			want: true,
		},
		{
			name: "source range and port range",
			rules: []tailcfg.FilterRule{{
				SrcIPs:   []string{"100.64.0.1-100.64.0.20"},
				DstPorts: []tailcfg.NetPortRange{{IP: "0.0.0.0/32", Ports: tailcfg.PortRange{First: 50, Last: 60}}},
			}},
			src:  v4,
			want: true,
		},
		{
			name: "capability grant only",
			rules: []tailcfg.FilterRule{{
				SrcIPs: []string{"100.64.0.9"},
				CapGrant: []tailcfg.CapGrant{{
					Dsts: []netip.Prefix{netip.MustParsePrefix("0.0.0.0/32")},
					Caps: []peercap.Cap{"example.com/cap"},
				}},
			}},
			src:  v4,
			want: false,
		},
		{
			// The client asks about 2000::, which a connector that is
			// not an exit node does not hold.
			name:  "ipv6 peer of a connector",
			rules: []tailcfg.FilterRule{{SrcIPs: []string{"fd7a:115c:a1e0::9/128"}, DstPorts: synthesised}},
			src:   v6,
			want:  false,
		},
		{
			name: "ipv6 peer of an exit node",
			rules: []tailcfg.FilterRule{{
				SrcIPs:   []string{"fd7a:115c:a1e0::9/128"},
				DstPorts: []tailcfg.NetPortRange{{IP: "2000::/3", Ports: tailcfg.PortRangeAny}},
			}},
			src:      v6,
			exitNode: true,
			want:     true,
		},
		{
			name:  "ipv4 rule for an ipv6 peer",
			rules: []tailcfg.FilterRule{{SrcIPs: []string{"100.64.0.9"}, DstPorts: synthesised}},
			src:   v6,
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, policyutil.AnswersPeerDNS(tt.rules, tt.src), "control plane")
			assert.Equal(t, tt.want, clientAnswersPeerDNS(t, tt.rules, tt.src, tt.exitNode), "Tailscale's filter")
		})
	}
}
