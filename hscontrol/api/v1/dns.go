package apiv1

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/audit"
	"github.com/juanfont/headscale/hscontrol/scope"
	"github.com/juanfont/headscale/hscontrol/state"
	"github.com/juanfont/headscale/hscontrol/types"
	"tailscale.com/tailcfg"
)

func init() {
	registrations = append(registrations, registerDNS)
}

const tagDNS = "DNS"

// DNSRecord is an extra record MagicDNS serves.
type DNSRecord struct {
	Name  string `doc:"Fully qualified name, without trailing dot." json:"name"`
	Type  string `doc:"A or AAAA; empty picks one from the value."  enum:",A,AAAA" json:"type"`
	Value string `json:"value"`
}

// DNSSettings is the part of the DNS configuration that can change while
// the server runs.
type DNSSettings struct {
	Nameservers      []string `doc:"IP, IP:port, https or tls URL." json:"nameservers"      nullable:"false"`
	OverrideLocalDNS bool     `doc:"Used for every query."          json:"overrideLocalDns"`
	// SplitNameservers are the resolvers per domain.
	SplitNameservers map[string][]string `json:"splitNameservers" nullable:"false"`
	// UseWithExitNode lists the global nameservers a machine keeps using while it has an exit node
	// selected; needs overrideLocalDns.
	UseWithExitNode []string `json:"useWithExitNode" nullable:"false"`
	// SplitUseWithExitNode is the same per split DNS domain; the domain's route survives the exit node
	// only when every one of its nameservers is listed.
	SplitUseWithExitNode map[string][]string `json:"splitUseWithExitNode" nullable:"false"`
	// SearchDomains are added after the base domain.
	SearchDomains []string `json:"searchDomains" nullable:"false"`
	// ExtraRecords are served by MagicDNS.
	ExtraRecords []DNSRecord `json:"extraRecords" nullable:"false"`
}

// SetDNSRequestBody is the body of setDNS. Absent fields are empty: the
// request replaces every setting, so send the whole configuration.
type SetDNSRequestBody struct {
	Nameservers      []string            `json:"nameservers,omitempty"      required:"false"`
	OverrideLocalDNS bool                `json:"overrideLocalDns,omitempty" required:"false"`
	SplitNameservers map[string][]string `json:"splitNameservers,omitempty" required:"false"`
	// UseWithExitNode lists the global nameservers a machine keeps using while it has an exit node
	// selected; needs overrideLocalDns.
	UseWithExitNode []string `json:"useWithExitNode,omitempty" required:"false"`
	// SplitUseWithExitNode is the same per split DNS domain.
	SplitUseWithExitNode map[string][]string `json:"splitUseWithExitNode,omitempty" required:"false"`
	SearchDomains        []string            `json:"searchDomains,omitempty"        required:"false"`
	ExtraRecords         []DNSRecord         `json:"extraRecords,omitempty"         required:"false"`
}

// DNS is the DNS configuration in force and where it comes from.
type DNS struct {
	MagicDNS         bool        `doc:"From the config file."                    json:"magicDns"`
	BaseDomain       string      `doc:"From the config file."                    json:"baseDomain"`
	Effective        DNSSettings `doc:"What clients receive."                    json:"effective"`
	FromFile         DNSSettings `doc:"The config file's values."                json:"fromFile"`
	Overridden       bool        `doc:"Settings set through the API are in use." json:"overridden"`
	ExtraRecordsPath string      `doc:"Set when a file owns the extra records."  json:"extraRecordsPath"`
}

type (
	dnsOutput struct {
		Body DNS
	}

	setDNSInput struct {
		Body SetDNSRequestBody
	}
)

func dnsRecordsFrom(records []tailcfg.DNSRecord) []DNSRecord {
	out := make([]DNSRecord, 0, len(records))
	for _, r := range records {
		out = append(out, DNSRecord{Name: r.Name, Type: r.Type, Value: r.Value})
	}

	return out
}

func dnsRecordsTo(records []DNSRecord) []tailcfg.DNSRecord {
	out := make([]tailcfg.DNSRecord, 0, len(records))
	for _, r := range records {
		out = append(out, tailcfg.DNSRecord{Name: r.Name, Type: r.Type, Value: r.Value})
	}

	return out
}

func dnsSettingsFrom(s types.DNSSettings) DNSSettings {
	out := DNSSettings{
		Nameservers:          s.Nameservers,
		OverrideLocalDNS:     s.OverrideLocalDNS,
		SplitNameservers:     s.SplitNameservers,
		UseWithExitNode:      s.UseWithExitNode,
		SplitUseWithExitNode: s.SplitUseWithExitNode,
		SearchDomains:        s.SearchDomains,
		ExtraRecords:         dnsRecordsFrom(s.ExtraRecords),
	}

	if out.Nameservers == nil {
		out.Nameservers = []string{}
	}

	if out.SplitNameservers == nil {
		out.SplitNameservers = map[string][]string{}
	}

	if out.UseWithExitNode == nil {
		out.UseWithExitNode = []string{}
	}

	if out.SplitUseWithExitNode == nil {
		out.SplitUseWithExitNode = map[string][]string{}
	}

	if out.SearchDomains == nil {
		out.SearchDomains = []string{}
	}

	return out
}

func dnsSettingsTo(s SetDNSRequestBody) types.DNSSettings {
	return types.DNSSettings{
		Nameservers:          s.Nameservers,
		OverrideLocalDNS:     s.OverrideLocalDNS,
		SplitNameservers:     s.SplitNameservers,
		UseWithExitNode:      s.UseWithExitNode,
		SplitUseWithExitNode: s.SplitUseWithExitNode,
		SearchDomains:        s.SearchDomains,
		ExtraRecords:         dnsRecordsTo(s.ExtraRecords),
	}
}

func dnsFrom(st state.DNSStatus) DNS {
	return DNS{
		MagicDNS:         st.MagicDNS,
		BaseDomain:       st.BaseDomain,
		Effective:        dnsSettingsFrom(st.Effective),
		FromFile:         dnsSettingsFrom(st.FromFile),
		Overridden:       st.Overridden,
		ExtraRecordsPath: st.ExtraRecordsPath,
	}
}

func registerDNS(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "getDNS",
		Method:      http.MethodGet,
		Path:        "/api/v1/dns",
		Summary:     "Get DNS settings",
		Description: "Returns the DNS configuration clients receive, the config file's values " +
			"and whether settings set through the API replace them.",
		Tags:     []string{tagDNS},
		Security: bearerAuth,
	}, scope.DNSRead), func(_ context.Context, _ *struct{}) (*dnsOutput, error) {
		return &dnsOutput{Body: dnsFrom(b.State.DNS())}, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "setDNS",
		Method:      http.MethodPut,
		Path:        "/api/v1/dns",
		Summary:     "Set DNS settings",
		Description: "Replaces the runtime DNS settings and pushes them to every client. " +
			"MagicDNS and the base domain stay in the config file. While " +
			"dns.extra_records_path is set that file owns the extra records.",
		Tags:     []string{tagDNS},
		Security: bearerAuth,
	}, scope.DNS), "dns.set", "", ""), func(ctx context.Context, in *setDNSInput) (*dnsOutput, error) {
		st, c, err := b.State.SetDNS(dnsSettingsTo(in.Body))
		if err != nil {
			return nil, mapError("setting dns", err)
		}

		audit.Detail(ctx, "nameservers", st.Effective.Nameservers)
		audit.Detail(ctx, "overrideLocalDns", st.Effective.OverrideLocalDNS)
		audit.Detail(ctx, "searchDomains", st.Effective.SearchDomains)

		b.Change(c)

		return &dnsOutput{Body: dnsFrom(st)}, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "resetDNS",
		Method:      http.MethodDelete,
		Path:        "/api/v1/dns",
		Summary:     "Reset DNS settings",
		Description: "Drops the runtime DNS settings so the config file is in force again.",
		Tags:        []string{tagDNS},
		Security:    bearerAuth,
	}, scope.DNS), "dns.reset", "", ""), func(_ context.Context, _ *struct{}) (*dnsOutput, error) {
		st, c, err := b.State.ResetDNS()
		if err != nil {
			return nil, mapError("resetting dns", err)
		}

		b.Change(c)

		return &dnsOutput{Body: dnsFrom(st)}, nil
	})
}
