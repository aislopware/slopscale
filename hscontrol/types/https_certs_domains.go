package types

import "strings"

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
// tailnet has a base domain. Nil otherwise.
func (nv NodeView) CertDomains(cfg *Config) []string {
	if !cfg.HTTPSCerts.Enabled || nv.MagicDNSName(cfg) == "" {
		return nil
	}

	base := strings.TrimSuffix(cfg.BaseDomain, ".")
	domains := []string{nv.MagicDNSName(cfg)}

	// A node approved to host a service serves it under the service's
	// name, so it may get a certificate for that name too.
	for _, name := range nv.HostedServices() {
		domains = append(domains, strings.ToLower(name.WithoutPrefix()+"."+base))
	}

	return domains
}

// ACMEChallengeAllowed reports whether name is the DNS-01 challenge
// record of one of the node's cert domains.
func (nv NodeView) ACMEChallengeAllowed(cfg *Config, name string) bool {
	name = strings.ToLower(strings.TrimSuffix(name, "."))

	for _, domain := range nv.CertDomains(cfg) {
		if name == ACMEChallengePrefix+domain {
			return true
		}
	}

	return false
}
