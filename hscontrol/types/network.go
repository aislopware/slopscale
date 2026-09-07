package types

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"tailscale.com/net/tsaddr"
)

// NetworkID identifies a network in the networks table.
type NetworkID uint64

// String renders the ID in base 10.
func (id NetworkID) String() string {
	return strconv.FormatUint(uint64(id), 10)
}

// Network is a set of prefixes reached through routing nodes, the way
// NetBird's networks and routes work. The routing nodes advertise the
// prefixes and the network approves them; the groups say which machines
// get the routes and, once the tailnet enforces access, may reach them.
// A network whose prefixes are the exit routes is an exit node offer.
type Network struct {
	ID          NetworkID
	Name        string
	Description string
	Enabled     bool
	// Prefixes are the destinations, in canonical form.
	Prefixes []netip.Prefix
	// RouterNodeIDs are the nodes that route the prefixes. Several nodes
	// make a high-availability pair; the primary is elected as usual.
	RouterNodeIDs []NodeID
	// GroupIDs are the groups whose machines get the routes.
	GroupIDs  []GroupID
	CreatedAt time.Time
	UpdatedAt time.Time
}

// IsExitNode reports whether the network offers the exit routes.
func (n Network) IsExitNode() bool {
	return slices.ContainsFunc(n.Prefixes, tsaddr.IsExitRoute)
}

// Routes reports whether the node routes the network and it is on.
func (n Network) Routes(nodeID NodeID) bool {
	return n.Enabled && slices.Contains(n.RouterNodeIDs, nodeID)
}

// Covers reports whether the prefix is one of the network's.
func (n Network) Covers(prefix netip.Prefix) bool {
	return slices.Contains(n.Prefixes, prefix)
}

// Errors returned by the network validation.
var (
	ErrNetworkNameEmpty     = errors.New("network name must not be empty")
	ErrNetworkNameTooLong   = errors.New("network name must be at most 64 characters")
	ErrNetworkNameTaken     = errors.New("network name already exists")
	ErrNetworkNoPrefixes    = errors.New("network needs at least one prefix")
	ErrNetworkNoGroups      = errors.New("network needs at least one group")
	ErrNetworkPrefixInvalid = errors.New("prefix must be an IP address or CIDR such as 10.0.0.0/24")
	ErrNetworkNotFound      = errors.New("network not found")
)

// ParseNetworkPrefixes parses what an operator typed into canonical
// prefixes. A bare address becomes a host prefix; either exit route
// brings the other, because clients advertise them as a pair.
func ParseNetworkPrefixes(values []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(values))

	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}

		var (
			prefix netip.Prefix
			err    error
		)

		if strings.Contains(v, "/") {
			prefix, err = netip.ParsePrefix(v)
		} else {
			var addr netip.Addr

			addr, err = netip.ParseAddr(v)
			if err == nil {
				prefix = netip.PrefixFrom(addr, addr.BitLen())
			}
		}

		if err != nil {
			return nil, fmt.Errorf("%w: %q", ErrNetworkPrefixInvalid, v)
		}

		prefix = prefix.Masked()

		if !slices.Contains(out, prefix) {
			out = append(out, prefix)
		}
	}

	if slices.ContainsFunc(out, tsaddr.IsExitRoute) {
		for _, exit := range tsaddr.ExitRoutes() {
			if !slices.Contains(out, exit) {
				out = append(out, exit)
			}
		}
	}

	return out, nil
}

// ValidateNetwork checks the fields the operator controls. Node and
// group existence is checked by the caller against the model.
func ValidateNetwork(n Network) error {
	switch {
	case strings.TrimSpace(n.Name) == "":
		return ErrNetworkNameEmpty
	case len([]rune(n.Name)) > maxAccessNameLength:
		return ErrNetworkNameTooLong
	case len(n.Prefixes) == 0:
		return ErrNetworkNoPrefixes
	case len(n.GroupIDs) == 0:
		return ErrNetworkNoGroups
	}

	return nil
}
