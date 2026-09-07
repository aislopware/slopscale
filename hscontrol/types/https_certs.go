package types

import (
	"errors"
	"time"
)

// DNSProviderKind names a way to publish DNS records.
type DNSProviderKind string

const (
	DNSProviderCloudflare DNSProviderKind = "cloudflare"
	DNSProviderRFC2136    DNSProviderKind = "rfc2136"
	DNSProviderCommand    DNSProviderKind = "command"
)

// ErrDNSProviderUnknown is returned for a provider the server does not
// have.
var ErrDNSProviderUnknown = errors.New("unknown DNS provider")

// ACMEChallengePrefix is the label ACME DNS-01 challenges live under.
const ACMEChallengePrefix = "_acme-challenge."

// HTTPSCertsConfig lets machines get HTTPS certificates for their
// MagicDNS names: the server publishes the ACME DNS-01 challenge
// records a client asks for; see docs/ref/https-certificates.md.
type HTTPSCertsConfig struct {
	// Enabled announces every machine's MagicDNS name as a cert domain
	// and answers /machine/set-dns.
	Enabled bool
	// Provider publishes the records.
	Provider DNSProviderKind
	// TTL is the records' time to live.
	TTL time.Duration

	Cloudflare CloudflareDNSConfig
	RFC2136    RFC2136Config
	Command    CommandDNSConfig
}

// CloudflareDNSConfig publishes through the Cloudflare API.
type CloudflareDNSConfig struct {
	// APIToken needs Zone:Read and DNS:Edit on the zone.
	APIToken string
	// ZoneID names the zone; empty finds it from the record's name.
	ZoneID string
}

// RFC2136Config publishes with dynamic DNS updates.
type RFC2136Config struct {
	// Server is the authoritative server, host:port.
	Server string
	// Zone is the zone the records go in; empty derives it from the
	// base domain.
	Zone string
	// TSIG signs the updates; empty sends them unsigned.
	TSIGKeyName   string
	TSIGSecret    string
	TSIGAlgorithm string
}

// CommandDNSConfig runs a program with the record name and value.
type CommandDNSConfig struct {
	Path string
}
