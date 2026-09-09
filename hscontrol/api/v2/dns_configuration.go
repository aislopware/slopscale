package apiv2

import (
	"context"
	"maps"
	"net/http"
	"slices"

	"github.com/aislopware/slopscale/hscontrol/api/principal"
	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
)

func init() {
	registrations = append(registrations, registerDNSConfiguration)
}

// DNSConfiguration is Tailscale's whole-tailnet DNS shape, which the
// Terraform tailscale_dns_configuration resource reads and writes in one
// go. Every field maps onto [types.DNSSettings]; magicDNS comes from the
// config file, so a POST may only repeat the value in force.
type DNSConfiguration struct {
	Nameservers []DNSResolver               `json:"nameservers" nullable:"false"`
	SplitDNS    map[string][]DNSResolver    `json:"splitDNS"    nullable:"false"`
	SearchPaths []string                    `json:"searchPaths" nullable:"false"`
	Preferences DNSConfigurationPreferences `json:"preferences"`
}

// DNSResolver is one nameserver and whether clients keep using it while an
// exit node is selected.
type DNSResolver struct {
	Address         string `json:"address"`
	UseWithExitNode bool   `json:"useWithExitNode,omitempty"`
}

// DNSConfigurationPreferences carries the two tailnet-wide switches.
// OverrideLocalDNS is stored; MagicDNS is set in the config file.
type DNSConfigurationPreferences struct {
	OverrideLocalDNS bool `json:"overrideLocalDNS,omitempty"`
	MagicDNS         bool `json:"magicDNS,omitempty"`
}

type (
	dnsConfigurationOutput struct {
		Body DNSConfiguration
	}
	setDNSConfigurationInput struct {
		Tailnet string `path:"tailnet"`
		Body    DNSConfiguration
	}
)

func registerDNSConfiguration(api huma.API, b Backend) {
	huma.Register(api, principal.RequireScope(huma.Operation{
		OperationID: "getDNSConfiguration",
		Method:      http.MethodGet,
		Path:        "/api/v2/tailnet/{tailnet}/dns/configuration",
		Summary:     "Get the whole DNS configuration",
		Tags:        dnsTags,
		Security:    security,
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.DNSRead), func(_ context.Context, in *tailnetInput) (*dnsConfigurationOutput, error) {
		err := requireDefaultTailnet(in.Tailnet)
		if err != nil {
			return nil, err
		}

		st := b.State.DNS()

		return &dnsConfigurationOutput{Body: dnsConfigurationFrom(st.Effective, st.MagicDNS)}, nil
	})

	huma.Register(api, audit.Declare(principal.RequireScope(huma.Operation{
		OperationID: "setDNSConfiguration",
		Method:      http.MethodPost,
		Path:        "/api/v2/tailnet/{tailnet}/dns/configuration",
		Summary:     "Replace the whole DNS configuration",
		Description: "Replaces the nameservers, split DNS, search paths and preferences in one write. " +
			"magicDNS is set in the config file; the request is accepted only when it repeats the " +
			"current value. Extra records, which this shape has no field for, are left alone.",
		Tags:     dnsTags,
		Security: security,
		Errors:   []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.DNS), "dns.set", "", ""), func(
		ctx context.Context, in *setDNSConfigurationInput,
	) (*dnsConfigurationOutput, error) {
		return handleSetDNSConfiguration(ctx, b, in)
	})
}

func handleSetDNSConfiguration(
	ctx context.Context, b Backend, in *setDNSConfigurationInput,
) (*dnsConfigurationOutput, error) {
	err := requireDefaultTailnet(in.Tailnet)
	if err != nil {
		return nil, err
	}

	if in.Body.Preferences.MagicDNS != b.Cfg.DNSConfig.MagicDNS {
		return nil, mapError("updating dns configuration", types.ErrDNSMagicDNSFromFile)
	}

	status := b.State.DNS()

	// The Tailscale shape has no extra records, so they survive the replace;
	// records the dns.extra_records_path file owns are never ours to write.
	settings := types.DNSSettings{ExtraRecords: status.Effective.ExtraRecords}
	if status.ExtraRecordsPath != "" {
		settings.ExtraRecords = nil
	}

	settings.SearchDomains = in.Body.SearchPaths
	settings.OverrideLocalDNS = in.Body.Preferences.OverrideLocalDNS
	settings.Nameservers, settings.UseWithExitNode = splitResolvers(in.Body.Nameservers)

	for _, domain := range slices.Sorted(maps.Keys(in.Body.SplitDNS)) {
		servers, withExitNode := splitResolvers(in.Body.SplitDNS[domain])

		if settings.SplitNameservers == nil {
			settings.SplitNameservers = map[string][]string{}
		}

		settings.SplitNameservers[domain] = servers

		if len(withExitNode) > 0 {
			if settings.SplitUseWithExitNode == nil {
				settings.SplitUseWithExitNode = map[string][]string{}
			}

			settings.SplitUseWithExitNode[domain] = withExitNode
		}
	}

	st, c, err := b.State.SetDNS(settings)
	if err != nil {
		return nil, mapError("updating dns configuration", err)
	}

	audit.Detail(ctx, "nameservers", st.Effective.Nameservers)
	audit.Detail(ctx, "splitDomains", splitDomains(st.Effective.SplitNameservers))
	audit.Detail(ctx, "searchDomains", st.Effective.SearchDomains)
	b.Change(c)

	return &dnsConfigurationOutput{Body: dnsConfigurationFrom(st.Effective, st.MagicDNS)}, nil
}

// splitResolvers separates a resolver list into the addresses and the
// subset kept while an exit node is selected.
func splitResolvers(resolvers []DNSResolver) ([]string, []string) {
	addresses := make([]string, 0, len(resolvers))

	var withExitNode []string

	for _, r := range resolvers {
		addresses = append(addresses, r.Address)

		if r.UseWithExitNode {
			withExitNode = append(withExitNode, r.Address)
		}
	}

	return addresses, withExitNode
}

func dnsConfigurationFrom(s types.DNSSettings, magicDNS bool) DNSConfiguration {
	out := DNSConfiguration{
		Nameservers: resolvers(s.Nameservers, s.UseWithExitNode),
		SplitDNS:    map[string][]DNSResolver{},
		SearchPaths: emptyIfNil(s.SearchDomains),
		Preferences: DNSConfigurationPreferences{
			OverrideLocalDNS: s.OverrideLocalDNS,
			MagicDNS:         magicDNS,
		},
	}

	for domain, servers := range s.SplitNameservers {
		out.SplitDNS[domain] = resolvers(servers, s.SplitUseWithExitNode[domain])
	}

	return out
}

func resolvers(addresses, withExitNode []string) []DNSResolver {
	out := make([]DNSResolver, 0, len(addresses))
	for _, a := range addresses {
		out = append(out, DNSResolver{Address: a, UseWithExitNode: slices.Contains(withExitNode, a)})
	}

	return out
}
