package apiv2

import (
	"context"
	"maps"
	"net/http"

	"github.com/aislopware/slopscale/hscontrol/api/principal"
	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/danielgtaylor/huma/v2"
)

func init() {
	registrations = append(registrations, registerDNS)
}

// DNSNameservers is the Tailscale nameservers body. overrideLocalDns is a
// Slopscale addition: Tailscale keeps it in its console only.
type DNSNameservers struct {
	DNS              []string `json:"dns"                        nullable:"false"`
	MagicDNS         bool     `json:"magicDNS,omitempty"`
	OverrideLocalDNS bool     `json:"overrideLocalDns,omitempty"`
}

// DNSPreferences is the Tailscale preferences body. MagicDNS is set in the
// config file here, so a POST may only repeat the current value.
type DNSPreferences struct {
	MagicDNS bool `json:"magicDNS"`
}

// DNSSearchPaths is the Tailscale search paths body.
type DNSSearchPaths struct {
	SearchPaths []string `json:"searchPaths" nullable:"false"`
}

// DNSSplit maps a domain to its resolvers. In a PATCH a null value
// removes the domain.
type DNSSplit map[string][]string

type (
	tailnetInput struct {
		Tailnet string `path:"tailnet"`
	}
	nameserversOutput struct {
		Body DNSNameservers
	}
	setNameserversInput struct {
		Tailnet string `path:"tailnet"`
		Body    struct {
			DNS              []string `json:"dns"`
			OverrideLocalDNS *bool    `json:"overrideLocalDns,omitempty"`
		}
	}
	preferencesOutput struct {
		Body DNSPreferences
	}
	setPreferencesInput struct {
		Tailnet string `path:"tailnet"`
		Body    DNSPreferences
	}
	searchPathsOutput struct {
		Body DNSSearchPaths
	}
	setSearchPathsInput struct {
		Tailnet string `path:"tailnet"`
		Body    DNSSearchPaths
	}
	splitOutput struct {
		Body DNSSplit
	}
	setSplitInput struct {
		Tailnet string `path:"tailnet"`
		Body    DNSSplit
	}
)

func dnsSplit(m map[string][]string) DNSSplit {
	if m == nil {
		return DNSSplit{}
	}

	return DNSSplit(m)
}

// updateDNS applies edit to the effective settings and stores the result.
// Extra records the dns.extra_records_path file owns are left out of the
// write, since they are not the tailnet's to change here.
func updateDNS(b Backend, edit func(*types.DNSSettings)) (types.DNSSettings, change.Change, error) {
	status := b.State.DNS()

	settings := status.Effective
	if status.ExtraRecordsPath != "" {
		settings.ExtraRecords = nil
	}

	edit(&settings)
	// The v2 endpoints edit one aspect at a time and know nothing of the
	// exit node flags, so drop the ones their edit made stale instead of
	// rejecting the edit.
	settings = settings.PruneUseWithExitNode()

	st, c, err := b.State.SetDNS(settings)
	if err != nil {
		return types.DNSSettings{}, change.Change{}, mapError("updating dns", err)
	}

	return st.Effective, c, nil
}

func registerDNS(api huma.API, b Backend) {
	registerDNSNameservers(api, b)
	registerDNSPreferences(api, b)
	registerDNSSearchPaths(api, b)
	registerDNSSplit(api, b)
}

var dnsTags = []string{"DNS", tagTailscaleCompat}

func registerDNSNameservers(api huma.API, b Backend) {
	huma.Register(api, principal.RequireScope(huma.Operation{
		OperationID: "getDNSNameservers",
		Method:      http.MethodGet,
		Path:        "/api/v2/tailnet/{tailnet}/dns/nameservers",
		Summary:     "Get DNS nameservers",
		Tags:        dnsTags,
		Security:    security,
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.DNSRead), func(_ context.Context, in *tailnetInput) (*nameserversOutput, error) {
		err := requireDefaultTailnet(in.Tailnet)
		if err != nil {
			return nil, err
		}

		st := b.State.DNS()

		return &nameserversOutput{Body: DNSNameservers{
			DNS:              emptyIfNil(st.Effective.Nameservers),
			MagicDNS:         st.MagicDNS,
			OverrideLocalDNS: st.Effective.OverrideLocalDNS,
		}}, nil
	})

	huma.Register(api, audit.Declare(principal.RequireScope(huma.Operation{
		OperationID: "setDNSNameservers",
		Method:      http.MethodPost,
		Path:        "/api/v2/tailnet/{tailnet}/dns/nameservers",
		Summary:     "Set DNS nameservers",
		Description: "Replaces the global nameservers. overrideLocalDns, when given, sets " +
			"whether clients use them for every query.",
		Tags:     dnsTags,
		Security: security,
		Errors:   []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.DNS), "dns.set", "", ""), func(ctx context.Context, in *setNameserversInput) (*nameserversOutput, error) {
		err := requireDefaultTailnet(in.Tailnet)
		if err != nil {
			return nil, err
		}

		effective, c, err := updateDNS(b, func(s *types.DNSSettings) {
			s.Nameservers = in.Body.DNS
			if in.Body.OverrideLocalDNS != nil {
				s.OverrideLocalDNS = *in.Body.OverrideLocalDNS
			}
		})
		if err != nil {
			return nil, err
		}

		audit.Detail(ctx, "nameservers", effective.Nameservers)
		b.Change(c)

		return &nameserversOutput{Body: DNSNameservers{
			DNS:              emptyIfNil(effective.Nameservers),
			MagicDNS:         b.Cfg.DNSConfig.MagicDNS,
			OverrideLocalDNS: effective.OverrideLocalDNS,
		}}, nil
	})
}

func registerDNSPreferences(api huma.API, b Backend) {
	huma.Register(api, principal.RequireScope(huma.Operation{
		OperationID: "getDNSPreferences",
		Method:      http.MethodGet,
		Path:        "/api/v2/tailnet/{tailnet}/dns/preferences",
		Summary:     "Get DNS preferences",
		Tags:        dnsTags,
		Security:    security,
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.DNSRead), func(_ context.Context, in *tailnetInput) (*preferencesOutput, error) {
		err := requireDefaultTailnet(in.Tailnet)
		if err != nil {
			return nil, err
		}

		return &preferencesOutput{Body: DNSPreferences{MagicDNS: b.Cfg.DNSConfig.MagicDNS}}, nil
	})

	huma.Register(api, audit.Declare(principal.RequireScope(huma.Operation{
		OperationID: "setDNSPreferences",
		Method:      http.MethodPost,
		Path:        "/api/v2/tailnet/{tailnet}/dns/preferences",
		Summary:     "Set DNS preferences",
		Description: "MagicDNS is set in the config file; the request is accepted only when it " +
			"repeats the current value.",
		Tags:     dnsTags,
		Security: security,
		Errors:   []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.DNS), "dns.set", "", ""), func(_ context.Context, in *setPreferencesInput) (*preferencesOutput, error) {
		err := requireDefaultTailnet(in.Tailnet)
		if err != nil {
			return nil, err
		}

		if in.Body.MagicDNS != b.Cfg.DNSConfig.MagicDNS {
			return nil, mapError("updating dns preferences", types.ErrDNSMagicDNSFromFile)
		}

		return &preferencesOutput{Body: DNSPreferences{MagicDNS: b.Cfg.DNSConfig.MagicDNS}}, nil
	})
}

func registerDNSSearchPaths(api huma.API, b Backend) {
	huma.Register(api, principal.RequireScope(huma.Operation{
		OperationID: "getDNSSearchPaths",
		Method:      http.MethodGet,
		Path:        "/api/v2/tailnet/{tailnet}/dns/searchpaths",
		Summary:     "Get DNS search paths",
		Tags:        dnsTags,
		Security:    security,
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.DNSRead), func(_ context.Context, in *tailnetInput) (*searchPathsOutput, error) {
		err := requireDefaultTailnet(in.Tailnet)
		if err != nil {
			return nil, err
		}

		return &searchPathsOutput{Body: DNSSearchPaths{
			SearchPaths: emptyIfNil(b.State.DNS().Effective.SearchDomains),
		}}, nil
	})

	huma.Register(api, audit.Declare(principal.RequireScope(huma.Operation{
		OperationID: "setDNSSearchPaths",
		Method:      http.MethodPost,
		Path:        "/api/v2/tailnet/{tailnet}/dns/searchpaths",
		Summary:     "Set DNS search paths",
		Description: "Replaces the search domains. The base domain is always searched and is not part of the list.",
		Tags:        dnsTags,
		Security:    security,
		Errors:      []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.DNS), "dns.set", "", ""), func(ctx context.Context, in *setSearchPathsInput) (*searchPathsOutput, error) {
		err := requireDefaultTailnet(in.Tailnet)
		if err != nil {
			return nil, err
		}

		effective, c, err := updateDNS(b, func(s *types.DNSSettings) {
			s.SearchDomains = in.Body.SearchPaths
		})
		if err != nil {
			return nil, err
		}

		audit.Detail(ctx, "searchDomains", effective.SearchDomains)
		b.Change(c)

		return &searchPathsOutput{Body: DNSSearchPaths{SearchPaths: emptyIfNil(effective.SearchDomains)}}, nil
	})
}

func registerDNSSplit(api huma.API, b Backend) {
	huma.Register(api, principal.RequireScope(huma.Operation{
		OperationID: "getDNSSplit",
		Method:      http.MethodGet,
		Path:        "/api/v2/tailnet/{tailnet}/dns/split-dns",
		Summary:     "Get split DNS",
		Tags:        dnsTags,
		Security:    security,
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.DNSRead), func(_ context.Context, in *tailnetInput) (*splitOutput, error) {
		err := requireDefaultTailnet(in.Tailnet)
		if err != nil {
			return nil, err
		}

		return &splitOutput{Body: dnsSplit(b.State.DNS().Effective.SplitNameservers)}, nil
	})

	huma.Register(api, audit.Declare(principal.RequireScope(huma.Operation{
		OperationID: "updateDNSSplit",
		Method:      http.MethodPatch,
		Path:        "/api/v2/tailnet/{tailnet}/dns/split-dns",
		Summary:     "Update split DNS",
		Description: "Sets the resolvers of the given domains; a null value removes the domain. " +
			"Other domains keep their resolvers.",
		Tags:     dnsTags,
		Security: security,
		Errors:   []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.DNS), "dns.set", "", ""), func(ctx context.Context, in *setSplitInput) (*splitOutput, error) {
		err := requireDefaultTailnet(in.Tailnet)
		if err != nil {
			return nil, err
		}

		effective, c, err := updateDNS(b, func(s *types.DNSSettings) {
			merged := maps.Clone(s.SplitNameservers)
			if merged == nil {
				merged = map[string][]string{}
			}

			for domain, servers := range in.Body {
				if servers == nil {
					delete(merged, domain)
				} else {
					merged[domain] = servers
				}
			}

			s.SplitNameservers = merged
		})
		if err != nil {
			return nil, err
		}

		audit.Detail(ctx, "splitDomains", splitDomains(effective.SplitNameservers))
		b.Change(c)

		return &splitOutput{Body: dnsSplit(effective.SplitNameservers)}, nil
	})

	huma.Register(api, audit.Declare(principal.RequireScope(huma.Operation{
		OperationID: "setDNSSplit",
		Method:      http.MethodPut,
		Path:        "/api/v2/tailnet/{tailnet}/dns/split-dns",
		Summary:     "Replace split DNS",
		Description: "Replaces every split DNS domain with the given map.",
		Tags:        dnsTags,
		Security:    security,
		Errors:      []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.DNS), "dns.set", "", ""), func(ctx context.Context, in *setSplitInput) (*splitOutput, error) {
		err := requireDefaultTailnet(in.Tailnet)
		if err != nil {
			return nil, err
		}

		effective, c, err := updateDNS(b, func(s *types.DNSSettings) {
			s.SplitNameservers = map[string][]string(in.Body)
		})
		if err != nil {
			return nil, err
		}

		audit.Detail(ctx, "splitDomains", splitDomains(effective.SplitNameservers))
		b.Change(c)

		return &splitOutput{Body: dnsSplit(effective.SplitNameservers)}, nil
	})
}

func splitDomains(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for d := range m {
		out = append(out, d)
	}

	return out
}
