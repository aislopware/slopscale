package types

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/conf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/util/set"
)

// validBaseConfig passes validation on its own; tests append the keys
// under test, which must not repeat these.
const validBaseConfig = `---
server_url: https://example.com
noise:
  private_key_path: noise_private.key
prefixes:
  v4: 100.64.0.0/10
database:
  type: sqlite3
dns:
  magic_dns: false
  override_local_dns: false
`

func loadTestConfig(t *testing.T, yaml string) {
	t.Helper()

	conf.Reset()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(yaml), 0o600))
	require.NoError(t, LoadConfig(dir, false))
}

func reasons(errs []*ConfigError) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Reason
	}

	return out
}

func TestValidateListenerCollisions(t *testing.T) {
	const acme = "tls_letsencrypt_hostname: example.com\n"

	tests := []struct {
		name    string
		extra   string
		wantErr string // empty: no error
	}{
		{
			name:    "acme-collision-numeric",
			extra:   acme + "listen_addr: \":80\"\ntls_letsencrypt_listen: \":80\"\n",
			wantErr: "listen_addr and tls_letsencrypt_listen would bind the same tcp socket",
		},
		{
			name:    "acme-collision-named-vs-numeric",
			extra:   acme + "listen_addr: 0.0.0.0:80\ntls_letsencrypt_listen: \":http\"\n",
			wantErr: "listen_addr and tls_letsencrypt_listen would bind the same tcp socket",
		},
		{
			name:    "acme-collision-unset-listen-is-port-80",
			extra:   acme + "listen_addr: 0.0.0.0:80\n",
			wantErr: "listen_addr and tls_letsencrypt_listen would bind the same tcp socket",
		},
		{
			name:    "acme-collision-https-named",
			extra:   acme + "listen_addr: \":443\"\ntls_letsencrypt_listen: \":https\"\n",
			wantErr: "listen_addr and tls_letsencrypt_listen would bind the same tcp socket",
		},
		{
			name:  "acme-canonical",
			extra: acme + "listen_addr: 0.0.0.0:443\ntls_letsencrypt_listen: \":http\"\n",
		},
		{
			name:  "acme-without-hostname-is-skipped",
			extra: "listen_addr: 0.0.0.0:80\ntls_letsencrypt_listen: \":http\"\n",
		},
		{
			name:  "acme-tls-alpn-01-is-skipped",
			extra: acme + "tls_letsencrypt_challenge_type: TLS-ALPN-01\nlisten_addr: 0.0.0.0:80\n",
		},
		{
			name:  "acme-different-ports",
			extra: acme + "listen_addr: \":8080\"\ntls_letsencrypt_listen: \":8081\"\n",
		},
		{
			name:    "metrics-on-listen-port",
			extra:   "listen_addr: 0.0.0.0:8080\nmetrics_listen_addr: 127.0.0.1:8080\n",
			wantErr: "listen_addr and metrics_listen_addr would bind the same tcp socket",
		},
		{
			name:  "metrics-same-port-different-hosts",
			extra: "listen_addr: 127.0.0.1:9090\nmetrics_listen_addr: 127.0.0.2:9090\n",
		},
		{
			name:    "funnel-default-addrs-on-listen-port",
			extra:   "listen_addr: 0.0.0.0:443\nfunnel:\n  enabled: true\n",
			wantErr: "listen_addr and funnel.listen_addrs[0] would bind the same tcp socket",
		},
		{
			name:  "funnel-disabled-is-skipped",
			extra: "listen_addr: 0.0.0.0:443\n",
		},
		{
			name: "funnel-duplicate-addrs",
			extra: "listen_addr: 0.0.0.0:8080\nfunnel:\n  enabled: true\n" +
				"  listen_addrs: [\":8443\", \"0.0.0.0:8443\"]\n",
			wantErr: "funnel.listen_addrs[0] and funnel.listen_addrs[1] would bind the same tcp socket",
		},
		{
			name:    "funnel-unparseable-addr",
			extra:   "funnel:\n  enabled: true\n  listen_addrs: [\"8443\"]\n",
			wantErr: "cannot parse funnel.listen_addrs[0]",
		},
		{
			name:  "stun-udp-does-not-collide-with-tcp",
			extra: "listen_addr: 0.0.0.0:3478\n",
		},
		{
			name:    "stun-unparseable-addr",
			extra:   "derp:\n  server:\n    stun_listen_addr: \"3478\"\n",
			wantErr: "cannot parse derp.server.stun_listen_addr",
		},
		{
			name:  "stun-off-is-skipped",
			extra: "derp:\n  server:\n    stun_enabled: false\n    stun_listen_addr: \"3478\"\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loadTestConfig(t, validBaseConfig+tt.extra)

			err := validateServerConfig()
			if tt.wantErr == "" {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, ErrConfig)
			assert.Contains(t, reasons(ConfigErrors(err)), tt.wantErr)
		})
	}
}

// TestFormerlyFatalRulesAreReported proves the rules that used to end the
// process from inside a section reader, and the removed keys, come back as
// one report instead.
func TestFormerlyFatalRulesAreReported(t *testing.T) {
	loadTestConfig(t, `---
server_url: https://example.com
noise:
  private_key_path: noise_private.key
prefixes:
  v4: 100.64.0.0/10
database:
  type: mysql
dns:
  magic_dns: true
  override_local_dns: false
  extra_records:
    - name: a.example.com
      type: A
      value: 100.64.0.1
  extra_records_path: /var/lib/slopscale/records.json
derp:
  server:
    automatically_add_embedded_derp_region: false
    stun_listen_addr: ""
dns_config:
  use_username_in_magic_dns: true
oidc:
  expiry: 1h
`)

	_, err := LoadServerConfig()
	require.ErrorIs(t, err, ErrConfig)

	got := reasons(ConfigErrors(err))
	for _, want := range []string{
		"database.type has an unsupported value",
		"dns.base_domain is required when dns.magic_dns is true",
		"dns.extra_records and dns.extra_records_path are mutually exclusive",
		"derp.server.stun_listen_addr is required when the embedded relay runs STUN",
		"derp.paths is required when derp.server.automatically_add_embedded_derp_region is false",
		"configuration key oidc.expiry has been removed; use node.expiry instead",
		"configuration key dns_config.use_username_in_magic_dns has been removed",
	} {
		assert.Contains(t, got, want)
	}
}

// TestLoadServerConfig_CollectsAcrossSubBuilders proves every section
// reader's refusal lands in the one report, with its sentinel reachable.
func TestLoadServerConfig_CollectsAcrossSubBuilders(t *testing.T) {
	loadTestConfig(t, `---
server_url: https://example.com
listen_addr: 0.0.0.0:8080
noise:
  private_key_path: noise_private.key
prefixes:
  v4: not-a-cidr
  v6: also-not-a-cidr
  allocation: bogus
oidc:
  client_secret: hunter2
  client_secret_path: /nonexistent/secret
database:
  type: sqlite3
dns:
  magic_dns: false
  override_local_dns: false
  base_domain: example.com
trusted_proxies: ["0.0.0.0/0"]
notifications:
  smtp:
    host: smtp.example.com
    from: slopscale@example.com
    encryption: ssl
control_dial_plan: ["not-an-ip"]
`)

	_, err := LoadServerConfig()
	require.Error(t, err)

	got := reasons(ConfigErrors(err))
	assert.Len(t, got, 8, "one error per refusal: %q", got)

	for _, want := range []error{
		ErrConfig,
		ErrInvalidAllocationStrategy,
		errOidcMutuallyExclusive,
		errTrustedProxyZeroRange,
		errServerURLSame,
		errSMTPEncryption,
		ErrControlDialPlanAddrInvalid,
	} {
		require.ErrorIs(t, err, want)
	}

	rendered := err.Error()
	for _, want := range []string{"prefixes.v4", "prefixes.v6", "prefixes.allocation", "oidc.client_secret_path"} {
		assert.Contains(t, rendered, want)
	}

	assert.NotContains(t, rendered, "hunter2", "the client secret is never rendered")
}

func TestDeprecatedKeysWarnWithoutFailing(t *testing.T) {
	loadTestConfig(t, validBaseConfig+"ephemeral_node_inactivity_timeout: 5m\n")

	require.NoError(t, validateServerConfig())
}

func TestDeprecatorReportsEachKeyOnce(t *testing.T) {
	loadTestConfig(t, validBaseConfig+"acl_policy_path: /etc/slopscale/acl.hujson\n")

	d := deprecator{seen: make(set.Set[string])}
	d.fatalIfNewKeyIsNotUsed("policy.path", "acl_policy_path")
	d.fatal("acl_policy_path")

	v := &configValidator{}
	d.apply(v)

	errs := ConfigErrors(v.Err())
	require.Len(t, errs, 1)
	assert.Equal(t, "configuration key acl_policy_path has been removed; use policy.path instead", errs[0].Reason)
	assert.Equal(t, "remove acl_policy_path and set policy.path", errs[0].Hint)
	assert.Equal(t, []KV{{"acl_policy_path", "/etc/slopscale/acl.hujson"}}, errs[0].Current)
}

// A config-file DNS name is held to the settings API's rule: a client drops
// or, before the pinned version, panics on a name it cannot parse, and a
// newline reaches resolv.conf.
func TestValidateDNSNames(t *testing.T) {
	const head = `---
server_url: https://example.com
noise:
  private_key_path: noise_private.key
prefixes:
  v4: 100.64.0.0/10
database:
  type: sqlite3
dns:
  magic_dns: false
  override_local_dns: false
`

	long := strings.Repeat("a", 64) + ".example.com"

	tests := []struct {
		name    string
		dns     string
		wantErr string // empty: no error
	}{
		{
			name: "valid-names",
			dns: "  search_domains: [Corp.Example.com., svc.cluster.local]\n" +
				"  nameservers:\n    split:\n      _msdcs.example.com: [10.0.0.1]\n" +
				"  extra_records:\n    - {name: Printer.fritz.box, type: A, value: 100.64.0.9}\n",
		},
		{
			name:    "search-domain-label-too-long",
			dns:     "  search_domains: [" + long + "]\n",
			wantErr: "dns.search_domains holds an invalid entry",
		},
		{
			name:    "search-domain-with-space",
			dns:     "  search_domains: [\"a b.com\"]\n",
			wantErr: "dns.search_domains holds an invalid entry",
		},
		{
			name:    "search-domain-with-newline",
			dns:     "  search_domains: [\"evil.com\\nnameserver 1.1.1.1\"]\n",
			wantErr: "dns.search_domains holds an invalid entry",
		},
		{
			name:    "split-domain-with-space",
			dns:     "  nameservers:\n    split:\n      \"a b.com\": [10.0.0.1]\n",
			wantErr: "dns.nameservers.split holds an invalid entry",
		},
		{
			name:    "record-name-with-newline",
			dns:     "  extra_records:\n    - {name: \"evil.com\\nx\", type: A, value: 100.64.0.9}\n",
			wantErr: "dns.extra_records holds an invalid entry",
		},
		{
			name:    "record-value-wrong-family",
			dns:     "  extra_records:\n    - {name: host.example.com, type: AAAA, value: 100.64.0.9}\n",
			wantErr: "dns.extra_records holds an invalid entry",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loadTestConfig(t, head+tt.dns)

			_, err := LoadServerConfig()
			if tt.wantErr == "" {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, ErrDNSSettingsInvalid)
			assert.Contains(t, reasons(ConfigErrors(err)), tt.wantErr)
		})
	}
}
