package hscontrol

import (
	"context"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/clientversion"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"tailscale.com/types/views"
)

// Rollout pacing: one client is asked every clientUpdateRolloutTick, so
// the gateways of a tailnet never restart together, and a client that is
// still behind is asked again only after clientUpdateRetry, or as soon as
// a newer release comes out.
const (
	clientUpdateRolloutTick = 2 * time.Minute
	clientUpdateRetry       = 6 * time.Hour
	clientUpdateTimeout     = 30 * time.Second
)

// clientUpdateAttempt is when a node was last asked to update, and to
// which release.
type clientUpdateAttempt struct {
	latest string
	at     time.Time
}

// runClientUpdateRollout asks the clients that allow it to move to the
// latest release, the way the hosted control plane rolls out updates. A
// connected client does not update by itself: its own check runs only
// while it is logged out or stopped, and otherwise it waits for the
// control server's c2n /update.
func (h *Slopscale) runClientUpdateRollout(ctx context.Context) error {
	ticker := time.NewTicker(clientUpdateRolloutTick)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		h.rollOutClientUpdate(ctx, time.Now())
	}
}

// rollOutClientUpdate asks the next node due an update, if any. A refusal
// (an SSH session in progress, a platform that cannot update itself) or a
// failed update is retried later; a node that updated reconnects running
// the latest release and is not picked again.
func (h *Slopscale) rollOutClientUpdate(ctx context.Context, now time.Time) {
	if h.clientUpdatesAsked == nil {
		h.clientUpdatesAsked = make(map[types.NodeID]clientUpdateAttempt)
	}

	latest := h.state.LatestClientVersion()

	node, ok := nextClientUpdate(h.state.ListNodes(), latest, h.clientUpdatesAsked, now)
	if !ok {
		return
	}

	h.clientUpdatesAsked[node.ID()] = clientUpdateAttempt{latest: latest, at: now}

	ctx, cancel := context.WithTimeout(ctx, clientUpdateTimeout)
	defer cancel()

	_, err := h.state.StartClientUpdate(ctx, node.ID(), true, false, h.Change)

	level, msg := zerolog.InfoLevel, "client update started"
	if err != nil {
		level, msg = zerolog.WarnLevel, "client update refused; asking again later"
	}

	e := log.WithLevel(level).Err(err)
	e = e.Uint64("node.id", node.ID().Uint64())
	e = e.Str("node.name", node.GivenName())
	e = e.Str("client.version", clientversion.Short(node.Hostinfo().IPNVersion()))
	e = e.Str("client.latest", latest)
	e.Msg(msg)
}

// nextClientUpdate returns the node with the lowest ID that is due an
// update: connected, allowing remote updates, behind latest, and not
// asked for this release within [clientUpdateRetry].
func nextClientUpdate(
	nodes views.Slice[types.NodeView], latest string, asked map[types.NodeID]clientUpdateAttempt, now time.Time,
) (types.NodeView, bool) {
	var pick types.NodeView

	for _, node := range nodes.All() {
		hi := node.Hostinfo()
		if !node.IsOnline().Valid() || !node.IsOnline().Get() || !hi.Valid() || !hi.AllowsUpdate() ||
			!clientversion.Outdated(hi.IPNVersion(), latest) {
			continue
		}

		if a, ok := asked[node.ID()]; ok && a.latest == latest && now.Sub(a.at) < clientUpdateRetry {
			continue
		}

		if !pick.Valid() || node.ID() < pick.ID() {
			pick = node
		}
	}

	return pick, pick.Valid()
}

// RollOutClientUpdateForTest runs one step of the client update rollout
// at now, for tests that do not run [Slopscale.Serve] and its ticker.
func (h *Slopscale) RollOutClientUpdateForTest(tb testing.TB, now time.Time) {
	tb.Helper()

	h.rollOutClientUpdate(tb.Context(), now)
}
