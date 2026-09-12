package hsic

import "github.com/aislopware/slopscale/hscontrol/types"

func MinimumConfigYAML() string {
	// derp.urls defaults to Tailscale's public map, which would publish 28
	// public regions next to the embedded one and let clients pick whichever
	// relay is nearest to the runner. The file empties it; [WithPublicDERP]
	// sets the variable, which still wins over this.
	return `
private_key_path: /tmp/private.key
noise:
  private_key_path: /tmp/noise_private.key
derp:
  urls: []
`
}

func DefaultConfigEnv() map[string]string {
	return map[string]string{
		"SLOPSCALE_LOG_LEVEL":                         "trace",
		"SLOPSCALE_POLICY_PATH":                       "",
		"SLOPSCALE_DATABASE_TYPE":                     "sqlite",
		"SLOPSCALE_DATABASE_SQLITE_PATH":              "/tmp/integration_test_db.sqlite3",
		"SLOPSCALE_DATABASE_DEBUG":                    "0",
		"SLOPSCALE_DATABASE_GORM_SLOW_THRESHOLD":      "1",
		"SLOPSCALE_EPHEMERAL_NODE_INACTIVITY_TIMEOUT": "30m",
		"SLOPSCALE_PREFIXES_V4":                       "100.64.0.0/10",
		"SLOPSCALE_PREFIXES_V6":                       "fd7a:115c:a1e0::/48",
		"SLOPSCALE_DNS_BASE_DOMAIN":                   "slopscale.net",
		"SLOPSCALE_DNS_MAGIC_DNS":                     "true",
		"SLOPSCALE_DNS_OVERRIDE_LOCAL_DNS":            "false",
		"SLOPSCALE_DNS_NAMESERVERS_GLOBAL":            "127.0.0.11 1.1.1.1",
		"SLOPSCALE_PRIVATE_KEY_PATH":                  "/tmp/private.key",
		"SLOPSCALE_NOISE_PRIVATE_KEY_PATH":            "/tmp/noise_private.key",
		"SLOPSCALE_METRICS_LISTEN_ADDR":               "0.0.0.0:9090",
		"SLOPSCALE_DEBUG_PORT":                        "40000",

		// POST /api/v1/debug/node mints a node from key material the
		// caller hands it, so a server only registers it when asked.
		// `slopscale debug create-node` is how the CLI node tests get a
		// node without a client, and without this they get a 404.
		"SLOPSCALE_DEBUG_NODE_API_ENABLED": "true",

		// Embedded DERP is the default for test isolation.
		// Tests should not depend on external DERP infrastructure.
		// Use [WithPublicDERP] to opt out for tests that explicitly
		// need public DERP relays. The empty URL list lives in
		// [MinimumConfigYAML]; see the note there for why.
		"SLOPSCALE_DERP_AUTO_UPDATE_ENABLED":     "false",
		"SLOPSCALE_DERP_UPDATE_FREQUENCY":        "1m",
		"SLOPSCALE_DERP_SERVER_ENABLED":          "true",
		"SLOPSCALE_DERP_SERVER_REGION_ID":        "999",
		"SLOPSCALE_DERP_SERVER_REGION_CODE":      binSlopscale,
		"SLOPSCALE_DERP_SERVER_REGION_NAME":      "Slopscale Embedded DERP",
		"SLOPSCALE_DERP_SERVER_STUN_LISTEN_ADDR": "0.0.0.0:3478",
		"SLOPSCALE_DERP_SERVER_PRIVATE_KEY_PATH": "/tmp/derp.key",
		"DERP_DEBUG_LOGS":                        "true",
		"DERP_PROBER_DEBUG_LOGS":                 "true",

		// a bunch of tests (ACL/Policy) rely on predictable IP alloc,
		// so ensure the sequential alloc is used by default.
		"SLOPSCALE_PREFIXES_ALLOCATION": string(types.IPAllocationStrategySequential),
	}
}
