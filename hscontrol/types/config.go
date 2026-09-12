package types

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aislopware/slopscale/hscontrol/conf"
	"github.com/aislopware/slopscale/hscontrol/egress"
	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/prometheus/common/model"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go4.org/netipx"
	"tailscale.com/net/tsaddr"
	"tailscale.com/tailcfg"
	"tailscale.com/types/dnstype"
	"tailscale.com/util/dnsname"
	"tailscale.com/util/set"
)

const (
	PKCEMethodPlain string = "plain"
	PKCEMethodS256  string = "S256"

	defaultNodeStoreBatchSize = 100
)

var (
	errOidcMutuallyExclusive    = errors.New("oidc_client_secret and oidc_client_secret_path are mutually exclusive")
	errOIDCIssuerInvalid        = errors.New("oidc.issuer must be a valid http(s) URL")
	errOIDCClientIDRequired     = errors.New("oidc.client_id is required when oidc.issuer is set")
	errOIDCClientSecretRequired = errors.New(
		"oidc.client_secret or oidc.client_secret_path is required when oidc.issuer is set",
	)
	errServerURLSuffix = errors.New(
		"server_url cannot be part of base_domain in a way that could make the DERP and slopscale server unreachable",
	)
	errServerURLSame = errors.New(
		"server_url cannot use the same domain as base_domain in a way that could make the DERP and " +
			"slopscale server unreachable",
	)
	errInvalidPKCEMethod         = errors.New("pkce.method must be either 'plain' or 'S256'")
	errTrustedProxyZeroRange     = errors.New("0.0.0.0/0 and ::/0 are not allowed")
	ErrNoPrefixConfigured        = errors.New("no IPv4 or IPv6 prefix configured, minimum one prefix is required")
	ErrInvalidAllocationStrategy = errors.New("invalid prefix allocation strategy")
)

type IPAllocationStrategy string

const (
	IPAllocationStrategySequential IPAllocationStrategy = "sequential"
	IPAllocationStrategyRandom     IPAllocationStrategy = "random"
)

type PolicyMode string

const (
	PolicyModeDB   = "database"
	PolicyModeFile = "file"
)

// EphemeralConfig contains configuration for ephemeral node lifecycle.
type EphemeralConfig struct {
	// InactivityTimeout is how long an ephemeral node can be offline
	// before it is automatically deleted.
	InactivityTimeout time.Duration
}

// HARouteConfig contains configuration for HA subnet router health probing.
type HARouteConfig struct {
	// ProbeInterval is how often HA subnet routers are probed.
	// A zero or negative duration disables probing.
	ProbeInterval time.Duration

	// ProbeTimeout is the maximum time to wait for a probe response
	// before declaring a node unhealthy. Must be less than [HARouteConfig.ProbeInterval].
	ProbeTimeout time.Duration
}

// RouteConfig contains configuration for route behaviour.
type RouteConfig struct {
	HA HARouteConfig
}

// AuditConfig configures the audit log.
type AuditConfig struct {
	// Retention is how long audit events are kept before the background
	// collector deletes them. Zero keeps them forever.
	Retention time.Duration
}

// PreAuthKeysConfig contains configuration for pre-auth key lifecycle.
type PreAuthKeysConfig struct {
	// RevokedRetention is how long a soft-revoked pre-auth key (revoked via the
	// v2 API's DELETE) is kept retrievable before the background collector
	// hard-deletes it. A zero or negative duration disables the collector.
	RevokedRetention time.Duration
}

// NodeConfig contains configuration for node lifecycle and expiry.
type NodeConfig struct {
	// Expiry is the default key expiry duration for non-tagged nodes.
	// Applies to all registration methods (auth key, CLI, web, OIDC).
	// Tagged nodes are exempt and never expire.
	// A zero/negative duration means no default expiry (nodes never expire).
	Expiry time.Duration

	// Ephemeral contains configuration for ephemeral node lifecycle.
	Ephemeral EphemeralConfig

	// Routes contains configuration for route behaviour.
	Routes RouteConfig
}

// Config contains the initial Slopscale configuration.
type Config struct {
	ServerURL           string
	Addr                string
	MetricsAddr         string
	TrustedProxies      []netip.Prefix
	Node                NodeConfig
	PreAuthKeys         PreAuthKeysConfig
	Audit               AuditConfig
	PrefixV4            *netip.Prefix
	PrefixV6            *netip.Prefix
	IPAllocation        IPAllocationStrategy
	NoisePrivateKeyPath string
	BaseDomain          string
	Log                 LogConfig
	DisableUpdateCheck  bool

	Database DatabaseConfig

	DERP DERPConfig

	TLS TLSConfig

	ACMEURL   string
	ACMEEmail string

	// DNSConfig is the slopscale representation of the DNS configuration.
	// It is kept in the config update for some settings that are
	// not directly converted into a [tailcfg.DNSConfig].
	DNSConfig DNSConfig

	// TailcfgDNSConfig is the tailcfg representation of the DNS configuration,
	// it can be used directly when sending Netmaps to clients. Read it
	// through [Config.CloneTailcfgDNSConfig]: the setters below rebuild it
	// while the server runs.
	TailcfgDNSConfig *tailcfg.DNSConfig

	// dnsOverride is the DNS settings from the settings table, nil when
	// the config file is in force; see [Config.SetDNSOverride].
	dnsOverride *DNSSettings
	// dnsFileRecords holds the records read from dns.extra_records_path
	// once the watcher has read the file.
	dnsFileRecords    []tailcfg.DNSRecord
	dnsFileRecordsSet bool

	UnixSocket           string
	UnixSocketPermission fs.FileMode

	OIDC OIDCConfig

	LogTail    LogTailConfig
	Taildrop   TaildropConfig
	AutoUpdate AutoUpdateConfig

	CLI CLIConfig

	Policy PolicyConfig

	// SMTP is the mail server email webhooks send through.
	SMTP SMTPConfig

	// SSHRecording is the embedded session recorder.
	SSHRecording SSHRecordingConfig

	// Funnel is the embedded Funnel ingress.
	Funnel FunnelConfig

	// ClientUpdates looks up the latest client release for the clients;
	// see [ClientUpdatesConfig].
	ClientUpdates ClientUpdatesConfig

	// ControlDialPlan lists the addresses clients try for the server
	// before resolving its name; see [Config.DialPlan].
	ControlDialPlan []netip.Addr

	// Egress bounds where the server's own outbound requests may go.
	Egress EgressConfig

	// Debug turns on endpoints meant for developing slopscale.
	Debug DebugConfig

	// HTTPSCerts is certificate assistance for machines' MagicDNS names.
	HTTPSCerts HTTPSCertsConfig

	Tuning Tuning
}

type DNSConfig struct {
	MagicDNS         bool   `mapstructure:"magic_dns"`
	BaseDomain       string `mapstructure:"base_domain"`
	OverrideLocalDNS bool   `mapstructure:"override_local_dns"`
	Nameservers      Nameservers
	SearchDomains    []string            `mapstructure:"search_domains"`
	ExtraRecords     []tailcfg.DNSRecord `mapstructure:"extra_records"`
	ExtraRecordsPath string              `mapstructure:"extra_records_path"`
}

type Nameservers struct {
	Global []string
	Split  map[string][]string

	// UseWithExitNode lists the global nameservers a client keeps using
	// while it has an exit node selected; the rest of its DNS goes
	// through the exit node then. The client only honours it for the
	// resolvers it is told to use for every query, so it needs
	// override_local_dns. SplitUseWithExitNode is the same per split
	// DNS domain: a domain's route survives the exit node only when
	// every one of its nameservers is listed.
	UseWithExitNode      []string
	SplitUseWithExitNode map[string][]string
}

type SqliteConfig struct {
	Path              string
	WriteAheadLog     bool
	WALAutoCheckPoint int
}

type PostgresConfig struct {
	Host                string
	Port                int
	Name                string
	User                string
	Pass                string `json:"-"` // never serialise the database password
	Ssl                 string
	MaxOpenConnections  int
	MaxIdleConnections  int
	ConnMaxIdleTimeSecs int
}

// QueryLogConfig controls the SQL query log written at debug level.
type QueryLogConfig struct {
	// Enabled turns the query log on. It follows database.debug.
	Enabled bool
	// SlowThreshold marks queries that take longer as slow; zero disables
	// the slow-query warning.
	SlowThreshold time.Duration
	// LogNotFound logs queries that returned no rows as errors.
	LogNotFound bool
	// Parameterized logs placeholders instead of the bound arguments.
	Parameterized bool
}

type DatabaseConfig struct {
	// Type sets the database type, either "sqlite3" or "postgres"
	Type  string
	Debug bool

	// QueryLog configures the SQL query log.
	QueryLog QueryLogConfig

	Sqlite   SqliteConfig
	Postgres PostgresConfig
}

type TLSConfig struct {
	CertPath string
	KeyPath  string

	LetsEncrypt LetsEncryptConfig
}

type LetsEncryptConfig struct {
	Listen        string
	Hostname      string
	CacheDir      string
	ChallengeType string
}

// OIDCGroupsConfig is oidc.groups: when Sync is on, every group in the
// login's groups claim that starts with Prefix becomes a slopscale group
// of the same name (prefix stripped) with the user as a member, and the
// user leaves the synced groups the claim no longer lists. Groups made by
// an operator are never taken over by name.
type OIDCGroupsConfig struct {
	Sync   bool
	Prefix string
}

// SyncedNames returns the group names to mirror for a login's groups
// claim: those with the prefix, stripped, trimmed, valid as a group name
// and deduplicated, in claim order.
func (c OIDCGroupsConfig) SyncedNames(claimed []string) []string {
	if !c.Sync {
		return nil
	}

	var names []string

	for _, claim := range claimed {
		name, ok := strings.CutPrefix(claim, c.Prefix)
		if !ok {
			continue
		}

		name = strings.TrimSpace(name)
		if ValidateGroupName(name) != nil || slices.Contains(names, name) {
			continue
		}

		names = append(names, name)
	}

	return names
}

type PKCEConfig struct {
	Enabled bool
	Method  string
}

type OIDCConfig struct {
	OnlyStartIfOIDCIsAvailable bool
	Issuer                     string
	ClientID                   string
	ClientSecret               string `json:"-"` // never serialise the OIDC client secret
	Scope                      []string
	ExtraParams                map[string]string
	AllowedDomains             []string
	AllowedUsers               []string
	AllowedGroups              []string
	// AdminUsers are email addresses that hold the admin role: a user
	// signing in with one of them is promoted from member to admin on
	// every login, so a fresh deployment can name its administrators in
	// configuration. The owner and users who already hold a higher role
	// are left alone.
	AdminUsers []string
	// Groups mirrors the provider's groups claim into slopscale groups on
	// every login; see [OIDCGroupsConfig].
	Groups OIDCGroupsConfig
	// MatchByEmail lets a login whose iss/sub identifier is unknown take
	// over the one existing OIDC user with the same verified email, so a
	// provider switch keeps users and their machines instead of failing
	// on the taken name and email.
	MatchByEmail          bool
	EmailVerifiedRequired bool
	UseExpiryFromToken    bool
	PKCE                  PKCEConfig
}

type DERPConfig struct {
	ServerEnabled                      bool
	AutomaticallyAddEmbeddedDerpRegion bool
	ServerRegionID                     tailcfg.DERPRegionID
	ServerRegionCode                   string
	ServerRegionName                   string
	ServerPrivateKeyPath               string
	ServerVerifyClients                bool
	STUNEnabled                        bool
	STUNAddr                           string
	URLs                               []url.URL
	Paths                              []string
	DERPMap                            *tailcfg.DERPMap
	AutoUpdate                         bool
	UpdateFrequency                    time.Duration
	IPv4                               string
	IPv6                               string
}

type LogTailConfig struct {
	Enabled bool
}

type TaildropConfig struct {
	Enabled bool
}

// AutoUpdateConfig controls the tailnet-wide default for client
// auto-update. When Enabled is true, slopscale emits the
// [tailcfg.NodeAttrDefaultAutoUpdate] cap with value [true] on every
// node's CapMap; clients fall back to that default unless they have
// opted in or out locally.
type AutoUpdateConfig struct {
	Enabled bool
}

type CLIConfig struct {
	Address  string
	APIKey   string `json:"-"` // never serialise the slopscale admin API key
	Timeout  time.Duration
	Insecure bool
}

type PolicyConfig struct {
	Path string
	Mode PolicyMode
	// GeoIPDatabase is the path of a MaxMind country database (an .mmdb
	// such as GeoLite2-Country) the ip:country posture attribute is
	// looked up in; empty leaves the attribute unset.
	GeoIPDatabase string
}

func (p *PolicyConfig) IsEmpty() bool {
	return p.Mode == PolicyModeFile && p.Path == ""
}

type LogConfig struct {
	Format string
	Level  zerolog.Level
}

// SMTPEncryption is how the SMTP connection is secured.
type SMTPEncryption string

// The encryptions notifications.smtp.encryption accepts.
const (
	// SMTPStartTLS connects in the clear and upgrades with STARTTLS; the
	// upgrade is required, not opportunistic.
	SMTPStartTLS SMTPEncryption = "starttls"
	// SMTPImplicitTLS opens a TLS connection, the port 465 way.
	SMTPImplicitTLS SMTPEncryption = "tls"
	// SMTPNoEncryption sends in the clear; only for a relay on localhost.
	SMTPNoEncryption SMTPEncryption = "none"
)

// SMTPConfig is the mail server email webhooks send through; see
// docs/ref/webhooks.md. An empty Host means email endpoints are refused.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string `json:"-"` // never serialise the mail password
	// From is the sender address, with an optional display name.
	From       string
	Encryption SMTPEncryption
}

// Configured reports whether a mail server is set.
func (c SMTPConfig) Configured() bool {
	return c.Host != ""
}

// Addr is the host:port to dial.
func (c SMTPConfig) Addr() string {
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

var errSMTPEncryption = errors.New("notifications.smtp.encryption must be starttls, tls or none")

func smtpConfig() (SMTPConfig, error) {
	cfg := SMTPConfig{
		Host:       conf.GetString("notifications.smtp.host"),
		Port:       conf.GetInt("notifications.smtp.port"),
		Username:   conf.GetString("notifications.smtp.username"),
		Password:   conf.GetString("notifications.smtp.password"),
		From:       conf.GetString("notifications.smtp.from"),
		Encryption: SMTPEncryption(conf.GetString("notifications.smtp.encryption")),
	}

	if !cfg.Configured() {
		return cfg, nil
	}

	switch cfg.Encryption {
	case SMTPStartTLS, SMTPImplicitTLS, SMTPNoEncryption:
	default:
		return SMTPConfig{}, fmt.Errorf("%w, not %q", errSMTPEncryption, cfg.Encryption)
	}

	if cfg.From == "" {
		return SMTPConfig{}, errSMTPFrom
	}

	return cfg, nil
}

var errSMTPFrom = errors.New("notifications.smtp.from is required when notifications.smtp.host is set")

var (
	errHTTPSCertsProvider = errors.New(
		"https_certificates.provider must be cloudflare, rfc2136 or command when https_certificates.enabled is set",
	)
	errHTTPSCertsBaseDomain = errors.New("https_certificates.enabled needs dns.base_domain")
	errCloudflareToken      = errors.New("https_certificates.cloudflare.api_token is required")
	errRFC2136Server        = errors.New("https_certificates.rfc2136.server is required")
	errCommandPath          = errors.New("https_certificates.command.path is required")
)

// httpsCertsConfig reads the certificate assistance settings and checks
// that the chosen provider has what it needs.
func httpsCertsConfig() (HTTPSCertsConfig, error) {
	cfg := HTTPSCertsConfig{
		Enabled:  conf.GetBool("https_certificates.enabled"),
		Provider: DNSProviderKind(conf.GetString("https_certificates.provider")),
		TTL:      conf.GetDuration("https_certificates.ttl"),
		Cloudflare: CloudflareDNSConfig{
			APIToken: conf.GetString("https_certificates.cloudflare.api_token"),
			ZoneID:   conf.GetString("https_certificates.cloudflare.zone_id"),
		},
		RFC2136: RFC2136Config{
			Server:        conf.GetString("https_certificates.rfc2136.server"),
			Zone:          conf.GetString("https_certificates.rfc2136.zone"),
			TSIGKeyName:   conf.GetString("https_certificates.rfc2136.tsig_key_name"),
			TSIGSecret:    conf.GetString("https_certificates.rfc2136.tsig_secret"),
			TSIGAlgorithm: conf.GetString("https_certificates.rfc2136.tsig_algorithm"),
		},
		Command: CommandDNSConfig{
			Path: conf.GetString("https_certificates.command.path"),
		},
	}

	if !cfg.Enabled {
		return cfg, nil
	}

	if conf.GetString("dns.base_domain") == "" {
		return cfg, errHTTPSCertsBaseDomain
	}

	switch cfg.Provider {
	case DNSProviderCloudflare:
		if cfg.Cloudflare.APIToken == "" {
			return cfg, errCloudflareToken
		}
	case DNSProviderRFC2136:
		if cfg.RFC2136.Server == "" {
			return cfg, errRFC2136Server
		}
	case DNSProviderCommand:
		if cfg.Command.Path == "" {
			return cfg, errCommandPath
		}
	default:
		return cfg, errHTTPSCertsProvider
	}

	return cfg, nil
}

func funnelConfig() (FunnelConfig, error) {
	rawPorts := conf.GetIntSlice("funnel.ports")
	ports := make([]uint16, 0, len(rawPorts))

	for _, p := range rawPorts {
		if p < 1 || p > 65535 {
			return FunnelConfig{}, fmt.Errorf("%w: %d", ErrFunnelPortInvalid, p)
		}

		ports = append(ports, uint16(p))
	}

	cfg := FunnelConfig{
		Enabled:     conf.GetBool("funnel.enabled"),
		ListenAddrs: conf.GetStringSlice("funnel.listen_addrs"),
		StateDir:    util.AbsolutePathFromConfigPath(conf.GetString("funnel.state_dir")),
		Ports:       ports,
	}

	err := cfg.Validate()
	if err != nil {
		return FunnelConfig{}, err
	}

	return cfg, nil
}

func sshRecordingConfig() SSHRecordingConfig {
	return SSHRecordingConfig{
		Enabled:         conf.GetBool("ssh_recording.enabled"),
		Dir:             util.AbsolutePathFromConfigPath(conf.GetString("ssh_recording.dir")),
		StateDir:        util.AbsolutePathFromConfigPath(conf.GetString("ssh_recording.state_dir")),
		Retention:       conf.GetDuration("ssh_recording.retention"),
		MaxSessionBytes: conf.GetInt64("ssh_recording.max_session_bytes"),
	}
}

// EgressConfig bounds the addresses the server's own outbound requests
// (webhooks, log streams, DERP map URLs) may reach; see
// [github.com/aislopware/slopscale/hscontrol/egress].
type EgressConfig struct {
	// DenyPrivateTargets refuses the private ranges as well as the
	// addresses that are always refused.
	DenyPrivateTargets bool
	// AllowLoopbackTargets re-allows loopback, for development.
	AllowLoopbackTargets bool
}

// Policy is the guard policy this config asks for.
func (c EgressConfig) Policy() egress.Policy {
	return egress.Policy{
		AllowLoopback: c.AllowLoopbackTargets,
		DenyPrivate:   c.DenyPrivateTargets,
	}
}

func egressConfig() EgressConfig {
	return EgressConfig{
		DenyPrivateTargets:   conf.GetBool("egress.deny_private_targets"),
		AllowLoopbackTargets: conf.GetBool("egress.allow_loopback_targets"),
	}
}

// DebugConfig holds the endpoints that exist for developing slopscale and
// are not part of the supported API.
type DebugConfig struct {
	// NodeAPIEnabled registers POST /api/v1/debug/node, which mints a node
	// from key material the caller supplies. Development only.
	NodeAPIEnabled bool
}

func debugConfig() DebugConfig {
	return DebugConfig{
		NodeAPIEnabled: conf.GetBool("debug.node_api_enabled"),
	}
}

// Tuning contains advanced performance tuning parameters for Slopscale.
// These settings control internal batching, timeouts, and resource allocation.
// The defaults are carefully chosen for typical deployments and should rarely
// need adjustment. Changes to these values can significantly impact performance
// and resource usage.
type Tuning struct {
	// NotifierSendTimeout is the maximum time to wait when sending notifications
	// to connected clients about network changes.
	NotifierSendTimeout time.Duration

	// BatchChangeDelay controls how long to wait before sending batched updates
	// to clients when multiple changes occur in rapid succession.
	BatchChangeDelay time.Duration

	// NodeMapSessionBufferedChanSize sets the buffer size for the channel that
	// queues map updates to be sent to connected clients.
	NodeMapSessionBufferedChanSize int

	// BatcherWorkers controls the number of parallel workers processing map
	// updates for connected clients.
	BatcherWorkers int

	// RegisterCacheExpiration is how long registration cache entries remain
	// valid before being eligible for eviction.
	RegisterCacheExpiration time.Duration

	// RegisterCacheMaxEntries bounds the number of pending registration
	// entries the auth cache will hold. Older entries are evicted (LRU)
	// when the cap is reached, preventing unauthenticated cache-fill DoS.
	// A value of 0 falls back to defaultRegisterCacheMaxEntries (1024).
	RegisterCacheMaxEntries int

	// NodeStoreBatchSize controls how many write operations are accumulated
	// before rebuilding the in-memory node snapshot.
	//
	// The NodeStore batches write operations (add/update/delete nodes) before
	// rebuilding its in-memory data structures. Rebuilding involves recalculating
	// peer relationships between all nodes based on the current ACL policy, which
	// is computationally expensive and scales with the square of the number of nodes.
	//
	// By batching writes, Slopscale can process N operations but only rebuild once,
	// rather than rebuilding N times. This significantly reduces CPU usage during
	// bulk operations like initial sync or policy updates.
	//
	// Trade-off: Higher values reduce CPU usage from rebuilds but increase latency
	// for individual operations waiting for their batch to complete.
	NodeStoreBatchSize int

	// NodeStoreBatchTimeout is the maximum time to wait before processing a
	// partial batch of node operations.
	//
	// When [Tuning.NodeStoreBatchSize] operations haven't accumulated, this timeout ensures
	// writes don't wait indefinitely. The batch processes when either the size
	// threshold is reached OR this timeout expires, whichever comes first.
	//
	// Trade-off: Lower values provide faster response for individual operations
	// but trigger more frequent (expensive) peer map rebuilds. Higher values
	// optimize for bulk throughput at the cost of individual operation latency.
	NodeStoreBatchTimeout time.Duration
}

func validatePKCEMethod(method string) error {
	if method != PKCEMethodPlain && method != PKCEMethodS256 {
		return errInvalidPKCEMethod
	}

	return nil
}

// validateOIDCConfig validates the OIDC settings, called when oidc.issuer is
// set. It fails fast on a setup that cannot work: an invalid PKCE method, a
// malformed issuer URL (which would otherwise surface as an opaque discovery
// error or, worse, resolve to an unintended provider), or a missing client
// id/secret.
func validateOIDCConfig() error {
	err := validatePKCEMethod(conf.GetString("oidc.pkce.method"))
	if err != nil {
		return err
	}

	issuer := conf.GetString("oidc.issuer")

	u, err := url.Parse(issuer)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("%w: got %q", errOIDCIssuerInvalid, issuer)
	}

	if conf.GetString("oidc.client_id") == "" {
		return errOIDCClientIDRequired
	}

	if conf.GetString("oidc.client_secret") == "" && conf.GetString("oidc.client_secret_path") == "" {
		return errOIDCClientSecretRequired
	}

	return nil
}

// Domain returns the hostname/domain part of the [Config.ServerURL].
// If the [Config.ServerURL] is not a valid URL, it returns the [Config.BaseDomain].
func (c *Config) Domain() string {
	u, err := url.Parse(c.ServerURL)
	if err != nil {
		return c.BaseDomain
	}

	return u.Hostname()
}

// setNodeServiceDefaults sets the defaults for the services a node
// reaches the server for: certificates, SSH recording, Funnel, egress and
// the debug node API.
func setNodeServiceDefaults() {
	conf.SetDefault("https_certificates.enabled", false)
	conf.SetDefault("https_certificates.ttl", time.Minute)
	conf.SetDefault("https_certificates.rfc2136.tsig_algorithm", "hmac-sha256")
	conf.SetDefault("ssh_recording.enabled", false)
	conf.SetDefault("ssh_recording.dir", "/var/lib/slopscale/recordings")
	conf.SetDefault("ssh_recording.state_dir", "/var/lib/slopscale/recorder")
	conf.SetDefault("ssh_recording.max_session_bytes", 0)
	conf.SetDefault("funnel.enabled", false)
	conf.SetDefault("funnel.listen_addrs", []string{":443", ":8443", ":10000"})
	conf.SetDefault("funnel.state_dir", "/var/lib/slopscale/ingress")
	conf.SetDefault("funnel.ports", []int{443, 8443, 10000})
	conf.SetDefault("client_updates.check", true)
	conf.SetDefault("client_updates.interval", DefaultClientUpdatesInterval)
	conf.SetDefault("egress.deny_private_targets", false)
	conf.SetDefault("egress.allow_loopback_targets", false)
	conf.SetDefault("debug.node_api_enabled", false)
	conf.SetDefault("notifications.smtp.encryption", string(SMTPStartTLS))
}

// LoadConfig prepares and loads the Slopscale configuration into the conf store.
// This means it sets the default values, reads the configuration file and
// environment variables, and handles deprecated configuration options.
// It has to be called before [LoadServerConfig] and [LoadCLIConfig].
// The configuration is not validated and the caller should check for errors
// using a validation function.
func LoadConfig(path string, isFile bool) error {
	if isFile {
		conf.SetConfigFile(path)
	} else {
		conf.SetConfigName("config")

		if path == "" {
			conf.AddConfigPath("/etc/slopscale/")
			conf.AddConfigPath("$HOME/.slopscale")
			conf.AddConfigPath(".")
		} else {
			// For testing
			conf.AddConfigPath(path)
		}
	}

	// SLOPSCALE_A_B overrides a.b; see the conf package for the rules.
	conf.SetEnvPrefix("slopscale")

	conf.SetDefault("policy.mode", "file")

	conf.SetDefault("notifications.smtp.port", 587)

	setNodeServiceDefaults()

	conf.SetDefault("tls_letsencrypt_cache_dir", "/var/www/.cache")
	conf.SetDefault("tls_letsencrypt_challenge_type", HTTP01ChallengeType)

	conf.SetDefault("log.level", "info")
	conf.SetDefault("log.format", TextLogFormat)

	conf.SetDefault("dns.magic_dns", true)
	conf.SetDefault("dns.base_domain", "")
	conf.SetDefault("dns.override_local_dns", true)
	conf.SetDefault("dns.nameservers.global", []string{})
	conf.SetDefault("dns.nameservers.split", map[string]string{})
	conf.SetDefault("dns.nameservers.use_with_exit_node.global", []string{})
	conf.SetDefault("dns.nameservers.use_with_exit_node.split", map[string]string{})
	conf.SetDefault("dns.search_domains", []string{})

	conf.SetDefault("derp.server.enabled", true)
	conf.SetDefault("derp.server.region_id", 999)
	conf.SetDefault("derp.server.region_code", "slopscale")
	conf.SetDefault("derp.server.region_name", "Slopscale Embedded DERP")
	conf.SetDefault("derp.server.verify_clients", true)
	conf.SetDefault("derp.server.stun_enabled", true)
	conf.SetDefault("derp.server.stun_listen_addr", "0.0.0.0:3478")
	conf.SetDefault("derp.server.automatically_add_embedded_derp_region", true)
	conf.SetDefault("derp.urls", []string{TailscaleDERPMapURL})
	conf.SetDefault("derp.auto_update_enabled", true)
	conf.SetDefault("derp.update_frequency", "3h")

	conf.SetDefault("unix_socket", "/var/run/slopscale/slopscale.sock")
	conf.SetDefault("unix_socket_permission", "0o770")

	conf.SetDefault("cli.timeout", "5s")
	conf.SetDefault("cli.insecure", false)

	conf.SetDefault("database.postgres.ssl", false)
	conf.SetDefault("database.postgres.max_open_conns", 10)
	conf.SetDefault("database.postgres.max_idle_conns", 10)
	conf.SetDefault("database.postgres.conn_max_idle_time_secs", 3600)

	conf.SetDefault("database.sqlite.write_ahead_log", true)
	conf.SetDefault("database.sqlite.wal_autocheckpoint", 1000) // SQLite default

	conf.SetDefault("oidc.scope", []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail})
	conf.SetDefault("oidc.only_start_if_oidc_is_available", true)
	conf.SetDefault("oidc.use_expiry_from_token", false)
	conf.SetDefault("oidc.pkce.enabled", false)
	conf.SetDefault("oidc.pkce.method", "S256")
	conf.SetDefault("oidc.email_verified_required", true)

	conf.SetDefault("logtail.enabled", false)
	conf.SetDefault("taildrop.enabled", true)
	conf.SetDefault("auto_update.enabled", false)

	conf.SetDefault("node.expiry", "0")
	conf.SetDefault("node.ephemeral.inactivity_timeout", "120s")
	conf.SetDefault("preauth_keys.revoked_retention", "168h")
	conf.SetDefault("audit.retention", "0")
	conf.SetDefault("node.routes.ha.probe_interval", "10s")
	conf.SetDefault("node.routes.ha.probe_timeout", "5s")

	conf.SetDefault("tuning.notifier_send_timeout", "800ms")
	conf.SetDefault("tuning.batch_change_delay", "800ms")
	conf.SetDefault("tuning.node_mapsession_buffered_chan_size", 30)
	conf.SetDefault("tuning.node_store_batch_size", defaultNodeStoreBatchSize)
	conf.SetDefault("tuning.node_store_batch_timeout", "500ms")

	conf.SetDefault("prefixes.allocation", string(IPAllocationStrategySequential))

	err := conf.ReadInConfig()
	if err != nil {
		if errors.Is(err, conf.ErrConfigFileNotFound) {
			log.Warn().Msg("no config file found, using defaults")
			return nil
		}

		return fmt.Errorf("fatal error reading config file: %w", err)
	}

	return nil
}

// resolveEphemeralInactivityTimeout resolves the ephemeral inactivity timeout
// from config, supporting both the new key (node.ephemeral.inactivity_timeout)
// and the old key (ephemeral_node_inactivity_timeout) for backwards compatibility.
//
// Both keys are read explicitly rather than aliased so a value written
// under the new key in the config file is never shadowed by the old one.
func resolveEphemeralInactivityTimeout() time.Duration {
	// New key takes precedence if explicitly set in config.
	if conf.IsSet("node.ephemeral.inactivity_timeout") &&
		conf.GetString("node.ephemeral.inactivity_timeout") != "" {
		return conf.GetDuration("node.ephemeral.inactivity_timeout")
	}

	// Fall back to old key for backwards compatibility.
	if conf.IsSet("ephemeral_node_inactivity_timeout") {
		return conf.GetDuration("ephemeral_node_inactivity_timeout")
	}

	// Default
	return conf.GetDuration("node.ephemeral.inactivity_timeout")
}

// resolveNodeExpiry parses the node.expiry config value.
// Returns 0 if set to "0" (no default expiry) or on parse failure.
func resolveNodeExpiry() time.Duration {
	value := conf.GetString("node.expiry")
	if value == "" || value == "0" {
		return 0
	}

	expiry, err := model.ParseDuration(value)
	if err != nil {
		log.Warn().
			Str("value", value).
			Msg("failed to parse node.expiry, defaulting to no expiry")

		return 0
	}

	return time.Duration(expiry)
}

func validateServerConfig() error {
	depr := deprecator{
		warns:  make(set.Set[string]),
		fatals: make(set.Set[string]),
	}

	// Deprecated keys are checked after the file is read.
	// Alias the old ACL Policy path with the new configuration option.
	depr.fatalIfNewKeyIsNotUsed("policy.path", "acl_policy_path")

	// Move dns_config -> dns
	depr.fatalIfNewKeyIsNotUsed("dns.magic_dns", "dns_config.magic_dns")
	depr.fatalIfNewKeyIsNotUsed("dns.base_domain", "dns_config.base_domain")
	depr.fatalIfNewKeyIsNotUsed("dns.override_local_dns", "dns_config.override_local_dns")
	depr.fatalIfNewKeyIsNotUsed("dns.nameservers.global", "dns_config.nameservers")
	depr.fatalIfNewKeyIsNotUsed("dns.nameservers.split", "dns_config.restricted_nameservers")
	depr.fatalIfNewKeyIsNotUsed("dns.search_domains", "dns_config.domains")
	depr.fatalIfNewKeyIsNotUsed("dns.extra_records", "dns_config.extra_records")
	depr.fatal("dns.use_username_in_magic_dns")
	depr.fatal("dns_config.use_username_in_magic_dns")

	// Removed since version v0.26.0
	depr.fatal("oidc.strip_email_domain")
	depr.fatal("oidc.map_legacy_users")

	// Removed since v0.29.0: `randomize_client_port` moved to the ACL
	// policy as a top-level `randomizeClientPort` field, matching the
	// Tailscale-hosted control plane schema. Per-node `nodeAttrs`
	// entries granting `https://tailscale.com/cap/randomize-client-port`
	// also work.
	depr.fatalWithHint("randomize_client_port",
		`Set "randomizeClientPort": true at the top level of your policy file `+
			`(see policy.path / policy.mode), or grant the cap per-node via a `+
			`"nodeAttrs" entry. See CHANGELOG.md (BREAKING / Configuration).`)

	// Deprecated: ephemeral_node_inactivity_timeout -> node.ephemeral.inactivity_timeout
	depr.warnNoAlias("node.ephemeral.inactivity_timeout", "ephemeral_node_inactivity_timeout")

	// Removed: oidc.expiry -> node.expiry
	depr.fatalIfSet("oidc.expiry", "node.expiry")

	// OIDC is activated by setting oidc.issuer (see app.go), not by a
	// dedicated oidc.enabled key. Gate validation on the real activation
	// condition so a misconfiguration fails at startup.
	if conf.GetString("oidc.issuer") != "" {
		err := validateOIDCConfig()
		if err != nil {
			return err
		}
	}

	depr.Log()

	if conf.IsSet("dns.extra_records") && conf.IsSet("dns.extra_records_path") {
		log.Fatal().
			Msg("fatal config error: dns.extra_records and dns.extra_records_path are mutually exclusive. " +
				"Remove one of them from the config file")
	}

	// Collect any validation errors and return them all at once
	var errorText string
	if (conf.GetString("tls_letsencrypt_hostname") != "") &&
		((conf.GetString("tls_cert_path") != "") || (conf.GetString("tls_key_path") != "")) {
		errorText += "Fatal config error: set either tls_letsencrypt_hostname or tls_cert_path/tls_key_path, not both\n"
	}

	if conf.GetString("noise.private_key_path") == "" {
		errorText += "Fatal config error: slopscale now requires a new `noise.private_key_path` field in the config " +
			"file for the Tailscale v2 protocol\n"
	}

	if (conf.GetString("tls_letsencrypt_hostname") != "") &&
		(conf.GetString("tls_letsencrypt_challenge_type") == TLSALPN01ChallengeType) &&
		(!strings.HasSuffix(conf.GetString("listen_addr"), ":443")) {
		// this is only a warning because there could be something sitting in front of
		// slopscale that redirects the traffic (e.g. an iptables rule)
		log.Warn().
			Msg("Warning: when using tls_letsencrypt_hostname with TLS-ALPN-01 as challenge type, " +
				"slopscale must be reachable on port 443, i.e. listen_addr should probably end in :443")
	}

	if (conf.GetString("tls_letsencrypt_challenge_type") != HTTP01ChallengeType) &&
		(conf.GetString("tls_letsencrypt_challenge_type") != TLSALPN01ChallengeType) {
		errorText += "Fatal config error: the only supported values for tls_letsencrypt_challenge_type are " +
			"HTTP-01 and TLS-ALPN-01\n"
	}

	if !strings.HasPrefix(conf.GetString("server_url"), "http://") &&
		!strings.HasPrefix(conf.GetString("server_url"), "https://") {
		errorText += "Fatal config error: server_url must start with https:// or http://\n"
	}

	// Minimum inactivity time out is keepalive timeout (60s) plus a few seconds
	// to avoid races
	const minInactivityTimeout = 65 * time.Second

	ephemeralTimeout := resolveEphemeralInactivityTimeout()
	if ephemeralTimeout <= minInactivityTimeout {
		errorText += fmt.Sprintf(
			"Fatal config error: node.ephemeral.inactivity_timeout (%s) is set too low, must be more than %s",
			ephemeralTimeout,
			minInactivityTimeout,
		)
	}

	if conf.GetBool("dns.override_local_dns") {
		if global := conf.GetStringSlice("dns.nameservers.global"); len(global) == 0 {
			errorText += "Fatal config error: dns.nameservers.global must be set when dns.override_local_dns is true\n"
		}
	}

	errorText += useWithExitNodeConfigErrors()

	// Validate HA health probing parameters
	if haInterval := conf.GetDuration(
		"node.routes.ha.probe_interval",
	); haInterval > 0 {
		if haInterval < 2*time.Second {
			errorText += fmt.Sprintf(
				"Fatal config error: node.routes.ha.probe_interval (%s) must be >= 2s\n",
				haInterval,
			)
		}

		haTimeout := conf.GetDuration("node.routes.ha.probe_timeout")
		if haTimeout < 1*time.Second {
			errorText += fmt.Sprintf(
				"Fatal config error: node.routes.ha.probe_timeout (%s) must be >= 1s\n",
				haTimeout,
			)
		}

		if haTimeout >= haInterval {
			errorText += fmt.Sprintf(
				"Fatal config error: node.routes.ha.probe_timeout (%s) must be less than "+
					"node.routes.ha.probe_interval (%s)\n",
				haTimeout,
				haInterval,
			)
		}
	}

	// Validate tuning parameters
	if size := conf.GetInt("tuning.node_store_batch_size"); size <= 0 {
		errorText += fmt.Sprintf(
			"Fatal config error: tuning.node_store_batch_size must be positive, got %d\n",
			size,
		)
	}

	if timeout := conf.GetDuration("tuning.node_store_batch_timeout"); timeout <= 0 {
		errorText += fmt.Sprintf(
			"Fatal config error: tuning.node_store_batch_timeout must be positive, got %s\n",
			timeout,
		)
	}

	if errorText != "" {
		//nolint:err113 // aggregated validation text, not a sentinel
		return errors.New(strings.TrimSuffix(errorText, "\n"))
	}

	return nil
}

func tlsConfig() TLSConfig {
	return TLSConfig{
		LetsEncrypt: LetsEncryptConfig{
			Hostname: conf.GetString("tls_letsencrypt_hostname"),
			Listen:   conf.GetString("tls_letsencrypt_listen"),
			CacheDir: util.AbsolutePathFromConfigPath(
				conf.GetString("tls_letsencrypt_cache_dir"),
			),
			ChallengeType: conf.GetString("tls_letsencrypt_challenge_type"),
		},
		CertPath: util.AbsolutePathFromConfigPath(
			conf.GetString("tls_cert_path"),
		),
		KeyPath: util.AbsolutePathFromConfigPath(
			conf.GetString("tls_key_path"),
		),
	}
}

func derpConfig() DERPConfig {
	serverEnabled := conf.GetBool("derp.server.enabled")
	serverRegionID := conf.GetInt64("derp.server.region_id")
	serverRegionCode := conf.GetString("derp.server.region_code")
	serverRegionName := conf.GetString("derp.server.region_name")
	serverVerifyClients := conf.GetBool("derp.server.verify_clients")
	stunEnabled := conf.GetBool("derp.server.stun_enabled")
	stunAddr := conf.GetString("derp.server.stun_listen_addr")
	privateKeyPath := util.AbsolutePathFromConfigPath(
		conf.GetString("derp.server.private_key_path"),
	)
	// The relay key lives next to the noise key unless the file says
	// otherwise, so the embedded relay works with no derp section at all.
	if privateKeyPath == "" {
		if noisePath := util.AbsolutePathFromConfigPath(conf.GetString("noise.private_key_path")); noisePath != "" {
			privateKeyPath = filepath.Join(filepath.Dir(noisePath), "derp_server_private.key")
		}
	}

	ipv4 := conf.GetString("derp.server.ipv4")
	ipv6 := conf.GetString("derp.server.ipv6")
	automaticallyAddEmbeddedDerpRegion := conf.GetBool(
		"derp.server.automatically_add_embedded_derp_region",
	)

	if serverEnabled && stunEnabled && stunAddr == "" {
		log.Fatal().
			Msg("derp.server.stun_listen_addr must be set if derp.server.enabled and derp.server.stun_enabled are true")
	}

	urlStrs := conf.GetStringSlice("derp.urls")

	urls := make([]url.URL, 0, len(urlStrs))
	for _, urlStr := range urlStrs {
		urlAddr, err := url.Parse(urlStr)
		if err != nil {
			log.Error().
				Caller().
				Str("url", urlStr).
				Err(err).
				Msg("Failed to parse url, ignoring...")

			continue
		}

		urls = append(urls, *urlAddr)
	}

	paths := conf.GetStringSlice("derp.paths")

	if serverEnabled && !automaticallyAddEmbeddedDerpRegion && len(paths) == 0 {
		log.Fatal().
			Msg("Disabling derp.server.automatically_add_embedded_derp_region requires to configure " +
				"the derp server in derp.paths")
	}

	autoUpdate := conf.GetBool("derp.auto_update_enabled")
	updateFrequency := conf.GetDuration("derp.update_frequency")

	return DERPConfig{
		ServerEnabled:                      serverEnabled,
		ServerRegionID:                     tailcfg.DERPRegionID(serverRegionID),
		ServerRegionCode:                   serverRegionCode,
		ServerRegionName:                   serverRegionName,
		ServerVerifyClients:                serverVerifyClients,
		ServerPrivateKeyPath:               privateKeyPath,
		STUNEnabled:                        stunEnabled,
		STUNAddr:                           stunAddr,
		URLs:                               urls,
		Paths:                              paths,
		AutoUpdate:                         autoUpdate,
		UpdateFrequency:                    updateFrequency,
		IPv4:                               ipv4,
		IPv6:                               ipv6,
		AutomaticallyAddEmbeddedDerpRegion: automaticallyAddEmbeddedDerpRegion,
	}
}

func logtailConfig() LogTailConfig {
	enabled := conf.GetBool("logtail.enabled")

	return LogTailConfig{
		Enabled: enabled,
	}
}

func policyConfig() PolicyConfig {
	policyPath := conf.GetString("policy.path")
	policyMode := conf.GetString("policy.mode")

	return PolicyConfig{
		Path:          policyPath,
		Mode:          PolicyMode(policyMode),
		GeoIPDatabase: conf.GetString("policy.geoip_database"),
	}
}

func logConfig() LogConfig {
	logLevelStr := conf.GetString("log.level")

	logLevel, err := zerolog.ParseLevel(logLevelStr)
	if err != nil {
		logLevel = zerolog.DebugLevel
	}

	logFormatOpt := conf.GetString("log.format")

	var logFormat string

	switch logFormatOpt {
	case JSONLogFormat:
		logFormat = JSONLogFormat
	case TextLogFormat:
		logFormat = TextLogFormat
	case "":
		logFormat = TextLogFormat
	default:
		log.Error().
			Caller().
			Str("func", "GetLogConfig").
			Msgf("Could not parse log format: %s. Valid choices are 'json' or 'text'", logFormatOpt)
	}

	return LogConfig{
		Format: logFormat,
		Level:  logLevel,
	}
}

// queryLogConfig reads database.query_log. The pre-jet database.gorm keys
// are honoured with a deprecation warning so existing configs keep working.
func queryLogConfig(enabled bool) QueryLogConfig {
	cfg := QueryLogConfig{Enabled: enabled}

	if conf.IsSet("database.gorm") {
		log.Warn().Msg("database.gorm is deprecated, move its settings to database.query_log")

		cfg.SlowThreshold = time.Duration(conf.GetInt64("database.gorm.slow_threshold")) * time.Millisecond
		cfg.LogNotFound = !conf.GetBool("database.gorm.skip_err_record_not_found")
		cfg.Parameterized = conf.GetBool("database.gorm.parameterized_queries")
	}

	if conf.IsSet("database.query_log.slow_threshold") {
		cfg.SlowThreshold = time.Duration(conf.GetInt64("database.query_log.slow_threshold")) * time.Millisecond
	}

	if conf.IsSet("database.query_log.log_not_found") {
		cfg.LogNotFound = conf.GetBool("database.query_log.log_not_found")
	}

	if conf.IsSet("database.query_log.parameterized") {
		cfg.Parameterized = conf.GetBool("database.query_log.parameterized")
	}

	return cfg
}

func databaseConfig() DatabaseConfig {
	debug := conf.GetBool("database.debug")

	dbType := conf.GetString("database.type")

	queryLog := queryLogConfig(debug)

	switch dbType {
	case DatabaseSqlite, DatabasePostgres:
		break
	case "sqlite":
		dbType = "sqlite3"
	default:
		log.Fatal().
			Msgf("invalid database type %q, must be sqlite, sqlite3 or postgres", dbType)
	}

	return DatabaseConfig{
		Type:     dbType,
		Debug:    debug,
		QueryLog: queryLog,
		Sqlite: SqliteConfig{
			Path: util.AbsolutePathFromConfigPath(
				conf.GetString("database.sqlite.path"),
			),
			WriteAheadLog:     conf.GetBool("database.sqlite.write_ahead_log"),
			WALAutoCheckPoint: conf.GetInt("database.sqlite.wal_autocheckpoint"),
		},
		Postgres: PostgresConfig{
			Host:               conf.GetString("database.postgres.host"),
			Port:               conf.GetInt("database.postgres.port"),
			Name:               conf.GetString("database.postgres.name"),
			User:               conf.GetString("database.postgres.user"),
			Pass:               conf.GetString("database.postgres.pass"),
			Ssl:                conf.GetString("database.postgres.ssl"),
			MaxOpenConnections: conf.GetInt("database.postgres.max_open_conns"),
			MaxIdleConnections: conf.GetInt("database.postgres.max_idle_conns"),
			ConnMaxIdleTimeSecs: conf.GetInt(
				"database.postgres.conn_max_idle_time_secs",
			),
		},
	}
}

func dns() (DNSConfig, error) {
	var dns DNSConfig

	// Read key by key rather than unmarshalled so environment overrides
	// apply to each field.
	dns.MagicDNS = conf.GetBool("dns.magic_dns")
	dns.BaseDomain = conf.GetString("dns.base_domain")
	dns.OverrideLocalDNS = conf.GetBool("dns.override_local_dns")
	dns.Nameservers.Global = conf.GetStringSlice("dns.nameservers.global")
	dns.Nameservers.Split = conf.GetStringMapStringSlice("dns.nameservers.split")
	// Unset stays nil, so a config without the key compares equal to one
	// built from the settings API.
	if global := conf.GetStringSlice("dns.nameservers.use_with_exit_node.global"); len(global) > 0 {
		dns.Nameservers.UseWithExitNode = global
	}

	if split := conf.GetStringMapStringSlice("dns.nameservers.use_with_exit_node.split"); len(split) > 0 {
		dns.Nameservers.SplitUseWithExitNode = split
	}

	dns.SearchDomains = conf.GetStringSlice("dns.search_domains")
	dns.ExtraRecordsPath = conf.GetString("dns.extra_records_path")

	if conf.IsSet("dns.extra_records") {
		var extraRecords []tailcfg.DNSRecord

		err := conf.UnmarshalKey("dns.extra_records", &extraRecords)
		if err != nil {
			return DNSConfig{}, fmt.Errorf("unmarshalling dns extra records: %w", err)
		}

		dns.ExtraRecords = NormalizeExtraRecords(extraRecords)
	}

	return dns, nil
}

// parseResolvers turns the nameservers of the config file into resolvers
// with [ParseResolver]. An entry the client could not use is logged and
// left out; the API validates the same rule up front instead. When domain
// is non-empty, it is included in the warning.
func parseResolvers(nameservers []string, domain string, useWithExitNode []string) []*dnstype.Resolver {
	var resolvers []*dnstype.Resolver

	for _, nsStr := range nameservers {
		resolver, err := ParseResolver(nsStr)
		if err != nil {
			e := log.Warn().Err(err).Str("nameserver", nsStr)
			if domain != "" {
				e = e.Str("domain", domain)
			}

			e.Msg("unusable nameserver, ignoring")

			continue
		}

		resolver.UseWithExitNode = slices.Contains(useWithExitNode, nsStr)

		resolvers = append(resolvers, resolver)
	}

	return resolvers
}

// globalResolvers returns the global DNS resolvers
// defined in the config file.
func (d *DNSConfig) globalResolvers() []*dnstype.Resolver {
	return parseResolvers(d.Nameservers.Global, "", d.Nameservers.UseWithExitNode)
}

// splitResolvers returns a map of domain to DNS resolvers.
func (d *DNSConfig) splitResolvers() map[string][]*dnstype.Resolver {
	routes := make(map[string][]*dnstype.Resolver)

	for domain, nameservers := range d.Nameservers.Split {
		routes[domain] = parseResolvers(nameservers, domain, d.Nameservers.SplitUseWithExitNode[domain])
	}

	return routes
}

// useWithExitNodeConfigErrors checks dns.nameservers.use_with_exit_node
// against the nameservers it refers to, the way [DNSSettings.Validate]
// does for the runtime settings.
func useWithExitNodeConfigErrors() string {
	settings := DNSSettings{
		Nameservers:          conf.GetStringSlice("dns.nameservers.global"),
		OverrideLocalDNS:     conf.GetBool("dns.override_local_dns"),
		SplitNameservers:     conf.GetStringMapStringSlice("dns.nameservers.split"),
		UseWithExitNode:      conf.GetStringSlice("dns.nameservers.use_with_exit_node.global"),
		SplitUseWithExitNode: conf.GetStringMapStringSlice("dns.nameservers.use_with_exit_node.split"),
	}

	err := settings.validateUseWithExitNode()
	if err != nil {
		return "Fatal config error: " + err.Error() + "\n"
	}

	return ""
}

func dnsToTailcfgDNS(dns DNSConfig) *tailcfg.DNSConfig {
	cfg := tailcfg.DNSConfig{}

	if dns.BaseDomain == "" && dns.MagicDNS {
		log.Fatal().Msg("dns.base_domain must be set when using MagicDNS (dns.magic_dns)")
	}

	cfg.Proxied = dns.MagicDNS

	cfg.ExtraRecords = dns.ExtraRecords
	if dns.OverrideLocalDNS {
		cfg.Resolvers = dns.globalResolvers()
	} else {
		cfg.FallbackResolvers = dns.globalResolvers()
	}

	routes := dns.splitResolvers()

	cfg.Routes = routes
	if dns.BaseDomain != "" {
		cfg.Domains = []string{dns.BaseDomain}
	}

	cfg.Domains = append(cfg.Domains, dns.SearchDomains...)

	return &cfg
}

// warnBanner prints a highly visible warning banner to the log output.
// It wraps the provided lines in an ASCII-art box with a "Warning!" header.
// This is intended for critical configuration issues that users must not ignore.
func warnBanner(lines []string) {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString("################################################################\n")
	b.WriteString("###      __          __              _             _         ###\n")
	b.WriteString("###      \\ \\        / /             (_)           | |        ###\n")
	b.WriteString("###       \\ \\  /\\  / /_ _ _ __ _ __  _ _ __   __ _| |        ###\n")
	b.WriteString("###        \\ \\/  \\/ / _` | '__| '_ \\| | '_ \\ / _` | |        ###\n")
	b.WriteString("###         \\  /\\  / (_| | |  | | | | | | | | (_| |_|        ###\n")
	b.WriteString("###          \\/  \\/ \\__,_|_|  |_| |_|_|_| |_|\\__, (_)        ###\n")
	b.WriteString("###                                           __/ |          ###\n")
	b.WriteString("###                                          |___/           ###\n")
	b.WriteString("################################################################\n")
	b.WriteString("###                                                          ###\n")

	for _, line := range lines {
		fmt.Fprintf(&b, "###  %-54s  ###\n", line)
	}

	b.WriteString("###                                                          ###\n")
	b.WriteString("################################################################")

	log.Warn().Msg(b.String())
}

func parsePrefixConfig(key string, standardRange netip.Prefix, family string) (*netip.Prefix, bool, error) {
	s := conf.GetString(key)

	if s == "" {
		return nil, false, nil
	}

	prefix, err := netip.ParsePrefix(s)
	if err != nil {
		return nil, false, fmt.Errorf("parsing %s prefix from config: %w", family, err)
	}

	builder := netipx.IPSetBuilder{}
	builder.AddPrefix(standardRange)

	ipSet, _ := builder.IPSet()

	return &prefix, !ipSet.ContainsPrefix(prefix), nil
}

// trustedProxies rejects 0.0.0.0/0 and ::/0 because they defeat the
// peer-trust gate and almost always indicate misconfiguration.
func trustedProxies() ([]netip.Prefix, error) {
	raw := conf.GetStringSlice("trusted_proxies")
	if len(raw) == 0 {
		return nil, nil
	}

	out := make([]netip.Prefix, 0, len(raw))
	for i, s := range raw {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("trusted_proxies[%d] %q: %w", i, s, err)
		}

		if p.Bits() == 0 {
			return nil, fmt.Errorf("trusted_proxies[%d] %q: %w", i, s, errTrustedProxyZeroRange)
		}

		out = append(out, p.Masked())
	}

	return out, nil
}

// LoadCLIConfig returns the needed configuration for the CLI client
// of Slopscale to connect to a Slopscale server.
func LoadCLIConfig() (*Config, error) {
	logConfig := logConfig()
	zerolog.SetGlobalLevel(logConfig.Level)

	return &Config{
		DisableUpdateCheck: conf.GetBool("disable_check_updates"),
		UnixSocket:         conf.GetString("unix_socket"),
		CLI: CLIConfig{
			Address:  conf.GetString("cli.address"),
			APIKey:   conf.GetString("cli.api_key"),
			Timeout:  conf.GetDuration("cli.timeout"),
			Insecure: conf.GetBool("cli.insecure"),
		},
		Log: logConfig,
	}, nil
}

// oidcConfig reads the identity provider settings; the client secret
// comes from the file at oidc.client_secret_path when one is set.
func oidcConfig() (OIDCConfig, error) {
	clientSecret := conf.GetString("oidc.client_secret")

	clientSecretPath := conf.GetString("oidc.client_secret_path")
	if clientSecretPath != "" && clientSecret != "" {
		return OIDCConfig{}, errOidcMutuallyExclusive
	}

	if clientSecretPath != "" {
		secretPath := os.ExpandEnv(clientSecretPath)

		secretBytes, err := os.ReadFile(secretPath)
		if err != nil {
			return OIDCConfig{}, fmt.Errorf("reading OIDC client secret from %q: %w", secretPath, err)
		}

		clientSecret = strings.TrimSpace(string(secretBytes))
	}

	return OIDCConfig{
		OnlyStartIfOIDCIsAvailable: conf.GetBool(
			"oidc.only_start_if_oidc_is_available",
		),
		Issuer:         conf.GetString("oidc.issuer"),
		ClientID:       conf.GetString("oidc.client_id"),
		ClientSecret:   clientSecret,
		Scope:          conf.GetStringSlice("oidc.scope"),
		ExtraParams:    conf.GetStringMapString("oidc.extra_params"),
		AllowedDomains: conf.GetStringSlice("oidc.allowed_domains"),
		AllowedUsers:   conf.GetStringSlice("oidc.allowed_users"),
		AllowedGroups:  conf.GetStringSlice("oidc.allowed_groups"),
		AdminUsers:     conf.GetStringSlice("oidc.admin_users"),
		Groups: OIDCGroupsConfig{
			Sync:   conf.GetBool("oidc.groups.sync"),
			Prefix: conf.GetString("oidc.groups.prefix"),
		},
		MatchByEmail:          conf.GetBool("oidc.match_by_email"),
		EmailVerifiedRequired: conf.GetBool("oidc.email_verified_required"),
		UseExpiryFromToken:    conf.GetBool("oidc.use_expiry_from_token"),
		PKCE: PKCEConfig{
			Enabled: conf.GetBool("oidc.pkce.enabled"),
			Method:  conf.GetString("oidc.pkce.method"),
		},
	}, nil
}

// LoadServerConfig returns the full Slopscale configuration to
// host a Slopscale server. This is called as part of `slopscale serve`.
//
//nolint:funlen // legacy: one linear read of every config key; splitting it would only scatter the key list
func LoadServerConfig() (*Config, error) {
	err := validateServerConfig()
	if err != nil {
		return nil, err
	}

	logConfig := logConfig()
	zerolog.SetGlobalLevel(logConfig.Level)

	prefix4, v4NonStandard, err := parsePrefixConfig("prefixes.v4", tsaddr.CGNATRange(), "IPv4")
	if err != nil {
		return nil, err
	}

	prefix6, v6NonStandard, err := parsePrefixConfig("prefixes.v6", tsaddr.TailscaleULARange(), "IPv6")
	if err != nil {
		return nil, err
	}

	trusted, err := trustedProxies()
	if err != nil {
		return nil, err
	}

	if prefix4 == nil && prefix6 == nil {
		return nil, ErrNoPrefixConfigured
	}

	if v4NonStandard || v6NonStandard {
		warnBanner([]string{
			"You have overridden the default Slopscale IP prefixes",
			"with a range outside of the standard CGNAT and/or ULA",
			"ranges. This is NOT a supported configuration.",
			"",
			"Using subsets of the default ranges (100.64.0.0/10 for",
			"IPv4, fd7a:115c:a1e0::/48 for IPv6) is fine. Using",
			"ranges outside of these will cause undefined behaviour",
			"as the Tailscale client is NOT designed to operate on",
			"any other ranges.",
			"",
			"Set the prefixes back to subsets of the standard",
			"ranges as described in the example configuration.",
			"",
			"Any issue raised using a range outside of the",
			"supported range will be labelled as wontfix",
			"and closed.",
		})
	}

	allocStr := conf.GetString("prefixes.allocation")

	var alloc IPAllocationStrategy

	switch allocStr {
	case string(IPAllocationStrategySequential):
		alloc = IPAllocationStrategySequential
	case string(IPAllocationStrategyRandom):
		alloc = IPAllocationStrategyRandom
	default:
		return nil, fmt.Errorf(
			"%w: %q, allowed options: %s, %s",
			ErrInvalidAllocationStrategy,
			allocStr,
			IPAllocationStrategySequential,
			IPAllocationStrategyRandom,
		)
	}

	dnsConfig, err := dns()
	if err != nil {
		return nil, err
	}

	derpConfig := derpConfig()
	logTailConfig := logtailConfig()

	smtp, err := smtpConfig()
	if err != nil {
		return nil, err
	}

	oidcCfg, err := oidcConfig()
	if err != nil {
		return nil, err
	}

	httpsCerts, err := httpsCertsConfig()
	if err != nil {
		return nil, err
	}

	funnel, err := funnelConfig()
	if err != nil {
		return nil, err
	}

	clientUpdates, err := clientUpdatesConfig()
	if err != nil {
		return nil, err
	}

	dialPlan, err := controlDialPlanConfig()
	if err != nil {
		return nil, err
	}

	serverURL := conf.GetString("server_url")

	// BaseDomain cannot be the same as the server URL.
	// This is because Tailscale takes over the domain in BaseDomain,
	// causing the slopscale server and DERP to be unreachable.
	// For Tailscale upstream, the following is true:
	// - DERP run on their own domains
	// - Control plane runs on login.tailscale.com/controlplane.tailscale.com
	// - MagicDNS (BaseDomain) for users is on a *.ts.net domain per tailnet (e.g. tail-scale.ts.net)
	if dnsConfig.BaseDomain != "" {
		err := isSafeServerURL(serverURL, dnsConfig.BaseDomain)
		if err != nil {
			return nil, err
		}
	}

	return &Config{
		ServerURL:          serverURL,
		Addr:               conf.GetString("listen_addr"),
		MetricsAddr:        conf.GetString("metrics_listen_addr"),
		TrustedProxies:     trusted,
		DisableUpdateCheck: false,

		PrefixV4:     prefix4,
		PrefixV6:     prefix6,
		IPAllocation: alloc,

		NoisePrivateKeyPath: util.AbsolutePathFromConfigPath(
			conf.GetString("noise.private_key_path"),
		),
		BaseDomain: dnsConfig.BaseDomain,

		DERP: derpConfig,

		Node: NodeConfig{
			Expiry: resolveNodeExpiry(),
			Ephemeral: EphemeralConfig{
				InactivityTimeout: resolveEphemeralInactivityTimeout(),
			},
			Routes: RouteConfig{
				HA: HARouteConfig{
					ProbeInterval: conf.GetDuration("node.routes.ha.probe_interval"),
					ProbeTimeout:  conf.GetDuration("node.routes.ha.probe_timeout"),
				},
			},
		},

		PreAuthKeys: PreAuthKeysConfig{
			RevokedRetention: conf.GetDuration("preauth_keys.revoked_retention"),
		},

		Audit: AuditConfig{
			Retention: conf.GetDuration("audit.retention"),
		},

		Database: databaseConfig(),

		TLS: tlsConfig(),

		DNSConfig:        dnsConfig,
		TailcfgDNSConfig: dnsToTailcfgDNS(dnsConfig),

		ACMEEmail: conf.GetString("acme_email"),
		ACMEURL:   conf.GetString("acme_url"),

		UnixSocket:           conf.GetString("unix_socket"),
		UnixSocketPermission: util.GetFileMode("unix_socket_permission"),

		OIDC: oidcCfg,

		LogTail: logTailConfig,
		Taildrop: TaildropConfig{
			Enabled: conf.GetBool("taildrop.enabled"),
		},
		AutoUpdate: AutoUpdateConfig{
			Enabled: conf.GetBool("auto_update.enabled"),
		},

		Policy: policyConfig(),

		SMTP: smtp,

		SSHRecording: sshRecordingConfig(),

		Funnel: funnel,

		ClientUpdates: clientUpdates,

		ControlDialPlan: dialPlan,

		Egress: egressConfig(),

		Debug: debugConfig(),

		HTTPSCerts: httpsCerts,

		CLI: CLIConfig{
			Address:  conf.GetString("cli.address"),
			APIKey:   conf.GetString("cli.api_key"),
			Timeout:  conf.GetDuration("cli.timeout"),
			Insecure: conf.GetBool("cli.insecure"),
		},

		Log: logConfig,

		Tuning: Tuning{
			NotifierSendTimeout: conf.GetDuration("tuning.notifier_send_timeout"),
			BatchChangeDelay:    conf.GetDuration("tuning.batch_change_delay"),
			NodeMapSessionBufferedChanSize: conf.GetInt(
				"tuning.node_mapsession_buffered_chan_size",
			),
			BatcherWorkers: func() int {
				if workers := conf.GetInt("tuning.batcher_workers"); workers > 0 {
					return workers
				}

				return DefaultBatcherWorkers()
			}(),
			RegisterCacheExpiration: conf.GetDuration("tuning.register_cache_expiration"),
			RegisterCacheMaxEntries: conf.GetInt("tuning.register_cache_max_entries"),
			NodeStoreBatchSize:      conf.GetInt("tuning.node_store_batch_size"),
			NodeStoreBatchTimeout:   conf.GetDuration("tuning.node_store_batch_timeout"),
		},
	}, nil
}

// BaseDomain cannot be a suffix of the server URL.
// This is because Tailscale takes over the domain in BaseDomain,
// causing the slopscale server and DERP to be unreachable.
// For Tailscale upstream, the following is true:
// - DERP run on their own domains.
// - Control plane runs on login.tailscale.com/controlplane.tailscale.com.
// - MagicDNS (BaseDomain) for users is on a *.ts.net domain per tailnet (e.g. tail-scale.ts.net).
func isSafeServerURL(serverURL, baseDomain string) error {
	server, err := url.Parse(serverURL)
	if err != nil {
		return fmt.Errorf("parsing server URL %q: %w", serverURL, err)
	}

	if server.Hostname() == baseDomain {
		return errServerURLSame
	}

	if strings.HasSuffix(server.Hostname(), "."+baseDomain) {
		return errServerURLSuffix
	}

	return nil
}

type deprecator struct {
	warns  set.Set[string]
	fatals set.Set[string]
}

func (d *deprecator) String() string {
	var b strings.Builder

	for _, w := range d.warns.Slice() {
		fmt.Fprintf(&b, "WARN: %s\n", w)
	}

	for _, f := range d.fatals.Slice() {
		fmt.Fprintf(&b, "FATAL: %s\n", f)
	}

	return b.String()
}

func (d *deprecator) Log() {
	if len(d.fatals) > 0 {
		log.Fatal().Msg("\n" + d.String())
	} else if len(d.warns) > 0 {
		log.Warn().Msg("\n" + d.String())
	}
}

// fatal deprecates and adds an entry to the fatal list of options if the oldKey is set.
func (d *deprecator) fatal(oldKey string) {
	if conf.IsSet(oldKey) {
		d.fatals.Add(
			fmt.Sprintf(
				"The %q configuration key has been removed. See the changelog for details.",
				oldKey,
			),
		)
	}
}

// fatalWithHint behaves like fatal but appends a remediation pointer to
// the message so operators see exactly what to do without leaving the
// terminal. Use it when the removed key has a clean replacement on the
// policy side.
func (d *deprecator) fatalWithHint(oldKey, hint string) {
	if conf.IsSet(oldKey) {
		d.fatals.Add(
			fmt.Sprintf(
				"The %q configuration key has been removed. %s",
				oldKey,
				hint,
			),
		)
	}
}

// fatalIfNewKeyIsNotUsed deprecates and adds an entry to the fatal list of options if the oldKey
// is set and the new key is _not_ set.
// If the new key is set, a warning is emitted instead.
func (d *deprecator) fatalIfNewKeyIsNotUsed(newKey, oldKey string) {
	if conf.IsSet(oldKey) && !conf.IsSet(newKey) {
		d.fatals.Add(
			fmt.Sprintf(
				"The %q configuration key is deprecated. Use %q instead. %q has been removed.",
				oldKey,
				newKey,
				oldKey,
			),
		)
	} else if conf.IsSet(oldKey) {
		d.warns.Add(
			fmt.Sprintf(
				"The %q configuration key is deprecated. Use %q instead. %q has been removed.",
				oldKey,
				newKey,
				oldKey,
			),
		)
	}
}

// fatalIfSet fatals if the oldKey is set at all, regardless of whether
// the newKey is set. Use this when the old key has been fully removed
// and any use of it should be a hard error.
func (d *deprecator) fatalIfSet(oldKey, newKey string) {
	if conf.IsSet(oldKey) {
		d.fatals.Add(
			fmt.Sprintf(
				"The %q configuration key has been removed. Use %q instead.",
				oldKey,
				newKey,
			),
		)
	}
}

// warnNoAlias deprecates and adds an option to log a warning if the oldKey is set.
func (d *deprecator) warnNoAlias(newKey, oldKey string) {
	if conf.IsSet(oldKey) {
		d.warns.Add(
			fmt.Sprintf(
				"The %q configuration key is deprecated. Use %q instead. %q has been removed.",
				oldKey,
				newKey,
				oldKey,
			),
		)
	}
}

// tailcfgDNSMu guards the runtime DNS state of a [Config]: the override
// from the settings table, the records from the extra-records file and the
// [Config.TailcfgDNSConfig] rebuilt from them, between their writers and the
// per-client map builds that clone the result. It is a package-level lock so
// [Config] stays freely copyable during construction.
var tailcfgDNSMu sync.RWMutex

// CloneTailcfgDNSConfig returns a deep copy of [Config.TailcfgDNSConfig], or
// nil if none is set. Safe for concurrent use with the DNS setters.
func (c *Config) CloneTailcfgDNSConfig() *tailcfg.DNSConfig {
	tailcfgDNSMu.RLock()
	defer tailcfgDNSMu.RUnlock()

	if c.TailcfgDNSConfig == nil {
		return nil
	}

	return c.TailcfgDNSConfig.Clone()
}

// LocalDNSDomains lists the domains the client resolves itself when
// MagicDNS is on: the base domain and the reverse zones of the tailnet's
// prefixes. A split DNS route for one of them, or a name under one,
// would send the client to a nameserver for names only it knows.
func (c *Config) LocalDNSDomains() []string {
	tailcfgDNSMu.RLock()
	defer tailcfgDNSMu.RUnlock()

	if c.TailcfgDNSConfig == nil || !c.TailcfgDNSConfig.Proxied {
		return nil
	}

	var out []string

	for domain, resolvers := range c.TailcfgDNSConfig.Routes {
		if resolvers != nil && len(resolvers) == 0 {
			out = append(out, domain)
		}
	}

	if base := c.effectiveDNSLocked().BaseDomain; base != "" {
		out = append(out, strings.TrimSuffix(base, "."))
	}

	slices.Sort(out)

	return out
}

// ResolvesLocally reports whether domain is one of [Config.LocalDNSDomains]
// or a name under one.
func (c *Config) ResolvesLocally(domain string) bool {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))

	for _, local := range c.LocalDNSDomains() {
		if domain == local || strings.HasSuffix(domain, "."+local) {
			return true
		}
	}

	return false
}

// SetExtraRecords replaces the extra records read from
// dns.extra_records_path and rebuilds [Config.TailcfgDNSConfig]. Safe for
// concurrent use with [Config.CloneTailcfgDNSConfig].
func (c *Config) SetExtraRecords(records []tailcfg.DNSRecord) {
	tailcfgDNSMu.Lock()
	defer tailcfgDNSMu.Unlock()

	// Normalize as dns.extra_records from the config file is normalized.
	// DNS names are case-insensitive, but a client matches an extra record
	// against the lowercased query name, so "Printer.fritz.box" in the
	// records file never resolves and the query falls through to the global
	// nameserver.
	c.dnsFileRecords = NormalizeExtraRecords(records)
	c.dnsFileRecordsSet = true

	c.rebuildTailcfgDNSLocked()
}

// SetDNSOverride replaces the DNS settings the tailnet runs with; nil
// returns to the config file. It rebuilds [Config.TailcfgDNSConfig].
func (c *Config) SetDNSOverride(settings *DNSSettings) {
	tailcfgDNSMu.Lock()
	defer tailcfgDNSMu.Unlock()

	if settings == nil {
		c.dnsOverride = nil
	} else {
		s := settings.Clone()
		c.dnsOverride = &s
	}

	c.rebuildTailcfgDNSLocked()
}

// DNSOverride returns a copy of the override set with [Config.SetDNSOverride],
// or nil when the config file is in force.
func (c *Config) DNSOverride() *DNSSettings {
	tailcfgDNSMu.RLock()
	defer tailcfgDNSMu.RUnlock()

	if c.dnsOverride == nil {
		return nil
	}

	s := c.dnsOverride.Clone()

	return &s
}

// EffectiveDNS returns the DNS configuration the tailnet runs with: the
// file's values with the override and the extra-records file applied.
func (c *Config) EffectiveDNS() DNSConfig {
	tailcfgDNSMu.RLock()
	defer tailcfgDNSMu.RUnlock()

	return c.effectiveDNSLocked()
}

// RebuildTailcfgDNS recomputes [Config.TailcfgDNSConfig] from the file, the
// override and the extra-records file, including the MagicDNS reverse
// zones for the tailnet's prefixes.
func (c *Config) RebuildTailcfgDNS() {
	tailcfgDNSMu.Lock()
	defer tailcfgDNSMu.Unlock()

	c.rebuildTailcfgDNSLocked()
}

func (c *Config) effectiveDNSLocked() DNSConfig {
	d := c.DNSConfig
	if c.dnsOverride != nil {
		d = c.dnsOverride.apply(d)
	}

	if c.dnsFileRecordsSet {
		d.ExtraRecords = c.dnsFileRecords
	}

	return d
}

func (c *Config) rebuildTailcfgDNSLocked() {
	if c.TailcfgDNSConfig == nil {
		return
	}

	cfg := dnsToTailcfgDNS(c.effectiveDNSLocked())
	c.addMagicDNSRoutes(cfg)
	c.TailcfgDNSConfig = cfg
}

// addMagicDNSRoutes maps the IPv4/IPv6 reverse zones of the tailnet's
// prefixes to an empty (non-nil) resolver slice so the client resolves
// them itself. It is a no-op unless MagicDNS is on.
func (c *Config) addMagicDNSRoutes(cfg *tailcfg.DNSConfig) {
	if !cfg.Proxied {
		return
	}

	var magicDNSDomains []dnsname.FQDN
	if c.PrefixV4 != nil {
		magicDNSDomains = append(magicDNSDomains, util.GenerateIPv4DNSRootDomain(*c.PrefixV4)...)
	}

	if c.PrefixV6 != nil {
		magicDNSDomains = append(magicDNSDomains, util.GenerateIPv6DNSRootDomain(*c.PrefixV6)...)
	}

	if cfg.Routes == nil {
		cfg.Routes = make(map[string][]*dnstype.Resolver)
	}

	for _, d := range magicDNSDomains {
		// Empty non-nil slice rather than nil: tailcfg.DNSConfig.Clone
		// and dns.Config.Clone in tailscale drop map entries whose
		// value is nil (see tailscale.com/tailcfg/tailcfg_clone.go and
		// tailscale.com/net/dns/dns_clone.go: `if sv == nil { continue }`).
		// Sending nil here caused the client's wgengine LinkChange:major
		// handler to clobber /etc/resolv.conf on every tunnel-IP rebind:
		// the handler reapplies a Clone of lastDNSConfig and the magic
		// DNS routes vanish, taking the resolver with them for ~6 min
		// until the next route-changing netmap. Empty slice survives
		// Clone and carries the same "resolve locally" semantics
		// (tailscale.com/ipn/ipnlocal/node_backend.go:869 documents the
		// empty-resolver Routes form for Issue 2706).
		cfg.Routes[d.WithoutTrailingDot()] = []*dnstype.Resolver{}
	}
}
