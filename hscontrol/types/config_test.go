package types

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/conf"
	"github.com/aislopware/slopscale/hscontrol/dnsprovider"
	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/dnstype"
)

func TestReadConfig(t *testing.T) {
	tests := []struct {
		name       string
		configPath string
		setup      func(*testing.T) (any, error)
		want       any
		wantErr    string
	}{
		{
			name:       "unmarshal-dns-full-config",
			configPath: "testdata/dns_full.yaml",
			setup: func(_ *testing.T) (any, error) {
				dns, err := dns()
				if err != nil {
					return nil, err
				}

				return dns, nil
			},
			want: DNSConfig{
				MagicDNS:         true,
				BaseDomain:       "example.com",
				OverrideLocalDNS: false,
				Nameservers: Nameservers{
					Global: []string{
						"1.1.1.1",
						"1.0.0.1",
						"2606:4700:4700::1111",
						"2606:4700:4700::1001",
						"https://dns.nextdns.io/abc123",
					},
					Split: map[string][]string{
						"darp.slopscale.net": {"1.1.1.1", "8.8.8.8"},
						"foo.bar.com":        {"1.1.1.1"},
					},
				},
				ExtraRecords: []tailcfg.DNSRecord{
					{Name: "grafana.myvpn.example.com", Type: "A", Value: "100.64.0.3"},
					{Name: "prometheus.myvpn.example.com", Type: "A", Value: "100.64.0.4"},
				},
				SearchDomains: []string{"test.com", "bar.com"},
			},
		},
		{
			name:       "dns-to-tailcfg.DNSConfig",
			configPath: "testdata/dns_full.yaml",
			setup: func(_ *testing.T) (any, error) {
				dns, err := dns()
				if err != nil {
					return nil, err
				}

				return dnsToTailcfgDNS(dns), nil
			},
			want: &tailcfg.DNSConfig{
				Proxied: true,
				Domains: []string{"example.com", "test.com", "bar.com"},
				FallbackResolvers: []*dnstype.Resolver{
					{Addr: "1.1.1.1"},
					{Addr: "1.0.0.1"},
					{Addr: "2606:4700:4700::1111"},
					{Addr: "2606:4700:4700::1001"},
					{Addr: "https://dns.nextdns.io/abc123"},
				},
				Routes: map[string][]*dnstype.Resolver{
					"darp.slopscale.net": {{Addr: "1.1.1.1"}, {Addr: "8.8.8.8"}},
					"foo.bar.com":        {{Addr: "1.1.1.1"}},
				},
				ExtraRecords: []tailcfg.DNSRecord{
					{Name: "grafana.myvpn.example.com", Type: "A", Value: "100.64.0.3"},
					{Name: "prometheus.myvpn.example.com", Type: "A", Value: "100.64.0.4"},
				},
			},
		},
		{
			name:       "unmarshal-dns-full-no-magic",
			configPath: "testdata/dns_full_no_magic.yaml",
			setup: func(_ *testing.T) (any, error) {
				dns, err := dns()
				if err != nil {
					return nil, err
				}

				return dns, nil
			},
			want: DNSConfig{
				MagicDNS:         false,
				BaseDomain:       "example.com",
				OverrideLocalDNS: false,
				Nameservers: Nameservers{
					Global: []string{
						"1.1.1.1",
						"1.0.0.1",
						"2606:4700:4700::1111",
						"2606:4700:4700::1001",
						"https://dns.nextdns.io/abc123",
					},
					Split: map[string][]string{
						"darp.slopscale.net": {"1.1.1.1", "8.8.8.8"},
						"foo.bar.com":        {"1.1.1.1"},
					},
				},
				ExtraRecords: []tailcfg.DNSRecord{
					{Name: "grafana.myvpn.example.com", Type: "A", Value: "100.64.0.3"},
					{Name: "prometheus.myvpn.example.com", Type: "A", Value: "100.64.0.4"},
				},
				SearchDomains: []string{"test.com", "bar.com"},
			},
		},
		{
			name:       "dns-to-tailcfg.DNSConfig",
			configPath: "testdata/dns_full_no_magic.yaml",
			setup: func(_ *testing.T) (any, error) {
				dns, err := dns()
				if err != nil {
					return nil, err
				}

				return dnsToTailcfgDNS(dns), nil
			},
			want: &tailcfg.DNSConfig{
				Proxied: false,
				Domains: []string{"example.com", "test.com", "bar.com"},
				FallbackResolvers: []*dnstype.Resolver{
					{Addr: "1.1.1.1"},
					{Addr: "1.0.0.1"},
					{Addr: "2606:4700:4700::1111"},
					{Addr: "2606:4700:4700::1001"},
					{Addr: "https://dns.nextdns.io/abc123"},
				},
				Routes: map[string][]*dnstype.Resolver{
					"darp.slopscale.net": {{Addr: "1.1.1.1"}, {Addr: "8.8.8.8"}},
					"foo.bar.com":        {{Addr: "1.1.1.1"}},
				},
				ExtraRecords: []tailcfg.DNSRecord{
					{Name: "grafana.myvpn.example.com", Type: "A", Value: "100.64.0.3"},
					{Name: "prometheus.myvpn.example.com", Type: "A", Value: "100.64.0.4"},
				},
			},
		},
		{
			name:       "base-domain-in-server-url-err",
			configPath: "testdata/base-domain-in-server-url.yaml",
			setup: func(_ *testing.T) (any, error) {
				return LoadServerConfig()
			},
			want:    nil,
			wantErr: "Fatal config error: server_url is a subdomain of dns.base_domain",
		},
		{
			name:       "base-domain-not-in-server-url",
			configPath: "testdata/base-domain-not-in-server-url.yaml",
			setup: func(_ *testing.T) (any, error) {
				cfg, err := LoadServerConfig()
				if err != nil {
					return nil, err
				}

				return map[string]string{
					"server_url":  cfg.ServerURL,
					"base_domain": cfg.BaseDomain,
				}, err
			},
			want: map[string]string{
				"server_url":  "https://derp.no",
				"base_domain": "clients.derp.no",
			},
			wantErr: "",
		},
		{
			name:       "dns-override-true-errors",
			configPath: "testdata/dns-override-true-error.yaml",
			setup: func(_ *testing.T) (any, error) {
				return LoadServerConfig()
			},
			wantErr: "Fatal config error: dns.nameservers.global is required when dns.override_local_dns is true",
		},
		{
			name:       "dns-override-true",
			configPath: "testdata/dns-override-true.yaml",
			setup: func(_ *testing.T) (any, error) {
				_, err := LoadServerConfig()
				if err != nil {
					return nil, err
				}

				dns, err := dns()
				if err != nil {
					return nil, err
				}

				return dnsToTailcfgDNS(dns), nil
			},
			want: &tailcfg.DNSConfig{
				Proxied: true,
				Domains: []string{"derp2.no"},
				Routes:  map[string][]*dnstype.Resolver{},
				Resolvers: []*dnstype.Resolver{
					{Addr: "1.1.1.1"},
					{Addr: "1.0.0.1"},
				},
			},
		},
		{
			name:       "dns-use-with-exit-node",
			configPath: "testdata/dns_use_with_exit_node.yaml",
			setup: func(_ *testing.T) (any, error) {
				_, err := LoadServerConfig()
				if err != nil {
					return nil, err
				}

				dns, err := dns()
				if err != nil {
					return nil, err
				}

				return dnsToTailcfgDNS(dns), nil
			},
			want: &tailcfg.DNSConfig{
				Proxied: true,
				Domains: []string{"example.com"},
				Resolvers: []*dnstype.Resolver{
					{Addr: "1.1.1.1", UseWithExitNode: true},
					{Addr: "8.8.8.8"},
				},
				Routes: map[string][]*dnstype.Resolver{
					"corp.example": {
						{Addr: "10.0.0.1", UseWithExitNode: true},
						{Addr: "10.0.0.2", UseWithExitNode: true},
					},
					"lab.example": {{Addr: "10.0.0.3"}},
				},
			},
		},
		{
			name:       "dns-use-with-exit-node-needs-override",
			configPath: "testdata/dns_use_with_exit_node_no_override.yaml",
			setup: func(_ *testing.T) (any, error) {
				return LoadServerConfig()
			},
			wantErr: "Fatal config error: " + ErrDNSUseWithExitNodeNeedsOverride.Error(),
		},
		{
			name:       "policy-path-is-loaded",
			configPath: "testdata/policy-path-is-loaded.yaml",
			setup: func(_ *testing.T) (any, error) {
				cfg, err := LoadServerConfig()
				if err != nil {
					return nil, err
				}

				return map[string]string{
					"policy.mode": string(cfg.Policy.Mode),
					"policy.path": cfg.Policy.Path,
				}, err
			},
			want: map[string]string{
				"policy.mode": "file",
				"policy.path": "/etc/policy.hujson",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf.Reset()

			err := LoadConfig(tt.configPath, true)
			require.NoError(t, err)

			got, err := tt.setup(t)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)

				return
			}

			require.NoError(t, err)

			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ReadConfig() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestReadConfigFromEnv(t *testing.T) {
	tests := []struct {
		name      string
		configEnv map[string]string
		setup     func(*testing.T) (any, error)
		want      any
	}{
		{
			name: "test-random-base-settings-with-env",
			configEnv: map[string]string{
				"SLOPSCALE_LOG_LEVEL":                       "trace",
				"SLOPSCALE_DATABASE_SQLITE_WRITE_AHEAD_LOG": "false",
				"SLOPSCALE_PREFIXES_V4":                     "100.64.0.0/10",
			},
			setup: func(t *testing.T) (any, error) {
				t.Logf("all settings: %#v", conf.AllSettings())

				assert.Equal(t, "trace", conf.GetString("log.level"))
				assert.Equal(t, "100.64.0.0/10", conf.GetString("prefixes.v4"))
				assert.False(t, conf.GetBool("database.sqlite.write_ahead_log"))

				return nil, nil //nolint:nilnil // test setup returns nil to indicate no expected value
			},
			want: nil,
		},
		{
			name: "unmarshal-dns-full-config",
			configEnv: map[string]string{
				"SLOPSCALE_DNS_MAGIC_DNS":          "true",
				"SLOPSCALE_DNS_BASE_DOMAIN":        "example.com",
				"SLOPSCALE_DNS_OVERRIDE_LOCAL_DNS": "false",
				"SLOPSCALE_DNS_NAMESERVERS_GLOBAL": `1.1.1.1 8.8.8.8`,
				"SLOPSCALE_DNS_SEARCH_DOMAINS":     "test.com bar.com",

				// TODO(kradalby): Figure out how to pass these as env vars
				// "SLOPSCALE_DNS_NAMESERVERS_SPLIT":  `{foo.bar.com: ["1.1.1.1"]}`,
				// "SLOPSCALE_DNS_EXTRA_RECORDS":
				//   `[{ name: "prometheus.myvpn.example.com", type: "A", value: "100.64.0.4" }]`,
			},
			setup: func(t *testing.T) (any, error) {
				t.Logf("all settings: %#v", conf.AllSettings())

				dns, err := dns()
				if err != nil {
					return nil, err
				}

				return dns, nil
			},
			want: DNSConfig{
				MagicDNS:         true,
				BaseDomain:       "example.com",
				OverrideLocalDNS: false,
				Nameservers: Nameservers{
					Global: []string{"1.1.1.1", "8.8.8.8"},
					Split:  map[string][]string{
						// "foo.bar.com": {"1.1.1.1"},
					},
				},
				// ExtraRecords: []tailcfg.DNSRecord{
				// 	{Name: "prometheus.myvpn.example.com", Type: "A", Value: "100.64.0.4"},
				// },
				SearchDomains: []string{"test.com", "bar.com"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.configEnv {
				t.Setenv(k, v)
			}

			conf.Reset()

			err := LoadConfig("testdata/minimal.yaml", true)
			require.NoError(t, err)

			got, err := tt.setup(t)
			require.NoError(t, err)

			if diff := cmp.Diff(tt.want, got, cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("ReadConfig() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestTLSConfigValidation(t *testing.T) {
	tmpDir := t.TempDir()

	var err error

	configYaml := []byte(`---
tls_letsencrypt_hostname: example.com
tls_letsencrypt_challenge_type: dns-01
tls_cert_path: abc.pem
noise:
  private_key_path: noise_private.key`)

	// Populate a custom config file
	configFilePath := filepath.Join(tmpDir, "config.yaml")

	err = os.WriteFile(configFilePath, configYaml, 0o600)
	if err != nil {
		t.Fatalf("Couldn't write file %s", configFilePath)
	}

	// Check configuration validation errors (1)
	err = LoadConfig(tmpDir, false)
	require.NoError(t, err)

	err = validateServerConfig()
	require.Error(t, err)
	assert.Contains(
		t,
		err.Error(),
		"Fatal config error: tls.acme.hostname and tls.cert_path/tls.key_path are mutually exclusive",
	)
	assert.Contains(
		t,
		err.Error(),
		"Fatal config error: tls.acme.challenge_type has an unsupported value",
	)
	assert.Contains(
		t,
		err.Error(),
		"Fatal config error: server_url is missing a scheme",
	)

	// Check configuration validation errors (2)
	configYaml = []byte(`---
noise:
  private_key_path: noise_private.key
server_url: http://127.0.0.1:8080
tls_letsencrypt_hostname: example.com
tls_letsencrypt_challenge_type: TLS-ALPN-01
`)

	err = os.WriteFile(configFilePath, configYaml, 0o600)
	if err != nil {
		t.Fatalf("Couldn't write file %s", configFilePath)
	}

	err = LoadConfig(tmpDir, false)
	require.NoError(t, err)
}

// TestTLSConfigKeys pins how the tls block reads: its own keys first,
// then the flat keys it replaced, then the defaults, so a config written
// for an older release keeps its certificate.
func TestTLSConfigKeys(t *testing.T) {
	load := func(t *testing.T, yaml string) TLSConfig {
		t.Helper()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o600))
		require.NoError(t, LoadConfig(dir, false))

		return tlsConfig()
	}

	t.Run("flat keys", func(t *testing.T) {
		got := load(t, `---
acme_url: https://acme.example.com/directory
acme_email: ops@example.com
tls_letsencrypt_hostname: hs.example.com
tls_letsencrypt_challenge_type: TLS-ALPN-01
tls_letsencrypt_listen: ":8081"
tls_letsencrypt_cache_dir: /var/lib/slopscale/cache
`)
		assert.Equal(t, ACMEConfig{
			Hostname:      "hs.example.com",
			ChallengeType: TLSALPN01ChallengeType,
			HTTPListen:    ":8081",
			URL:           "https://acme.example.com/directory",
			Email:         "ops@example.com",
			CacheDir:      "/var/lib/slopscale/cache",
		}, got.ACME)
	})

	t.Run("tls block wins over flat keys", func(t *testing.T) {
		got := load(t, `---
tls_letsencrypt_hostname: old.example.com
tls:
  acme:
    hostname: hs.example.com
    challenge_type: DNS-01
`)
		assert.Equal(t, "hs.example.com", got.ACME.Hostname)
		assert.Equal(t, DNS01ChallengeType, got.ACME.ChallengeType)
		assert.Equal(t, DefaultACMEListenAddr, got.ACME.HTTPListen)
		assert.Equal(t, "/var/www/.cache", got.ACME.CacheDir)
	})

	t.Run("nulls fall back to flat keys", func(t *testing.T) {
		// The NixOS module writes its unset tls options as null.
		got := load(t, `---
tls_letsencrypt_hostname: hs.example.com
tls_letsencrypt_challenge_type: TLS-ALPN-01
tls_cert_path: ""
tls:
  cert_path: null
  key_path: null
  acme:
    hostname: ""
    challenge_type: null
    http_listen: null
    cache_dir: /var/lib/slopscale/.cache
`)
		assert.Equal(t, "hs.example.com", got.ACME.Hostname)
		assert.Equal(t, TLSALPN01ChallengeType, got.ACME.ChallengeType)
		assert.Equal(t, DefaultACMEListenAddr, got.ACME.HTTPListen)
		assert.Equal(t, "/var/lib/slopscale/.cache", got.ACME.CacheDir)
		assert.Empty(t, got.CertPath)
	})

	t.Run("static key pair", func(t *testing.T) {
		got := load(t, `---
tls:
  cert_path: /etc/ssl/hs.crt
  key_path: /etc/ssl/hs.key
`)
		assert.Equal(t, "/etc/ssl/hs.crt", got.CertPath)
		assert.Equal(t, "/etc/ssl/hs.key", got.KeyPath)
		assert.Empty(t, got.ACME.Hostname)
	})

	t.Run("environment", func(t *testing.T) {
		t.Setenv("SLOPSCALE_TLS_ACME_HOSTNAME", "env.example.com")

		got := load(t, "---\ntls_letsencrypt_hostname: old.example.com\n")
		assert.Equal(t, "env.example.com", got.ACME.Hostname)
	})
}

// TestHTTPSCertsConfig covers the provider settings: the existing keys
// keep working, a provider is needed by DNS-01 for the server's own
// certificate even without machine certificates, and the zone must hold
// the base domain.
func TestHTTPSCertsConfig(t *testing.T) {
	load := func(t *testing.T, yaml string) (HTTPSCertsConfig, error) {
		t.Helper()

		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o600))
		require.NoError(t, LoadConfig(dir, false))

		return httpsCertsConfig()
	}

	t.Run("off needs nothing", func(t *testing.T) {
		got, err := load(t, "---\nhttps_certificates:\n  provider: cloudflare\n")
		require.NoError(t, err)
		assert.Nil(t, got.Provider)
	})

	t.Run("rfc2136 with its zone", func(t *testing.T) {
		got, err := load(t, `---
dns:
  base_domain: ts.example.com
https_certificates:
  enabled: true
  provider: rfc2136
  rfc2136:
    server: 127.0.0.1:53
    zone: example.com
    tsig_key_name: slopscale
    tsig_secret: c2VjcmV0
`)
		require.NoError(t, err)
		require.IsType(t, &dnsprovider.RFC2136{}, got.Provider)
		assert.Equal(t, "example.com", got.Zone)
		assert.Equal(t, []string{"1.1.1.1:53", "8.8.8.8:53"}, got.Resolvers)
	})

	t.Run("zone defaults to the base domain", func(t *testing.T) {
		t.Setenv("SLOPSCALE_HTTPS_CERTIFICATES_CLOUDFLARE_API_TOKEN", "from-env")

		got, err := load(t, `---
dns:
  base_domain: ts.example.com
https_certificates:
  enabled: true
  provider: cloudflare
  resolvers: [9.9.9.9, "[2620:fe::fe]:53"]
`)
		require.NoError(t, err)
		assert.Equal(t, &dnsprovider.Cloudflare{APIToken: "from-env"}, got.Provider)
		assert.Equal(t, "ts.example.com", got.Zone)
		assert.Equal(t, []string{"9.9.9.9:53", "[2620:fe::fe]:53"}, got.Resolvers)
	})

	t.Run("base domain outside the zone", func(t *testing.T) {
		_, err := load(t, `---
dns:
  base_domain: ts.example.com
https_certificates:
  enabled: true
  zone: example.net
  provider: command
  command:
    path: /bin/true
`)
		require.ErrorIs(t, err, errHTTPSCertsZone)
	})

	t.Run("uppercase base domain", func(t *testing.T) {
		_, err := load(t, `---
dns:
  base_domain: TS.example.com
https_certificates:
  enabled: true
  provider: command
  command:
    path: /bin/true
`)
		require.ErrorIs(t, err, errHTTPSCertsBaseDomainName)
	})

	t.Run("server DNS-01 needs a provider", func(t *testing.T) {
		_, err := load(t, `---
tls:
  acme:
    hostname: hs.example.com
    challenge_type: DNS-01
`)
		require.ErrorIs(t, err, errHTTPSCertsProvider)
	})

	t.Run("server DNS-01 without machine certificates", func(t *testing.T) {
		got, err := load(t, `---
tls:
  acme:
    hostname: hs.example.com
    challenge_type: DNS-01
https_certificates:
  provider: command
  command:
    path: /bin/true
`)
		require.NoError(t, err)
		assert.Equal(t, &dnsprovider.Command{Path: "/bin/true"}, got.Provider)
		assert.Empty(t, got.Zone)
	})

	t.Run("acme-dns needs a registration per name for machines", func(t *testing.T) {
		_, err := load(t, `---
dns:
  base_domain: ts.example.com
https_certificates:
  enabled: true
  provider: acmedns
  acmedns:
    username: u
    password: p
    subdomain: reg
    server_url: https://auth.acme-dns.io
`)
		require.ErrorIs(t, err, dnsprovider.ErrProviderConfig)
		require.ErrorContains(t, err, "registration per name")
	})
}

func TestPKCEMethodValidation(t *testing.T) {
	tmpDir := t.TempDir()

	// OIDC is active (issuer set) with PKCE enabled and an invalid method.
	// There is no oidc.enabled key. validateServerConfig must reject the
	// invalid method rather than silently skipping the check.
	configYaml := []byte(`---
noise:
  private_key_path: noise_private.key
server_url: http://127.0.0.1:8080
dns:
  override_local_dns: false
oidc:
  issuer: https://idp.example.com
  client_id: slopscale
  pkce:
    enabled: true
    method: S256-typo
`)

	configFilePath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configFilePath, configYaml, 0o600)
	require.NoError(t, err)

	err = LoadConfig(tmpDir, false)
	require.NoError(t, err)

	err = validateServerConfig()
	require.Error(t, err)
	assert.Contains(t, err.Error(), errInvalidPKCEMethod.Error())
}

// TestOIDCConfigValidation covers the issuer-URL and required-field checks that
// fail an unworkable OIDC setup fast at config load.
func TestOIDCConfigValidation(t *testing.T) {
	tests := []struct {
		name      string
		oidcBlock string
		wantErr   string
	}{
		{
			name: "non-http issuer",
			oidcBlock: `
  issuer: ftp://idp.example.com
  client_id: slopscale
  client_secret: sekret`,
			wantErr: "valid http(s) URL",
		},
		{
			name: "missing client_id",
			oidcBlock: `
  issuer: https://idp.example.com
  client_secret: sekret`,
			wantErr: "client_id is required",
		},
		{
			name: "missing client_secret",
			oidcBlock: `
  issuer: https://idp.example.com
  client_id: slopscale`,
			wantErr: "client_secret",
		},
		{
			name: "valid",
			oidcBlock: `
  issuer: https://idp.example.com
  client_id: slopscale
  client_secret: sekret`,
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configYaml := []byte(`---
noise:
  private_key_path: noise_private.key
server_url: http://127.0.0.1:8080
database:
  type: sqlite
dns:
  magic_dns: false
  override_local_dns: false
oidc:` + tt.oidcBlock + "\n")

			require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "config.yaml"), configYaml, 0o600))
			require.NoError(t, LoadConfig(tmpDir, false))

			err := validateServerConfig()
			if tt.wantErr == "" {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// The unset case is the regression: the default is spelled "0o770", which a
// base-8 parse rejects, so the socket silently became 0700.
// https://github.com/juanfont/headscale/issues/3529
func TestUnixSocketPermission(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		env     string
		want    fs.FileMode
		wantErr bool
	}{
		{name: "unset-uses-default", want: 0o770},
		{name: "null-uses-default", yaml: `unix_socket_permission: null`, want: 0o770},
		{name: "quoted-leading-zero", yaml: `unix_socket_permission: "0770"`, want: 0o770},
		{name: "quoted-plain", yaml: `unix_socket_permission: "770"`, want: 0o770},
		{name: "quoted-0o-prefix", yaml: `unix_socket_permission: "0o770"`, want: 0o770},
		{name: "quoted-0o-prefix-owner-only", yaml: `unix_socket_permission: "0o700"`, want: 0o700},
		{name: "quoted-zero", yaml: `unix_socket_permission: "0000"`, want: 0},
		{name: "env", env: "0660", want: 0o660},
		{name: "env-0o-prefix", env: "0o660", want: 0o660},
		{name: "env-overrides-file", yaml: `unix_socket_permission: "0770"`, env: "0700", want: 0o700},
		{name: "env-invalid-is-error", env: "bogus", wantErr: true},
		{name: "empty-is-error", yaml: `unix_socket_permission: ""`, wantErr: true},
		{name: "symbolic-is-error", yaml: `unix_socket_permission: "rwxrwx---"`, wantErr: true},
		{name: "special-bits-are-error", yaml: `unix_socket_permission: "1770"`, wantErr: true},
		{name: "out-of-range-is-error", yaml: `unix_socket_permission: "17777"`, wantErr: true},
		// YAML reads unquoted 0770 as int 504, indistinguishable from a literal 504.
		{name: "unquoted-leading-zero-is-error", yaml: `unix_socket_permission: 0770`, wantErr: true},
		{name: "unquoted-0o-prefix-is-error", yaml: `unix_socket_permission: 0o770`, wantErr: true},
		{name: "unquoted-plain-is-error", yaml: `unix_socket_permission: 770`, wantErr: true},
		{name: "float-is-error", yaml: `unix_socket_permission: 770.0`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.env != "" {
				t.Setenv("SLOPSCALE_UNIX_SOCKET_PERMISSION", tt.env)
			}

			tmpDir := t.TempDir()
			cfg := `---
server_url: https://example.com
listen_addr: 0.0.0.0:8080
prefixes:
  v4: 100.64.0.0/10
noise:
  private_key_path: noise_private.key
database:
  type: sqlite
dns:
  magic_dns: false
  override_local_dns: false
` + tt.yaml + "\n"
			require.NoError(t, os.WriteFile(
				filepath.Join(tmpDir, "config.yaml"), []byte(cfg), 0o600))
			require.NoError(t, LoadConfig(tmpDir, false))

			got, err := LoadServerConfig()
			if tt.wantErr {
				require.ErrorIs(t, err, util.ErrInvalidFileMode,
					"invalid unix_socket_permission must not silently fall back")
				assert.Contains(t, err.Error(), "unix_socket_permission")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got.UnixSocketPermission,
				"got %#o, want %#o", got.UnixSocketPermission, tt.want)
		})
	}
}

// OK
// server_url: slopscale.com, base: clients.slopscale.com
// server_url: slopscale.com, base: slopscale.net
//
// NOT OK
// server_url: server.slopscale.com, base: slopscale.com.
func TestSafeServerURL(t *testing.T) {
	tests := []struct {
		serverURL, baseDomain,
		wantErr string
	}{
		{
			serverURL:  "https://example.com",
			baseDomain: "example.org",
		},
		{
			serverURL:  "https://slopscale.com",
			baseDomain: "slopscale.com",
			wantErr:    errServerURLSame.Error(),
		},
		{
			serverURL:  "https://slopscale.com",
			baseDomain: "clients.slopscale.com",
		},
		{
			serverURL:  "https://slopscale.com",
			baseDomain: "clients.subdomain.slopscale.com",
		},
		{
			serverURL:  "https://slopscale.kristoffer.com",
			baseDomain: "mybase",
		},
		{
			serverURL:  "https://server.slopscale.com",
			baseDomain: "slopscale.com",
			wantErr:    errServerURLSuffix.Error(),
		},
		{
			serverURL:  "https://server.subdomain.slopscale.com",
			baseDomain: "slopscale.com",
			wantErr:    errServerURLSuffix.Error(),
		},
		{
			serverURL: "http://foo\x00",
			wantErr: `parsing server URL "http://foo\x00": ` +
				`parse "http://foo\x00": net/url: invalid control character in URL`,
		},
	}

	for _, tt := range tests {
		testName := fmt.Sprintf("server=%s domain=%s", tt.serverURL, tt.baseDomain)
		t.Run(testName, func(t *testing.T) {
			err := isSafeServerURL(tt.serverURL, tt.baseDomain)
			if tt.wantErr != "" {
				assert.EqualError(t, err, tt.wantErr)

				return
			}

			assert.NoError(t, err)
		})
	}
}

func TestSafeServerURLWithPort(t *testing.T) {
	tests := []struct {
		serverURL, baseDomain,
		wantErr string
	}{
		{
			serverURL:  "https://server.slopscale.com:443",
			baseDomain: "slopscale.com",
			wantErr:    errServerURLSuffix.Error(),
		},
		{
			serverURL:  "https://server.subdomain.slopscale.com:8080",
			baseDomain: "slopscale.com",
			wantErr:    errServerURLSuffix.Error(),
		},
		{
			serverURL:  "https://slopscale.com:443",
			baseDomain: "slopscale.com",
			wantErr:    errServerURLSame.Error(),
		},
		{
			serverURL:  "https://example.com:8080",
			baseDomain: "example.org",
		},
	}

	for _, tt := range tests {
		testName := fmt.Sprintf("server=%s domain=%s", tt.serverURL, tt.baseDomain)
		t.Run(testName, func(t *testing.T) {
			err := isSafeServerURL(tt.serverURL, tt.baseDomain)
			if tt.wantErr != "" {
				assert.EqualError(t, err, tt.wantErr)

				return
			}

			assert.NoError(t, err)
		})
	}
}

// TestConfigJSONOmitsSecrets verifies that marshalling a [Config] to JSON
// (as /debug/config does via [state.State.DebugConfig]) does not leak the
// Postgres password, the OIDC client secret, or the slopscale admin
// API key. Operators who widen metrics_listen_addr to 0.0.0.0 should
// not be able to read these back via debug endpoints reachable over
// CGNAT/loopback.
func TestConfigJSONOmitsSecrets(t *testing.T) {
	const (
		secretPostgresPass = "p0stgres-secret-marker"
		secretClientSecret = "oidc-client-secret-marker"
		secretAPIKey       = "slopscale-cli-api-key-marker"
	)

	cfg := &Config{
		Database: DatabaseConfig{
			Postgres: PostgresConfig{
				Pass: secretPostgresPass,
			},
		},
		OIDC: OIDCConfig{
			ClientSecret: secretClientSecret,
		},
		CLI: CLIConfig{
			APIKey: secretAPIKey,
		},
	}

	//nolint:musttag // Config is not a JSON contract; the test only checks secrets are redacted
	out, err := json.Marshal(cfg)
	require.NoError(t, err)

	body := string(out)
	for _, secret := range []string{secretPostgresPass, secretClientSecret, secretAPIKey} {
		assert.NotContains(t, body, secret,
			"marshalled Config must not contain secret %q", secret)
	}
}

func TestTrustedProxies(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    []netip.Prefix
		wantErr string
	}{
		{
			name:  "unset",
			input: nil,
			want:  nil,
		},
		{
			name:  "empty",
			input: []string{},
			want:  nil,
		},
		{
			name:  "single-v4",
			input: []string{"10.0.0.0/16"},
			want:  []netip.Prefix{netip.MustParsePrefix("10.0.0.0/16")},
		},
		{
			name:  "single-v6",
			input: []string{"fd00::/8"},
			want:  []netip.Prefix{netip.MustParsePrefix("fd00::/8")},
		},
		{
			name:  "mixed-v4-v6",
			input: []string{"127.0.0.1/32", "::1/128", "10.0.0.0/16"},
			want: []netip.Prefix{
				netip.MustParsePrefix("127.0.0.1/32"),
				netip.MustParsePrefix("::1/128"),
				netip.MustParsePrefix("10.0.0.0/16"),
			},
		},
		{
			name:  "non-canonical-masked",
			input: []string{"10.0.0.5/16"},
			want:  []netip.Prefix{netip.MustParsePrefix("10.0.0.0/16")},
		},
		{
			name:    "bare-ip-rejected",
			input:   []string{"10.0.0.1"},
			wantErr: `trusted_proxies[0] "10.0.0.1"`,
		},
		{
			name:    "garbage-reports-index",
			input:   []string{"10.0.0.0/16", "not-an-ip"},
			wantErr: `trusted_proxies[1] "not-an-ip"`,
		},
		{
			name:    "ipv4-zero-rejected",
			input:   []string{"0.0.0.0/0"},
			wantErr: "0.0.0.0/0 and ::/0 are not allowed",
		},
		{
			name:    "ipv6-zero-rejected",
			input:   []string{"::/0"},
			wantErr: "0.0.0.0/0 and ::/0 are not allowed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conf.Reset()

			if tt.input != nil {
				conf.Set("trusted_proxies", tt.input)
			}

			got, err := trustedProxies()

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)

				return
			}

			require.NoError(t, err)

			if diff := cmp.Diff(tt.want, got, cmpopts.EquateComparable(netip.Prefix{})); diff != "" {
				t.Errorf("trustedProxies() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestSetExtraRecordsNormalizesNames proves the records read from
// dns.extra_records_path are normalized the way the ones written inline in the
// config file already are. DNS names are case-insensitive, but a client
// matches an extra record against the lowercased query name, so a
// mixed-case "Printer.fritz.box" in the records file never resolved and the
// query fell through to the global nameserver.
func TestSetExtraRecordsNormalizesNames(t *testing.T) {
	cfg := &Config{
		DNSConfig:        DNSConfig{MagicDNS: false},
		TailcfgDNSConfig: &tailcfg.DNSConfig{},
	}

	cfg.SetExtraRecords([]tailcfg.DNSRecord{
		{Name: "Printer.Fritz.Box.", Type: "a", Value: " 192.168.0.2 "},
	})

	got := cfg.CloneTailcfgDNSConfig()
	require.NotNil(t, got)
	require.Len(t, got.ExtraRecords, 1)
	assert.Equal(t, "printer.fritz.box", got.ExtraRecords[0].Name)
	assert.Equal(t, "A", got.ExtraRecords[0].Type)
	assert.Equal(t, "192.168.0.2", got.ExtraRecords[0].Value)
}
