package types

import (
	"os"
	"path/filepath"
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
