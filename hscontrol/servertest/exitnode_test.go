package servertest_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/net/tsaddr"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
	"tailscale.com/types/netmap"
)

const exitNodeWait = 10 * time.Second

// peerSuggested reports whether the peer named hostname in nm carries
// suggest-exit-node in its CapMap.
func peerSuggested(nm *netmap.NetworkMap, hostname string) bool {
	if nm == nil {
		return false
	}

	for _, p := range nm.Peers {
		if p.Hostinfo().Valid() && p.Hostinfo().Hostname() == hostname {
			return p.CapMap().Contains(nodecap.SuggestExitNode)
		}
	}

	return false
}

// TestGlobalExitNodeEndToEnd proves exit node suggestions through the v1
// API and the clients' netmaps. Every approved exit node is suggested on
// its peers' view of it, as the hosted control plane does, with no policy
// involved; marking a global exit node approves its routes, narrows the
// suggestion to the marked nodes, gives them suggest-exit-node in their
// own view and every client auto-exit-node; clearing the mark widens the
// suggestion again and keeps the routes. The subtests build on one
// another, so they run in order.
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
	other := servertest.NewClient(t, srv, "exit-2", servertest.WithUser(owner))
	laptop := servertest.NewClient(t, srv, "laptop-1", servertest.WithUser(owner))
	exit.WaitForPeerCount(t, 2, exitNodeWait)
	other.WaitForPeerCount(t, 2, exitNodeWait)
	laptop.WaitForPeerCount(t, 2, exitNodeWait)

	for _, c := range []*servertest.TestClient{exit, other} {
		c.Direct().SetHostinfo(&tailcfg.Hostinfo{
			BackendLogID: "servertest-" + c.Name,
			Hostname:     c.Name,
			RoutableIPs:  tsaddr.ExitRoutes(),
		})

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		_ = c.Direct().SendUpdate(ctx)

		cancel()
	}

	exitURL := v1 + "/node/" + exit.NodeIDString() + "/global-exit-node"
	exitRoutes := []any{"0.0.0.0/0", "::/0"}

	t.Run("an unapproved exit node is neither suggested nor approved", func(t *testing.T) {
		laptop.WaitForCondition(t, "peers advertising exit routes", exitNodeWait, func(nm *netmap.NetworkMap) bool {
			advertising := 0

			for _, p := range nm.Peers {
				if p.Hostinfo().Valid() && p.Hostinfo().RoutableIPs().Len() == 2 {
					advertising++
				}
			}

			return advertising == 2
		})

		assert.False(t, hasCap(laptop.Netmap(), nodecap.AutoExitNode))
		assert.False(t, peerSuggested(laptop.Netmap(), "exit-1"))
		assert.False(t, peerSuggested(laptop.Netmap(), "exit-2"))

		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/node/"+exit.NodeIDString(), nil)
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, false, field(t, body, "node", "globalExitNode"))
		assert.Equal(t, []any{}, field(t, body, "node", "approvedRoutes"))
	})

	t.Run("an approved exit node is suggested to its peers", func(t *testing.T) {
		status, body := apiCall(
			t,
			client,
			ownerKey,
			http.MethodPost,
			v1+"/node/"+other.NodeIDString()+"/approve_routes",
			map[string]any{"routes": []string{"0.0.0.0/0", "::/0"}},
		)
		require.Equal(t, http.StatusOK, status, body)

		laptop.WaitForCondition(t, "exit-2 suggested", exitNodeWait, func(nm *netmap.NetworkMap) bool {
			return peerSuggested(nm, "exit-2")
		})
		assert.False(t, hasCap(laptop.Netmap(), nodecap.AutoExitNode))
		assert.False(t, peerSuggested(laptop.Netmap(), "exit-1"))
		assert.False(t, hasCap(other.Netmap(), nodecap.SuggestExitNode), "the self view carries no suggestion")
	})

	t.Run("marking approves the routes, narrows the suggestion and hands out the caps", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, exitURL, nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, field(t, body, "node", "globalExitNode"))
		assert.ElementsMatch(t, exitRoutes, field(t, body, "node", "approvedRoutes"))

		laptop.WaitForCondition(t, "auto-exit-node and only the marked exit suggested", exitNodeWait,
			func(nm *netmap.NetworkMap) bool {
				return hasCap(nm, nodecap.AutoExitNode) &&
					peerSuggested(nm, "exit-1") &&
					!peerSuggested(nm, "exit-2")
			})
		exit.WaitForCondition(t, "self caps on the exit node", exitNodeWait, func(nm *netmap.NetworkMap) bool {
			return hasCap(nm, nodecap.AutoExitNode) && hasCap(nm, nodecap.SuggestExitNode)
		})
	})

	t.Run("clearing the mark widens the suggestion again and keeps the routes", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, exitURL, map[string]bool{"enabled": false})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, false, field(t, body, "node", "globalExitNode"))
		assert.ElementsMatch(t, exitRoutes, field(t, body, "node", "approvedRoutes"))

		laptop.WaitForCondition(t, "both exits suggested, auto-exit-node gone", exitNodeWait,
			func(nm *netmap.NetworkMap) bool {
				return !hasCap(nm, nodecap.AutoExitNode) &&
					peerSuggested(nm, "exit-1") &&
					peerSuggested(nm, "exit-2")
			})
		exit.WaitForCondition(t, "self caps gone", exitNodeWait, func(nm *netmap.NetworkMap) bool {
			return !hasCap(nm, nodecap.AutoExitNode) && !hasCap(nm, nodecap.SuggestExitNode)
		})
	})
}
