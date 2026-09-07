package types

import "strings"

// CertDomains are the names a node may get certificates for: its
// MagicDNS name, when the server assists with certificates and the
// tailnet has a base domain. Nil otherwise.
func (nv NodeView) CertDomains(cfg *Config) []string {
	if !cfg.HTTPSCerts.Enabled || cfg.BaseDomain == "" || nv.GivenName() == "" {
		return nil
	}

	return []string{strings.ToLower(nv.GivenName() + "." + strings.TrimSuffix(cfg.BaseDomain, "."))}
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
