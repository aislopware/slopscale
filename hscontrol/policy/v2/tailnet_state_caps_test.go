package v2

import (
	"net/netip"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/juanfont/headscale/hscontrol/types"
	"tailscale.com/net/tsaddr"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
)

// TestPeerCapMap pins which self caps reach the peer view: the caps the
// client reads from a peer entry, each under its own condition, and
// nothing else.
func TestPeerCapMap(t *testing.T) {
	t.Parallel()

	exitRoutes := []netip.Prefix{tsaddr.AllIPv4(), tsaddr.AllIPv6()}
	exitNode := &types.Node{
		Hostinfo:       &tailcfg.Hostinfo{RoutableIPs: exitRoutes},
		ApprovedRoutes: exitRoutes,
	}
	plainNode := &types.Node{Hostinfo: &tailcfg.Hostinfo{}}

	empty := []tailcfg.RawMessage{}

	tests := []struct {
		name string
		peer *types.Node
		self tailcfg.NodeCapMap
		want tailcfg.NodeCapMap
	}{
		{
			name: "no self caps",
			peer: exitNode,
			self: nil,
			want: nil,
		},
		{
			name: "self-only caps stay on the self view",
			peer: plainNode,
			self: tailcfg.NodeCapMap{nodecap.SSH: empty, nodecap.FileSharing: empty},
			want: nil,
		},
		{
			name: "suggest-exit-node needs an exit node",
			peer: plainNode,
			self: tailcfg.NodeCapMap{nodecap.SuggestExitNode: empty},
			want: nil,
		},
		{
			name: "suggest-exit-node on an exit node",
			peer: exitNode,
			self: tailcfg.NodeCapMap{nodecap.SuggestExitNode: empty},
			want: tailcfg.NodeCapMap{nodecap.SuggestExitNode: empty},
		},
		{
			name: "dns-subdomain-resolve reaches every peer",
			peer: plainNode,
			self: tailcfg.NodeCapMap{nodecap.DNSSubdomainResolve: empty, nodecap.SSH: empty},
			want: tailcfg.NodeCapMap{nodecap.DNSSubdomainResolve: empty},
		},
		{
			name: "both peer caps on an exit node",
			peer: exitNode,
			self: tailcfg.NodeCapMap{nodecap.DNSSubdomainResolve: empty, nodecap.SuggestExitNode: empty},
			want: tailcfg.NodeCapMap{nodecap.DNSSubdomainResolve: empty, nodecap.SuggestExitNode: empty},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := PeerCapMap(tt.peer.View(), tt.self)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("PeerCapMap mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
