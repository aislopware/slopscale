package hscontrol

import (
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"tailscale.com/tailcfg"
	"tailscale.com/types/views"
)

// TestNextClientUpdate proves which node the rollout asks next: only a
// connected one that allows remote updates and is behind, lowest ID
// first, and one already asked for this release only after the retry
// interval or once a newer release is out.
func TestNextClientUpdate(t *testing.T) {
	t.Parallel()

	online, offline := true, false

	node := func(id types.NodeID, version string, allows bool, isOnline *bool) types.NodeView {
		return (&types.Node{
			ID:       id,
			IsOnline: isOnline,
			Hostinfo: &tailcfg.Hostinfo{IPNVersion: version, AllowsUpdate: allows},
		}).View()
	}

	nodes := views.SliceOf([]types.NodeView{
		node(9, "1.102.4-t1", true, &online),
		node(3, "1.104.5-t1", true, &online),  // runs the latest
		node(4, "1.102.4-t1", false, &online), // owner did not allow it
		node(5, "1.102.4-t1", true, &offline), // not connected
		node(7, "1.98.4-t1", true, &online),
		(&types.Node{ID: 2, IsOnline: &online}).View(), // no Hostinfo yet
	})

	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	asked := map[types.NodeID]clientUpdateAttempt{}

	pick := func(latest string, at time.Time) types.NodeID {
		n, ok := nextClientUpdate(nodes, latest, asked, at)
		if !ok {
			return 0
		}

		return n.ID()
	}

	assert.Zero(t, pick("", now), "nothing while the latest release is unknown")
	assert.Equal(t, types.NodeID(7), pick("1.104.5", now), "the lowest ID that is due")

	asked[7] = clientUpdateAttempt{latest: "1.104.5", at: now}
	assert.Equal(t, types.NodeID(9), pick("1.104.5", now.Add(clientUpdateRolloutTick)))

	asked[9] = clientUpdateAttempt{latest: "1.104.5", at: now.Add(clientUpdateRolloutTick)}
	assert.Zero(t, pick("1.104.5", now.Add(time.Hour)), "both asked, neither due again yet")
	assert.Equal(t, types.NodeID(7), pick("1.104.5", now.Add(clientUpdateRetry)), "asked again after the retry")
	assert.Equal(t, types.NodeID(3), pick("1.106.0", now.Add(time.Hour)), "a new release makes everyone due")
}
