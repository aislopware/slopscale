package state

import (
	"sync"
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

	_, _, err := s.SetGlobalExitNode(types.NodeID(999), true, nil)
	require.ErrorIs(t, err, ErrNodeNotInNodeStore)

	negative := -1
	_, _, err = s.SetGlobalExitNode(exit.ID, true, &negative)
	require.ErrorIs(t, err, ErrExitNodePriorityNegative)

	// Nobody carries the exit-node caps yet.
	_, ok := s.polMan.NodeCapMap(other.ID)[nodecap.AutoExitNode]
	assert.False(t, ok)

	view, c, err := s.SetGlobalExitNode(exit.ID, true, nil)
	require.NoError(t, err)
	assert.True(t, view.IsGlobalExitNode())
	assert.Equal(t, 0, view.ExitNodePriority())
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

	_, ok = s.polMan.NodeCapMap(other.ID)[nodecap.TrafficSteering]
	assert.False(t, ok, "no priority, the client picks by DERP latency")

	priority := 20
	view, c, err = s.SetGlobalExitNode(exit.ID, true, &priority)
	require.NoError(t, err)
	assert.Equal(t, 20, view.ExitNodePriority())
	assert.True(t, c.IncludeSelf, "traffic-steering lives on the self node")

	for _, id := range []types.NodeID{exit.ID, other.ID} {
		_, ok = s.polMan.NodeCapMap(id)[nodecap.TrafficSteering]
		assert.True(t, ok, "node %d picks by priority while one is set", id)
	}

	fromDB, err = s.DB().GetNodeByID(exit.ID)
	require.NoError(t, err)
	assert.Equal(t, 20, fromDB.ExitNodePriority)

	view, _, err = s.SetGlobalExitNode(exit.ID, true, nil)
	require.NoError(t, err)
	assert.Equal(t, 20, view.ExitNodePriority(), "re-marking without a priority keeps it")

	cs, _ := s.Connect(exit.ID)
	assert.NotEmpty(t, runtimePeerComputationReasons(cs),
		"a global exit node coming online sends peers a full map so their clients take it up again")

	view, _, err = s.SetGlobalExitNode(exit.ID, false, nil)
	require.NoError(t, err)
	assert.False(t, view.IsGlobalExitNode())
	assert.Equal(t, 0, view.ExitNodePriority(), "clearing the mark resets the priority")
	assert.True(t, view.IsExitNode(), "clearing the mark keeps the approved routes")

	for _, id := range []types.NodeID{exit.ID, other.ID} {
		_, ok = s.polMan.NodeCapMap(id)[nodecap.TrafficSteering]
		assert.False(t, ok, "node %d loses traffic-steering with the last priority", id)
	}

	for _, id := range []types.NodeID{exit.ID, other.ID} {
		_, ok = s.polMan.NodeCapMap(id)[nodecap.AutoExitNode]
		assert.False(t, ok, "node %d loses auto-exit-node with the last global exit node", id)
	}

	_, ok = s.polMan.NodeCapMap(exit.ID)[nodecap.SuggestExitNode]
	assert.False(t, ok)
}

// TestSetGlobalExitNodeKeepsPriorityUnderConcurrentCalls proves that calls
// are serialised: a call without a priority keeps whatever the node has at
// the moment it applies, and overlapping calls leave the NodeStore and the
// database with the same value.
func TestSetGlobalExitNodeKeepsPriorityUnderConcurrentCalls(t *testing.T) {
	t.Parallel()

	s := newRoleTestState(t)
	user := createUserWithRole(t, s, "exit-owner", "")

	exit := s.CreateRegisteredNodeForTest(user, "exit-1")
	exit.Hostinfo = &tailcfg.Hostinfo{RoutableIPs: tsaddr.ExitRoutes()}
	s.PutNodeInStoreForTest(*exit)

	ten := 10
	_, _, err := s.SetGlobalExitNode(exit.ID, true, &ten)
	require.NoError(t, err)

	var wg sync.WaitGroup

	for range 8 {
		wg.Go(func() {
			_, _, keepErr := s.SetGlobalExitNode(exit.ID, true, nil)
			assert.NoError(t, keepErr)
		})
	}

	twenty := 20

	wg.Go(func() {
		_, _, setErr := s.SetGlobalExitNode(exit.ID, true, &twenty)
		assert.NoError(t, setErr)
	})

	wg.Go(func() {
		_, _, clearErr := s.SetGlobalExitNode(exit.ID, false, nil)
		assert.NoError(t, clearErr)
	})

	wg.Wait()

	view, ok := s.GetNodeByID(exit.ID)
	require.True(t, ok)

	fromDB, err := s.DB().GetNodeByID(exit.ID)
	require.NoError(t, err)
	assert.Equal(t, view.IsGlobalExitNode(), fromDB.GlobalExitNode, "store and database agree on the mark")
	assert.Equal(t, view.ExitNodePriority(), fromDB.ExitNodePriority, "store and database agree on the priority")

	if view.IsGlobalExitNode() {
		assert.Equal(t, 20, view.ExitNodePriority(), "a keep never resurrects the value an explicit set replaced")
	} else {
		assert.Equal(t, 0, view.ExitNodePriority(), "clearing resets the priority")
	}
}
