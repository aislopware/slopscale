package servertest_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/servertest"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/netmap"
)

const dnsWait = 10 * time.Second

// TestDNSSettingsEndToEnd proves the runtime DNS settings: the file's
// values reach the client first, a PUT replaces them in the next netmap
// (with the MagicDNS reverse zones intact), the v2 endpoints edit one
// aspect at a time, roles gate the writes, and a reset returns to the
// file. The subtests build on one another, so they run in order.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestDNSSettingsEndToEnd(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t, servertest.WithDNS(types.DNSConfig{
		MagicDNS:      true,
		BaseDomain:    "ts.example",
		Nameservers:   types.Nameservers{Global: []string{"1.1.1.1"}},
		SearchDomains: []string{"file.example"},
	}))
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"
	v2 := srv.URL + "/api/v2/tailnet/-/dns"

	owner := srv.CreateUser(t, "dns-owner")
	netAdmin := srv.CreateUser(t, "dns-net")
	itAdmin := srv.CreateUser(t, "dns-it")
	ownerKey := srv.CreateAPIKey(t, owner)
	netKey := srv.CreateAPIKey(t, netAdmin)
	itKey := srv.CreateAPIKey(t, itAdmin)

	for _, u := range []struct {
		user *types.User
		role types.Role
	}{{netAdmin, types.RoleNetworkAdmin}, {itAdmin, types.RoleITAdmin}} {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/user/"+userID(u.user)+"/role",
			map[string]string{"role": string(u.role)})
		require.Equal(t, http.StatusOK, status, body)
	}

	node := servertest.NewClient(t, srv, "dns-1", servertest.WithUser(owner))

	node.WaitForCondition(t, "netmap with the file's DNS", dnsWait, func(nm *netmap.NetworkMap) bool {
		return nm.SelfNode.Valid() && len(nm.DNS.FallbackResolvers) == 1
	})

	t.Run("the file is in force at first", func(t *testing.T) {
		nm := node.Netmap()
		assert.Equal(t, "1.1.1.1", nm.DNS.FallbackResolvers[0].Addr)
		assert.Equal(t, []string{"ts.example", "file.example"}, nm.DNS.Domains)
		assert.Contains(t, nm.DNS.Routes, "64.100.in-addr.arpa")

		status, body := apiCall(t, client, itKey, http.MethodGet, v1+"/dns", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, false, body["overridden"])
		assert.Equal(t, true, body["magicDns"])
		assert.Equal(t, "ts.example", body["baseDomain"])
		assert.Equal(t, []any{"1.1.1.1"}, field(t, body, "effective", "nameservers"))
		assert.Equal(t, []any{"1.1.1.1"}, field(t, body, "fromFile", "nameservers"))
	})

	t.Run("a network admin replaces the settings and the client gets them", func(t *testing.T) {
		status, body := apiCall(t, client, netKey, http.MethodPut, v1+"/dns", map[string]any{
			"nameservers":      []string{"9.9.9.9", "https://dns.nextdns.io/abc123"},
			"overrideLocalDns": true,
			"splitNameservers": map[string][]string{"Corp.Example": {"10.0.0.1:5353"}},
			"searchDomains":    []string{"api.example"},
			"extraRecords":     []map[string]string{{"name": "grafana.ts.example", "type": "A", "value": "100.64.0.9"}},
		})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, body["overridden"])
		assert.Equal(t, []any{"1.1.1.1"}, field(t, body, "fromFile", "nameservers"), "the file is still reported")

		node.WaitForCondition(t, "netmap with the override", dnsWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.DNS.Resolvers) == 2
		})

		nm := node.Netmap()
		assert.Equal(t, "9.9.9.9", nm.DNS.Resolvers[0].Addr)
		assert.Empty(t, nm.DNS.FallbackResolvers, "override local DNS moves the resolvers")
		assert.Equal(t, []string{"ts.example", "api.example"}, nm.DNS.Domains)
		require.Contains(t, nm.DNS.Routes, "corp.example", "the domain is lowercased")
		assert.Equal(t, "10.0.0.1:5353", nm.DNS.Routes["corp.example"][0].Addr, "a port survives to the client")
		assert.True(t, strings.HasPrefix(nm.DNS.Resolvers[1].Addr, "https://dns.nextdns.io/abc123?device_"),
			"NextDNS gets the node's identity: %s", nm.DNS.Resolvers[1].Addr)
		assert.Contains(t, nm.DNS.Routes, "64.100.in-addr.arpa", "the MagicDNS zones survive")
		require.Len(t, nm.DNS.ExtraRecords, 1)
		assert.Equal(t, "grafana.ts.example", nm.DNS.ExtraRecords[0].Name)
		assert.True(t, nm.DNS.Proxied)
	})

	t.Run("bad input is rejected", func(t *testing.T) {
		status, body := apiCall(t, client, netKey, http.MethodPut, v1+"/dns", map[string]any{
			"nameservers": []string{"one.one.one.one"},
		})
		assert.Equal(t, http.StatusBadRequest, status, body)

		for _, unsupported := range []string{"tls://dns.example", "https://dns.example/dns-query"} {
			status, body = apiCall(t, client, netKey, http.MethodPut, v1+"/dns", map[string]any{
				"nameservers": []string{unsupported},
			})
			assert.Equal(t, http.StatusBadRequest, status, "the client cannot use %s: %v", unsupported, body)
		}

		status, body = apiCall(t, client, netKey, http.MethodPut, v1+"/dns", map[string]any{
			"extraRecords": []map[string]string{{"name": "x.ts.example", "type": "MX", "value": "mail"}},
		})
		assert.Equal(t, http.StatusUnprocessableEntity, status, "the enum is checked by the schema: %v", body)

		status, body = apiCall(t, client, netKey, http.MethodPut, v1+"/dns", map[string]any{
			"extraRecords": []map[string]string{{"name": "x.ts.example", "type": "A", "value": "fd00::1"}},
		})
		assert.Equal(t, http.StatusBadRequest, status, body)
	})

	t.Run("an IT admin reads but cannot write", func(t *testing.T) {
		status, body := apiCall(t, client, itKey, http.MethodGet, v1+"/dns", nil)
		assert.Equal(t, http.StatusOK, status, body)

		status, body = apiCall(t, client, itKey, http.MethodPut, v1+"/dns", map[string]any{"nameservers": []string{}})
		assert.Equal(t, http.StatusForbidden, status, body)

		status, body = apiCall(t, client, itKey, http.MethodDelete, v1+"/dns", nil)
		assert.Equal(t, http.StatusForbidden, status, body)

		status, body = apiCall(t, client, itKey, http.MethodPost, v2+"/nameservers", map[string]any{"dns": []string{}})
		assert.Equal(t, http.StatusForbidden, status, body)
	})

	t.Run("the v2 endpoints edit one aspect at a time", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodGet, v2+"/nameservers", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, []any{"9.9.9.9", "https://dns.nextdns.io/abc123"}, body["dns"])
		assert.Equal(t, true, body["magicDNS"])

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v2+"/nameservers", map[string]any{
			"dns": []string{"8.8.8.8"},
		})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, []any{"8.8.8.8"}, body["dns"])
		assert.Equal(t, true, body["overrideLocalDns"], "the flag is kept unless given")

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v2+"/preferences", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, body["magicDNS"])

		status, body = apiCall(
			t,
			client,
			ownerKey,
			http.MethodPost,
			v2+"/preferences",
			map[string]any{"magicDNS": false},
		)
		assert.Equal(t, http.StatusBadRequest, status, "MagicDNS lives in the file: %v", body)

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v2+"/searchpaths", map[string]any{
			"searchPaths": []string{"one.example", "two.example"},
		})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, []any{"one.example", "two.example"}, body["searchPaths"])

		status, body = apiCall(t, client, ownerKey, http.MethodPatch, v2+"/split-dns", map[string]any{
			"lab.example":  []string{"10.1.0.1"},
			"corp.example": nil,
		})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, map[string]any{"lab.example": []any{"10.1.0.1"}}, body)

		status, body = apiCall(t, client, ownerKey, http.MethodPut, v2+"/split-dns", map[string]any{
			"put.example": []string{"10.2.0.1"},
		})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, map[string]any{"put.example": []any{"10.2.0.1"}}, body)

		node.WaitForCondition(t, "netmap with the v2 edits", dnsWait, func(nm *netmap.NetworkMap) bool {
			_, ok := nm.DNS.Routes["put.example"]

			return ok && len(nm.DNS.Resolvers) == 1
		})

		nm := node.Netmap()
		assert.Equal(t, "8.8.8.8", nm.DNS.Resolvers[0].Addr)
		assert.Equal(t, []string{"ts.example", "one.example", "two.example"}, nm.DNS.Domains)
		assert.NotContains(t, nm.DNS.Routes, "lab.example")
		assert.Len(t, nm.DNS.ExtraRecords, 1, "the other aspects are kept")
	})

	t.Run("a reset returns to the file", func(t *testing.T) {
		status, body := apiCall(t, client, netKey, http.MethodDelete, v1+"/dns", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, false, body["overridden"])

		node.WaitForCondition(t, "netmap with the file's DNS again", dnsWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.DNS.FallbackResolvers) == 1 && len(nm.DNS.Resolvers) == 0
		})

		nm := node.Netmap()
		assert.Equal(t, "1.1.1.1", nm.DNS.FallbackResolvers[0].Addr)
		assert.Equal(t, []string{"ts.example", "file.example"}, nm.DNS.Domains)
		assert.Empty(t, nm.DNS.ExtraRecords)

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/audit", nil)
		require.Equal(t, http.StatusOK, status, body)

		events, ok := body["events"].([]any)
		require.True(t, ok)

		actions := map[string]bool{}

		for _, e := range events {
			event, ok := e.(map[string]any)
			require.True(t, ok)

			action, ok := event["action"].(string)
			require.True(t, ok)

			actions[action] = true
		}

		assert.True(t, actions["dns.set"], "audit: %v", actions)
		assert.True(t, actions["dns.reset"], "audit: %v", actions)
	})
}

// TestDNSEditsWithFileOwnedRecords checks that dns.extra_records_path
// owning the records does not block edits of everything else: the v2
// endpoints and a v1 PUT without records still work, while the file's
// records stay in the effective settings.
func TestDNSEditsWithFileOwnedRecords(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t, servertest.WithDNS(types.DNSConfig{
		MagicDNS:         true,
		BaseDomain:       "ts.example",
		ExtraRecordsPath: t.TempDir() + "/records.json",
		Nameservers:      types.Nameservers{Global: []string{"1.1.1.1"}},
	}))
	srv.App.SetExtraRecordsForTest([]tailcfg.DNSRecord{{Name: "file.ts.example", Type: "A", Value: "100.64.0.7"}})

	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"
	v2 := srv.URL + "/api/v2/tailnet/-/dns"

	owner := srv.CreateUser(t, "dns-file-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/dns", nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.Len(t, field(t, body, "effective", "extraRecords"), 1, "the file's records are effective")

	status, body = apiCall(t, client, ownerKey, http.MethodPost, v2+"/nameservers", map[string]any{
		"dns": []string{"9.9.9.9"},
	})
	require.Equal(t, http.StatusOK, status, "a v2 edit leaves the file's records alone: %v", body)

	status, body = apiCall(t, client, ownerKey, http.MethodPost, v2+"/searchpaths", map[string]any{
		"searchPaths": []string{"lab.example"},
	})
	require.Equal(t, http.StatusOK, status, body)

	status, body = apiCall(t, client, ownerKey, http.MethodPut, v1+"/dns", map[string]any{
		"nameservers":  []string{"8.8.8.8"},
		"extraRecords": []map[string]string{{"name": "x.ts.example", "type": "A", "value": "100.64.0.1"}},
	})
	assert.Equal(t, http.StatusBadRequest, status, "records are the file's while the path is set: %v", body)

	status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/dns", nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, []any{"9.9.9.9"}, field(t, body, "effective", "nameservers"))
	assert.Equal(t, []any{"lab.example"}, field(t, body, "effective", "searchDomains"))
	assert.Len(t, field(t, body, "effective", "extraRecords"), 1, "the file's records survive the edits")
}
