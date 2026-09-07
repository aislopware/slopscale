package servertest_test

import (
	"maps"
	"net/http"
	"testing"

	"github.com/juanfont/headscale/hscontrol/servertest"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/types/netmap"
)

// TestGroupDNSRules proves split DNS per group: a rule's domains reach
// the machines of its groups and nobody else, a machine joining the
// group picks it up, a domain the tailnet also splits keeps the global
// resolvers first, disabling and deleting withdraw it, the API refuses
// bad input, and a group a rule names cannot be deleted. The subtests
// build on one another, so they run in order.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestGroupDNSRules(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t, servertest.WithDNS(types.DNSConfig{
		MagicDNS:   true,
		BaseDomain: "ts.example",
		Nameservers: types.Nameservers{
			Global: []string{"1.1.1.1"},
			Split:  map[string][]string{"shared.example": {"9.9.9.9"}},
		},
	}))
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "gdns-owner")
	eng := srv.CreateUser(t, "gdns-eng")
	guest := srv.CreateUser(t, "gdns-guest")
	ownerKey := srv.CreateAPIKey(t, owner)

	engNode := servertest.NewClient(t, srv, "eng-laptop", servertest.WithUser(eng))
	guestNode := servertest.NewClient(t, srv, "guest-laptop", servertest.WithUser(guest))

	for _, c := range []*servertest.TestClient{engNode, guestNode} {
		c.WaitForCondition(t, "netmap with DNS", dnsWait, func(nm *netmap.NetworkMap) bool {
			return nm.SelfNode.Valid() && nm.DNS.Routes != nil
		})
	}

	status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/group", map[string]any{
		"name": "Engineering", "userIds": []string{userID(eng)},
	})
	require.Equal(t, http.StatusOK, status, body)

	engGroupID, ok := field(t, body, "group", "id").(string)
	require.True(t, ok)

	routeAddrs := func(nm *netmap.NetworkMap, domain string) []string {
		addrs := make([]string, 0, len(nm.DNS.Routes[domain]))
		for _, r := range nm.DNS.Routes[domain] {
			addrs = append(addrs, r.Addr)
		}

		return addrs
	}

	var ruleID string

	t.Run("bad rules are rejected", func(t *testing.T) {
		good := map[string]any{
			"name": "Corp", "domains": []string{"corp.example"},
			"nameservers": []string{"10.0.0.53"}, "groupIds": []string{engGroupID},
		}

		for _, bad := range []map[string]any{
			{"name": ""},
			{"domains": []string{}},
			{"nameservers": []string{}},
			{"groupIds": []string{}},
			{"domains": []string{"corp..example"}},
			{"nameservers": []string{"ns.example"}},
		} {
			body := maps.Clone(good)
			maps.Copy(body, bad)

			status, resp := apiCall(t, client, ownerKey, http.MethodPost, v1+"/dns/rule", body)
			assert.Equal(t, http.StatusBadRequest, status, resp)
		}

		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/dns/rule", map[string]any{
			"name":        "Ghost",
			"domains":     []string{"corp.example"},
			"nameservers": []string{"10.0.0.53"},
			"groupIds":    []string{"99999"},
		})
		assert.Equal(t, http.StatusNotFound, status, body)
	})

	t.Run("a rule reaches the group's machines only", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/dns/rule", map[string]any{
			"name":        "Corp DNS",
			"domains":     []string{"Corp.Example.", "shared.example"},
			"nameservers": []string{" 10.0.0.53 ", "10.0.0.53", "10.0.0.54:5353"},
			"groupIds":    []string{engGroupID},
		})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(
			t,
			[]any{"corp.example", "shared.example"},
			field(t, body, "rule", "domains"),
			"domains are normalized",
		)
		assert.Equal(
			t,
			[]any{"10.0.0.53", "10.0.0.54:5353"},
			field(t, body, "rule", "nameservers"),
			"blanks and duplicates go",
		)

		id, ok := field(t, body, "rule", "id").(string)
		require.True(t, ok)

		ruleID = id

		engNode.WaitForCondition(t, "the corp route", dnsWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.DNS.Routes["corp.example"]) == 2
		})

		nm := engNode.Netmap()
		assert.Equal(t, []string{"10.0.0.53", "10.0.0.54:5353"}, routeAddrs(nm, "corp.example"))
		assert.Equal(t, []string{"9.9.9.9", "10.0.0.53", "10.0.0.54:5353"}, routeAddrs(nm, "shared.example"),
			"the tailnet's own resolvers come first")
		assert.Contains(t, nm.DNS.Routes, "64.100.in-addr.arpa", "the MagicDNS zones survive")

		assert.Empty(t, routeAddrs(guestNode.Netmap(), "corp.example"), "the guest is outside the group")
		assert.Equal(t, []string{"9.9.9.9"}, routeAddrs(guestNode.Netmap(), "shared.example"))
	})

	t.Run("joining the group brings the rule", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/group/"+engGroupID+"/member",
			map[string]any{"userId": userID(guest)})
		require.Equal(t, http.StatusOK, status, body)

		guestNode.WaitForCondition(t, "the corp route at the guest", dnsWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.DNS.Routes["corp.example"]) == 2
		})

		status, body = apiCall(t, client, ownerKey, http.MethodDelete,
			v1+"/group/"+engGroupID+"/user/"+userID(guest), nil)
		require.Equal(t, http.StatusOK, status, body)

		guestNode.WaitForCondition(t, "the corp route gone from the guest", dnsWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.DNS.Routes["corp.example"]) == 0
		})
	})

	t.Run("disabling withdraws the rule and enabling brings it back", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPut, v1+"/dns/rule/"+ruleID, map[string]any{
			"name": "Corp DNS", "enabled": false,
			"domains": []string{"corp.example"}, "nameservers": []string{"10.0.0.53"}, "groupIds": []string{engGroupID},
		})
		require.Equal(t, http.StatusOK, status, body)

		engNode.WaitForCondition(t, "the corp route withdrawn", dnsWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.DNS.Routes["corp.example"]) == 0
		})

		status, body = apiCall(t, client, ownerKey, http.MethodPut, v1+"/dns/rule/"+ruleID, map[string]any{
			"name":    "Corp DNS",
			"domains": []string{"corp.example"}, "nameservers": []string{"10.0.0.53"}, "groupIds": []string{engGroupID},
		})
		require.Equal(t, http.StatusOK, status, body)

		engNode.WaitForCondition(t, "the corp route back", dnsWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.DNS.Routes["corp.example"]) == 1
		})
	})

	t.Run("a group a rule names cannot be deleted", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodDelete, v1+"/group/"+engGroupID, nil)
		assert.Equal(t, http.StatusConflict, status, body)
	})

	t.Run("deleting the rule withdraws it", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodDelete, v1+"/dns/rule/"+ruleID, nil)
		require.Equal(t, http.StatusOK, status, body)

		engNode.WaitForCondition(t, "the corp route gone", dnsWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.DNS.Routes["corp.example"]) == 0
		})

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/dns/rule/"+ruleID, nil)
		assert.Equal(t, http.StatusNotFound, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/dns/rule", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Empty(t, body["rules"])
	})
}
