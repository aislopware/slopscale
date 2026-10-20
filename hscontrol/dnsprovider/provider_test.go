package dnsprovider_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"codeberg.org/miekg/dns"
	"codeberg.org/miekg/dns/dnsutil"
	"github.com/aislopware/slopscale/hscontrol/dnsprovider"
	"github.com/libdns/libdns"
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
	nextID  int
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
		f.nextID++
		rec["id"] = "rec-" + strconv.Itoa(f.nextID)
		f.records = append(f.records, rec)
		f.mu.Unlock()

		reply(w, rec)
	})
	mux.HandleFunc("DELETE /zones/{zone}/dns_records/{id}", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()

		f.calls = append(f.calls, "DELETE record "+r.PathValue("id"))

		for i, rec := range f.records {
			if rec["id"] == r.PathValue("id") {
				f.records = slices.Delete(f.records, i, i+1)

				reply(w, map[string]any{"id": r.PathValue("id")})

				return
			}
		}

		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success": false, "errors": []map[string]any{{"code": 81044, "message": "Record does not exist."}},
		})
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

// txt is one challenge record, relative to zone.
func txt(name, value string) []libdns.Record {
	return []libdns.Record{libdns.TXT{Name: name, TTL: time.Minute, Text: value}}
}

// TestCloudflare proves the provider finds the zone by walking up the
// name, creates the record once and leaves an existing value alone,
// deletes exactly the value it is given, and reports the API's own error
// text.
func TestCloudflare(t *testing.T) {
	t.Parallel()

	const zone = "ts.example.com."

	api := newFakeCloudflare(t, "example.com", "zone-1")
	provider := dnsprovider.NewCloudflare("good-token", "").WithBase(api.URL)

	ctx := t.Context()

	added, err := provider.AppendRecords(ctx, zone, txt("_acme-challenge.laptop", "token-1"))
	require.NoError(t, err)
	require.Len(t, added, 1)

	again, err := provider.AppendRecords(ctx, zone, txt("_acme-challenge.laptop", "token-1"))
	require.NoError(t, err, "already there")
	assert.Equal(t, added, again, "the existing record comes back with its id")

	api.mu.Lock()
	records := slices.Clone(api.records)
	calls := slices.Clone(api.calls)
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

	// A second value at the name, as in a wildcard order, and a value
	// the operator keeps there.
	_, err = provider.AppendRecords(ctx, zone, txt("_acme-challenge.laptop", "token-2"))
	require.NoError(t, err)
	_, err = provider.AppendRecords(ctx, zone, txt("_acme-challenge.laptop", "operator"))
	require.NoError(t, err)

	// By the id AppendRecords returned, and by looking the value up.
	_, err = provider.DeleteRecords(ctx, zone, added)
	require.NoError(t, err)
	_, err = provider.DeleteRecords(ctx, zone, txt("_acme-challenge.laptop", "token-2"))
	require.NoError(t, err)

	// Gone already, or never there: not an error.
	_, err = provider.DeleteRecords(ctx, zone, added)
	require.NoError(t, err)
	_, err = provider.DeleteRecords(ctx, zone, txt("_acme-challenge.laptop", ""))
	require.NoError(t, err, "an empty value deletes nothing")

	api.mu.Lock()
	require.Len(t, api.records, 1)
	assert.Equal(t, "operator", api.records[0]["content"])
	api.mu.Unlock()

	bad := dnsprovider.NewCloudflare("wrong", "").WithBase(api.URL)
	_, err = bad.AppendRecords(ctx, zone, txt("_acme-challenge.laptop", "token-3"))
	require.ErrorContains(t, err, "Invalid access token")

	missing := dnsprovider.NewCloudflare("good-token", "").WithBase(api.URL)
	_, err = missing.AppendRecords(ctx, "elsewhere.net.", txt("_acme-challenge.laptop", "token-3"))
	require.ErrorIs(t, err, dnsprovider.ErrZoneNotFound)

	pinned := dnsprovider.NewCloudflare("good-token", "zone-1").WithBase(api.URL)
	_, err = pinned.AppendRecords(ctx, zone, txt("_acme-challenge.other", "token-4"))
	require.NoError(t, err)

	_, err = pinned.AppendRecords(ctx, zone, []libdns.Record{libdns.RR{Name: "x", Type: "A", Data: "192.0.2.1"}})
	require.ErrorIs(t, err, dnsprovider.ErrNotTXT)
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

// TestCommand proves the program gets the name and the value, that its
// failure and output come back as the error, and that records are only
// deleted when a delete program is configured.
func TestCommand(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	out := filepath.Join(dir, "records")
	script := filepath.Join(dir, "set-txt.sh")
	remove := filepath.Join(dir, "delete-txt.sh")

	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\n"+
		`if [ "$1" = "fail.example.com" ]; then echo "zone is read-only"; exit 3; fi`+"\n"+
		`echo "add $1 $2" >> "`+out+`"`+"\n"), 0o700))
	require.NoError(t, os.WriteFile(remove, []byte("#!/bin/sh\n"+
		`echo "delete $1 $2" >> "`+out+`"`+"\n"), 0o700))

	provider := &dnsprovider.Command{Path: script}

	_, err := provider.AppendRecords(t.Context(), "example.com.", txt("_acme-challenge.laptop", "token"))
	require.NoError(t, err)

	_, err = provider.DeleteRecords(t.Context(), "example.com.", txt("_acme-challenge.laptop", "token"))
	require.NoError(t, err, "without a delete program the record stays")

	provider.DeletePath = remove

	_, err = provider.DeleteRecords(t.Context(), "example.com.", txt("_acme-challenge.laptop", "token"))
	require.NoError(t, err)

	got, err := os.ReadFile(out)
	require.NoError(t, err)
	assert.Equal(t, "add _acme-challenge.laptop.example.com token\n"+
		"delete _acme-challenge.laptop.example.com token\n", string(got))

	_, err = provider.AppendRecords(t.Context(), "example.com.", txt("fail", "token"))
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

	u := &updateServer{zone: dnsutil.Fqdn(zone), txts: map[string][]string{}}

	var signer dns.HmacTSIG

	if keyName != "" {
		key, err := base64.StdEncoding.DecodeString(secret)
		require.NoError(t, err)

		signer = dns.HmacTSIG{Secret: key}
		keyName = dnsutil.Fqdn(keyName)
	}

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	u.address = listener.Addr().String()
	u.server = &dns.Server{
		Listener: listener,
		Handler: dns.HandlerFunc(func(_ context.Context, w dns.ResponseWriter, r *dns.Msg) {
			// The server hands the handler a message with only the
			// question unpacked.
			require.NoError(t, r.Unpack())

			reply := dnsutil.SetReply(new(dns.Msg), r)

			u.mu.Lock()
			defer u.mu.Unlock()

			if r.Opcode != dns.OpcodeUpdate || len(r.Question) != 1 || r.Question[0].Header().Name != u.zone {
				reply.Rcode = dns.RcodeRefused
				_, _ = reply.WriteTo(w)

				return
			}

			// A signed request is verified with the key it names and
			// answered with a signature over the request MAC, like a
			// real authoritative server.
			var tsig dns.TSIGOption

			if keyName != "" {
				rr, ok := lastTSIG(r)
				if !ok || rr.Hdr.Name != keyName || dns.TSIGVerify(r, signer, &tsig) != nil {
					reply.Rcode = dns.RcodeNotAuth
					_, _ = reply.WriteTo(w)

					return
				}

				u.signed = true
			}

			for _, rr := range r.Ns {
				txt, ok := rr.(*dns.TXT)
				if !ok {
					continue
				}

				// Class NONE deletes the one record (RFC 2136 section 2.5.4).
				if txt.Hdr.Class == dns.ClassNONE {
					u.txts[txt.Hdr.Name] = slices.DeleteFunc(u.txts[txt.Hdr.Name], func(v string) bool {
						return slices.Contains(txt.Txt, v)
					})

					continue
				}

				u.txts[txt.Hdr.Name] = append(u.txts[txt.Hdr.Name], txt.Txt...)
			}

			if keyName != "" {
				reply.Pseudo = append(reply.Pseudo, dns.NewTSIG(keyName, dns.HmacSHA256, 0))
				require.NoError(t, dns.TSIGSign(reply, signer, &tsig))
			}

			_, _ = reply.WriteTo(w)
		}),
	}

	go func() { _ = u.server.ListenAndServe() }()

	t.Cleanup(func() { u.server.Shutdown(t.Context()) })

	return u
}

func lastTSIG(m *dns.Msg) (*dns.TSIG, bool) {
	if len(m.Pseudo) == 0 {
		return nil, false
	}

	rr, ok := m.Pseudo[len(m.Pseudo)-1].(*dns.TSIG)

	return rr, ok
}

// TestRFC2136 proves the update lands in the zone, signed when a key
// is configured, that a deletion takes out only the value it names, and
// that a name outside the zone is refused before anything is sent.
func TestRFC2136(t *testing.T) {
	t.Parallel()

	const (
		secret = "c2VjcmV0c2VjcmV0c2VjcmV0c2VjcmV0c2VjcmV0"
		zone   = "ts.example.com"
		fqdn   = "_acme-challenge.laptop.ts.example.com."
	)

	t.Run("signed", func(t *testing.T) {
		t.Parallel()

		server := newUpdateServer(t, zone, "slopscale", secret)

		provider, err := dnsprovider.NewRFC2136(server.address, "slopscale", secret, "hmac-sha256")
		require.NoError(t, err)

		_, err = provider.AppendRecords(t.Context(), zone, txt("_acme-challenge.laptop", "token"))
		require.NoError(t, err)
		_, err = provider.AppendRecords(t.Context(), zone, txt("_acme-challenge.laptop", "other"))
		require.NoError(t, err)

		server.mu.Lock()
		assert.True(t, server.signed)
		assert.Equal(t, []string{"token", "other"}, server.txts[fqdn])
		server.mu.Unlock()

		_, err = provider.DeleteRecords(t.Context(), zone, txt("_acme-challenge.laptop", "token"))
		require.NoError(t, err)

		server.mu.Lock()
		assert.Equal(t, []string{"other"}, server.txts[fqdn])
		server.mu.Unlock()

		_, err = provider.DeleteRecords(t.Context(), zone, txt("_acme-challenge.laptop", ""))
		require.ErrorIs(t, err, dnsprovider.ErrProviderConfig, "an empty value would delete every record")
	})

	t.Run("unsigned", func(t *testing.T) {
		t.Parallel()

		server := newUpdateServer(t, zone, "", "")

		provider, err := dnsprovider.NewRFC2136(server.address, "", "", "")
		require.NoError(t, err)

		_, err = provider.AppendRecords(t.Context(), zone, txt("_acme-challenge.laptop", "token"))
		require.NoError(t, err)

		server.mu.Lock()
		assert.Equal(t, []string{"token"}, server.txts[fqdn])
		server.mu.Unlock()

		_, err = provider.AppendRecords(t.Context(), zone, txt("_acme-challenge.laptop.elsewhere.net.", "token"))
		require.ErrorIs(t, err, dnsprovider.ErrNameOutsideZone)
	})

	t.Run("wrong key is refused by the server", func(t *testing.T) {
		t.Parallel()

		server := newUpdateServer(t, zone, "slopscale", secret)

		provider, err := dnsprovider.NewRFC2136(server.address, "slopscale",
			"d3JvbmdzZWNyZXR3cm9uZ3NlY3JldHdyb25nc2VjcmV0", "hmac-sha256")
		require.NoError(t, err)

		_, err = provider.AppendRecords(t.Context(), zone, txt("_acme-challenge.laptop", "token"))
		require.Error(t, err)
	})

	_, err := dnsprovider.NewRFC2136("127.0.0.1:53", "k", secret, "hmac-sha9000")
	require.ErrorIs(t, err, dnsprovider.ErrTSIGAlgorithm)

	_, err = dnsprovider.NewRFC2136("127.0.0.1:53", "k", "not base64!", "hmac-sha256")
	require.ErrorIs(t, err, dnsprovider.ErrTSIGSecret)
}
