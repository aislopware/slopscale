package state

import (
	"slices"

	"github.com/aislopware/slopscale/hscontrol/types"
)

// HasFunnelIngress reports whether an ingress node has joined the
// tailnet: the embedded one or one started with `slopscale ingress`.
// Without one Funnel cannot deliver anything, so `tailscale funnel` is
// told so instead of being let through.
func (s *State) HasFunnelIngress() bool {
	for _, node := range s.nodeStore.ListNodes().All() {
		if slices.Contains(node.Tags().AsSlice(), types.FunnelIngressTag) {
			return true
		}
	}

	return false
}

// FunnelIngressNodes returns the ingress nodes that have joined.
func (s *State) FunnelIngressNodes() []types.NodeView {
	var out []types.NodeView

	for _, node := range s.nodeStore.ListNodes().All() {
		if slices.Contains(node.Tags().AsSlice(), types.FunnelIngressTag) {
			out = append(out, node)
		}
	}

	return out
}
