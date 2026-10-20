package types

import (
	"strings"

	"github.com/aislopware/slopscale/hscontrol/dnsprovider"
)

// maxDNSNameLength is the longest name DNS carries, without the trailing
// dot; the challenge record's name has to fit.
const maxDNSNameLength = 253

// MagicDNSName is the name the tailnet knows the node by: its given name
// under the tailnet's base domain, lowercased and without a trailing dot.
// Empty when the tailnet has no base domain, so the node has no such name.
func (nv NodeView) MagicDNSName(cfg *Config) string {
	base := strings.TrimSuffix(cfg.BaseDomain, ".")
	if base == "" || nv.GivenName() == "" {
		return ""
	}

	return strings.ToLower(nv.GivenName() + "." + base)
}

// CertDomains are the names a node may get certificates for: its
// MagicDNS name, when the server assists with certificates and the
// tailnet has a base domain, and the names of the services it is
// approved to host. Nil otherwise.
//
// A name is left out when another party could hold it too: a given name
// from before renames were lowercased, which can differ from another
// node's name by case only; a name the operator gave an extra DNS
// record at or below, since a node names itself through its hostname
// and a wildcard order shares the bare name's challenge record; and a
// name whose challenge the provider answers from a record shared with
// other names.
func (nv NodeView) CertDomains(cfg *Config) []string {
	if !cfg.HTTPSCerts.Enabled || nv.MagicDNSName(cfg) == "" {
		return nil
	}

	base := strings.TrimSuffix(cfg.BaseDomain, ".")

	var domains []string

	if nv.GivenName() == strings.ToLower(nv.GivenName()) && certNameAllowed(cfg, nv.MagicDNSName(cfg)) {
		domains = append(domains, nv.MagicDNSName(cfg))
	}

	// A node approved to host a service serves it under the service's
	// name, so it may get a certificate for that name too.
	for _, name := range nv.HostedServices() {
		domain := strings.ToLower(name.WithoutPrefix() + "." + base)
		if certNameAllowed(cfg, domain) {
			domains = append(domains, domain)
		}
	}

	return domains
}

// certNameAllowed reports whether a node may get a certificate for
// domain, a lowercase name under the base domain.
func certNameAllowed(cfg *Config, domain string) bool {
	if len(ACMEChallengePrefix+domain) > maxDNSNameLength {
		return false
	}

	if cfg.extraRecordAtOrBelow(domain) {
		return false
	}

	if iso, ok := cfg.HTTPSCerts.Provider.(dnsprovider.Isolator); ok && !iso.Isolates(domain) {
		return false
	}

	return true
}

// ACMEChallengeAllowed reports whether name is the DNS-01 challenge
// record of one of the node's cert domains. The client builds the name
// from CertDomains, so it matches exactly; folding case would let
// lookalike Unicode letters match.
func (nv NodeView) ACMEChallengeAllowed(cfg *Config, name string) bool {
	name = strings.TrimSuffix(name, ".")

	for _, domain := range nv.CertDomains(cfg) {
		if name == ACMEChallengePrefix+domain {
			return true
		}
	}

	return false
}
