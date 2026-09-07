package dnsprovider_test

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/dnsprovider"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCloudflare is enough of the Cloudflare API for the provider: one
// zone, listing and creating TXT records, and a bearer token check.
type fakeCloudflare struct {
	*httptest.Server

	mu      sync.Mutex
	zone    string
	zoneID  string
	records []map[string]any
	calls   []string
}

func newFakeCloudflare(t *testing.T, zone, zoneID string) *fakeCloudflare {
	t.Helper()

	f := &fakeCloudflare{zone: zone, zoneID: zoneID}
	mux := http.NewServeMux()

	reply := func(w http.ResponseWriter, result any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "errors": []any{}, "result": result})
	}

	mux.HandleFunc("GET /zones", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, "GET /zones?name="+r.URL.Query().Get("name"))
		f.mu.Unlock()

		if r.URL.Query().Get("name") == f.zone {
			reply(w, []map[string]any{{"id": f.zoneID, "name": f.zone}})

			return
		}

		reply(w, []map[string]any{})
	})
	mux.HandleFunc("GET /zones/{zone}/dns_records", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()

		f.calls = append(f.calls, "GET records "+r.URL.Query().Get("name"))

		var out []map[string]any

		for _, rec := range f.records {
			if rec["name"] == r.URL.Query().Get("name") {
				out = append(out, rec)
			}
		}

		reply(w, out)
	})
	mux.HandleFunc("POST /zones/{zone}/dns_records", func(w http.ResponseWriter, r *http.Request) {
		var rec map[string]any

		_ = json.NewDecoder(r.Body).Decode(&rec)

		f.mu.Lock()
		f.calls = append(f.calls, "POST record "+r.PathValue("zone"))
		rec["id"] = "rec-1"
		f.records = append(f.records, rec)
		f.mu.Unlock()

		reply(w, rec)
	})

	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer good-token" {
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": false, "errors": []map[string]any{{"code": 9109, "message": "Invalid access token"}},
			})

			return
		}

		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.Close)

	return f
}

// TestCloudflare proves the provider finds the zone by walking up the
// name, creates the record once and leaves an existing value alone, and
// reports the API's own error text.
func TestCloudflare(t *testing.T) {
	t.Parallel()

	api := newFakeCloudflare(t, "example.com", "zone-1")
	provider := dnsprovider.NewCloudflare(types.CloudflareDNSConfig{APIToken: "good-token"}, time.Minute).
		WithBase(api.URL)

	ctx := t.Context()

	require.NoError(t, provider.SetTXT(ctx, "_acme-challenge.laptop.ts.example.com", "token-1"))
	require.NoError(t, provider.SetTXT(ctx, "_acme-challenge.laptop.ts.example.com", "token-1"), "already there")

	api.mu.Lock()
	records := api.records
	calls := api.calls
	api.mu.Unlock()

	require.Len(t, records, 1)
	assert.Equal(t, "TXT", records[0]["type"])
	assert.Equal(t, "_acme-challenge.laptop.ts.example.com", records[0]["name"])
	assert.Equal(t, "token-1", records[0]["content"])
	assert.InDelta(t, 60, records[0]["ttl"], 0)

	assert.Contains(t, calls, "GET /zones?name=laptop.ts.example.com")
	assert.Contains(t, calls, "GET /zones?name=ts.example.com")
	assert.Contains(t, calls, "GET /zones?name=example.com")
	assert.Equal(t, 1, countPrefix(calls, "POST record zone-1"))

	bad := dnsprovider.NewCloudflare(types.CloudflareDNSConfig{APIToken: "wrong"}, time.Minute).WithBase(api.URL)
	err := bad.SetTXT(ctx, "_acme-challenge.laptop.ts.example.com", "token-2")
	require.ErrorContains(t, err, "Invalid access token")

	missing := dnsprovider.NewCloudflare(types.CloudflareDNSConfig{APIToken: "good-token"}, time.Minute).
		WithBase(api.URL)
	err = missing.SetTXT(ctx, "_acme-challenge.laptop.elsewhere.net", "token-3")
	require.ErrorIs(t, err, dnsprovider.ErrZoneNotFound)

	pinned := dnsprovider.NewCloudflare(
		types.CloudflareDNSConfig{APIToken: "good-token", ZoneID: "zone-1"}, time.Minute,
	).WithBase(api.URL)
	require.NoError(t, pinned.SetTXT(ctx, "_acme-challenge.other.ts.example.com", "token-4"))
}

func countPrefix(items []string, prefix string) int {
	n := 0

	for _, item := range items {
		if strings.HasPrefix(item, prefix) {
			n++
		}
	}

	return n
}

// TestCommand proves the program gets the name and the value, and that
// its failure and output come back as the error.
func TestCommand(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	out := filepath.Join(dir, "records")
	script := filepath.Join(dir, "set-txt.sh")

	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\n"+
		`if [ "$1" = "fail.example.com" ]; then echo "zone is read-only"; exit 3; fi`+"\n"+
		`echo "$1 $2" >> "`+out+`"`+"\n"), 0o700))

	provider := dnsprovider.NewCommand(types.CommandDNSConfig{Path: script})

	require.NoError(t, provider.SetTXT(t.Context(), "_acme-challenge.laptop.example.com", "token"))

	got, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "_acme-challenge.laptop.example.com token\n", string(got))

	err = provider.SetTXT(t.Context(), "fail.example.com", "token")
	require.ErrorContains(t, err, "zone is read-only")
}

// updateServer is an authoritative server that accepts dynamic updates
// for one zone and remembers the TXT records it was given.
type updateServer struct {
	mu      sync.Mutex
	zone    string
	txts    map[string][]string
	signed  bool
	server  *dns.Server
	address string
}

func newUpdateServer(t *testing.T, zone, keyName, secret string) *updateServer {
	t.Helper()

	u := &updateServer{zone: dns.Fqdn(zone), txts: map[string][]string{}}

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	u.address = listener.Addr().String()
	u.server = &dns.Server{
		Listener: listener,
		// The default acceptor turns UPDATE away with NOTIMP.
		MsgAcceptFunc: func(dns.Header) dns.MsgAcceptAction { return dns.MsgAccept },
		Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
			reply := new(dns.Msg)
			reply.SetReply(r)

			u.mu.Lock()
			defer u.mu.Unlock()

			if r.Opcode != dns.OpcodeUpdate || len(r.Question) != 1 || r.Question[0].Name != u.zone {
				reply.Rcode = dns.RcodeRefused
				_ = w.WriteMsg(reply)

				return
			}

			if keyName != "" {
				if r.IsTsig() == nil || w.TsigStatus() != nil {
					reply.Rcode = dns.RcodeNotAuth
					_ = w.WriteMsg(reply)

					return
				}

				u.signed = true
			}

			for _, rr := range r.Ns {
				if txt, ok := rr.(*dns.TXT); ok {
					u.txts[txt.Hdr.Name] = append(u.txts[txt.Hdr.Name], txt.Txt...)
				}
			}

			_ = w.WriteMsg(reply)
		}),
	}

	if keyName != "" {
		u.server.TsigSecret = map[string]string{dns.Fqdn(keyName): secret}
	}

	go func() { _ = u.server.ActivateAndServe() }()

	t.Cleanup(func() { _ = u.server.Shutdown() })

	return u
}

// TestRFC2136 proves the update lands in the zone, signed when a key
// is configured, and that a name outside the zone is refused before
// anything is sent.
func TestRFC2136(t *testing.T) {
	t.Parallel()

	const secret = "c2VjcmV0c2VjcmV0c2VjcmV0c2VjcmV0c2VjcmV0"

	t.Run("signed", func(t *testing.T) {
		t.Parallel()

		server := newUpdateServer(t, "ts.example.com", "headscale", secret)

		provider, err := dnsprovider.NewRFC2136(types.RFC2136Config{
			Server: server.address, TSIGKeyName: "headscale", TSIGSecret: secret, TSIGAlgorithm: "hmac-sha256",
		}, "ts.example.com", time.Minute)
		require.NoError(t, err)

		require.NoError(t, provider.SetTXT(t.Context(), "_acme-challenge.laptop.ts.example.com", "token"))

		server.mu.Lock()
		defer server.mu.Unlock()

		assert.True(t, server.signed)
		assert.Equal(t, []string{"token"}, server.txts["_acme-challenge.laptop.ts.example.com."])
	})

	t.Run("unsigned", func(t *testing.T) {
		t.Parallel()

		server := newUpdateServer(t, "ts.example.com", "", "")

		provider, err := dnsprovider.NewRFC2136(types.RFC2136Config{Server: server.address}, "ts.example.com",
			time.Minute)
		require.NoError(t, err)

		require.NoError(t, provider.SetTXT(t.Context(), "_acme-challenge.laptop.ts.example.com", "token"))

		err = provider.SetTXT(t.Context(), "_acme-challenge.laptop.elsewhere.net", "token")
		require.ErrorIs(t, err, dnsprovider.ErrNameOutsideZone)
	})

	t.Run("wrong key is refused by the server", func(t *testing.T) {
		t.Parallel()

		server := newUpdateServer(t, "ts.example.com", "headscale", secret)

		provider, err := dnsprovider.NewRFC2136(types.RFC2136Config{
			Server: server.address, TSIGKeyName: "headscale",
			TSIGSecret: "d3JvbmdzZWNyZXR3cm9uZ3NlY3JldHdyb25nc2VjcmV0", TSIGAlgorithm: "hmac-sha256",
		}, "ts.example.com", time.Minute)
		require.NoError(t, err)

		err = provider.SetTXT(t.Context(), "_acme-challenge.laptop.ts.example.com", "token")
		require.Error(t, err)
	})

	_, err := dnsprovider.NewRFC2136(types.RFC2136Config{
		Server: "127.0.0.1:53", TSIGKeyName: "k", TSIGSecret: secret, TSIGAlgorithm: "hmac-sha9000",
	}, "ts.example.com", time.Minute)
	require.ErrorIs(t, err, dnsprovider.ErrTSIGAlgorithm)
}

// TestNew proves the config picks the provider, and that the zone for
// RFC 2136 falls back to the base domain.
func TestNew(t *testing.T) {
	t.Parallel()

	_, err := dnsprovider.New(types.HTTPSCertsConfig{Provider: "route53"}, "ts.example.com")
	require.ErrorIs(t, err, types.ErrDNSProviderUnknown)

	provider, err := dnsprovider.New(types.HTTPSCertsConfig{
		Provider: types.DNSProviderRFC2136, RFC2136: types.RFC2136Config{Server: "127.0.0.1:53"},
	}, "ts.example.com")
	require.NoError(t, err)

	err = provider.SetTXT(t.Context(), "_acme-challenge.laptop.elsewhere.net", "token")
	require.ErrorIs(t, err, dnsprovider.ErrNameOutsideZone, "the base domain is the zone")
}
