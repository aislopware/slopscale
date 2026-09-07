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
	"github.com/juanfont/headscale/hscontrol/types/change"
)

// tagAccessControl groups the endpoints in the generated spec and docs.
const tagAccessControl = "Access control"

func init() {
	registrations = append(
		registrations,
		registerGroups,
		registerGroupMembers,
		registerAccessRules,
		registerAccessRuleSwitch,
	)
}

// Group is a named set of machines: the machines listed directly and
// every machine owned by the users listed. The builtin "all" group holds
// every machine and lists none.
type Group struct {
	ID          string   `format:"uint64"                                                      json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Builtin     string   `doc:"Empty for operator-made groups, \"all\" for the builtin group." json:"builtin"`
	Requestable bool     `doc:"Whether members may request to join the group for a while."     json:"requestable"`
	NodeIDs     []string `json:"nodeIds"                                                       nullable:"false"`
	UserIDs     []string `json:"userIds"                                                       nullable:"false"`
	// Expiries lists the temporary memberships; a member absent from it
	// is permanent.
	Expiries  []GroupMemberExpiry `json:"expiries"  nullable:"false"`
	CreatedAt time.Time           `json:"createdAt"`
	UpdatedAt time.Time           `json:"updatedAt"`
}

// GroupMemberExpiry is when a temporary membership ends.
type GroupMemberExpiry struct {
	NodeID    string    `format:"uint64"  json:"nodeId,omitempty"`
	UserID    string    `format:"uint64"  json:"userId,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// AccessRule lets the source groups reach the destination groups.
type AccessRule struct {
	ID          string `format:"uint64"                                               json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Protocol    string `doc:"One of all, tcp, udp, icmp."                             json:"protocol"`
	Ports       string `doc:"Comma-separated ports and ranges, empty for every port." json:"ports"`
	// Bidirectional lets both sides start connections; otherwise only
	// the sources do.
	Bidirectional       bool     `json:"bidirectional"`
	SourceGroupIDs      []string `json:"sourceGroupIds"      nullable:"false"`
	DestinationGroupIDs []string `json:"destinationGroupIds" nullable:"false"`
	// PostureIDs are the postures a source must satisfy, any one of
	// them; empty means the rule checks none.
	PostureIDs []string `json:"postureIds" nullable:"false"`
	// ExpiresAt is when the rule stops applying; null never does.
	ExpiresAt *time.Time `json:"expiresAt" nullable:"true"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

// GroupRequestBody creates or updates a group. Members are replaced when
// given and left alone when omitted.
type GroupRequestBody struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Requestable bool      `doc:"Whether members may ask to join for a while." json:"requestable,omitempty"`
	NodeIDs     *[]string `json:"nodeIds,omitempty"`
	UserIDs     *[]string `json:"userIds,omitempty"`
}

// GroupMemberRequestBody names a machine or user to add, for good or
// until an expiry.
type GroupMemberRequestBody struct {
	NodeID    string     `format:"uint64"                                         json:"nodeId,omitempty"`
	UserID    string     `format:"uint64"                                         json:"userId,omitempty"`
	ExpiresAt *time.Time `doc:"When the membership ends; omitted means for good." json:"expiresAt,omitempty"`
}

// AccessRuleRequestBody creates or replaces a rule.
type AccessRuleRequestBody struct {
	Name                string   `json:"name"`
	Description         string   `json:"description,omitempty"`
	Enabled             *bool    `doc:"Defaults to true."                                json:"enabled,omitempty"`
	Protocol            string   `doc:"One of all, tcp, udp, icmp."                      json:"protocol"`
	Ports               string   `json:"ports,omitempty"`
	Bidirectional       bool     `json:"bidirectional,omitempty"`
	SourceGroupIDs      []string `json:"sourceGroupIds"`
	DestinationGroupIDs []string `json:"destinationGroupIds"`
	PostureIDs          []string `doc:"Postures a source must satisfy, any one of them." json:"postureIds,omitempty"`
	// ExpiresAt is when the rule stops applying; omitted or null means
	// never.
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

type (
	groupIDInput struct {
		ID string `format:"uint64" path:"id"`
	}
	groupBodyInput struct {
		Body GroupRequestBody
	}
	groupUpdateInput struct {
		ID   string `format:"uint64" path:"id"`
		Body GroupRequestBody
	}
	groupMemberInput struct {
		ID   string `format:"uint64" path:"id"`
		Body GroupMemberRequestBody
	}
	groupNodeInput struct {
		ID     string `format:"uint64" path:"id"`
		NodeID string `format:"uint64" path:"nodeId"`
	}
	groupUserInput struct {
		ID     string `format:"uint64" path:"id"`
		UserID string `format:"uint64" path:"userId"`
	}
	groupOutput struct {
		Body struct {
			Group Group `json:"group"`
		}
	}
	listGroupsOutput struct {
		Body struct {
			Groups []Group `json:"groups" nullable:"false"`
		}
	}
	emptyOutput struct {
		Body struct{}
	}

	ruleIDInput struct {
		ID string `format:"uint64" path:"id"`
	}
	ruleBodyInput struct {
		Body AccessRuleRequestBody
	}
	ruleUpdateInput struct {
		ID   string `format:"uint64" path:"id"`
		Body AccessRuleRequestBody
	}
	ruleEnabledInput struct {
		ID   string `format:"uint64" path:"id"`
		Body struct {
			Enabled bool `json:"enabled"`
		}
	}
	ruleOutput struct {
		Body struct {
			Rule AccessRule `json:"rule"`
		}
	}
	listRulesOutput struct {
		Body struct {
			Rules []AccessRule `json:"rules" nullable:"false"`
			// PolicyFileEnforces tells a client what the rules stand on:
			// false means the tailnet is allow-all as soon as no rule is
			// enabled.
			PolicyFileEnforces bool `json:"policyFileEnforces"`
		}
	}
)

func groupFrom(g types.AccessGroup) Group {
	out := Group{
		ID:          formatID(uint64(g.ID)),
		Name:        g.Name,
		Description: g.Description,
		Builtin:     g.Builtin,
		Requestable: g.Requestable,
		NodeIDs:     make([]string, 0, len(g.NodeIDs)),
		UserIDs:     make([]string, 0, len(g.UserIDs)),
		Expiries:    []GroupMemberExpiry{},
		CreatedAt:   g.CreatedAt,
		UpdatedAt:   g.UpdatedAt,
	}

	for _, id := range g.NodeIDs {
		out.NodeIDs = append(out.NodeIDs, formatID(id.Uint64()))

		if at := g.NodeExpiry(id); !at.IsZero() {
			out.Expiries = append(out.Expiries, GroupMemberExpiry{NodeID: formatID(id.Uint64()), ExpiresAt: at})
		}
	}

	for _, id := range g.UserIDs {
		out.UserIDs = append(out.UserIDs, formatID(uint64(id)))

		if at := g.UserExpiry(id); !at.IsZero() {
			out.Expiries = append(out.Expiries, GroupMemberExpiry{UserID: formatID(uint64(id)), ExpiresAt: at})
		}
	}

	return out
}

func ruleFrom(r types.AccessRule) AccessRule {
	out := AccessRule{
		ID:                  formatID(uint64(r.ID)),
		Name:                r.Name,
		Description:         r.Description,
		Enabled:             r.Enabled,
		Protocol:            string(r.Protocol),
		Ports:               r.Ports,
		Bidirectional:       r.Bidirectional,
		SourceGroupIDs:      make([]string, 0, len(r.SourceGroupIDs)),
		DestinationGroupIDs: make([]string, 0, len(r.DestinationGroupIDs)),
		PostureIDs:          make([]string, 0, len(r.PostureIDs)),
		ExpiresAt:           r.ExpiresAt,
		CreatedAt:           r.CreatedAt,
		UpdatedAt:           r.UpdatedAt,
	}

	for _, id := range r.PostureIDs {
		out.PostureIDs = append(out.PostureIDs, formatID(uint64(id)))
	}

	for _, id := range r.SourceGroupIDs {
		out.SourceGroupIDs = append(out.SourceGroupIDs, formatID(uint64(id)))
	}

	for _, id := range r.DestinationGroupIDs {
		out.DestinationGroupIDs = append(out.DestinationGroupIDs, formatID(uint64(id)))
	}

	return out
}

func parseGroupID(s string) (types.GroupID, error) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, huma.Error400BadRequest("invalid group id", err)
	}

	return types.GroupID(id), nil
}

func parseGroupIDs(field string, ids []string) ([]types.GroupID, error) {
	out := make([]types.GroupID, 0, len(ids))

	for _, s := range ids {
		id, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid group id in "+field, err)
		}

		out = append(out, types.GroupID(id))
	}

	return out, nil
}

func parseNodeIDs(ids []string) ([]types.NodeID, error) {
	out := make([]types.NodeID, 0, len(ids))

	for _, s := range ids {
		id, err := parseNodeID(s)
		if err != nil {
			return nil, err
		}

		out = append(out, id)
	}

	return out, nil
}

func parseUserIDs(ids []string) ([]types.UserID, error) {
	out := make([]types.UserID, 0, len(ids))

	for _, s := range ids {
		id, err := parseUserID(s)
		if err != nil {
			return nil, err
		}

		out = append(out, id)
	}

	return out, nil
}

func parseRuleID(s string) (types.AccessRuleID, error) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, huma.Error400BadRequest("invalid access rule id", err)
	}

	return types.AccessRuleID(id), nil
}

// ruleFromBody turns a request into the rule the state validates.
func ruleFromBody(body AccessRuleRequestBody) (types.AccessRule, error) {
	sources, err := parseGroupIDs("sourceGroupIds", body.SourceGroupIDs)
	if err != nil {
		return types.AccessRule{}, err
	}

	destinations, err := parseGroupIDs("destinationGroupIds", body.DestinationGroupIDs)
	if err != nil {
		return types.AccessRule{}, err
	}

	postures, err := parsePostureIDs(body.PostureIDs)
	if err != nil {
		return types.AccessRule{}, err
	}

	return types.AccessRule{
		Name:                body.Name,
		Description:         body.Description,
		Enabled:             body.Enabled == nil || *body.Enabled,
		Protocol:            types.AccessProtocol(body.Protocol),
		Ports:               body.Ports,
		Bidirectional:       body.Bidirectional,
		SourceGroupIDs:      sources,
		DestinationGroupIDs: destinations,
		PostureIDs:          postures,
		ExpiresAt:           body.ExpiresAt,
	}, nil
}

func registerGroups(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "listGroups",
		Method:      http.MethodGet,
		Path:        "/api/v1/group",
		Summary:     "List groups",
		Description: "Every group with the IDs of its member machines and users. The builtin " +
			"\"all\" group holds every machine and lists none.",
		Tags:     []string{"Access control"},
		Security: bearerAuth,
	}, scope.PolicyFileRead), func(_ context.Context, _ *struct{}) (*listGroupsOutput, error) {
		model := b.State.AccessModel()

		out := &listGroupsOutput{}

		out.Body.Groups = make([]Group, 0, len(model.Groups))
		for _, g := range model.Groups {
			out.Body.Groups = append(out.Body.Groups, groupFrom(g))
		}

		return out, nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "getGroup",
		Method:      http.MethodGet,
		Path:        "/api/v1/group/{id}",
		Summary:     "Get group",
		Tags:        []string{tagAccessControl},
		Security:    bearerAuth,
	}, scope.PolicyFileRead), func(_ context.Context, in *groupIDInput) (*groupOutput, error) {
		id, err := parseGroupID(in.ID)
		if err != nil {
			return nil, err
		}

		group, err := b.State.GetGroup(id)
		if err != nil {
			return nil, mapError("getting group", err)
		}

		out := &groupOutput{}
		out.Body.Group = groupFrom(group)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "createGroup",
		Method:      http.MethodPost,
		Path:        "/api/v1/group",
		Summary:     "Create group",
		Description: "Creates a group, with its machines and users when given.",
		Tags:        []string{tagAccessControl},
		Security:    bearerAuth,
	}, scope.PolicyFile), "group.create", "group", ""), func(
		ctx context.Context, in *groupBodyInput,
	) (*groupOutput, error) {
		group, c, err := b.State.CreateGroup(in.Body.Name, in.Body.Description, in.Body.Requestable)
		if err != nil {
			return nil, mapError("creating group", err)
		}

		audit.Target(ctx, "", formatID(uint64(group.ID)), group.Name)

		b.Change(c)

		if in.Body.NodeIDs != nil || in.Body.UserIDs != nil {
			group, c, err = setGroupMembersFromBody(b, group, in.Body)
			if err != nil {
				return nil, err
			}

			b.Change(c)
		}

		out := &groupOutput{}
		out.Body.Group = groupFrom(group)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "updateGroup",
		Method:      http.MethodPatch,
		Path:        "/api/v1/group/{id}",
		Summary:     "Update group",
		Description: "Renames or re-describes a group and replaces its machines and users when " +
			"given. The builtin group cannot be changed.",
		Tags:     []string{"Access control"},
		Security: bearerAuth,
	}, scope.PolicyFile), "group.update", "group", "id"), func(
		ctx context.Context, in *groupUpdateInput,
	) (*groupOutput, error) {
		id, err := parseGroupID(in.ID)
		if err != nil {
			return nil, err
		}

		group, c, err := b.State.UpdateGroup(id, in.Body.Name, in.Body.Description, in.Body.Requestable)
		if err != nil {
			return nil, mapError("updating group", err)
		}

		audit.Target(ctx, "", "", group.Name)

		b.Change(c)

		if in.Body.NodeIDs != nil || in.Body.UserIDs != nil {
			group, c, err = setGroupMembersFromBody(b, group, in.Body)
			if err != nil {
				return nil, err
			}

			b.Change(c)
		}

		out := &groupOutput{}
		out.Body.Group = groupFrom(group)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "deleteGroup",
		Method:      http.MethodDelete,
		Path:        "/api/v1/group/{id}",
		Summary:     "Delete group",
		Description: "Deletes a group no access rule names. The builtin group cannot be deleted.",
		Tags:        []string{tagAccessControl},
		Security:    bearerAuth,
	}, scope.PolicyFile), "group.delete", "group", "id"), deleteGroup(b))
}

// deleteGroup names the group on the audit event before it is gone, then
// lets the state refuse the builtin group or one a rule still names.
func deleteGroup(b Backend) func(ctx context.Context, in *groupIDInput) (*emptyOutput, error) {
	return func(ctx context.Context, in *groupIDInput) (*emptyOutput, error) {
		id, err := parseGroupID(in.ID)
		if err != nil {
			return nil, err
		}

		group, getErr := b.State.GetGroup(id)
		if getErr == nil {
			audit.Target(ctx, "", "", group.Name)
		}

		c, err := b.State.DeleteGroup(id)
		if err != nil {
			return nil, mapError("deleting group", err)
		}

		b.Change(c)

		return &emptyOutput{}, nil
	}
}

// registerGroupMembers adds the endpoints that change one membership at a
// time; the console's dialogs and the CLI use these rather than PATCH.
func registerGroupMembers(api huma.API, b Backend) {
	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "addGroupMember",
		Method:      http.MethodPost,
		Path:        "/api/v1/group/{id}/member",
		Summary:     "Add group member",
		Description: "Adds a machine (nodeId) or a user (userId) to the group, for good or until expiresAt.",
		Tags:        []string{tagAccessControl},
		Security:    bearerAuth,
	}, scope.PolicyFile), "group.member.add", "group", "id"), func(
		ctx context.Context, in *groupMemberInput,
	) (*groupOutput, error) {
		id, err := parseGroupID(in.ID)
		if err != nil {
			return nil, err
		}

		var (
			group types.AccessGroup
			c     change.Change
		)

		if in.Body.ExpiresAt != nil {
			audit.Detail(ctx, "expiresAt", in.Body.ExpiresAt.Format(time.RFC3339))
		}

		switch {
		case in.Body.NodeID != "" && in.Body.UserID != "":
			return nil, huma.Error400BadRequest("give either nodeId or userId, not both")
		case in.Body.NodeID != "":
			nodeID, parseErr := parseNodeID(in.Body.NodeID)
			if parseErr != nil {
				return nil, parseErr
			}

			audit.Detail(ctx, "nodeId", in.Body.NodeID)

			group, c, err = b.State.AddGroupNode(id, nodeID, in.Body.ExpiresAt)
		case in.Body.UserID != "":
			userID, parseErr := parseUserID(in.Body.UserID)
			if parseErr != nil {
				return nil, parseErr
			}

			audit.Detail(ctx, "userId", in.Body.UserID)

			group, c, err = b.State.AddGroupUser(id, userID, in.Body.ExpiresAt)
		default:
			return nil, huma.Error400BadRequest("give nodeId or userId")
		}

		if err != nil {
			return nil, mapError("adding group member", err)
		}

		audit.Target(ctx, "", "", group.Name)

		b.Change(c)

		out := &groupOutput{}
		out.Body.Group = groupFrom(group)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "removeGroupNode",
		Method:      http.MethodDelete,
		Path:        "/api/v1/group/{id}/node/{nodeId}",
		Summary:     "Remove machine from group",
		Tags:        []string{tagAccessControl},
		Security:    bearerAuth,
	}, scope.PolicyFile), "group.member.remove", "group", "id"), func(
		ctx context.Context, in *groupNodeInput,
	) (*groupOutput, error) {
		id, err := parseGroupID(in.ID)
		if err != nil {
			return nil, err
		}

		nodeID, err := parseNodeID(in.NodeID)
		if err != nil {
			return nil, err
		}

		audit.Detail(ctx, "nodeId", in.NodeID)

		return removeGroupMember(ctx, b, func() (types.AccessGroup, change.Change, error) {
			return b.State.RemoveGroupNode(id, nodeID)
		})
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "removeGroupUser",
		Method:      http.MethodDelete,
		Path:        "/api/v1/group/{id}/user/{userId}",
		Summary:     "Remove user from group",
		Tags:        []string{tagAccessControl},
		Security:    bearerAuth,
	}, scope.PolicyFile), "group.member.remove", "group", "id"), func(
		ctx context.Context, in *groupUserInput,
	) (*groupOutput, error) {
		id, err := parseGroupID(in.ID)
		if err != nil {
			return nil, err
		}

		userID, err := parseUserID(in.UserID)
		if err != nil {
			return nil, err
		}

		audit.Detail(ctx, "userId", in.UserID)

		return removeGroupMember(ctx, b, func() (types.AccessGroup, change.Change, error) {
			return b.State.RemoveGroupUser(id, userID)
		})
	})
}

// removeGroupMember runs one membership removal and shapes its result the way
// both remove endpoints answer: the group name on the audit event, the change
// broadcast, the updated group in the body.
func removeGroupMember(
	ctx context.Context,
	b Backend,
	remove func() (types.AccessGroup, change.Change, error),
) (*groupOutput, error) {
	group, c, err := remove()
	if err != nil {
		return nil, mapError("removing group member", err)
	}

	audit.Target(ctx, "", "", group.Name)

	b.Change(c)

	out := &groupOutput{}
	out.Body.Group = groupFrom(group)

	return out, nil
}

// setGroupMembersFromBody replaces the members a create or update body
// carries, keeping the side it omits.
func setGroupMembersFromBody(b Backend, group types.AccessGroup, body GroupRequestBody) (
	types.AccessGroup, change.Change, error,
) {
	nodeIDs := group.NodeIDs
	userIDs := group.UserIDs

	if body.NodeIDs != nil {
		parsed, err := parseNodeIDs(*body.NodeIDs)
		if err != nil {
			return types.AccessGroup{}, change.Change{}, err
		}

		nodeIDs = parsed
	}

	if body.UserIDs != nil {
		parsed, err := parseUserIDs(*body.UserIDs)
		if err != nil {
			return types.AccessGroup{}, change.Change{}, err
		}

		userIDs = parsed
	}

	group, c, err := b.State.SetGroupMembers(group.ID, nodeIDs, userIDs)
	if err != nil {
		return types.AccessGroup{}, change.Change{}, mapError("setting group members", err)
	}

	return group, c, nil
}

func registerAccessRules(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "listAccessRules",
		Method:      http.MethodGet,
		Path:        "/api/v1/access-rule",
		Summary:     "List access rules",
		Tags:        []string{tagAccessControl},
		Security:    bearerAuth,
	}, scope.PolicyFileRead), func(_ context.Context, _ *struct{}) (*listRulesOutput, error) {
		rules := b.State.ListAccessRules()

		out := &listRulesOutput{}
		out.Body.PolicyFileEnforces = b.State.PolicyFileEnforces()

		out.Body.Rules = make([]AccessRule, 0, len(rules))
		for _, r := range rules {
			out.Body.Rules = append(out.Body.Rules, ruleFrom(r))
		}

		return out, nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "getAccessRule",
		Method:      http.MethodGet,
		Path:        "/api/v1/access-rule/{id}",
		Summary:     "Get access rule",
		Tags:        []string{tagAccessControl},
		Security:    bearerAuth,
	}, scope.PolicyFileRead), func(_ context.Context, in *ruleIDInput) (*ruleOutput, error) {
		id, err := parseRuleID(in.ID)
		if err != nil {
			return nil, err
		}

		rule, err := b.State.GetAccessRule(id)
		if err != nil {
			return nil, mapError("getting access rule", err)
		}

		out := &ruleOutput{}
		out.Body.Rule = ruleFrom(rule)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "createAccessRule",
		Method:      http.MethodPost,
		Path:        "/api/v1/access-rule",
		Summary:     "Create access rule",
		Description: "Lets the source groups reach the destination groups on the protocol and " +
			"ports. Rules only allow: once one is enabled, or the policy file has rules, " +
			"everything not allowed is denied.",
		Tags:     []string{"Access control"},
		Security: bearerAuth,
	}, scope.PolicyFile), "access_rule.create", "access_rule", ""), func(
		ctx context.Context, in *ruleBodyInput,
	) (*ruleOutput, error) {
		rule, err := ruleFromBody(in.Body)
		if err != nil {
			return nil, err
		}

		created, c, err := b.State.CreateAccessRule(rule)
		if err != nil {
			return nil, mapError("creating access rule", err)
		}

		audit.Target(ctx, "", formatID(uint64(created.ID)), created.Name)
		auditRuleDetails(ctx, created)

		b.Change(c)

		out := &ruleOutput{}
		out.Body.Rule = ruleFrom(created)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "updateAccessRule",
		Method:      http.MethodPut,
		Path:        "/api/v1/access-rule/{id}",
		Summary:     "Replace access rule",
		Tags:        []string{tagAccessControl},
		Security:    bearerAuth,
	}, scope.PolicyFile), "access_rule.update", "access_rule", "id"), func(
		ctx context.Context, in *ruleUpdateInput,
	) (*ruleOutput, error) {
		id, err := parseRuleID(in.ID)
		if err != nil {
			return nil, err
		}

		rule, err := ruleFromBody(in.Body)
		if err != nil {
			return nil, err
		}

		rule.ID = id

		updated, c, err := b.State.UpdateAccessRule(rule)
		if err != nil {
			return nil, mapError("updating access rule", err)
		}

		audit.Target(ctx, "", "", updated.Name)
		auditRuleDetails(ctx, updated)

		b.Change(c)

		out := &ruleOutput{}
		out.Body.Rule = ruleFrom(updated)

		return out, nil
	})
}

// registerAccessRuleSwitch adds the endpoints that act on a rule without a
// body of its own: the enable switch and delete.
func registerAccessRuleSwitch(api huma.API, b Backend) {
	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "setAccessRuleEnabled",
		Method:      http.MethodPatch,
		Path:        "/api/v1/access-rule/{id}",
		Summary:     "Enable or disable access rule",
		Description: "Changes only the switch; the rest of the rule is read from the server, not the request.",
		Tags:        []string{tagAccessControl},
		Security:    bearerAuth,
	}, scope.PolicyFile), "access_rule.update", "access_rule", "id"), func(
		ctx context.Context, in *ruleEnabledInput,
	) (*ruleOutput, error) {
		id, err := parseRuleID(in.ID)
		if err != nil {
			return nil, err
		}

		updated, c, err := b.State.SetAccessRuleEnabled(id, in.Body.Enabled)
		if err != nil {
			return nil, mapError("updating access rule", err)
		}

		audit.Target(ctx, "", "", updated.Name)
		audit.Detail(ctx, "enabled", in.Body.Enabled)

		b.Change(c)

		out := &ruleOutput{}
		out.Body.Rule = ruleFrom(updated)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "deleteAccessRule",
		Method:      http.MethodDelete,
		Path:        "/api/v1/access-rule/{id}",
		Summary:     "Delete access rule",
		Tags:        []string{tagAccessControl},
		Security:    bearerAuth,
	}, scope.PolicyFile), "access_rule.delete", "access_rule", "id"), func(
		ctx context.Context, in *ruleIDInput,
	) (*emptyOutput, error) {
		id, err := parseRuleID(in.ID)
		if err != nil {
			return nil, err
		}

		rule, getErr := b.State.GetAccessRule(id)
		if getErr == nil {
			audit.Target(ctx, "", "", rule.Name)
		}

		c, err := b.State.DeleteAccessRule(id)
		if err != nil {
			return nil, mapError("deleting access rule", err)
		}

		b.Change(c)

		return &emptyOutput{}, nil
	})
}

func auditRuleDetails(ctx context.Context, rule types.AccessRule) {
	audit.Detail(ctx, "enabled", rule.Enabled)
	audit.Detail(ctx, "protocol", string(rule.Protocol))

	if rule.Ports != "" {
		audit.Detail(ctx, "ports", rule.Ports)
	}

	audit.Detail(ctx, "bidirectional", rule.Bidirectional)

	if len(rule.PostureIDs) > 0 {
		audit.Detail(ctx, "postures", len(rule.PostureIDs))
	}

	if rule.ExpiresAt != nil {
		audit.Detail(ctx, "expiresAt", rule.ExpiresAt.Format(time.RFC3339))
	}
}
