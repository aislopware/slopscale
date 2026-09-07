package types

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/dnstype"
)

func TestDNSSettingsNormalize(t *testing.T) {
	t.Parallel()

	got := DNSSettings{
		Nameservers:      []string{" 1.1.1.1 ", "", "1.1.1.1", "https://dns.example/dns-query"},
		SplitNameservers: map[string][]string{"Corp.Example.": {"10.0.0.1", " ", "10.0.0.1"}},
		SearchDomains:    []string{"Lab.Example.", "lab.example", ""},
		ExtraRecords:     []tailcfg.DNSRecord{{Name: "Grafana.Corp.", Type: "a", Value: " 10.0.0.5 "}},
	}.Normalize()

	assert.Equal(t, []string{"1.1.1.1", "https://dns.example/dns-query"}, got.Nameservers)
	assert.Equal(t, map[string][]string{"corp.example": {"10.0.0.1"}}, got.SplitNameservers)
	assert.Equal(t, []string{"lab.example"}, got.SearchDomains)
	assert.Equal(t, []tailcfg.DNSRecord{{Name: "grafana.corp", Type: "A", Value: "10.0.0.5"}}, got.ExtraRecords)

	empty := DNSSettings{}.Normalize()
	assert.Empty(t, empty.Nameservers)
	assert.Nil(t, empty.SplitNameservers)
	assert.Nil(t, empty.ExtraRecords)
}

func TestDNSSettingsValidate(t *testing.T) {
	t.Parallel()

	rec := func(name, typ, value string) DNSSettings {
		return DNSSettings{ExtraRecords: []tailcfg.DNSRecord{{Name: name, Type: typ, Value: value}}}
	}
	split := func(domain string, servers ...string) DNSSettings {
		return DNSSettings{SplitNameservers: map[string][]string{domain: servers}}
	}

	tests := []struct {
		name     string
		settings DNSSettings
		wantErr  error
	}{
		{
			name: "everything valid",
			settings: DNSSettings{
				Nameservers: []string{
					"1.1.1.1", "2606:4700:4700::1111", "10.0.0.1:5353", "https://dns.nextdns.io/abc123",
				},
				SplitNameservers: map[string][]string{"corp.example": {"10.0.0.1"}},
				SearchDomains:    []string{"lab.example"},
				ExtraRecords: []tailcfg.DNSRecord{
					{Name: "a.corp", Value: "10.0.0.5"},
					{Name: "a.corp", Type: "A", Value: "10.0.0.5"},
					{Name: "a.corp", Type: "AAAA", Value: "fd00::5"},
				},
			},
		},
		{name: "empty", settings: DNSSettings{}},
		{
			name:     "bad nameserver",
			settings: DNSSettings{Nameservers: []string{"one.one.one.one"}},
			wantErr:  ErrDNSNameserverInvalid,
		},
		{
			name:     "http nameserver",
			settings: DNSSettings{Nameservers: []string{"http://dns.example"}},
			wantErr:  ErrDNSNameserverInvalid,
		},
		{
			name:     "tls nameserver, which the client cannot use",
			settings: DNSSettings{Nameservers: []string{"tls://dns.example"}},
			wantErr:  ErrDNSResolverUnsupported,
		},
		{
			name:     "unknown DoH provider",
			settings: DNSSettings{Nameservers: []string{"https://dns.example/dns-query"}},
			wantErr:  ErrDNSResolverUnsupported,
		},
		{name: "bad split domain", settings: split("not a domain", "1.1.1.1"), wantErr: ErrDNSDomainInvalid},
		{name: "split without servers", settings: split("corp.example"), wantErr: ErrDNSSplitNoNameservers},
		{name: "split bad server", settings: split("corp.example", "x"), wantErr: ErrDNSNameserverInvalid},
		{
			name:     "bad search domain",
			settings: DNSSettings{SearchDomains: []string{"-bad"}},
			wantErr:  ErrDNSDomainInvalid,
		},
		{name: "record without name", settings: rec("", "", "1.1.1.1"), wantErr: ErrDNSRecordNameEmpty},
		{name: "record without value", settings: rec("a.corp", "", ""), wantErr: ErrDNSRecordValueEmpty},
		{name: "untyped record not ip", settings: rec("a.corp", "", "x"), wantErr: ErrDNSRecordValueNotIP},
		{name: "A record with v6", settings: rec("a.corp", "A", "fd00::5"), wantErr: ErrDNSRecordValueNotIPv4},
		{name: "AAAA record with v4", settings: rec("a.corp", "AAAA", "10.0.0.5"), wantErr: ErrDNSRecordValueNotIPv6},
		{name: "unknown record type", settings: rec("a.corp", "MX", "x"), wantErr: ErrDNSRecordTypeInvalid},
		{
			name:     "TXT record, which the client does not serve",
			settings: rec("a.corp", "TXT", "v=spf1 -all"),
			wantErr:  ErrDNSRecordTypeInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.settings.Validate()
			if tt.wantErr == nil {
				assert.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, tt.wantErr)
			assert.ErrorIs(t, err, ErrDNSSettingsInvalid)
		})
	}
}

// TestConfigDNSOverride proves the rebuild: the override replaces the
// file's values, the MagicDNS reverse zones survive it, the extra-records
// file wins over the override's records, and nil returns to the file.
func TestConfigDNSOverride(t *testing.T) {
	// Not parallel: the DNS state is guarded by a package-level lock.
	v4 := netip.MustParsePrefix("100.64.0.0/10")

	cfg := &Config{
		PrefixV4: &v4,
		DNSConfig: DNSConfig{
			MagicDNS:      true,
			BaseDomain:    "ts.example",
			Nameservers:   Nameservers{Global: []string{"1.1.1.1"}},
			SearchDomains: []string{"file.example"},
			ExtraRecords:  []tailcfg.DNSRecord{{Name: "file.ts.example", Value: "100.64.0.9"}},
		},
	}
	cfg.TailcfgDNSConfig = dnsToTailcfgDNS(cfg.DNSConfig)
	cfg.RebuildTailcfgDNS()

	before := cfg.CloneTailcfgDNSConfig()
	require.NotNil(t, before)
	assert.Equal(t, []*dnstype.Resolver{{Addr: "1.1.1.1"}}, before.FallbackResolvers)
	assert.Empty(t, before.Resolvers)
	assert.Equal(t, []string{"ts.example", "file.example"}, before.Domains)
	assert.Contains(t, before.Routes, "64.100.in-addr.arpa")
	assert.Nil(t, cfg.DNSOverride())

	cfg.SetDNSOverride(&DNSSettings{
		Nameservers:      []string{"9.9.9.9"},
		OverrideLocalDNS: true,
		SplitNameservers: map[string][]string{"corp.example": {"10.0.0.1"}},
		SearchDomains:    []string{"api.example"},
		ExtraRecords:     []tailcfg.DNSRecord{{Name: "api.ts.example", Value: "100.64.0.10"}},
	})

	after := cfg.CloneTailcfgDNSConfig()
	assert.True(t, after.Proxied, "MagicDNS stays as the file says")
	assert.Equal(t, []*dnstype.Resolver{{Addr: "9.9.9.9"}}, after.Resolvers)
	assert.Empty(t, after.FallbackResolvers)
	assert.Equal(t, []string{"ts.example", "api.example"}, after.Domains)
	assert.Equal(t, []*dnstype.Resolver{{Addr: "10.0.0.1"}}, after.Routes["corp.example"])
	assert.Contains(t, after.Routes, "64.100.in-addr.arpa", "the MagicDNS zones survive the rebuild")
	assert.Equal(t, []tailcfg.DNSRecord{{Name: "api.ts.example", Value: "100.64.0.10"}}, after.ExtraRecords)
	require.NotNil(t, cfg.DNSOverride())
	assert.Equal(t, []string{"9.9.9.9"}, cfg.EffectiveDNS().Nameservers.Global)

	cfg.SetExtraRecords([]tailcfg.DNSRecord{{Name: "watched.ts.example", Value: "100.64.0.11"}})
	withFile := cfg.CloneTailcfgDNSConfig()
	assert.Equal(t, []tailcfg.DNSRecord{{Name: "watched.ts.example", Value: "100.64.0.11"}}, withFile.ExtraRecords,
		"the extra-records file owns the records")
	assert.Equal(t, []*dnstype.Resolver{{Addr: "9.9.9.9"}}, withFile.Resolvers, "the override stays")

	cfg.SetDNSOverride(nil)
	reset := cfg.CloneTailcfgDNSConfig()
	assert.Equal(t, []*dnstype.Resolver{{Addr: "1.1.1.1"}}, reset.FallbackResolvers)
	assert.Equal(t, []string{"ts.example", "file.example"}, reset.Domains)
	assert.Equal(t, []tailcfg.DNSRecord{{Name: "watched.ts.example", Value: "100.64.0.11"}}, reset.ExtraRecords)
	assert.Nil(t, cfg.DNSOverride())
}
