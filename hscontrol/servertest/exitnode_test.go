package servertest_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/net/tsaddr"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
	"tailscale.com/types/netmap"
)

const exitNodeWait = 10 * time.Second

// peerHasCap reports whether the peer named hostname in nm carries want
// in its CapMap.
func peerHasCap(nm *netmap.NetworkMap, hostname string, want nodecap.Cap) bool {
	if nm == nil {
		return false
	}

	for _, p := range nm.Peers {
		if p.Hostinfo().Valid() && p.Hostinfo().Hostname() == hostname {
			return p.CapMap().Contains(want)
		}
	}

	return false
}

// TestGlobalExitNodeEndToEnd proves the global exit node through the v1
// API and the clients' netmaps: marking a node that advertises exit
// routes approves them, gives it suggest-exit-node on every other
// client's peer view and every client auto-exit-node in its self view,
// all without a policy; clearing the mark takes the caps away and keeps
// the routes. The subtests build on one another, so they run in order.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestGlobalExitNodeEndToEnd(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "exit-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	exit := servertest.NewClient(t, srv, "exit-1", servertest.WithUser(owner))
	laptop := servertest.NewClient(t, srv, "laptop-1", servertest.WithUser(owner))
	exit.WaitForPeerCount(t, 1, exitNodeWait)
	laptop.WaitForPeerCount(t, 1, exitNodeWait)

	exit.Direct().SetHostinfo(&tailcfg.Hostinfo{
		BackendLogID: "servertest-exit-1",
		Hostname:     "exit-1",
		RoutableIPs:  tsaddr.ExitRoutes(),
	})

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	_ = exit.Direct().SendUpdate(ctx)

	exitURL := v1 + "/node/" + exit.NodeIDString() + "/global-exit-node"

	t.Run("an unmarked exit node is neither suggested nor approved", func(t *testing.T) {
		laptop.WaitForCondition(t, "peer advertising exit routes", exitNodeWait, func(nm *netmap.NetworkMap) bool {
			for _, p := range nm.Peers {
				if p.Hostinfo().Valid() && p.Hostinfo().RoutableIPs().Len() == 2 {
					return true
				}
			}

			return false
		})

		assert.False(t, hasCap(laptop.Netmap(), nodecap.AutoExitNode))
		assert.False(t, peerHasCap(laptop.Netmap(), "exit-1", nodecap.SuggestExitNode))

		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/node/"+exit.NodeIDString(), nil)
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, false, field(t, body, "node", "globalExitNode"))
		assert.Equal(t, []any{}, field(t, body, "node", "approvedRoutes"))
	})

	t.Run("marking approves the routes and hands out the caps", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, exitURL, nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, field(t, body, "node", "globalExitNode"))
		assert.ElementsMatch(t, []any{"0.0.0.0/0", "::/0"}, field(t, body, "node", "approvedRoutes"))

		laptop.WaitForCondition(t, "auto-exit-node and a suggested exit peer", exitNodeWait,
			func(nm *netmap.NetworkMap) bool {
				return hasCap(nm, nodecap.AutoExitNode) && peerHasCap(nm, "exit-1", nodecap.SuggestExitNode)
			})
		exit.WaitForCondition(t, "self caps on the exit node", exitNodeWait, func(nm *netmap.NetworkMap) bool {
			return hasCap(nm, nodecap.AutoExitNode) && hasCap(nm, nodecap.SuggestExitNode)
		})
	})

	t.Run("clearing the mark takes the caps away and keeps the routes", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, exitURL, map[string]bool{"enabled": false})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, false, field(t, body, "node", "globalExitNode"))
		assert.ElementsMatch(t, []any{"0.0.0.0/0", "::/0"}, field(t, body, "node", "approvedRoutes"))

		laptop.WaitForCondition(t, "caps gone", exitNodeWait, func(nm *netmap.NetworkMap) bool {
			return !hasCap(nm, nodecap.AutoExitNode) && !peerHasCap(nm, "exit-1", nodecap.SuggestExitNode)
		})
		exit.WaitForCondition(t, "self caps gone", exitNodeWait, func(nm *netmap.NetworkMap) bool {
			return !hasCap(nm, nodecap.AutoExitNode) && !hasCap(nm, nodecap.SuggestExitNode)
		})
	})
}
