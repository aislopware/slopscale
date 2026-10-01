package state

import (
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

// failNodeUpdates makes every node row update fail until the returned
// func is called; the test's cleanup calls it too.
func failNodeUpdates(t *testing.T, s *State) func() {
	t.Helper()

	s.db.SetQueryHook(func(query string) error {
		if strings.HasPrefix(query, "UPDATE nodes") {
			return errInjectedNodeUpdate
		}

		return nil
	})

	stop := func() { s.db.SetQueryHook(nil) }
	t.Cleanup(stop)

	return stop
}

// TestPersistNodeToDBEmptyForPayloadOnlyChange proves persistNodeToDB
// returns an empty change when the write did not touch anything policy
// reads, so each caller can tell a genuinely empty write apart and pick its
// own wire change (see TestPersistCallerChangeDecisions).
func TestPersistNodeToDBEmptyForPayloadOnlyChange(t *testing.T) {
	_, s, nodeID := persistTestSetup(t)
	t.Cleanup(func() { _ = s.Close() })

	genBefore := s.polMan.NodesGeneration()

	view, ok := s.nodeStore.UpdateNode(nodeID, func(n *types.Node) {
		n.Hostinfo = &tailcfg.Hostinfo{Hostname: "payload-only"}
	})
	require.True(t, ok)

	_, c, err := s.persistNodeToDB(view, genBefore)
	require.NoError(t, err)
	assert.True(t, c.IsEmpty(), "a payload-only write must not fabricate a change")
}

// TestPersistCallerChangeDecisions proves each persistNodeToDB caller picks
// its own wire change when persist reports none. A caller that returns
// nothing silently stops a node's peers from learning about it, so every
// case here checks the exact change, not just that persist succeeded.
func TestPersistCallerChangeDecisions(t *testing.T) {
	taggingPolicy := `{
		"tagOwners": {"tag:ci": ["persist-user@"]},
		"acls": [{"action": "accept", "src": ["*"], "dst": ["*:*"]}]
	}`

	tests := []struct {
		name             string
		policy           string
		setup            func(t *testing.T, s *State, nodeID types.NodeID)
		run              func(t *testing.T, s *State, nodeID types.NodeID) change.Change
		wantType         string
		wantOriginNode   bool
		wantPeersChanged bool
	}{
		{
			name: "RenameNode resends the whole node when the rename does not affect policy",
			run: func(t *testing.T, s *State, nodeID types.NodeID) change.Change {
				t.Helper()

				_, c, err := s.RenameNode(nodeID, "renamed")
				require.NoError(t, err)

				return c
			},
			wantType:         "peers",
			wantOriginNode:   true,
			wantPeersChanged: true,
		},
		{
			name:   "SetNodeTags reports a policy change for a real tag assignment",
			policy: taggingPolicy,
			run: func(t *testing.T, s *State, nodeID types.NodeID) change.Change {
				t.Helper()

				_, c, err := s.SetNodeTags(nodeID, []string{"tag:ci"})
				require.NoError(t, err)

				return c
			},
			wantType:         "policy",
			wantOriginNode:   true,
			wantPeersChanged: false,
		},
		{
			name:   "SetNodeTags resends the whole node when re-applying identical tags",
			policy: taggingPolicy,
			setup: func(t *testing.T, s *State, nodeID types.NodeID) {
				t.Helper()

				_, _, err := s.SetNodeTags(nodeID, []string{"tag:ci"})
				require.NoError(t, err)
			},
			run: func(t *testing.T, s *State, nodeID types.NodeID) change.Change {
				t.Helper()

				_, c, err := s.SetNodeTags(nodeID, []string{"tag:ci"})
				require.NoError(t, err)

				return c
			},
			wantType:         "peers",
			wantOriginNode:   true,
			wantPeersChanged: true,
		},
		{
			name: "SetApprovedRoutes of an unannounced route resends only the node",
			run: func(t *testing.T, s *State, nodeID types.NodeID) change.Change {
				t.Helper()

				_, c, err := s.SetApprovedRoutes(nodeID, []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")})
				require.NoError(t, err)

				return c
			},
			wantType:         "peers",
			wantOriginNode:   true,
			wantPeersChanged: true,
		},
		{
			name: "SaveNode resends the whole node for a payload-only save",
			run: func(t *testing.T, s *State, nodeID types.NodeID) change.Change {
				t.Helper()

				current, ok := s.nodeStore.GetNode(nodeID)
				require.True(t, ok)

				n := current.AsStruct()
				n.Hostinfo = &tailcfg.Hostinfo{Hostname: "saved-payload"}

				_, c, err := s.SaveNode(n.View())
				require.NoError(t, err)

				return c
			},
			wantType:         "peers",
			wantOriginNode:   true,
			wantPeersChanged: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, s, nodeID := persistTestSetup(t)
			t.Cleanup(func() { _ = s.Close() })

			if tt.policy != "" {
				_, err := s.SetPolicy([]byte(tt.policy))
				require.NoError(t, err)
			}

			if tt.setup != nil {
				tt.setup(t, s, nodeID)
			}

			c := tt.run(t, s, nodeID)

			assert.Equal(t, tt.wantType, c.Type())

			if tt.wantOriginNode {
				assert.Equal(t, nodeID, c.OriginNode)
			} else {
				assert.Zero(t, c.OriginNode)
			}

			if tt.wantPeersChanged {
				assert.Equal(t, []types.NodeID{nodeID}, c.PeersChanged)
			} else {
				assert.Empty(t, c.PeersChanged)
			}
		})
	}
}

// TestRetryAfterFailedPersistReportsPolicyChange proves a policy move made by
// a write whose database persist failed is still reported. The NodeStore write
// already fed the policy manager, so an identical retry sees nothing new; if
// neither call reported the move, clients would keep the filter and SSH
// policy the write revoked.
func TestRetryAfterFailedPersistReportsPolicyChange(t *testing.T) {
	_, s, nodeID := persistTestSetup(t)
	t.Cleanup(func() { _ = s.Close() })

	_, err := s.SetPolicy([]byte(`{
		"tagOwners": {"tag:ci": ["persist-user@"]},
		"acls": [{"action": "accept", "src": ["autogroup:member"], "dst": ["autogroup:self:*"]}]
	}`))
	require.NoError(t, err)

	stop := failNodeUpdates(t, s)

	_, failedC, err := s.SetNodeTags(nodeID, []string{"tag:ci"})
	require.ErrorIs(t, err, errInjectedNodeUpdate)
	stop()

	_, c, err := s.SetNodeTags(nodeID, []string{"tag:ci"})
	require.NoError(t, err)
	assert.True(t, failedC.IncludePolicy || c.IncludePolicy,
		"the failed call or its retry must report the policy move: failed %s, retry %s",
		failedC.Type(), c.Type())
	assert.Equal(t, nodeID, c.OriginNode)

	if !failedC.IsEmpty() {
		assert.Equal(t, nodeID, failedC.OriginNode,
			"a failed call's change must still refresh the tagged node's self view")
	}
}

// TestFailedPersistPolicyChangeSurvivesDroppedChange covers a policy move
// whose write failed to persist, followed by an unrelated map request whose
// change never reaches the batcher (its initial map failed) and then a
// successful retry. The changes that do get published must still carry the
// policy, or clients keep the filter and SSH policy the write revoked.
func TestFailedPersistPolicyChangeSurvivesDroppedChange(t *testing.T) {
	_, s, nodeID := persistTestSetup(t)
	t.Cleanup(func() { _ = s.Close() })

	_, err := s.SetPolicy([]byte(`{
		"tagOwners": {"tag:ci": ["persist-user@"]},
		"acls": [{"action": "accept", "src": ["autogroup:member"], "dst": ["autogroup:self:*"]}]
	}`))
	require.NoError(t, err)

	stop := failNodeUpdates(t, s)

	_, failedC, err := s.SetNodeTags(nodeID, []string{"tag:ci"})
	require.ErrorIs(t, err, errInjectedNodeUpdate)
	stop()

	nv, ok := s.GetNodeByID(nodeID)
	require.True(t, ok)

	// The map request's change is dropped, as when its initial map fails.
	_, err = s.UpdateNodeFromMapRequest(nodeID, tailcfg.MapRequest{
		NodeKey:  nv.NodeKey(),
		DiscoKey: nv.DiscoKey(),
		Hostinfo: &tailcfg.Hostinfo{Hostname: nv.Hostname()},
	})
	require.NoError(t, err)

	_, retryC, err := s.SetNodeTags(nodeID, []string{"tag:ci"})
	require.NoError(t, err)

	assert.True(t, failedC.IncludePolicy || retryC.IncludePolicy,
		"published changes must carry the policy move: failed %s, retry %s",
		failedC.Type(), retryC.Type())
}

// TestReloadPolicyReturnsChangesOnAutoApproveFailure covers a policy reload
// whose route auto-approval fails to persist. The NodeStore write already
// fed the approved routes to the policy manager, so no later write sees the
// policy move again; the reload must return its changes with the error for
// the caller to publish.
func TestReloadPolicyReturnsChangesOnAutoApproveFailure(t *testing.T) {
	_, s, nodeID := persistTestSetup(t)
	t.Cleanup(func() { _ = s.Close() })

	route := netip.MustParsePrefix("10.9.0.0/24")
	_, ok := s.nodeStore.UpdateNode(nodeID, func(n *types.Node) {
		n.Hostinfo = &tailcfg.Hostinfo{RoutableIPs: []netip.Prefix{route}}
	})
	require.True(t, ok)

	_, err := s.db.SetPolicy(`{
		"acls": [{"action": "accept", "src": ["*"], "dst": ["*:*"]}],
		"autoApprovers": {"routes": {"10.9.0.0/24": ["persist-user@"]}}
	}`)
	require.NoError(t, err)

	failNodeUpdates(t, s)

	cs, err := s.ReloadPolicy()
	require.ErrorIs(t, err, errInjectedNodeUpdate)

	approved, ok := s.GetNodeByID(nodeID)
	require.True(t, ok)
	require.Contains(t, approved.ApprovedRoutes().AsSlice(), route,
		"the NodeStore holds the approval the database write lost")

	assert.True(t, slices.ContainsFunc(cs, func(c change.Change) bool { return c.IncludePolicy }),
		"the reload must return its policy change with the error: %v", cs)
}

// TestNodeWriteChangeWhenPolicyRefreshFails fails both the NodeStore
// writer's SetNodes and the caller's. The write still reached the
// NodeStore, so the change returned with the error must resend the node to
// itself and its peers rather than be empty.
func TestNodeWriteChangeWhenPolicyRefreshFails(t *testing.T) {
	_, s, nodeID := persistTestSetup(t)
	t.Cleanup(func() { _ = s.Close() })

	_, err := s.SetPolicy([]byte(`{
		"tagOwners": {"tag:ci": ["persist-user@"]},
		"acls": [{"action": "accept", "src": ["*"], "dst": ["*:*"]}]
	}`))
	require.NoError(t, err)

	s.polMan = failingSetNodesPolicyManager{PolicyManager: s.polMan}
	swapStatePeersFunc(t, s, func(inner PeerPositionsFunc) PeerPositionsFunc { return inner })

	tests := []struct {
		name  string
		write func() (change.Change, error)
	}{
		{name: "SetNodeTags", write: func() (change.Change, error) {
			_, c, err := s.SetNodeTags(nodeID, []string{"tag:ci"})

			return c, err
		}},
		{name: "RenameNode", write: func() (change.Change, error) {
			_, c, err := s.RenameNode(nodeID, "renamed")

			return c, err
		}},
		{name: "SetNodeExpiry", write: func() (change.Change, error) {
			_, c, err := s.SetNodeExpiry(nodeID, nil)

			return c, err
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := tt.write()
			require.ErrorIs(t, err, errInjectedPolicyNodeUpdate)
			assert.Equal(t, nodeID, c.OriginNode, "change: %s", c.Type())
			assert.Contains(t, c.PeersChanged, nodeID, "change: %s", c.Type())
		})
	}
}
