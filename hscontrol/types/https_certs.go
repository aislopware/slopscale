package types

import (
	"time"

	"github.com/aislopware/slopscale/hscontrol/dnsprovider"
)

// ACMEChallengePrefix is the label ACME DNS-01 challenges live under.
const ACMEChallengePrefix = "_acme-challenge."

// defaultChallengeResolvers find the zone's nameservers when
// https_certificates.resolvers is empty: public resolvers see the zone
// as the CA does, which a split-horizon local resolver may not.
var defaultChallengeResolvers = []string{"1.1.1.1:53", "8.8.8.8:53"}

// HTTPSCertsConfig lets machines get HTTPS certificates for their
// MagicDNS names: the server publishes the ACME DNS-01 challenge
// records a client asks for; see docs/ref/https-certificates.md. The
// provider also answers the DNS-01 challenge of the server's own
// certificate (tls.acme.challenge_type DNS-01).
type HTTPSCertsConfig struct {
	// Enabled announces every machine's MagicDNS name as a cert domain
	// and answers /machine/set-dns.
	Enabled bool
	// ProviderName is the registry name of the provider, lowercase.
	ProviderName string
	// Provider publishes the records; nil when neither machines nor the
	// server's own certificate need one.
	Provider dnsprovider.Provider
	// Zone is the DNS zone machines' records are written to; the base
	// domain is in it.
	Zone string
	// TTL is the records' time to live.
	TTL time.Duration
	// PropagationTimeout is how long publishing waits for the zone's
	// authoritative nameservers to serve a record before answering,
	// because the client asks the CA to validate as soon as it gets the
	// answer. Zero answers as soon as the provider took the record.
	PropagationTimeout time.Duration
	// Resolvers find the zone's nameservers, host:port.
	Resolvers []string
}
