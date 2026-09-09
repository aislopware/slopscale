package state

import (
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/net/tsaddr"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
)

func TestSetGlobalExitNode(t *testing.T) {
	t.Parallel()

	s := newRoleTestState(t)
	user := createUserWithRole(t, s, "exit-owner", "")

	exit := s.CreateRegisteredNodeForTest(user, "exit-1")
	exit.Hostinfo = &tailcfg.Hostinfo{RoutableIPs: tsaddr.ExitRoutes()}
	s.PutNodeInStoreForTest(*exit)

	other := s.CreateRegisteredNodeForTest(user, "laptop-1")
	s.PutNodeInStoreForTest(*other)

	_, _, err := s.SetGlobalExitNode(types.NodeID(999), true)
	require.ErrorIs(t, err, ErrNodeNotInNodeStore)

	// Nobody carries the exit-node caps yet.
	_, ok := s.polMan.NodeCapMap(other.ID)[nodecap.AutoExitNode]
	assert.False(t, ok)

	view, c, err := s.SetGlobalExitNode(exit.ID, true)
	require.NoError(t, err)
	assert.True(t, view.IsGlobalExitNode())
	assert.True(t, view.IsExitNode(), "marking approves the exit routes")
	assert.ElementsMatch(t, tsaddr.ExitRoutes(), view.ApprovedRoutes().AsSlice())
	assert.True(t, c.RequiresRuntimePeerComputation)
	assert.True(t, c.IncludeSelf, "every client needs its own caps")

	_, ok = s.polMan.NodeCapMap(exit.ID)[nodecap.SuggestExitNode]
	assert.True(t, ok, "the global exit node is suggested")

	for _, id := range []types.NodeID{exit.ID, other.ID} {
		_, ok = s.polMan.NodeCapMap(id)[nodecap.AutoExitNode]
		assert.True(t, ok, "node %d may pick an exit node automatically", id)
	}

	fromDB, err := s.DB().GetNodeByID(exit.ID)
	require.NoError(t, err)
	assert.True(t, fromDB.GlobalExitNode)
	assert.ElementsMatch(t, tsaddr.ExitRoutes(), fromDB.ApprovedRoutes)

	view, _, err = s.SetGlobalExitNode(exit.ID, false)
	require.NoError(t, err)
	assert.False(t, view.IsGlobalExitNode())
	assert.True(t, view.IsExitNode(), "clearing the mark keeps the approved routes")

	for _, id := range []types.NodeID{exit.ID, other.ID} {
		_, ok = s.polMan.NodeCapMap(id)[nodecap.AutoExitNode]
		assert.False(t, ok, "node %d loses auto-exit-node with the last global exit node", id)
	}

	_, ok = s.polMan.NodeCapMap(exit.ID)[nodecap.SuggestExitNode]
	assert.False(t, ok)
}
