package types

import "strings"

// CertDomains are the names a node may get certificates for: its
// MagicDNS name, when the server assists with certificates and the
// tailnet has a base domain. Nil otherwise.
func (nv NodeView) CertDomains(cfg *Config) []string {
	if !cfg.HTTPSCerts.Enabled || cfg.BaseDomain == "" || nv.GivenName() == "" {
		return nil
	}

	base := strings.TrimSuffix(cfg.BaseDomain, ".")
	domains := []string{strings.ToLower(nv.GivenName() + "." + base)}

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
