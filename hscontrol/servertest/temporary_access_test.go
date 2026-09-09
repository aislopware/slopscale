package servertest_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/types/netmap"
)

// TestTemporaryAccess proves the access request flow end to end: a member
// with no admin scope files a request for one of their machines, may not
// decide it, the owner approves it for a while, the machine gains the
// access at once, and the membership and an expiring rule end when their
// time is up. The subtests build on one another.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestTemporaryAccess(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "temp-owner")
	ownerKey := srv.CreateAPIKey(t, owner)
	bob := srv.CreateUser(t, "temp-bob")
	bobKey := srv.CreateAPIKey(t, bob)

	server := servertest.NewClient(t, srv, "server", servertest.WithUser(owner))
	laptop := servertest.NewClient(t, srv, "laptop", servertest.WithUser(bob))
	laptop.WaitForPeerCount(t, 1, postureWait)

	var prodID, opsID, requestID, ruleID string

	t.Run("groups and a rule leave the laptop without peers", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/group", map[string]any{"name": "Prod"})
		require.Equal(t, http.StatusOK, status, body)

		id, ok := field(t, body, "group", "id").(string)
		require.True(t, ok)

		prodID = id

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/group/"+prodID+"/member",
			map[string]any{"nodeId": server.NodeIDString()})
		require.Equal(t, http.StatusOK, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/group",
			map[string]any{"name": "Ops", "requestable": true})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, field(t, body, "group", "requestable"))

		id, ok = field(t, body, "group", "id").(string)
		require.True(t, ok)

		opsID = id

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/access-rule", map[string]any{
			"name": "Ops to prod", "protocol": "tcp", "ports": "22",
			"sourceGroupIds": []string{opsID}, "destinationGroupIds": []string{prodID},
		})
		require.Equal(t, http.StatusOK, status, body)

		id, ok = field(t, body, "rule", "id").(string)
		require.True(t, ok)

		ruleID = id

		laptop.WaitForCondition(t, "no peers", postureWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.Peers) == 0
		})
	})

	t.Run("a member files a request but cannot decide it", func(t *testing.T) {
		status, body := apiCall(t, client, bobKey, http.MethodGet, v1+"/access-request/options", nil)
		require.Equal(t, http.StatusOK, status, body)

		groups, ok := field(t, body, "groups").([]any)
		require.True(t, ok)
		require.Len(t, groups, 1, "only the requestable group is offered")

		nodes, ok := field(t, body, "nodes").([]any)
		require.True(t, ok)
		require.Len(t, nodes, 1, "only the member's own machine")

		status, body = apiCall(t, client, bobKey, http.MethodPost, v1+"/access-request", map[string]any{
			"groupId": prodID, "durationSeconds": 3600,
		})
		assert.Equal(t, http.StatusBadRequest, status, "%v: a group that does not take requests", body)

		status, body = apiCall(t, client, bobKey, http.MethodPost, v1+"/access-request", map[string]any{
			"groupId": opsID, "nodeId": server.NodeIDString(), "durationSeconds": 3600,
		})
		assert.Equal(t, http.StatusBadRequest, status, "%v: somebody else's machine", body)

		status, body = apiCall(t, client, bobKey, http.MethodPost, v1+"/access-request", map[string]any{
			"groupId": opsID, "nodeId": laptop.NodeIDString(), "durationSeconds": 3600, "reason": "deploy",
		})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "pending", field(t, body, "request", "status"))

		id, ok := field(t, body, "request", "id").(string)
		require.True(t, ok)

		requestID = id

		status, body = apiCall(t, client, bobKey, http.MethodPost, v1+"/access-request", map[string]any{
			"groupId": opsID, "nodeId": laptop.NodeIDString(), "durationSeconds": 600,
		})
		assert.Equal(t, http.StatusConflict, status, "%v: one pending request per group and machine", body)

		status, _ = apiCall(t, client, bobKey, http.MethodPost, v1+"/access-request/"+requestID+"/approve", nil)
		assert.Equal(t, http.StatusForbidden, status, "a member holds no policy scope")

		status, body = apiCall(t, client, bobKey, http.MethodGet, v1+"/access-request", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, false, field(t, body, "canDecide"))

		requests, ok := field(t, body, "requests").([]any)
		require.True(t, ok)
		assert.Len(t, requests, 1)
	})

	t.Run("the owner approves and the laptop gains the access", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/access-request/"+requestID+"/approve",
			map[string]any{"note": "go ahead", "durationSeconds": 900})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "approved", field(t, body, "request", "status"))
		assert.Equal(t, "temp-owner", field(t, body, "request", "decidedBy"))
		assert.NotNil(t, field(t, body, "request", "expiresAt"))

		laptop.WaitForCondition(t, "the server as a peer", postureWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.Peers) == 1
		})

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/group/"+opsID, nil)
		require.Equal(t, http.StatusOK, status, body)

		expiries, ok := field(t, body, "group", "expiries").([]any)
		require.True(t, ok)
		require.Len(t, expiries, 1, "the membership carries its expiry")

		status, _ = apiCall(t, client, ownerKey, http.MethodPost, v1+"/access-request/"+requestID+"/deny",
			map[string]any{})
		assert.Equal(t, http.StatusConflict, status, "a decided request stays decided")
	})

	t.Run("the membership ends when its time is up", func(t *testing.T) {
		// The ticker sweeps by the clock, so instead of waiting fifteen
		// minutes run the sweep as if that time had passed.
		c, err := srv.State().ExpireAccess(time.Now(), time.Now().Add(time.Hour))
		require.NoError(t, err)
		require.False(t, c.IsEmpty(), "the policy changed")

		srv.App.Change(c)

		laptop.WaitForCondition(t, "no peers again", postureWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.Peers) == 0
		})
	})

	t.Run("an expiring rule stops applying", func(t *testing.T) {
		// A second rule keeps the policy enforcing once the first expires;
		// without any active rule the tailnet would be allow-all.
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/access-rule", map[string]any{
			"name": "Prod to prod", "protocol": "all",
			"sourceGroupIds": []string{prodID}, "destinationGroupIds": []string{prodID},
		})
		require.Equal(t, http.StatusOK, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/group/"+opsID+"/member",
			map[string]any{"nodeId": laptop.NodeIDString()})
		require.Equal(t, http.StatusOK, status, body)

		laptop.WaitForCondition(t, "the server back", postureWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.Peers) == 1
		})

		past := time.Now().Add(-time.Minute).Format(time.RFC3339)
		status, body = apiCall(t, client, ownerKey, http.MethodPut, v1+"/access-rule/"+ruleID, map[string]any{
			"name": "Ops to prod", "protocol": "tcp", "ports": "22", "expiresAt": past,
			"sourceGroupIds": []string{opsID}, "destinationGroupIds": []string{prodID},
		})
		assert.Equal(t, http.StatusBadRequest, status, "%v: an expiry in the past", body)

		soon := time.Now().Add(1500 * time.Millisecond)
		status, body = apiCall(t, client, ownerKey, http.MethodPut, v1+"/access-rule/"+ruleID, map[string]any{
			"name": "Ops to prod", "protocol": "tcp", "ports": "22", "expiresAt": soon.Format(time.RFC3339Nano),
			"sourceGroupIds": []string{opsID}, "destinationGroupIds": []string{prodID},
		})
		require.Equal(t, http.StatusOK, status, body)
		assert.NotNil(t, field(t, body, "rule", "expiresAt"))

		// The sweep answers with a change only once the expiry has passed.
		var c change.Change

		require.EventuallyWithT(t, func(collect *assert.CollectT) {
			swept, err := srv.State().ExpireAccess(soon.Add(-time.Second), time.Now())
			require.NoError(collect, err)
			assert.False(collect, swept.IsEmpty(), "the expired rule left the policy")

			if !swept.IsEmpty() {
				c = swept
			}
		}, postureWait, 100*time.Millisecond)

		srv.App.Change(c)

		laptop.WaitForCondition(t, "no peers once the rule expired", postureWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.Peers) == 0
		})

		rule, err := srv.State().GetAccessRule(mustRuleID(t, ruleID))
		require.NoError(t, err)
		assert.True(t, rule.Expired(time.Now()), "the rule is kept, marked expired")
	})
}

func mustRuleID(t *testing.T, s string) types.AccessRuleID {
	t.Helper()

	var id uint64

	for _, r := range s {
		require.True(t, r >= '0' && r <= '9')

		id = id*10 + uint64(r-'0')
	}

	return types.AccessRuleID(id)
}
