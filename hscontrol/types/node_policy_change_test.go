package types

import (
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

func policyChangeTestNode() Node {
	ipv4 := netip.MustParseAddr("100.64.0.1")
	now := time.Now()

	return Node{
		ID:         1,
		MachineKey: key.NewMachine().Public(),
		NodeKey:    key.NewNode().Public(),
		DiscoKey:   key.NewDisco().Public(),
		Hostname:   "node",
		GivenName:  "node",
		IPv4:       &ipv4,
		CreatedAt:  now,
		UpdatedAt:  now,
		Hostinfo:   &tailcfg.Hostinfo{Hostname: "node"},
	}
}

// TestHasPolicyChangeUserIDPointerIdentity ensures two distinct *uint
// values holding the same user ID do not register as a policy change.
func TestHasPolicyChangeUserIDPointerIdentity(t *testing.T) {
	userID := uint(7)

	a := policyChangeTestNode()
	a.UserID = &userID

	b := policyChangeTestNode()
	otherPtr := new(uint)
	*otherPtr = 7
	b.UserID = otherPtr

	// Different pointers, same value.
	require.NotSame(t, a.UserID, b.UserID)
	require.False(t, a.View().HasPolicyChange(b.View()),
		"same UserID value via distinct pointers must not register as policy change")

	// And a real change must register.
	c := policyChangeTestNode()
	otherVal := uint(8)
	c.UserID = &otherVal
	require.True(t, a.View().HasPolicyChange(c.View()),
		"different UserID value must register as policy change")
}

// TestHasPolicyChangeUserIDValidity covers the nil vs non-nil transition.
func TestHasPolicyChangeUserIDValidity(t *testing.T) {
	a := policyChangeTestNode()
	// a.UserID nil
	b := policyChangeTestNode()
	v := uint(1)
	b.UserID = &v

	require.True(t, a.View().HasPolicyChange(b.View()),
		"nil -> non-nil UserID must register as policy change")
}

// TestHasPolicyChangeExitRoutes covers the ExitRoutes comparison.
func TestHasPolicyChangeExitRoutes(t *testing.T) {
	exitV4 := netip.MustParsePrefix("0.0.0.0/0")
	exitV6 := netip.MustParsePrefix("::/0")

	base := policyChangeTestNode()
	base.Hostinfo.RoutableIPs = []netip.Prefix{exitV4, exitV6}
	base.ApprovedRoutes = nil // not approved -> no exit routes

	b := policyChangeTestNode()
	b.Hostinfo.RoutableIPs = []netip.Prefix{exitV4, exitV6}
	b.ApprovedRoutes = []netip.Prefix{exitV4, exitV6} // approved -> exit routes live

	require.True(t, base.View().HasPolicyChange(b.View()),
		"enabling exit routes must register as policy change")

	// Reverse: approved on both, no change.
	c := policyChangeTestNode()
	c.Hostinfo.RoutableIPs = []netip.Prefix{exitV4, exitV6}
	c.ApprovedRoutes = []netip.Prefix{exitV4, exitV6}

	require.False(t, b.View().HasPolicyChange(c.View()),
		"identical exit-route state must not register as policy change")
}

func TestHasPolicyChangeFields(t *testing.T) {
	subnet := netip.MustParsePrefix("10.0.0.0/24")
	exit := netip.MustParsePrefix("0.0.0.0/0")

	tests := []struct {
		name   string
		mutate func(*Node)
		want   bool
	}{
		{name: "no change", mutate: func(*Node) {}, want: false},
		{name: "last seen", mutate: func(n *Node) { n.LastSeen = new(time.Now()) }, want: false},
		{name: "expiry", mutate: func(n *Node) { n.Expiry = new(time.Now()) }, want: false},
		{name: "hostname", mutate: func(n *Node) { n.Hostname = "other" }, want: false},
		{name: "tags", mutate: func(n *Node) { n.Tags = []string{"tag:x"} }, want: true},
		{
			name: "ipv4",
			mutate: func(n *Node) {
				ip := netip.MustParseAddr("100.64.0.2")
				n.IPv4 = &ip
			},
			want: true,
		},
		{
			name:   "announced but unapproved subnet",
			mutate: func(n *Node) { n.Hostinfo.RoutableIPs = []netip.Prefix{subnet} },
			want:   false,
		},
		{
			name: "approved and announced subnet",
			mutate: func(n *Node) {
				n.Hostinfo.RoutableIPs = []netip.Prefix{subnet}
				n.ApprovedRoutes = []netip.Prefix{subnet}
			},
			want: true,
		},
		{
			name: "approved and announced exit",
			mutate: func(n *Node) {
				n.Hostinfo.RoutableIPs = []netip.Prefix{exit}
				n.ApprovedRoutes = []netip.Prefix{exit}
			},
			want: true,
		},
		{
			name:   "user association cleared",
			mutate: func(n *Node) { n.User = nil },
			want:   true,
		},
		{
			name:   "user association moved",
			mutate: func(n *Node) { n.User = &User{ID: 8} },
			want:   true,
		},
		{
			name:   "shared with a user",
			mutate: func(n *Node) { n.SharedWith = []UserID{9} },
			want:   true,
		},
		{
			name:   "marked as the global exit node",
			mutate: func(n *Node) { n.GlobalExitNode = true },
			want:   true,
		},
		{
			name:   "reported OS",
			mutate: func(n *Node) { n.Hostinfo.OS = "linux" },
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userID := uint(7)

			base := policyChangeTestNode()
			base.UserID = &userID
			base.User = &User{ID: userID}

			other := *base.Clone()
			tt.mutate(&other)

			require.Equal(t, tt.want, other.View().HasPolicyChange(base.View()))
		})
	}
}
