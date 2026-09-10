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

	"github.com/aislopware/slopscale/hscontrol/egress"
	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/prometheus/common/model"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/viper"
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
		Host:       viper.GetString("notifications.smtp.host"),
		Port:       viper.GetInt("notifications.smtp.port"),
		Username:   viper.GetString("notifications.smtp.username"),
		Password:   viper.GetString("notifications.smtp.password"),
		From:       viper.GetString("notifications.smtp.from"),
		Encryption: SMTPEncryption(viper.GetString("notifications.smtp.encryption")),
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
		Enabled:  viper.GetBool("https_certificates.enabled"),
		Provider: DNSProviderKind(viper.GetString("https_certificates.provider")),
		TTL:      viper.GetDuration("https_certificates.ttl"),
		Cloudflare: CloudflareDNSConfig{
			APIToken: viper.GetString("https_certificates.cloudflare.api_token"),
			ZoneID:   viper.GetString("https_certificates.cloudflare.zone_id"),
		},
		RFC2136: RFC2136Config{
			Server:        viper.GetString("https_certificates.rfc2136.server"),
			Zone:          viper.GetString("https_certificates.rfc2136.zone"),
			TSIGKeyName:   viper.GetString("https_certificates.rfc2136.tsig_key_name"),
			TSIGSecret:    viper.GetString("https_certificates.rfc2136.tsig_secret"),
			TSIGAlgorithm: viper.GetString("https_certificates.rfc2136.tsig_algorithm"),
		},
		Command: CommandDNSConfig{
			Path: viper.GetString("https_certificates.command.path"),
		},
	}

	if !cfg.Enabled {
		return cfg, nil
	}

	if viper.GetString("dns.base_domain") == "" {
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
	rawPorts := viper.GetIntSlice("funnel.ports")
	ports := make([]uint16, 0, len(rawPorts))

	for _, p := range rawPorts {
		if p < 1 || p > 65535 {
			return FunnelConfig{}, fmt.Errorf("%w: %d", ErrFunnelPortInvalid, p)
		}

		ports = append(ports, uint16(p))
	}

	cfg := FunnelConfig{
		Enabled:     viper.GetBool("funnel.enabled"),
		ListenAddrs: viper.GetStringSlice("funnel.listen_addrs"),
		StateDir:    util.AbsolutePathFromConfigPath(viper.GetString("funnel.state_dir")),
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
		Enabled:         viper.GetBool("ssh_recording.enabled"),
		Dir:             util.AbsolutePathFromConfigPath(viper.GetString("ssh_recording.dir")),
		StateDir:        util.AbsolutePathFromConfigPath(viper.GetString("ssh_recording.state_dir")),
		Retention:       viper.GetDuration("ssh_recording.retention"),
		MaxSessionBytes: viper.GetInt64("ssh_recording.max_session_bytes"),
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
		DenyPrivateTargets:   viper.GetBool("egress.deny_private_targets"),
		AllowLoopbackTargets: viper.GetBool("egress.allow_loopback_targets"),
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
		NodeAPIEnabled: viper.GetBool("debug.node_api_enabled"),
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
	err := validatePKCEMethod(viper.GetString("oidc.pkce.method"))
	if err != nil {
		return err
	}

	issuer := viper.GetString("oidc.issuer")

	u, err := url.Parse(issuer)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return fmt.Errorf("%w: got %q", errOIDCIssuerInvalid, issuer)
	}

	if viper.GetString("oidc.client_id") == "" {
		return errOIDCClientIDRequired
	}

	if viper.GetString("oidc.client_secret") == "" && viper.GetString("oidc.client_secret_path") == "" {
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
	viper.SetDefault("https_certificates.enabled", false)
	viper.SetDefault("https_certificates.ttl", time.Minute)
	viper.SetDefault("https_certificates.rfc2136.tsig_algorithm", "hmac-sha256")
	viper.SetDefault("ssh_recording.enabled", false)
	viper.SetDefault("ssh_recording.dir", "/var/lib/slopscale/recordings")
	viper.SetDefault("ssh_recording.state_dir", "/var/lib/slopscale/recorder")
	viper.SetDefault("ssh_recording.max_session_bytes", 0)
	viper.SetDefault("funnel.enabled", false)
	viper.SetDefault("funnel.listen_addrs", []string{":443", ":8443", ":10000"})
	viper.SetDefault("funnel.state_dir", "/var/lib/slopscale/ingress")
	viper.SetDefault("funnel.ports", []int{443, 8443, 10000})
	viper.SetDefault("client_updates.check", true)
	viper.SetDefault("client_updates.interval", DefaultClientUpdatesInterval)
	viper.SetDefault("egress.deny_private_targets", false)
	viper.SetDefault("egress.allow_loopback_targets", false)
	viper.SetDefault("debug.node_api_enabled", false)
	viper.SetDefault("notifications.smtp.encryption", string(SMTPStartTLS))
}

// LoadConfig prepares and loads the Slopscale configuration into Viper.
// This means it sets the default values, reads the configuration file and
// environment variables, and handles deprecated configuration options.
// It has to be called before [LoadServerConfig] and [LoadCLIConfig].
// The configuration is not validated and the caller should check for errors
// using a validation function.
func LoadConfig(path string, isFile bool) error {
	if isFile {
		viper.SetConfigFile(path)
	} else {
		viper.SetConfigName("config")

		if path == "" {
			viper.AddConfigPath("/etc/slopscale/")
			viper.AddConfigPath("$HOME/.slopscale")
			viper.AddConfigPath(".")
		} else {
			// For testing
			viper.AddConfigPath(path)
		}
	}

	envPrefix := "slopscale"
	viper.SetEnvPrefix(envPrefix)
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	viper.SetDefault("policy.mode", "file")

	viper.SetDefault("notifications.smtp.port", 587)

	setNodeServiceDefaults()

	viper.SetDefault("tls_letsencrypt_cache_dir", "/var/www/.cache")
	viper.SetDefault("tls_letsencrypt_challenge_type", HTTP01ChallengeType)

	viper.SetDefault("log.level", "info")
	viper.SetDefault("log.format", TextLogFormat)

	viper.SetDefault("dns.magic_dns", true)
	viper.SetDefault("dns.base_domain", "")
	viper.SetDefault("dns.override_local_dns", true)
	viper.SetDefault("dns.nameservers.global", []string{})
	viper.SetDefault("dns.nameservers.split", map[string]string{})
	viper.SetDefault("dns.nameservers.use_with_exit_node.global", []string{})
	viper.SetDefault("dns.nameservers.use_with_exit_node.split", map[string]string{})
	viper.SetDefault("dns.search_domains", []string{})

	viper.SetDefault("derp.server.enabled", true)
	viper.SetDefault("derp.server.region_id", 999)
	viper.SetDefault("derp.server.region_code", "slopscale")
	viper.SetDefault("derp.server.region_name", "Slopscale Embedded DERP")
	viper.SetDefault("derp.server.verify_clients", true)
	viper.SetDefault("derp.server.stun.enabled", true)
	viper.SetDefault("derp.server.stun_listen_addr", "0.0.0.0:3478")
	viper.SetDefault("derp.server.automatically_add_embedded_derp_region", true)
	viper.SetDefault("derp.urls", []string{TailscaleDERPMapURL})
	viper.SetDefault("derp.auto_update_enabled", true)
	viper.SetDefault("derp.update_frequency", "3h")

	viper.SetDefault("unix_socket", "/var/run/slopscale/slopscale.sock")
	viper.SetDefault("unix_socket_permission", "0o770")

	viper.SetDefault("cli.timeout", "5s")
	viper.SetDefault("cli.insecure", false)

	viper.SetDefault("database.postgres.ssl", false)
	viper.SetDefault("database.postgres.max_open_conns", 10)
	viper.SetDefault("database.postgres.max_idle_conns", 10)
	viper.SetDefault("database.postgres.conn_max_idle_time_secs", 3600)

	viper.SetDefault("database.sqlite.write_ahead_log", true)
	viper.SetDefault("database.sqlite.wal_autocheckpoint", 1000) // SQLite default

	viper.SetDefault("oidc.scope", []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail})
	viper.SetDefault("oidc.only_start_if_oidc_is_available", true)
	viper.SetDefault("oidc.use_expiry_from_token", false)
	viper.SetDefault("oidc.pkce.enabled", false)
	viper.SetDefault("oidc.pkce.method", "S256")
	viper.SetDefault("oidc.email_verified_required", true)

	viper.SetDefault("logtail.enabled", false)
	viper.SetDefault("taildrop.enabled", true)
	viper.SetDefault("auto_update.enabled", false)

	viper.SetDefault("node.expiry", "0")
	viper.SetDefault("node.ephemeral.inactivity_timeout", "120s")
	viper.SetDefault("preauth_keys.revoked_retention", "168h")
	viper.SetDefault("audit.retention", "0")
	viper.SetDefault("node.routes.ha.probe_interval", "10s")
	viper.SetDefault("node.routes.ha.probe_timeout", "5s")

	viper.SetDefault("tuning.notifier_send_timeout", "800ms")
	viper.SetDefault("tuning.batch_change_delay", "800ms")
	viper.SetDefault("tuning.node_mapsession_buffered_chan_size", 30)
	viper.SetDefault("tuning.node_store_batch_size", defaultNodeStoreBatchSize)
	viper.SetDefault("tuning.node_store_batch_timeout", "500ms")

	viper.SetDefault("prefixes.allocation", string(IPAllocationStrategySequential))

	err := viper.ReadInConfig()
	if err != nil {
		if _, ok := errors.AsType[viper.ConfigFileNotFoundError](err); ok {
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
// We cannot use viper.RegisterAlias here because aliases silently ignore
// config values set under the alias name. If a user writes the new key in
// their config file, RegisterAlias redirects reads to the old key (which
// has no config value), returning only the default and discarding the
// user's setting.
func resolveEphemeralInactivityTimeout() time.Duration {
	// New key takes precedence if explicitly set in config.
	if viper.IsSet("node.ephemeral.inactivity_timeout") &&
		viper.GetString("node.ephemeral.inactivity_timeout") != "" {
		return viper.GetDuration("node.ephemeral.inactivity_timeout")
	}

	// Fall back to old key for backwards compatibility.
	if viper.IsSet("ephemeral_node_inactivity_timeout") {
		return viper.GetDuration("ephemeral_node_inactivity_timeout")
	}

	// Default
	return viper.GetDuration("node.ephemeral.inactivity_timeout")
}

// resolveNodeExpiry parses the node.expiry config value.
// Returns 0 if set to "0" (no default expiry) or on parse failure.
func resolveNodeExpiry() time.Duration {
	value := viper.GetString("node.expiry")
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

	// Register aliases for backward compatibility
	// Has to be called _after_ viper.ReadInConfig()
	// https://github.com/spf13/viper/issues/560

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
	if viper.GetString("oidc.issuer") != "" {
		err := validateOIDCConfig()
		if err != nil {
			return err
		}
	}

	depr.Log()

	if viper.IsSet("dns.extra_records") && viper.IsSet("dns.extra_records_path") {
		log.Fatal().
			Msg("fatal config error: dns.extra_records and dns.extra_records_path are mutually exclusive. " +
				"Remove one of them from the config file")
	}

	// Collect any validation errors and return them all at once
	var errorText string
	if (viper.GetString("tls_letsencrypt_hostname") != "") &&
		((viper.GetString("tls_cert_path") != "") || (viper.GetString("tls_key_path") != "")) {
		errorText += "Fatal config error: set either tls_letsencrypt_hostname or tls_cert_path/tls_key_path, not both\n"
	}

	if viper.GetString("noise.private_key_path") == "" {
		errorText += "Fatal config error: slopscale now requires a new `noise.private_key_path` field in the config " +
			"file for the Tailscale v2 protocol\n"
	}

	if (viper.GetString("tls_letsencrypt_hostname") != "") &&
		(viper.GetString("tls_letsencrypt_challenge_type") == TLSALPN01ChallengeType) &&
		(!strings.HasSuffix(viper.GetString("listen_addr"), ":443")) {
		// this is only a warning because there could be something sitting in front of
		// slopscale that redirects the traffic (e.g. an iptables rule)
		log.Warn().
			Msg("Warning: when using tls_letsencrypt_hostname with TLS-ALPN-01 as challenge type, " +
				"slopscale must be reachable on port 443, i.e. listen_addr should probably end in :443")
	}

	if (viper.GetString("tls_letsencrypt_challenge_type") != HTTP01ChallengeType) &&
		(viper.GetString("tls_letsencrypt_challenge_type") != TLSALPN01ChallengeType) {
		errorText += "Fatal config error: the only supported values for tls_letsencrypt_challenge_type are " +
			"HTTP-01 and TLS-ALPN-01\n"
	}

	if !strings.HasPrefix(viper.GetString("server_url"), "http://") &&
		!strings.HasPrefix(viper.GetString("server_url"), "https://") {
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

	if viper.GetBool("dns.override_local_dns") {
		if global := viper.GetStringSlice("dns.nameservers.global"); len(global) == 0 {
			errorText += "Fatal config error: dns.nameservers.global must be set when dns.override_local_dns is true\n"
		}
	}

	errorText += useWithExitNodeConfigErrors()

	// Validate HA health probing parameters
	if haInterval := viper.GetDuration(
		"node.routes.ha.probe_interval",
	); haInterval > 0 {
		if haInterval < 2*time.Second {
			errorText += fmt.Sprintf(
				"Fatal config error: node.routes.ha.probe_interval (%s) must be >= 2s\n",
				haInterval,
			)
		}

		haTimeout := viper.GetDuration("node.routes.ha.probe_timeout")
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
	if size := viper.GetInt("tuning.node_store_batch_size"); size <= 0 {
		errorText += fmt.Sprintf(
			"Fatal config error: tuning.node_store_batch_size must be positive, got %d\n",
			size,
		)
	}

	if timeout := viper.GetDuration("tuning.node_store_batch_timeout"); timeout <= 0 {
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
			Hostname: viper.GetString("tls_letsencrypt_hostname"),
			Listen:   viper.GetString("tls_letsencrypt_listen"),
			CacheDir: util.AbsolutePathFromConfigPath(
				viper.GetString("tls_letsencrypt_cache_dir"),
			),
			ChallengeType: viper.GetString("tls_letsencrypt_challenge_type"),
		},
		CertPath: util.AbsolutePathFromConfigPath(
			viper.GetString("tls_cert_path"),
		),
		KeyPath: util.AbsolutePathFromConfigPath(
			viper.GetString("tls_key_path"),
		),
	}
}

func derpConfig() DERPConfig {
	serverEnabled := viper.GetBool("derp.server.enabled")
	serverRegionID := viper.GetInt64("derp.server.region_id")
	serverRegionCode := viper.GetString("derp.server.region_code")
	serverRegionName := viper.GetString("derp.server.region_name")
	serverVerifyClients := viper.GetBool("derp.server.verify_clients")
	stunAddr := viper.GetString("derp.server.stun_listen_addr")
	privateKeyPath := util.AbsolutePathFromConfigPath(
		viper.GetString("derp.server.private_key_path"),
	)
	// The relay key lives next to the noise key unless the file says
	// otherwise, so the embedded relay works with no derp section at all.
	if privateKeyPath == "" {
		if noisePath := util.AbsolutePathFromConfigPath(viper.GetString("noise.private_key_path")); noisePath != "" {
			privateKeyPath = filepath.Join(filepath.Dir(noisePath), "derp_server_private.key")
		}
	}

	ipv4 := viper.GetString("derp.server.ipv4")
	ipv6 := viper.GetString("derp.server.ipv6")
	automaticallyAddEmbeddedDerpRegion := viper.GetBool(
		"derp.server.automatically_add_embedded_derp_region",
	)

	if serverEnabled && stunAddr == "" {
		log.Fatal().
			Msg("derp.server.stun_listen_addr must be set if derp.server.enabled is true")
	}

	urlStrs := viper.GetStringSlice("derp.urls")

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

	paths := viper.GetStringSlice("derp.paths")

	if serverEnabled && !automaticallyAddEmbeddedDerpRegion && len(paths) == 0 {
		log.Fatal().
			Msg("Disabling derp.server.automatically_add_embedded_derp_region requires to configure " +
				"the derp server in derp.paths")
	}

	autoUpdate := viper.GetBool("derp.auto_update_enabled")
	updateFrequency := viper.GetDuration("derp.update_frequency")

	return DERPConfig{
		ServerEnabled:                      serverEnabled,
		ServerRegionID:                     tailcfg.DERPRegionID(serverRegionID),
		ServerRegionCode:                   serverRegionCode,
		ServerRegionName:                   serverRegionName,
		ServerVerifyClients:                serverVerifyClients,
		ServerPrivateKeyPath:               privateKeyPath,
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
	enabled := viper.GetBool("logtail.enabled")

	return LogTailConfig{
		Enabled: enabled,
	}
}

func policyConfig() PolicyConfig {
	policyPath := viper.GetString("policy.path")
	policyMode := viper.GetString("policy.mode")

	return PolicyConfig{
		Path:          policyPath,
		Mode:          PolicyMode(policyMode),
		GeoIPDatabase: viper.GetString("policy.geoip_database"),
	}
}

func logConfig() LogConfig {
	logLevelStr := viper.GetString("log.level")

	logLevel, err := zerolog.ParseLevel(logLevelStr)
	if err != nil {
		logLevel = zerolog.DebugLevel
	}

	logFormatOpt := viper.GetString("log.format")

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

	if viper.IsSet("database.gorm") {
		log.Warn().Msg("database.gorm is deprecated, move its settings to database.query_log")

		cfg.SlowThreshold = time.Duration(viper.GetInt64("database.gorm.slow_threshold")) * time.Millisecond
		cfg.LogNotFound = !viper.GetBool("database.gorm.skip_err_record_not_found")
		cfg.Parameterized = viper.GetBool("database.gorm.parameterized_queries")
	}

	if viper.IsSet("database.query_log.slow_threshold") {
		cfg.SlowThreshold = time.Duration(viper.GetInt64("database.query_log.slow_threshold")) * time.Millisecond
	}

	if viper.IsSet("database.query_log.log_not_found") {
		cfg.LogNotFound = viper.GetBool("database.query_log.log_not_found")
	}

	if viper.IsSet("database.query_log.parameterized") {
		cfg.Parameterized = viper.GetBool("database.query_log.parameterized")
	}

	return cfg
}

func databaseConfig() DatabaseConfig {
	debug := viper.GetBool("database.debug")

	dbType := viper.GetString("database.type")

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
				viper.GetString("database.sqlite.path"),
			),
			WriteAheadLog:     viper.GetBool("database.sqlite.write_ahead_log"),
			WALAutoCheckPoint: viper.GetInt("database.sqlite.wal_autocheckpoint"),
		},
		Postgres: PostgresConfig{
			Host:               viper.GetString("database.postgres.host"),
			Port:               viper.GetInt("database.postgres.port"),
			Name:               viper.GetString("database.postgres.name"),
			User:               viper.GetString("database.postgres.user"),
			Pass:               viper.GetString("database.postgres.pass"),
			Ssl:                viper.GetString("database.postgres.ssl"),
			MaxOpenConnections: viper.GetInt("database.postgres.max_open_conns"),
			MaxIdleConnections: viper.GetInt("database.postgres.max_idle_conns"),
			ConnMaxIdleTimeSecs: viper.GetInt(
				"database.postgres.conn_max_idle_time_secs",
			),
		},
	}
}

func dns() (DNSConfig, error) {
	var dns DNSConfig

	// TODO: Use this instead of manually getting settings when
	// UnmarshalKey is compatible with Environment Variables.
	// err := viper.UnmarshalKey("dns", &dns)
	// if err != nil {
	// 	return DNSConfig{}, fmt.Errorf("unmarshalling dns config: %w", err)
	// }

	dns.MagicDNS = viper.GetBool("dns.magic_dns")
	dns.BaseDomain = viper.GetString("dns.base_domain")
	dns.OverrideLocalDNS = viper.GetBool("dns.override_local_dns")
	dns.Nameservers.Global = viper.GetStringSlice("dns.nameservers.global")
	dns.Nameservers.Split = viper.GetStringMapStringSlice("dns.nameservers.split")
	// Unset stays nil, so a config without the key compares equal to one
	// built from the settings API.
	if global := viper.GetStringSlice("dns.nameservers.use_with_exit_node.global"); len(global) > 0 {
		dns.Nameservers.UseWithExitNode = global
	}

	if split := viper.GetStringMapStringSlice("dns.nameservers.use_with_exit_node.split"); len(split) > 0 {
		dns.Nameservers.SplitUseWithExitNode = split
	}

	dns.SearchDomains = viper.GetStringSlice("dns.search_domains")
	dns.ExtraRecordsPath = viper.GetString("dns.extra_records_path")

	if viper.IsSet("dns.extra_records") {
		var extraRecords []tailcfg.DNSRecord

		err := viper.UnmarshalKey("dns.extra_records", &extraRecords)
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
		Nameservers:          viper.GetStringSlice("dns.nameservers.global"),
		OverrideLocalDNS:     viper.GetBool("dns.override_local_dns"),
		SplitNameservers:     viper.GetStringMapStringSlice("dns.nameservers.split"),
		UseWithExitNode:      viper.GetStringSlice("dns.nameservers.use_with_exit_node.global"),
		SplitUseWithExitNode: viper.GetStringMapStringSlice("dns.nameservers.use_with_exit_node.split"),
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
	s := viper.GetString(key)

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
	raw := viper.GetStringSlice("trusted_proxies")
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
		DisableUpdateCheck: viper.GetBool("disable_check_updates"),
		UnixSocket:         viper.GetString("unix_socket"),
		CLI: CLIConfig{
			Address:  viper.GetString("cli.address"),
			APIKey:   viper.GetString("cli.api_key"),
			Timeout:  viper.GetDuration("cli.timeout"),
			Insecure: viper.GetBool("cli.insecure"),
		},
		Log: logConfig,
	}, nil
}

// oidcConfig reads the identity provider settings; the client secret
// comes from the file at oidc.client_secret_path when one is set.
func oidcConfig() (OIDCConfig, error) {
	clientSecret := viper.GetString("oidc.client_secret")

	clientSecretPath := viper.GetString("oidc.client_secret_path")
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
		OnlyStartIfOIDCIsAvailable: viper.GetBool(
			"oidc.only_start_if_oidc_is_available",
		),
		Issuer:         viper.GetString("oidc.issuer"),
		ClientID:       viper.GetString("oidc.client_id"),
		ClientSecret:   clientSecret,
		Scope:          viper.GetStringSlice("oidc.scope"),
		ExtraParams:    viper.GetStringMapString("oidc.extra_params"),
		AllowedDomains: viper.GetStringSlice("oidc.allowed_domains"),
		AllowedUsers:   viper.GetStringSlice("oidc.allowed_users"),
		AllowedGroups:  viper.GetStringSlice("oidc.allowed_groups"),
		AdminUsers:     viper.GetStringSlice("oidc.admin_users"),
		Groups: OIDCGroupsConfig{
			Sync:   viper.GetBool("oidc.groups.sync"),
			Prefix: viper.GetString("oidc.groups.prefix"),
		},
		MatchByEmail:          viper.GetBool("oidc.match_by_email"),
		EmailVerifiedRequired: viper.GetBool("oidc.email_verified_required"),
		UseExpiryFromToken:    viper.GetBool("oidc.use_expiry_from_token"),
		PKCE: PKCEConfig{
			Enabled: viper.GetBool("oidc.pkce.enabled"),
			Method:  viper.GetString("oidc.pkce.method"),
		},
	}, nil
}

// LoadServerConfig returns the full Slopscale configuration to
// host a Slopscale server. This is called as part of `slopscale serve`.
//
//nolint:funlen // legacy: one linear read of every viper key; splitting it would only scatter the key list
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

	allocStr := viper.GetString("prefixes.allocation")

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

	serverURL := viper.GetString("server_url")

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
		Addr:               viper.GetString("listen_addr"),
		MetricsAddr:        viper.GetString("metrics_listen_addr"),
		TrustedProxies:     trusted,
		DisableUpdateCheck: false,

		PrefixV4:     prefix4,
		PrefixV6:     prefix6,
		IPAllocation: alloc,

		NoisePrivateKeyPath: util.AbsolutePathFromConfigPath(
			viper.GetString("noise.private_key_path"),
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
					ProbeInterval: viper.GetDuration("node.routes.ha.probe_interval"),
					ProbeTimeout:  viper.GetDuration("node.routes.ha.probe_timeout"),
				},
			},
		},

		PreAuthKeys: PreAuthKeysConfig{
			RevokedRetention: viper.GetDuration("preauth_keys.revoked_retention"),
		},

		Audit: AuditConfig{
			Retention: viper.GetDuration("audit.retention"),
		},

		Database: databaseConfig(),

		TLS: tlsConfig(),

		DNSConfig:        dnsConfig,
		TailcfgDNSConfig: dnsToTailcfgDNS(dnsConfig),

		ACMEEmail: viper.GetString("acme_email"),
		ACMEURL:   viper.GetString("acme_url"),

		UnixSocket:           viper.GetString("unix_socket"),
		UnixSocketPermission: util.GetFileMode("unix_socket_permission"),

		OIDC: oidcCfg,

		LogTail: logTailConfig,
		Taildrop: TaildropConfig{
			Enabled: viper.GetBool("taildrop.enabled"),
		},
		AutoUpdate: AutoUpdateConfig{
			Enabled: viper.GetBool("auto_update.enabled"),
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
			Address:  viper.GetString("cli.address"),
			APIKey:   viper.GetString("cli.api_key"),
			Timeout:  viper.GetDuration("cli.timeout"),
			Insecure: viper.GetBool("cli.insecure"),
		},

		Log: logConfig,

		Tuning: Tuning{
			NotifierSendTimeout: viper.GetDuration("tuning.notifier_send_timeout"),
			BatchChangeDelay:    viper.GetDuration("tuning.batch_change_delay"),
			NodeMapSessionBufferedChanSize: viper.GetInt(
				"tuning.node_mapsession_buffered_chan_size",
			),
			BatcherWorkers: func() int {
				if workers := viper.GetInt("tuning.batcher_workers"); workers > 0 {
					return workers
				}

				return DefaultBatcherWorkers()
			}(),
			RegisterCacheExpiration: viper.GetDuration("tuning.register_cache_expiration"),
			RegisterCacheMaxEntries: viper.GetInt("tuning.register_cache_max_entries"),
			NodeStoreBatchSize:      viper.GetInt("tuning.node_store_batch_size"),
			NodeStoreBatchTimeout:   viper.GetDuration("tuning.node_store_batch_timeout"),
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
	if viper.IsSet(oldKey) {
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
	if viper.IsSet(oldKey) {
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
	if viper.IsSet(oldKey) && !viper.IsSet(newKey) {
		d.fatals.Add(
			fmt.Sprintf(
				"The %q configuration key is deprecated. Use %q instead. %q has been removed.",
				oldKey,
				newKey,
				oldKey,
			),
		)
	} else if viper.IsSet(oldKey) {
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
	if viper.IsSet(oldKey) {
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
	if viper.IsSet(oldKey) {
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
