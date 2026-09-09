package types

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"

	"tailscale.com/tailcfg/nodecap"
)

// FunnelIngressTag is the tag every Funnel ingress node carries: the
// embedded one and any started with `slopscale ingress`. The policy
// lets nodes with this tag open ingress connections to the nodes the
// `funnel` node attribute names.
const FunnelIngressTag = "tag:slopscale-ingress"

// FunnelIngressHostname is the embedded ingress node's name.
const FunnelIngressHostname = "slopscale-ingress"

// DefaultFunnelPorts are the ports Funnel may be turned on for when the
// config names none, the same three the hosted control plane allows.
var DefaultFunnelPorts = []uint16{443, 8443, 10000}

// ErrFunnelListenAddrInvalid is returned for an ingress listen address
// that is not host:port.
var ErrFunnelListenAddrInvalid = errors.New("funnel listen address must be host:port")

// ErrFunnelPortInvalid is returned for a Funnel port outside 1-65535.
var ErrFunnelPortInvalid = errors.New("funnel port must be between 1 and 65535")

// FunnelConfig is the embedded Funnel ingress; see docs/ref/funnel.md.
type FunnelConfig struct {
	// Enabled runs an ingress node inside the server. It joins the
	// tailnet as [FunnelIngressHostname] with [FunnelIngressTag] and
	// forwards public TLS connections to the node named by the SNI.
	Enabled bool
	// ListenAddrs are the public addresses the ingress accepts TLS
	// connections on; the port of each is the port the connection is
	// delivered to on the node, so they should match Ports.
	ListenAddrs []string
	// StateDir holds the ingress node's keys.
	StateDir string
	// Ports are the ports Funnel may be turned on for; empty means
	// [DefaultFunnelPorts].
	Ports []uint16
}

// FunnelPorts returns the ports Funnel may be turned on for, sorted.
func (c FunnelConfig) FunnelPorts() []uint16 {
	if len(c.Ports) == 0 {
		return slices.Clone(DefaultFunnelPorts)
	}

	ports := slices.Clone(c.Ports)
	slices.Sort(ports)

	return slices.Compact(ports)
}

// FunnelPortsCap is the node capability that tells the client which
// ports Funnel may be turned on for, in the hosted control plane's
// shape: "https://tailscale.com/cap/funnel-ports?ports=443,8443,10000".
func (c FunnelConfig) FunnelPortsCap() nodecap.Cap {
	ports := c.FunnelPorts()
	parts := make([]string, 0, len(ports))

	for _, p := range ports {
		parts = append(parts, strconv.Itoa(int(p)))
	}

	return nodecap.Cap(string(nodecap.FunnelPorts) + "?ports=" + strings.Join(parts, ","))
}

// Validate checks the listen addresses and ports.
func (c FunnelConfig) Validate() error {
	for _, addr := range c.ListenAddrs {
		_, _, err := net.SplitHostPort(addr)
		if err != nil {
			return fmt.Errorf("%w: %q", ErrFunnelListenAddrInvalid, addr)
		}
	}

	for _, p := range c.Ports {
		if p == 0 {
			return fmt.Errorf("%w: %d", ErrFunnelPortInvalid, p)
		}
	}

	return nil
}

// FunnelEnabled reports whether the client has a Funnel endpoint on, as
// it tells the server in [tailcfg.Hostinfo.IngressEnabled]; the console
// marks such machines the way the hosted control plane does.
func (nv NodeView) FunnelEnabled() bool {
	hi := nv.Hostinfo()

	return hi.Valid() && hi.IngressEnabled()
}
