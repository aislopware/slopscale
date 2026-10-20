package types

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/libdns/libdns"
	"github.com/stretchr/testify/assert"
	"tailscale.com/tailcfg"
)

// isolatingProvider isolates only the names it lists, as acme-dns does
// for the names it has a registration for.
type isolatingProvider struct{ names []string }

func (isolatingProvider) AppendRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error) {
	return nil, nil
}

func (isolatingProvider) DeleteRecords(context.Context, string, []libdns.Record) ([]libdns.Record, error) {
	return nil, nil
}

func (p isolatingProvider) Isolates(name string) bool { return slices.Contains(p.names, name) }

func (isolatingProvider) CheckIsolation() error { return nil }

// TestCertDomains covers which names a node may get a certificate for,
// and that only their challenge records may be published.
func TestCertDomains(t *testing.T) {
	t.Parallel()

	certsOn := func(records ...tailcfg.DNSRecord) *Config {
		return &Config{
			BaseDomain:       "ts.example.com",
			HTTPSCerts:       HTTPSCertsConfig{Enabled: true},
			TailcfgDNSConfig: &tailcfg.DNSConfig{ExtraRecords: records},
		}
	}
	node := func(name string) NodeView { return (&Node{GivenName: name}).View() }

	tests := []struct {
		name string
		cfg  *Config
		node NodeView
		want []string
	}{
		{name: "off", cfg: &Config{BaseDomain: "ts.example.com"}, node: node("laptop")},
		{
			name: "no base domain",
			cfg:  &Config{HTTPSCerts: HTTPSCertsConfig{Enabled: true}},
			node: node("laptop"),
		},
		{name: "magicdns name", cfg: certsOn(), node: node("laptop"), want: []string{"laptop.ts.example.com"}},
		{
			// A name from before renames were lowercased can differ from
			// another node's by case only.
			name: "uppercase given name",
			cfg:  certsOn(),
			node: node("Laptop"),
		},
		{
			name: "extra record at the name",
			cfg:  certsOn(tailcfg.DNSRecord{Name: "laptop.ts.example.com.", Type: "A", Value: "192.0.2.1"}),
			node: node("laptop"),
		},
		{
			// A wildcard order shares the bare name's challenge record.
			name: "extra record below the name",
			cfg:  certsOn(tailcfg.DNSRecord{Name: "api.LAPTOP.ts.example.com", Type: "A", Value: "192.0.2.1"}),
			node: node("laptop"),
		},
		{
			name: "extra record elsewhere",
			cfg:  certsOn(tailcfg.DNSRecord{Name: "laptop2.ts.example.com", Type: "A", Value: "192.0.2.1"}),
			node: node("laptop"),
			want: []string{"laptop.ts.example.com"},
		},
		{
			// A 241-character name is valid, its challenge record is not.
			name: "challenge name too long",
			cfg: &Config{
				BaseDomain: strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." +
					strings.Repeat("d", 45) + ".com",
				HTTPSCerts: HTTPSCertsConfig{Enabled: true},
			},
			node: node(strings.Repeat("a", 63)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.node.CertDomains(tt.cfg))
		})
	}

	t.Run("isolating provider", func(t *testing.T) {
		t.Parallel()

		cfg := certsOn()
		cfg.HTTPSCerts.Provider = isolatingProvider{names: []string{"laptop.ts.example.com"}}

		assert.Equal(t, []string{"laptop.ts.example.com"}, node("laptop").CertDomains(cfg))
		assert.Empty(t, node("desktop").CertDomains(cfg), "a name without its own registration")
	})

	t.Run("challenge name", func(t *testing.T) {
		t.Parallel()

		cfg := certsOn()
		laptop := node("laptop")

		assert.True(t, laptop.ACMEChallengeAllowed(cfg, "_acme-challenge.laptop.ts.example.com"))
		assert.True(t, laptop.ACMEChallengeAllowed(cfg, "_acme-challenge.laptop.ts.example.com."))
		assert.False(t, laptop.ACMEChallengeAllowed(cfg, "_acme-challenge.LAPTOP.ts.example.com"),
			"the name matches exactly")
		assert.False(t, laptop.ACMEChallengeAllowed(cfg, "laptop.ts.example.com"))
		assert.False(t, laptop.ACMEChallengeAllowed(cfg, "_acme-challenge.desktop.ts.example.com"))
	})
}
