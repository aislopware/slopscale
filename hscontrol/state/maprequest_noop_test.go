package state

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

// TestNoOpMapRequestSkipsPersist ensures an identical, no-op MapRequest does
// not issue a database UPDATE (nor the O(n) policy SetNodes scan that follows
// persistNodeToDB). The node state is unchanged, so persisting is pure waste on
// the hot map-request path.
func TestNoOpMapRequestSkipsPersist(t *testing.T) {
	t.Parallel()

	_, s, nodeID := persistTestSetup(t)
	t.Cleanup(func() { _ = s.Close() })

	var nodeUpdateCount atomic.Int64

	s.DB().SetQueryHook(func(query string) error {
		if strings.HasPrefix(query, "UPDATE nodes") {
			nodeUpdateCount.Add(1)
		}

		return nil
	})
	t.Cleanup(func() { s.DB().SetQueryHook(nil) })

	nv, ok := s.GetNodeByID(nodeID)
	require.True(t, ok, "node should exist in NodeStore")

	stored := nv.AsStruct()

	req := tailcfg.MapRequest{
		NodeKey:  stored.NodeKey,
		DiscoKey: stored.DiscoKey,
		Hostinfo: &tailcfg.Hostinfo{
			Hostname: stored.Hostname,
			NetInfo:  &tailcfg.NetInfo{PreferredDERP: 1},
		},
	}

	// First request establishes the Hostinfo/DERP state (expected to persist).
	_, err := s.UpdateNodeFromMapRequest(nodeID, req)
	require.NoError(t, err)

	nodeUpdateCount.Store(0)

	// Second request is value-identical: a no-op.
	req2 := tailcfg.MapRequest{
		NodeKey:  stored.NodeKey,
		DiscoKey: stored.DiscoKey,
		Hostinfo: &tailcfg.Hostinfo{
			Hostname: stored.Hostname,
			NetInfo:  &tailcfg.NetInfo{PreferredDERP: 1},
		},
	}

	c, err := s.UpdateNodeFromMapRequest(nodeID, req2)
	require.NoError(t, err)

	require.Equalf(t, int64(0), nodeUpdateCount.Load(),
		"no-op MapRequest should not issue any nodes-table UPDATE, got %d",
		nodeUpdateCount.Load())

	// Nothing moved, so nothing goes to the peers either: the change
	// used to be "node added", a peer change fanned out to every node
	// (juanfont/headscale#3417).
	require.True(t, c.IsEmpty(), "no-op MapRequest should yield an empty change, got %+v", c)
}
