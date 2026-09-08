package types

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"tailscale.com/net/dns/publicdns"
	"tailscale.com/tailcfg"
	"tailscale.com/types/dnstype"
)

// SettingDNS is the settings row that holds a [DNSSettings] as JSON. When
// the row exists the tailnet uses it instead of the dns section of the
// config file; deleting it returns to the file.
const SettingDNS SettingKey = "dns"

// DNSSettings is the part of the DNS configuration an operator can change
// while the server runs: everything in the config's dns section except
// MagicDNS and the base domain, which name the nodes and stay in the file.
type DNSSettings struct {
	// Nameservers are the global resolvers, as IP, IP:port, https:// (DoH)
	// or tls:// (DoT) addresses.
	Nameservers []string `json:"nameservers"`
	// OverrideLocalDNS makes clients use Nameservers for every query
	// instead of only when the client's own resolvers fail.
	OverrideLocalDNS bool `json:"overrideLocalDNS"`
	// SplitNameservers maps a domain to the resolvers that serve it.
	SplitNameservers map[string][]string `json:"splitNameservers"`
	// UseWithExitNode lists the global nameservers a client keeps using
	// while it has an exit node selected, each one of Nameservers;
	// needs OverrideLocalDNS, as the client only honours it for the
	// resolvers it uses for every query.
	UseWithExitNode []string `json:"useWithExitNode"`
	// SplitUseWithExitNode is the same per split DNS domain: the
	// domain's route survives the exit node only when every one of
	// its nameservers is listed.
	SplitUseWithExitNode map[string][]string `json:"splitUseWithExitNode"`
	// SearchDomains are appended to the base domain in the client's
	// search list.
	SearchDomains []string `json:"searchDomains"`
	// ExtraRecords are served by the client's MagicDNS resolver.
	ExtraRecords []tailcfg.DNSRecord `json:"extraRecords"`
}

// Settings returns the runtime-changeable part of the config file's dns
// section, the starting point for an override.
func (d *DNSConfig) Settings() DNSSettings {
	return DNSSettings{
		Nameservers:          slices.Clone(d.Nameservers.Global),
		OverrideLocalDNS:     d.OverrideLocalDNS,
		SplitNameservers:     cloneSplit(d.Nameservers.Split),
		UseWithExitNode:      slices.Clone(d.Nameservers.UseWithExitNode),
		SplitUseWithExitNode: cloneSplit(d.Nameservers.SplitUseWithExitNode),
		SearchDomains:        slices.Clone(d.SearchDomains),
		ExtraRecords:         slices.Clone(d.ExtraRecords),
	}
}

// Clone returns a deep copy.
func (s DNSSettings) Clone() DNSSettings {
	return DNSSettings{
		Nameservers:          slices.Clone(s.Nameservers),
		OverrideLocalDNS:     s.OverrideLocalDNS,
		SplitNameservers:     cloneSplit(s.SplitNameservers),
		UseWithExitNode:      slices.Clone(s.UseWithExitNode),
		SplitUseWithExitNode: cloneSplit(s.SplitUseWithExitNode),
		SearchDomains:        slices.Clone(s.SearchDomains),
		ExtraRecords:         slices.Clone(s.ExtraRecords),
	}
}

func cloneSplit(m map[string][]string) map[string][]string {
	if m == nil {
		return nil
	}

	out := make(map[string][]string, len(m))
	for k, v := range m {
		out[k] = slices.Clone(v)
	}

	return out
}

// ErrDNSSettingsInvalid is wrapped by every error [DNSSettings.Validate]
// returns, so callers can map them all to a bad request.
var ErrDNSSettingsInvalid = errors.New("invalid dns settings")

// Errors returned by [DNSSettings.Validate] and the setters.
var (
	ErrDNSNameserverInvalid = fmt.Errorf(
		"%w: nameserver must be an IP address, an IP:port, or the DNS over HTTPS URL of a known provider",
		ErrDNSSettingsInvalid,
	)
	// ErrDNSResolverUnsupported is returned for a resolver URL the
	// Tailscale client cannot use: it does DNS over HTTPS only for the
	// providers it knows (Cloudflare, Google, Quad9, NextDNS, ControlD
	// and the like) and DNS over TLS not at all.
	ErrDNSResolverUnsupported = fmt.Errorf(
		"%w: the Tailscale client supports DNS over HTTPS only for known providers and no tls:// resolvers",
		ErrDNSSettingsInvalid,
	)
	ErrDNSDomainInvalid      = fmt.Errorf("%w: invalid domain name", ErrDNSSettingsInvalid)
	ErrDNSSplitNoNameservers = fmt.Errorf(
		"%w: split DNS domain needs at least one nameserver", ErrDNSSettingsInvalid,
	)
	ErrDNSUseWithExitNodeUnknown = fmt.Errorf(
		"%w: a nameserver to use with an exit node must be one of the configured nameservers", ErrDNSSettingsInvalid,
	)
	ErrDNSUseWithExitNodeNeedsOverride = fmt.Errorf(
		"%w: global nameservers to use with an exit node need override local DNS, "+
			"as the client only keeps the resolvers it uses for every query", ErrDNSSettingsInvalid,
	)
	ErrDNSRecordNameEmpty   = fmt.Errorf("%w: record name must not be empty", ErrDNSSettingsInvalid)
	ErrDNSRecordTypeInvalid = fmt.Errorf(
		"%w: record type must be A, AAAA or empty; the client serves only address records", ErrDNSSettingsInvalid,
	)
	ErrDNSRecordValueEmpty   = fmt.Errorf("%w: record value must not be empty", ErrDNSSettingsInvalid)
	ErrDNSRecordValueNotIP   = fmt.Errorf("%w: record value must be an IP address", ErrDNSSettingsInvalid)
	ErrDNSRecordValueNotIPv4 = fmt.Errorf(
		"%w: A record value must be an IPv4 address", ErrDNSSettingsInvalid,
	)
	ErrDNSRecordValueNotIPv6 = fmt.Errorf(
		"%w: AAAA record value must be an IPv6 address", ErrDNSSettingsInvalid,
	)
	ErrDNSMagicDNSFromFile = fmt.Errorf(
		"%w: magicDNS and the base domain are set in the config file", ErrDNSSettingsInvalid,
	)
	ErrDNSExtraRecordsFromFile = fmt.Errorf(
		"%w: extra records are read from dns.extra_records_path", ErrDNSSettingsInvalid,
	)
)

// Normalize trims and lowercases what the operator typed and drops empty
// entries and duplicates, so the stored value is what the console shows.
// Call it before Validate.
func (s DNSSettings) Normalize() DNSSettings {
	out := DNSSettings{
		Nameservers:      normalizeList(s.Nameservers, strings.TrimSpace),
		OverrideLocalDNS: s.OverrideLocalDNS,
		SearchDomains:    normalizeList(s.SearchDomains, normalizeDomain),
	}

	if len(s.UseWithExitNode) > 0 {
		out.UseWithExitNode = normalizeList(s.UseWithExitNode, strings.TrimSpace)
	}

	if len(s.SplitNameservers) > 0 {
		out.SplitNameservers = make(map[string][]string, len(s.SplitNameservers))

		for domain, servers := range s.SplitNameservers {
			out.SplitNameservers[normalizeDomain(domain)] = normalizeList(servers, strings.TrimSpace)
		}
	}

	if len(s.SplitUseWithExitNode) > 0 {
		out.SplitUseWithExitNode = make(map[string][]string, len(s.SplitUseWithExitNode))

		for domain, servers := range s.SplitUseWithExitNode {
			out.SplitUseWithExitNode[normalizeDomain(domain)] = normalizeList(servers, strings.TrimSpace)
		}
	}

	if len(s.ExtraRecords) > 0 {
		out.ExtraRecords = NormalizeExtraRecords(s.ExtraRecords)
	}

	return out
}

// NormalizeExtraRecords returns the records with their names lowercased
// and trailing dots dropped, types uppercased, and whitespace trimmed.
// The client's resolver matches a query against the record name after
// lowercasing the query, so a name written as "Printer.fritz.box" never
// resolves (juanfont/headscale#2782); every path that takes records in,
// the config file, the watched file and the API, goes through here.
func NormalizeExtraRecords(records []tailcfg.DNSRecord) []tailcfg.DNSRecord {
	out := make([]tailcfg.DNSRecord, 0, len(records))

	for _, r := range records {
		out = append(out, tailcfg.DNSRecord{
			Name:  normalizeDomain(r.Name),
			Type:  strings.ToUpper(strings.TrimSpace(r.Type)),
			Value: strings.TrimSpace(r.Value),
		})
	}

	return out
}

func normalizeDomain(d string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(d)), ".")
}

func normalizeList(in []string, f func(string) string) []string {
	out := make([]string, 0, len(in))

	for _, v := range in {
		v = f(v)
		if v == "" || slices.Contains(out, v) {
			continue
		}

		out = append(out, v)
	}

	return out
}

// Validate checks every entry. Nameservers must be resolver addresses
// tailscaled accepts; domains must be valid DNS names; records must carry a
// value that matches their type.
func (s DNSSettings) Validate() error {
	for _, ns := range s.Nameservers {
		err := validateNameserver(ns)
		if err != nil {
			return err
		}
	}

	for domain, servers := range s.SplitNameservers {
		err := validateDomain(domain)
		if err != nil {
			return err
		}

		if len(servers) == 0 {
			return fmt.Errorf("%w: %q", ErrDNSSplitNoNameservers, domain)
		}

		for _, ns := range servers {
			err := validateNameserver(ns)
			if err != nil {
				return fmt.Errorf("%w for %q", err, domain)
			}
		}
	}

	for _, domain := range s.SearchDomains {
		err := validateDomain(domain)
		if err != nil {
			return err
		}
	}

	err := s.validateUseWithExitNode()
	if err != nil {
		return err
	}

	for _, r := range s.ExtraRecords {
		err := validateRecord(r)
		if err != nil {
			return err
		}
	}

	return nil
}

func validateNameserver(ns string) error {
	_, err := ParseResolver(ns)

	return err
}

// ParseResolver turns a nameserver as configured into the resolver the
// client receives. It accepts what the pinned client can use: an IP, an
// IP:port, or the DNS over HTTPS URL of a provider the client knows how
// to reach without bootstrap DNS. Validation and the map response share
// it, so nothing that passes validation is dropped on the way out.
func ParseResolver(ns string) (*dnstype.Resolver, error) {
	_, addrErr := netip.ParseAddr(ns)
	if addrErr == nil {
		return &dnstype.Resolver{Addr: ns}, nil
	}

	_, portErr := netip.ParseAddrPort(ns)
	if portErr == nil {
		return &dnstype.Resolver{Addr: ns}, nil
	}

	u, err := url.Parse(ns)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("%w: %q", ErrDNSNameserverInvalid, ns)
	}

	switch u.Scheme {
	case "https":
		if len(publicdns.DoHIPsOfBase(ns)) == 0 {
			return nil, fmt.Errorf("%w: %q", ErrDNSResolverUnsupported, ns)
		}

		return &dnstype.Resolver{Addr: ns}, nil
	case "tls":
		return nil, fmt.Errorf("%w: %q", ErrDNSResolverUnsupported, ns)
	default:
		return nil, fmt.Errorf("%w: %q", ErrDNSNameserverInvalid, ns)
	}
}

// domainLabelRe matches one DNS label: letters, digits, underscores and
// inner dashes, at most 63 characters.
var domainLabelRe = regexp.MustCompile(`^[a-z0-9_]([a-z0-9_-]{0,61}[a-z0-9_])?$`)

const maxDomainLength = 253

func validateDomain(domain string) error {
	if domain == "" || len(domain) > maxDomainLength {
		return fmt.Errorf("%w: %q", ErrDNSDomainInvalid, domain)
	}

	for label := range strings.SplitSeq(domain, ".") {
		if !domainLabelRe.MatchString(label) {
			return fmt.Errorf("%w: %q", ErrDNSDomainInvalid, domain)
		}
	}

	return nil
}

func validateRecord(r tailcfg.DNSRecord) error {
	if r.Name == "" {
		return ErrDNSRecordNameEmpty
	}

	err := validateDomain(r.Name)
	if err != nil {
		return err
	}

	if r.Value == "" {
		return fmt.Errorf("%w: %q", ErrDNSRecordValueEmpty, r.Name)
	}

	addr, addrErr := netip.ParseAddr(r.Value)

	switch r.Type {
	case "":
		if addrErr != nil {
			return fmt.Errorf("%w: %q", ErrDNSRecordValueNotIP, r.Value)
		}
	case "A":
		if addrErr != nil || !addr.Is4() {
			return fmt.Errorf("%w: %q", ErrDNSRecordValueNotIPv4, r.Value)
		}
	case "AAAA":
		if addrErr != nil || !addr.Is6() {
			return fmt.Errorf("%w: %q", ErrDNSRecordValueNotIPv6, r.Value)
		}
	default:
		return fmt.Errorf("%w: %q", ErrDNSRecordTypeInvalid, r.Type)
	}

	return nil
}

// PruneUseWithExitNode returns the settings with every exit node entry
// that no longer refers to a configured nameserver dropped, and the
// global ones dropped when OverrideLocalDNS is off, so an edit of the
// nameservers alone never fails [DNSSettings.Validate] on them.
func (s DNSSettings) PruneUseWithExitNode() DNSSettings {
	out := s.Clone()
	out.UseWithExitNode = nil
	out.SplitUseWithExitNode = nil

	if s.OverrideLocalDNS {
		for _, ns := range s.UseWithExitNode {
			if slices.Contains(s.Nameservers, ns) {
				out.UseWithExitNode = append(out.UseWithExitNode, ns)
			}
		}
	}

	for domain, servers := range s.SplitUseWithExitNode {
		var kept []string

		for _, ns := range servers {
			if slices.Contains(s.SplitNameservers[domain], ns) {
				kept = append(kept, ns)
			}
		}

		if len(kept) == 0 {
			continue
		}

		if out.SplitUseWithExitNode == nil {
			out.SplitUseWithExitNode = map[string][]string{}
		}

		out.SplitUseWithExitNode[domain] = kept
	}

	return out
}

// validateUseWithExitNode checks that every nameserver to keep with an
// exit node is configured, and that the global ones are used for every
// query, without which the client ignores the flag.
func (s DNSSettings) validateUseWithExitNode() error {
	if len(s.UseWithExitNode) > 0 && !s.OverrideLocalDNS {
		return ErrDNSUseWithExitNodeNeedsOverride
	}

	for _, ns := range s.UseWithExitNode {
		if !slices.Contains(s.Nameservers, ns) {
			return fmt.Errorf("%w: %q", ErrDNSUseWithExitNodeUnknown, ns)
		}
	}

	for domain, servers := range s.SplitUseWithExitNode {
		for _, ns := range servers {
			if !slices.Contains(s.SplitNameservers[domain], ns) {
				return fmt.Errorf("%w: %q for %q", ErrDNSUseWithExitNodeUnknown, ns, domain)
			}
		}
	}

	return nil
}

// apply returns the config with the settings in place of the file's
// values.
func (s DNSSettings) apply(d DNSConfig) DNSConfig {
	d.OverrideLocalDNS = s.OverrideLocalDNS
	d.Nameservers = Nameservers{
		Global:               slices.Clone(s.Nameservers),
		Split:                cloneSplit(s.SplitNameservers),
		UseWithExitNode:      slices.Clone(s.UseWithExitNode),
		SplitUseWithExitNode: cloneSplit(s.SplitUseWithExitNode),
	}
	d.SearchDomains = slices.Clone(s.SearchDomains)
	d.ExtraRecords = slices.Clone(s.ExtraRecords)

	return d
}
