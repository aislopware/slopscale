package apiv1

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/audit"
	"github.com/juanfont/headscale/hscontrol/scope"
	"github.com/juanfont/headscale/hscontrol/types"
)

func init() {
	registrations = append(registrations, registerGroupDNSRules, registerGroupDNSRuleDelete)
}

// DNSRule is split DNS for the machines of some groups only.
type DNSRule struct {
	ID          string    `format:"uint64"                                          json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Enabled     bool      `json:"enabled"`
	Domains     []string  `doc:"Zones the nameservers answer for."                  json:"domains"     nullable:"false"`
	Nameservers []string  `doc:"Resolvers, as IP, IP:port or a provider's DoH URL." json:"nameservers" nullable:"false"`
	GroupIDs    []string  `doc:"Groups whose machines receive the rule."            json:"groupIds"    nullable:"false"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// DNSRuleRequestBody creates or replaces a rule.
type DNSRuleRequestBody struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Enabled     *bool    `doc:"Defaults to true."                                  json:"enabled,omitempty"`
	Domains     []string `doc:"Zones the nameservers answer for."                  json:"domains"`
	Nameservers []string `doc:"Resolvers, as IP, IP:port or a provider's DoH URL." json:"nameservers"`
	GroupIDs    []string `doc:"Groups whose machines receive the rule."            json:"groupIds"`
}

type (
	dnsRuleIDInput struct {
		ID string `format:"uint64" path:"id"`
	}
	dnsRuleBodyInput struct {
		Body DNSRuleRequestBody
	}
	dnsRuleUpdateInput struct {
		ID   string `format:"uint64" path:"id"`
		Body DNSRuleRequestBody
	}
	dnsRuleOutput struct {
		Body struct {
			Rule DNSRule `json:"rule"`
		}
	}
	listDNSRulesOutput struct {
		Body struct {
			Rules []DNSRule `json:"rules" nullable:"false"`
		}
	}
)

func parseDNSRuleID(s string) (types.GroupDNSRuleID, error) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, huma.Error400BadRequest("invalid dns rule id", err)
	}

	return types.GroupDNSRuleID(id), nil
}

func dnsRuleFromBody(body DNSRuleRequestBody) (types.GroupDNSRule, error) {
	groups, err := parseGroupIDs("groupIds", body.GroupIDs)
	if err != nil {
		return types.GroupDNSRule{}, err
	}

	return types.GroupDNSRule{
		Name:        body.Name,
		Description: body.Description,
		Enabled:     body.Enabled == nil || *body.Enabled,
		Domains:     body.Domains,
		Nameservers: body.Nameservers,
		GroupIDs:    groups,
	}, nil
}

func dnsRuleFrom(r types.GroupDNSRule) DNSRule {
	out := DNSRule{
		ID:          formatID(uint64(r.ID)),
		Name:        r.Name,
		Description: r.Description,
		Enabled:     r.Enabled,
		Domains:     emptyIfNil(r.Domains),
		Nameservers: emptyIfNil(r.Nameservers),
		GroupIDs:    make([]string, 0, len(r.GroupIDs)),
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}

	for _, id := range r.GroupIDs {
		out.GroupIDs = append(out.GroupIDs, formatID(uint64(id)))
	}

	return out
}

func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}

	return s
}

func auditDNSRuleDetails(ctx context.Context, r types.GroupDNSRule) {
	audit.Detail(ctx, "enabled", r.Enabled)
	audit.Detail(ctx, "domains", r.Domains)
	audit.Detail(ctx, "nameservers", r.Nameservers)
	audit.Detail(ctx, "groupIds", r.GroupIDs)
}

func registerGroupDNSRules(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "listDNSRules",
		Method:      http.MethodGet,
		Path:        "/api/v1/dns/rule",
		Summary:     "List group DNS rules",
		Description: "A rule is split DNS for the machines of some groups only: queries for its " +
			"domains go to its nameservers on those machines.",
		Tags:     []string{tagDNS},
		Security: bearerAuth,
	}, scope.DNSRead), func(_ context.Context, _ *struct{}) (*listDNSRulesOutput, error) {
		rules := b.State.ListGroupDNSRules()

		out := &listDNSRulesOutput{}
		out.Body.Rules = make([]DNSRule, 0, len(rules))

		for _, r := range rules {
			out.Body.Rules = append(out.Body.Rules, dnsRuleFrom(r))
		}

		return out, nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "getDNSRule",
		Method:      http.MethodGet,
		Path:        "/api/v1/dns/rule/{id}",
		Summary:     "Get group DNS rule",
		Tags:        []string{tagDNS},
		Security:    bearerAuth,
	}, scope.DNSRead), func(_ context.Context, in *dnsRuleIDInput) (*dnsRuleOutput, error) {
		id, err := parseDNSRuleID(in.ID)
		if err != nil {
			return nil, err
		}

		rule, ok := b.State.GetGroupDNSRule(id)
		if !ok {
			return nil, mapError("getting dns rule", types.ErrGroupDNSRuleNotFound)
		}

		out := &dnsRuleOutput{}
		out.Body.Rule = dnsRuleFrom(rule)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "createDNSRule",
		Method:      http.MethodPost,
		Path:        "/api/v1/dns/rule",
		Summary:     "Create group DNS rule",
		Description: "The machines in the groups receive the rule at once.",
		Tags:        []string{tagDNS},
		Security:    bearerAuth,
	}, scope.DNS), "dns.rule.create", "dns_rule", ""), func(
		ctx context.Context, in *dnsRuleBodyInput,
	) (*dnsRuleOutput, error) {
		rule, err := dnsRuleFromBody(in.Body)
		if err != nil {
			return nil, err
		}

		created, c, err := b.State.CreateGroupDNSRule(rule)
		if err != nil {
			return nil, mapError("creating dns rule", err)
		}

		audit.Target(ctx, "", formatID(uint64(created.ID)), created.Name)
		auditDNSRuleDetails(ctx, created)

		b.Change(c)

		out := &dnsRuleOutput{}
		out.Body.Rule = dnsRuleFrom(created)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "updateDNSRule",
		Method:      http.MethodPut,
		Path:        "/api/v1/dns/rule/{id}",
		Summary:     "Replace group DNS rule",
		Tags:        []string{tagDNS},
		Security:    bearerAuth,
	}, scope.DNS), "dns.rule.update", "dns_rule", "id"), func(
		ctx context.Context, in *dnsRuleUpdateInput,
	) (*dnsRuleOutput, error) {
		return updateDNSRule(ctx, b, in)
	})
}

func updateDNSRule(ctx context.Context, b Backend, in *dnsRuleUpdateInput) (*dnsRuleOutput, error) {
	id, err := parseDNSRuleID(in.ID)
	if err != nil {
		return nil, err
	}

	rule, err := dnsRuleFromBody(in.Body)
	if err != nil {
		return nil, err
	}

	rule.ID = id

	updated, c, err := b.State.UpdateGroupDNSRule(rule)
	if err != nil {
		return nil, mapError("updating dns rule", err)
	}

	audit.Target(ctx, "", "", updated.Name)
	auditDNSRuleDetails(ctx, updated)

	b.Change(c)

	out := &dnsRuleOutput{}
	out.Body.Rule = dnsRuleFrom(updated)

	return out, nil
}

func registerGroupDNSRuleDelete(api huma.API, b Backend) {
	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "deleteDNSRule",
		Method:      http.MethodDelete,
		Path:        "/api/v1/dns/rule/{id}",
		Summary:     "Delete group DNS rule",
		Tags:        []string{tagDNS},
		Security:    bearerAuth,
	}, scope.DNS), "dns.rule.delete", "dns_rule", "id"), func(
		ctx context.Context, in *dnsRuleIDInput,
	) (*emptyOutput, error) {
		id, err := parseDNSRuleID(in.ID)
		if err != nil {
			return nil, err
		}

		rule, ok := b.State.GetGroupDNSRule(id)
		if !ok {
			return nil, mapError("deleting dns rule", types.ErrGroupDNSRuleNotFound)
		}

		audit.Target(ctx, "", "", rule.Name)

		c, err := b.State.DeleteGroupDNSRule(id)
		if err != nil {
			return nil, mapError("deleting dns rule", err)
		}

		b.Change(c)

		return &emptyOutput{}, nil
	})
}
