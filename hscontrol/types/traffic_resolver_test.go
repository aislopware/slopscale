package types

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAssignTrafficResolver checks that each client gets one gateway
// resolver, that the clients spread over the resolvers, that a resolver
// joining takes clients only for itself and one leaving moves only its
// own, and that a gateway running a resolver gets none.
func TestAssignTrafficResolver(t *testing.T) {
	t.Parallel()

	resolver := func(id NodeID) TrafficResolver {
		return TrafficResolver{Node: id, Addr: netip.AddrFrom4([4]byte{100, 64, 0, byte(id)})}
	}
	two := []TrafficResolver{resolver(1), resolver(2)}
	three := append(append([]TrafficResolver{}, two...), resolver(3))

	const clients = 1000

	perResolver := map[NodeID]int{}

	for id := NodeID(10); id < 10+clients; id++ {
		before, ok := AssignTrafficResolver(two, id, nil)
		require.True(t, ok)

		perResolver[before.Node]++

		after, ok := AssignTrafficResolver(three, id, nil)
		require.True(t, ok)

		if after != before {
			assert.Equal(t, resolver(3), after, "client %d moved to an old resolver", id)
		}

		again, _ := AssignTrafficResolver(three, id, nil)
		assert.Equal(t, after, again, "the choice is stable")
	}

	assert.InDelta(t, clients/2, perResolver[1], clients/10, "the clients spread over the resolvers")

	_, ok := AssignTrafficResolver(two, 1, nil)
	assert.False(t, ok, "a resolver's gateway gets none")

	_, ok = AssignTrafficResolver(two, 99, []netip.Addr{resolver(2).Addr})
	assert.False(t, ok, "neither does a node holding a resolver's address")

	_, ok = AssignTrafficResolver(nil, 99, nil)
	assert.False(t, ok)
}

// TestWithTrafficResolver checks the DNS a client gets with a gateway
// resolver: the resolver first, then the operator's global nameservers, all
// kept while the client uses an exit node, and the client's own resolver
// overridden.
func TestWithTrafficResolver(t *testing.T) {
	t.Parallel()

	resolver := netip.MustParseAddr("100.64.0.1")
	d := withTrafficResolver(DNSConfig{
		Nameservers: Nameservers{
			Global: []string{"1.1.1.1", "100.64.0.1", "https://dns.example/dns-query"},
			Split:  map[string][]string{"corp.example": {"10.0.0.53"}},
		},
	}, resolver)

	want := []string{"100.64.0.1", "1.1.1.1", "https://dns.example/dns-query"}
	assert.Equal(t, want, d.Nameservers.Global)
	assert.Equal(t, want, d.Nameservers.UseWithExitNode)
	assert.True(t, d.OverrideLocalDNS)
	assert.Equal(t, map[string][]string{"corp.example": {"10.0.0.53"}}, d.Nameservers.Split)
}
