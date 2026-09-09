package v2

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/require"
	"tailscale.com/types/views"
)

// benchPeerMapNodes builds count nodes owned by one user, each with a unique
// pair of addresses so the pair scan in BuildPeerMap does real work.
func benchPeerMapNodes(count int) (types.Users, views.Slice[types.NodeView]) {
	users := types.Users{
		{ID: 1, Name: "user1", Email: "user1@slopscale.net"},
	}

	nodes := make(types.Nodes, 0, count)

	for i := range count {
		n := node(
			fmt.Sprintf("node-%d", i),
			fmt.Sprintf("100.64.%d.%d", i/256, i%256),
			fmt.Sprintf("fd7a:115c:a1e0::%x", i+1),
			users[0],
		)
		n.ID = types.NodeID(i + 1)
		nodes = append(nodes, n)
	}

	return users, nodes.ViewSlice()
}

func BenchmarkBuildPeerMap(b *testing.B) {
	const allowAll = `{"acls":[{"action":"accept","src":["*"],"dst":["*:*"]}]}`

	for _, count := range []int{100, 500, 1000} {
		b.Run(strconv.Itoa(count), func(b *testing.B) {
			users, nodes := benchPeerMapNodes(count)

			pm, err := NewPolicyManager([]byte(allowAll), users, nodes)
			require.NoError(b, err)

			if got := len(pm.BuildPeerMap(nodes)); got != count {
				b.Fatalf("benchmark setup error: peer map has %d entries, want %d", got, count)
			}

			b.ReportAllocs()

			for b.Loop() {
				pm.BuildPeerMap(nodes)
			}
		})
	}
}
