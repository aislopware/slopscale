package servertest_test

import (
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/stretchr/testify/require"
	"tailscale.com/types/netmap"
)

// TestDeletedNodeIsToldToLogInAgain covers aislopware/slopscale#3410: a
// deleted node's long poll used to run on with keep-alives, and once it
// broke the client re-polled its old key into a 404 for good. Now the
// stream ends with the node's own entry expired, which tailscaled reads as
// NeedsLogin, and a later poll with the same key gets the same answer.
func TestDeletedNodeIsToldToLogInAgain(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	user := srv.CreateUser(t, "gone-user")

	victim := servertest.NewClient(t, srv, "gone-victim", servertest.WithUser(user))
	witness := servertest.NewClient(t, srv, "gone-witness", servertest.WithUser(user))

	victim.WaitForPeers(t, 1, 10*time.Second)
	witness.WaitForPeers(t, 1, 10*time.Second)

	nv, ok := srv.State().GetNodeByID(findNodeID(t, srv, "gone-victim"))
	require.True(t, ok)

	ch, err := srv.State().DeleteNode(nv)
	require.NoError(t, err)
	srv.App.Change(ch)

	selfExpired := func(nm *netmap.NetworkMap) bool {
		return nm.SelfNode.Valid() && nm.SelfNode.KeyExpiry().Before(time.Now())
	}

	victim.WaitForCondition(t, "the stream's last frame expires the self key", 10*time.Second, selfExpired)

	select {
	case <-victim.PollEnded():
	case <-time.After(10 * time.Second):
		t.Fatal("the deleted node's long poll did not end")
	}

	witness.WaitForPeerCount(t, 0, 10*time.Second)

	// The client polls again with the key the server no longer knows.
	require.NoError(t, victim.RestartPoll(t.Context()))
	victim.WaitForCondition(t, "a poll with an unknown key expires the self key", 10*time.Second, selfExpired)

	select {
	case <-victim.PollEnded():
	case <-time.After(10 * time.Second):
		t.Fatal("the unknown node's poll did not end")
	}

	// The witness is untouched by the whole episode.
	require.Empty(t, witness.Peers())
}
