package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

var errNoSuchHost = errors.New("no such host")

// tableResolver answers from a fixed table and counts lookups.
type tableResolver struct {
	table   map[string][]net.IP
	lookups atomic.Int64
}

func (r *tableResolver) LookupIP(_ context.Context, _, host string) ([]net.IP, error) {
	r.lookups.Add(1)

	ips, ok := r.table[host]
	if !ok {
		return nil, errNoSuchHost
	}

	return ips, nil
}

func bootstrapDERPMap() tailcfg.DERPMapView {
	return (&tailcfg.DERPMap{
		Regions: map[tailcfg.DERPRegionID]*tailcfg.DERPRegion{
			1: {RegionID: 1, Nodes: []*tailcfg.DERPNode{
				{Name: "1a", RegionID: 1, HostName: "derp.example.com"},
				{Name: "1b", RegionID: 1, HostName: "derp.headscale.invalid"},
			}},
		},
	}).View()
}

func bootstrapGet(t *testing.T, b *BootstrapDNS, target string) map[string][]string {
	t.Helper()

	rec := httptest.NewRecorder()
	b.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.Equal(t, "close", rec.Header().Get("Connection"))

	var entries map[string][]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &entries))

	return entries
}

func TestBootstrapDNS(t *testing.T) {
	t.Parallel()

	resolver := &tableResolver{table: map[string][]net.IP{
		"derp.example.com":    {net.ParseIP("192.0.2.1"), net.ParseIP("2001:db8::1")},
		"control.example.com": {net.ParseIP("192.0.2.2")},
		"other.example.com":   {net.ParseIP("192.0.2.3")},
	}}

	b := NewBootstrapDNS(bootstrapDERPMap, "https://control.example.com:8443")
	b.resolver = resolver

	// Without q the answer is every name that resolved: the DERP nodes and
	// the control server, not the one that failed to resolve.
	entries := bootstrapGet(t, b, "/bootstrap-dns")
	assert.Equal(t, map[string][]string{
		"derp.example.com":    {"192.0.2.1", "2001:db8::1"},
		"control.example.com": {"192.0.2.2"},
	}, entries)

	// q narrows the answer to the name the client is after.
	entries = bootstrapGet(t, b, "/bootstrap-dns?q=control.example.com")
	assert.Equal(t, map[string][]string{"control.example.com": {"192.0.2.2"}}, entries)

	// A name that is not ours is never looked up; the client gets the set
	// it would have got without q.
	lookups := resolver.lookups.Load()
	entries = bootstrapGet(t, b, "/bootstrap-dns?q=other.example.com")
	assert.Len(t, entries, 2)
	assert.NotContains(t, entries, "other.example.com")
	assert.Equal(t, lookups, resolver.lookups.Load(), "served from the cache")
}

func TestBootstrapDNSRefreshesOnSchedule(t *testing.T) {
	t.Parallel()

	resolver := &tableResolver{table: map[string][]net.IP{
		"derp.example.com": {net.ParseIP("192.0.2.1")},
	}}

	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	b := NewBootstrapDNS(bootstrapDERPMap, "http://127.0.0.1:8080")
	b.resolver = resolver
	b.now = func() time.Time { return now }

	// The control server is an address, which the client resolves itself,
	// so only the DERP names are looked up: one resolvable, one not.
	assert.Len(t, bootstrapGet(t, b, "/bootstrap-dns"), 1)
	assert.Equal(t, int64(2), resolver.lookups.Load())

	bootstrapGet(t, b, "/bootstrap-dns")
	assert.Equal(t, int64(2), resolver.lookups.Load(), "a fresh cache is not resolved again")

	now = now.Add(bootstrapDNSRefresh)
	resolver.table["derp.example.com"] = []net.IP{net.ParseIP("192.0.2.9")}

	entries := bootstrapGet(t, b, "/bootstrap-dns")
	assert.Equal(t, int64(4), resolver.lookups.Load(), "a stale cache is resolved again")
	assert.Equal(t, []string{"192.0.2.9"}, entries["derp.example.com"])

	// A round in which nothing resolves keeps the last answer.
	now = now.Add(bootstrapDNSRefresh)
	resolver.table = map[string][]net.IP{}

	entries = bootstrapGet(t, b, "/bootstrap-dns")
	assert.Equal(t, []string{"192.0.2.9"}, entries["derp.example.com"])
}

func TestBootstrapDNSEmptyMap(t *testing.T) {
	t.Parallel()

	b := NewBootstrapDNS(func() tailcfg.DERPMapView { return (&tailcfg.DERPMap{}).View() }, "http://[::1]:8080")
	b.resolver = &tableResolver{}

	assert.Empty(t, bootstrapGet(t, b, "/bootstrap-dns"))
}
