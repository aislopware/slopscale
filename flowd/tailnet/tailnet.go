// Package tailnet says which addresses belong to a tailnet, so the agent
// keeps only connections a tailnet node opened to somewhere else.
package tailnet

import (
	"net/netip"

	"tailscale.com/net/tsaddr"
)

// Contains reports whether addr is in the ranges Tailscale assigns node
// addresses from: 100.64.0.0/10 and fd7a:115c:a1e0::/48.
func Contains(addr netip.Addr) bool {
	addr = addr.Unmap()

	return tsaddr.CGNATRange().Contains(addr) || tsaddr.TailscaleULARange().Contains(addr)
}

// Egress reports whether a connection from src to dst leaves the tailnet:
// a tailnet node talking to an address outside it.
func Egress(src, dst netip.Addr) bool {
	return Contains(src) && dst.IsValid() && !Contains(dst)
}
