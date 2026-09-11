package state

import (
	"errors"
	"strings"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/db"
	"github.com/aislopware/slopscale/hscontrol/policy"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/types/views"
)

var (
	errInjectedNodeDelete       = errors.New("injected node delete failure")
	errInjectedPolicyNodeUpdate = errors.New("injected policy node update failure")
)

// failingSetNodesPolicyManager is a [policy.PolicyManager] whose SetNodes
// always fails, so a test can drive the policy refresh that follows a
// committed deletion into its error path.
type failingSetNodesPolicyManager struct {
	policy.PolicyManager
}

func (failingSetNodesPolicyManager) SetNodes(views.Slice[types.NodeView]) (bool, error) {
	return false, errInjectedPolicyNodeUpdate
}

// TestDeleteNodeKeepsStoreOnDBFailure ensures a deletion that never
// reaches the database leaves the node in the NodeStore and returns an
// empty change, so a live session is not torn down for a node that
// still exists.
func TestDeleteNodeKeepsStoreOnDBFailure(t *testing.T) {
	t.Parallel()

	_, s, nodeID := persistTestSetup(t)
	t.Cleanup(func() { _ = s.Close() })

	node, ok := s.GetNodeByID(nodeID)
	require.True(t, ok)

	s.DB().SetQueryHook(func(query string) error {
		if strings.HasPrefix(query, "DELETE FROM nodes") {
			return errInjectedNodeDelete
		}

		return nil
	})
	t.Cleanup(func() { s.DB().SetQueryHook(nil) })

	c, err := s.DeleteNode(node)
	s.DB().SetQueryHook(nil)
	require.ErrorIs(t, err, errInjectedNodeDelete)
	assert.True(t, c.IsEmpty(), "an uncommitted deletion must not stop the node's session")

	_, ok = s.GetNodeByID(nodeID)
	assert.True(t, ok, "a database failure must leave the in-memory node available")

	_, err = s.db.GetNodeByID(nodeID)
	assert.NoError(t, err, "a failed deletion must leave the durable node row available")
}

// TestDeleteNodeReturnsRemovalOnPolicyFailure ensures a deletion that
// committed still reports the removal when the policy refresh after it
// fails, so callers publish the change and the node's session ends.
func TestDeleteNodeReturnsRemovalOnPolicyFailure(t *testing.T) {
	t.Parallel()

	_, s, nodeID := persistTestSetup(t)
	t.Cleanup(func() { _ = s.Close() })

	node, ok := s.GetNodeByID(nodeID)
	require.True(t, ok)

	s.polMan = failingSetNodesPolicyManager{PolicyManager: s.polMan}

	c, err := s.DeleteNode(node)
	require.ErrorIs(t, err, errInjectedPolicyNodeUpdate)
	assert.Equal(t, []types.NodeID{nodeID}, c.PeersRemoved,
		"a committed deletion must still notify peers and stop the node's session")

	_, ok = s.GetNodeByID(nodeID)
	assert.False(t, ok, "a committed deletion must remove the in-memory node")

	_, err = s.db.GetNodeByID(nodeID)
	require.ErrorIs(t, err, db.ErrNotFound, "a committed deletion must remove the durable node row")
}
