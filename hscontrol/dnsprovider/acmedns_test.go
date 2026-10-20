package dnsprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/libdns/acmedns"
	"github.com/libdns/hetzner/v2"
	"github.com/libdns/libdns"
	"github.com/libdns/powerdns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noEnv(string) (any, bool) { return nil, false }

// TestNew covers the registry: settings decode through the providers'
// json tags, a secret can come from a file, an environment variable
// reaches a single key, and mistakes fail at startup.
func TestNew(t *testing.T) {
	t.Parallel()

	secret := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(secret, []byte("from-file\n"), 0o600))

	// The storage file acme-dns-client writes, one registration per name.
	registrations := filepath.Join(t.TempDir(), "clientstorage.json")
	require.NoError(t, os.WriteFile(registrations, []byte(`{"node.ts.example.com": {"username": "user", `+
		`"password": "pass", "fulldomain": "reg.auth.acme-dns.io", "subdomain": "reg", `+
		`"server_url": "https://auth.acme-dns.io", "allowfrom": []}}`), 0o600))

	tests := []struct {
		name     string
		provider string
		settings map[string]any
		env      map[string]any
		want     Provider
		wantErr  error
	}{
		{
			name:     "cloudflare from settings",
			provider: "cloudflare",
			settings: map[string]any{"api_token": "tok", "zone_id": "zone-1"},
			want:     &Cloudflare{APIToken: "tok", ZoneID: "zone-1"},
		},
		{
			name:     "name is case insensitive",
			provider: "CloudFlare",
			settings: map[string]any{"api_token": "tok"},
			want:     &Cloudflare{APIToken: "tok"},
		},
		{
			name:     "a key from the environment",
			provider: "cloudflare",
			settings: map[string]any{"api_token": "from-file-config"},
			env:      map[string]any{"api_token": "from-env"},
			want:     &Cloudflare{APIToken: "from-env"},
		},
		{
			name:     "cloudflare without a token",
			provider: "cloudflare",
			settings: map[string]any{"zone_id": "zone-1"},
			wantErr:  ErrProviderConfig,
		},
		{
			name:     "rfc2136 with the keys slopscale always took",
			provider: "rfc2136",
			settings: map[string]any{
				"server":         "ns1.example.com:53",
				"zone":           "example.com",
				"tsig_key_name":  "slopscale",
				"tsig_secret":    "c2VjcmV0",
				"tsig_algorithm": "hmac-sha256",
			},
			want: func() Provider {
				p, err := NewRFC2136("ns1.example.com:53", "slopscale", "c2VjcmV0", "hmac-sha256")
				require.NoError(t, err)

				p.Zone = "example.com"

				return p
			}(),
		},
		{
			name:     "rfc2136 without a server",
			provider: "rfc2136",
			settings: map[string]any{"zone": "example.com"},
			wantErr:  ErrProviderConfig,
		},
		{
			name:     "command",
			provider: "command",
			settings: map[string]any{"path": "/usr/local/bin/publish-txt"},
			want:     &Command{Path: "/usr/local/bin/publish-txt"},
		},
		{
			name:     "secret from path",
			provider: "cloudflare",
			settings: map[string]any{"api_token_path": secret},
			want:     &Cloudflare{APIToken: "from-file"},
		},
		{
			name:     "value and path both set",
			provider: "cloudflare",
			settings: map[string]any{"api_token": "tok", "api_token_path": secret},
			wantErr:  ErrProviderConfig,
		},
		{
			name:     "unreadable path",
			provider: "cloudflare",
			settings: map[string]any{"api_token_path": filepath.Join(t.TempDir(), "missing")},
			wantErr:  ErrProviderConfig,
		},
		{
			name:     "unknown key",
			provider: "cloudflare",
			settings: map[string]any{"auth_token": "tok"},
			wantErr:  ErrProviderConfig,
		},
		{
			name:     "unknown provider",
			provider: "route53",
			wantErr:  ErrUnknownProvider,
		},
		{
			name:     "hetzner",
			provider: "hetzner",
			settings: map[string]any{"api_token_path": secret},
			want:     &hetzner.Provider{APIToken: "from-file"},
		},
		{
			name:     "powerdns",
			provider: "powerdns",
			settings: map[string]any{"server_url": "https://pdns.example.com", "api_token": "tok"},
			want:     &powerdns.Provider{ServerURL: "https://pdns.example.com", APIToken: "tok"},
		},
		{
			name:     "acmedns",
			provider: "acmedns",
			settings: map[string]any{
				"username":      "user",
				"password_path": secret,
				"subdomain":     "reg",
				"server_url":    "https://auth.acme-dns.io",
			},
			want: &acmeDNS{
				Username:  "user",
				Password:  "from-file",
				Subdomain: "reg",
				ServerURL: "https://auth.acme-dns.io",
				served:    map[string][2]acmeDNSValue{},
			},
		},
		{
			name:     "acmedns per domain",
			provider: "acmedns",
			settings: map[string]any{"config": map[string]any{
				"node.ts.example.com": map[string]any{
					"username":   "user",
					"password":   "pass",
					"subdomain":  "reg",
					"server_url": "https://auth.acme-dns.io",
				},
			}},
			want: &acmeDNS{
				Configs: map[string]acmedns.DomainConfig{
					"node.ts.example.com": {
						Username:  "user",
						Password:  "pass",
						Subdomain: "reg",
						ServerURL: "https://auth.acme-dns.io",
					},
				},
				served: map[string][2]acmeDNSValue{},
			},
		},
		{
			name:     "acmedns per domain as the configuration store splits it",
			provider: "acmedns",
			settings: map[string]any{"config": map[string]any{
				"node": map[string]any{"ts": map[string]any{"example": map[string]any{"com": map[string]any{
					"username":   "user",
					"password":   "pass",
					"subdomain":  "reg",
					"server_url": "https://auth.acme-dns.io",
				}}}},
			}},
			want: &acmeDNS{
				Configs: map[string]acmedns.DomainConfig{
					"node.ts.example.com": {
						Username:  "user",
						Password:  "pass",
						Subdomain: "reg",
						ServerURL: "https://auth.acme-dns.io",
					},
				},
				served: map[string][2]acmeDNSValue{},
			},
		},
		{
			name:     "acmedns per domain from an acme-dns-client file",
			provider: "acmedns",
			settings: map[string]any{"config_path": registrations},
			want: &acmeDNS{
				Configs: map[string]acmedns.DomainConfig{
					"node.ts.example.com": {
						Username:   "user",
						Password:   "pass",
						Subdomain:  "reg",
						FullDomain: "reg.auth.acme-dns.io",
						ServerURL:  "https://auth.acme-dns.io",
					},
				},
				served: map[string][2]acmeDNSValue{},
			},
		},
		{
			name:     "acmedns per domain from the environment",
			provider: "acmedns",
			env: map[string]any{"config": `{"node.ts.example.com": {"username": "user", "password": "pass", ` +
				`"subdomain": "reg", "server_url": "https://auth.acme-dns.io"}}`},
			want: &acmeDNS{
				Configs: map[string]acmedns.DomainConfig{
					"node.ts.example.com": {
						Username:  "user",
						Password:  "pass",
						Subdomain: "reg",
						ServerURL: "https://auth.acme-dns.io",
					},
				},
				served: map[string][2]acmeDNSValue{},
			},
		},
		{
			name:     "acmedns server_url without scheme",
			provider: "acmedns",
			settings: map[string]any{
				"username":   "user",
				"password":   "pass",
				"subdomain":  "reg",
				"server_url": "auth.acme-dns.io",
			},
			wantErr: ErrProviderConfig,
		},
		{
			name:     "acmedns without subdomain",
			provider: "acmedns",
			settings: map[string]any{
				"username":   "user",
				"password":   "pass",
				"server_url": "https://auth.acme-dns.io",
			},
			wantErr: ErrProviderConfig,
		},
		{
			name:     "no settings",
			provider: "hetzner",
			wantErr:  ErrProviderConfig,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			get := func(key string) (any, bool) {
				v, ok := tt.env[key]

				return v, ok
			}

			got, err := New(tt.provider, tt.settings, get)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestAcmeDNSIsolation pins that node certificates need a registration
// per name: one shared record would let a node answer another's
// challenge.
func TestAcmeDNSIsolation(t *testing.T) {
	t.Parallel()

	one := newAcmeDNS()
	one.Username, one.Password, one.Subdomain, one.ServerURL = "u", "p", "reg", "https://auth.acme-dns.io"
	require.ErrorIs(t, one.CheckIsolation(), errAcmeDNSOneAccount)

	shared := newAcmeDNS()
	shared.Configs = map[string]acmedns.DomainConfig{
		"a.ts.example.com": {Subdomain: "reg"},
		"b.ts.example.com": {Subdomain: "reg"},
	}
	require.ErrorIs(t, shared.CheckIsolation(), errAcmeDNSShared)

	own := newAcmeDNS()
	own.Configs = map[string]acmedns.DomainConfig{
		"a.ts.example.com": {Subdomain: "reg-a"},
		"b.ts.example.com": {Subdomain: "reg-b"},
	}
	require.NoError(t, own.CheckIsolation())
	assert.True(t, own.Isolates("a.ts.example.com"))
	assert.False(t, own.Isolates("c.ts.example.com"))
}

// fakeAcmeDNS is an acme-dns server with one registration, reg. It has two
// rows, and an update overwrites the one stamped with the earlier second.
// acme-dns overwrites either on a tie; the fake picks the one written
// later, which a writer assuming first in, first out gets wrong.
type fakeAcmeDNS struct {
	*httptest.Server

	mu     sync.Mutex
	writes int
	rows   [2]fakeAcmeDNSRow
}

type fakeAcmeDNSRow struct {
	txt   string
	stamp int64 // acme-dns's LastUpdate, in whole seconds
	write int   // the order of writes, which acme-dns does not keep
}

func newFakeAcmeDNS(t *testing.T) *fakeAcmeDNS {
	t.Helper()

	f := &fakeAcmeDNS{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var update struct {
			Subdomain string `json:"subdomain"`
			Txt       string `json:"txt"`
		}

		err := json.NewDecoder(r.Body).Decode(&update)
		if r.URL.Path != "/update" || err != nil || update.Subdomain != "reg" || r.Header.Get("X-Api-User") != "user" {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		f.mu.Lock()
		defer f.mu.Unlock()

		i := 0
		if f.rows[1].stamp < f.rows[0].stamp ||
			(f.rows[1].stamp == f.rows[0].stamp && f.rows[1].write > f.rows[0].write) {
			i = 1
		}

		f.writes++
		f.rows[i] = fakeAcmeDNSRow{txt: update.Txt, stamp: time.Now().Unix(), write: f.writes}
	}))
	t.Cleanup(f.Close)

	return f
}

func (f *fakeAcmeDNS) registration() map[string]any {
	return map[string]any{
		"username":   "user",
		"password":   "pass",
		"subdomain":  "reg",
		"server_url": f.URL,
	}
}

func (f *fakeAcmeDNS) serving() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	var txts []string

	for _, row := range f.rows {
		if row.txt != "" {
			txts = append(txts, row.txt)
		}
	}

	return txts
}

func acmeTXT(node, value string) []libdns.Record {
	return []libdns.Record{libdns.TXT{Name: "_acme-challenge." + node, Text: value}}
}

// A registration serves the two values written last, and deleting is a
// no-op: an update overwrites the older value even if a CA may still be
// validating it, however many values were deleted. Two updates within one
// second carry the same stamp, leaving acme-dns free to overwrite either:
// after a then b, with a freed, c could overwrite b.
func TestAcmeDNSRefusesEviction(t *testing.T) {
	t.Parallel()

	srv := newFakeAcmeDNS(t)

	p, err := New("acmedns", srv.registration(), noEnv)
	require.NoError(t, err)

	ctx := t.Context()

	a, err := p.AppendRecords(ctx, testZone, acmeTXT("a", "value-a"))
	require.NoError(t, err)
	require.Len(t, a, 1, "certmagic needs the appended record back")

	b, err := p.AppendRecords(ctx, testZone, acmeTXT("b", "value-b"))
	require.NoError(t, err)

	_, err = p.AppendRecords(ctx, testZone, acmeTXT("c", "value-c"))
	require.ErrorIs(t, err, ErrTooManyChallenges)

	// The newer value is free, but an update overwrites the older one.
	_, err = p.DeleteRecords(ctx, testZone, b)
	require.NoError(t, err)

	_, err = p.AppendRecords(ctx, testZone, acmeTXT("c", "value-c"))
	require.ErrorIs(t, err, ErrTooManyChallenges)
	assert.ElementsMatch(t, []string{"value-a", "value-b"}, srv.serving(), "a refused value reached acme-dns")

	_, err = p.DeleteRecords(ctx, testZone, a)
	require.NoError(t, err)

	c, err := p.AppendRecords(ctx, testZone, acmeTXT("c", "value-c"))
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"value-b", "value-c"}, srv.serving())

	// b is free and now the older value; overwriting it leaves c alone.
	_, err = p.AppendRecords(ctx, testZone, acmeTXT("d", "value-d"))
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"value-c", "value-d"}, srv.serving())

	// Giving up while waiting to space updates writes nothing.
	_, err = p.DeleteRecords(ctx, testZone, c)
	require.NoError(t, err)

	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	_, err = p.AppendRecords(cancelled, testZone, acmeTXT("e", "value-e"))
	require.ErrorIs(t, err, context.Canceled)
	assert.ElementsMatch(t, []string{"value-c", "value-d"}, srv.serving())
}
