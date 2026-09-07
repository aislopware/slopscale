package apiv1

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/audit"
	"github.com/juanfont/headscale/hscontrol/scope"
	"github.com/juanfont/headscale/hscontrol/state"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/util"
)

func init() {
	registrations = append(registrations, registerAccessRequests)
}

// AccessRequest is a user's ask to join a requestable group for a while,
// for one machine or for every machine they own, and what came of it.
type AccessRequest struct {
	ID     string `format:"uint64" json:"id"`
	UserID string `format:"uint64" json:"userId"`
	// NodeID is the machine the access is for; empty means every machine
	// the user owns.
	NodeID  string `format:"uint64" json:"nodeId,omitempty"`
	GroupID string `format:"uint64" json:"groupId"`
	Reason  string `json:"reason"`
	// DurationSeconds is how long the membership lasts once approved.
	DurationSeconds int64  `json:"durationSeconds"`
	Status          string `doc:"One of pending, approved, denied, cancelled." json:"status"`
	// DecidedBy names who approved or denied.
	DecidedBy string     `json:"decidedBy"`
	Note      string     `doc:"What the approver said." json:"note"`
	CreatedAt time.Time  `json:"createdAt"`
	DecidedAt *time.Time `json:"decidedAt"              nullable:"true"`
	// ExpiresAt is when the granted membership ends; set on approval.
	ExpiresAt *time.Time `json:"expiresAt" nullable:"true"`
}

// AccessRequestBody files a request.
type AccessRequestBody struct {
	GroupID string `format:"uint64" json:"groupId"`
	// NodeID limits the access to one machine; omitted means every
	// machine the user owns.
	NodeID          string `format:"uint64"                                             json:"nodeId,omitempty"`
	DurationSeconds int64  `doc:"Between 300 (five minutes) and 2592000 (thirty days)." json:"durationSeconds"`
	Reason          string `json:"reason,omitempty"`
}

// AccessDecisionBody approves or denies a request.
type AccessDecisionBody struct {
	Note string `json:"note,omitempty"`
	// DurationSeconds overrides what was asked for; only an approval
	// reads it.
	DurationSeconds int64 `json:"durationSeconds,omitempty"`
}

// AccessRequestOption is a group a user may ask to join, or a machine
// the ask can be for.
type AccessRequestOption struct {
	ID          string `format:"uint64"              json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type (
	requestIDInput struct {
		ID string `format:"uint64" path:"id"`
	}
	listRequestsInput struct {
		Status string `doc:"Only requests in this status."                       query:"status"`
		Mine   bool   `doc:"Only the caller's own requests, whatever the scope." query:"mine"`
	}
	requestBodyInput struct {
		Body AccessRequestBody
	}
	decisionInput struct {
		ID   string `format:"uint64" path:"id"`
		Body AccessDecisionBody
	}
	requestOutput struct {
		Body struct {
			Request AccessRequest `json:"request"`
		}
	}
	listRequestsOutput struct {
		Body struct {
			Requests []AccessRequest `json:"requests" nullable:"false"`
			// CanDecide reports whether the caller may approve or deny.
			CanDecide bool `json:"canDecide"`
		}
	}
	requestOptionsOutput struct {
		Body struct {
			Groups []AccessRequestOption `json:"groups" nullable:"false"`
			Nodes  []AccessRequestOption `json:"nodes"  nullable:"false"`
		}
	}
)

func accessRequestFrom(r types.AccessRequest) AccessRequest {
	out := AccessRequest{
		ID:              formatID(uint64(r.ID)),
		UserID:          formatID(uint64(r.UserID)),
		GroupID:         formatID(uint64(r.GroupID)),
		Reason:          r.Reason,
		DurationSeconds: int64(r.Duration / time.Second),
		Status:          string(r.Status),
		DecidedBy:       r.DecidedBy,
		Note:            r.Note,
		CreatedAt:       r.CreatedAt,
		DecidedAt:       r.DecidedAt,
		ExpiresAt:       r.ExpiresAt,
	}

	if r.NodeID != nil {
		out.NodeID = formatID(r.NodeID.Uint64())
	}

	return out
}

func parseAccessRequestID(s string) (types.AccessRequestID, error) {
	id, err := strconv.ParseUint(s, util.Base10, 64)
	if err != nil {
		return 0, huma.Error400BadRequest("invalid access request id")
	}

	return types.AccessRequestID(id), nil
}

// deciderName is who a decision is recorded against: the user's name
// when the credential has one, otherwise the credential.
func deciderName(ctx context.Context, b Backend) string {
	p := caller(ctx)
	if p.HasUser() {
		user, err := b.State.GetUserByID(p.UserID)
		if err == nil {
			return user.Name
		}
	}

	if p.Credential != "" {
		return p.Credential
	}

	return "local"
}

// requireRequestAccess lets a caller without the policy scope see only
// its own requests. Not found, so the request's existence is not
// revealed.
func requireRequestAccess(ctx context.Context, req types.AccessRequest) error {
	p := caller(ctx)
	if p.Allows(scope.PolicyFileRead) {
		return nil
	}

	if !p.HasUser() || p.UserID != req.UserID {
		return huma.Error404NotFound("access request not found")
	}

	return nil
}

// registerAccessRequests adds the request flow. Filing and listing one's
// own requests need no scope, so a member may use them; deciding needs
// the policy scope.
func registerAccessRequests(api huma.API, b Backend) {
	registerAccessRequestReads(api, b)
	registerAccessRequestWrites(api, b)
}

func registerAccessRequestReads(api huma.API, b Backend) {
	huma.Register(api, huma.Operation{
		OperationID: "listAccessRequestOptions",
		Method:      http.MethodGet,
		Path:        "/api/v1/access-request/options",
		Summary:     "What the caller may request",
		Description: "The groups that take access requests and the caller's own machines. " +
			"Any authenticated caller may ask; a credential without a user gets no machines.",
		Tags:     []string{tagAccessControl},
		Security: bearerAuth,
	}, func(ctx context.Context, _ *struct{}) (*requestOptionsOutput, error) {
		opts := b.State.AccessRequestOptionsFor(caller(ctx).UserID)

		out := &requestOptionsOutput{}
		out.Body.Groups = make([]AccessRequestOption, 0, len(opts.Groups))
		out.Body.Nodes = make([]AccessRequestOption, 0, len(opts.Nodes))

		for _, g := range opts.Groups {
			out.Body.Groups = append(out.Body.Groups, AccessRequestOption{
				ID: formatID(uint64(g.ID)), Name: g.Name, Description: g.Description,
			})
		}

		for _, n := range opts.Nodes {
			out.Body.Nodes = append(out.Body.Nodes, AccessRequestOption{
				ID: formatID(n.ID().Uint64()), Name: n.GivenName(), Description: n.Hostname(),
			})
		}

		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listAccessRequests",
		Method:      http.MethodGet,
		Path:        "/api/v1/access-request",
		Summary:     "List access requests",
		Description: "Every request, newest first, for a caller with policy_file:read; " +
			"a caller without it sees its own.",
		Tags:     []string{tagAccessControl},
		Security: bearerAuth,
	}, func(ctx context.Context, in *listRequestsInput) (*listRequestsOutput, error) {
		p := caller(ctx)
		canDecide := p.Allows(scope.PolicyFile)

		var userID *types.UserID

		if in.Mine || !p.Allows(scope.PolicyFileRead) {
			if !p.HasUser() {
				return nil, huma.Error403Forbidden("the credential has no user, so it has no requests of its own")
			}

			id := p.UserID
			userID = &id
		}

		requests, err := b.State.ListAccessRequests(userID)
		if err != nil {
			return nil, mapError("listing access requests", err)
		}

		out := &listRequestsOutput{}
		out.Body.Requests = make([]AccessRequest, 0, len(requests))
		out.Body.CanDecide = canDecide

		for _, r := range requests {
			if in.Status != "" && string(r.Status) != in.Status {
				continue
			}

			out.Body.Requests = append(out.Body.Requests, accessRequestFrom(r))
		}

		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getAccessRequest",
		Method:      http.MethodGet,
		Path:        "/api/v1/access-request/{id}",
		Summary:     "Get access request",
		Tags:        []string{tagAccessControl},
		Security:    bearerAuth,
	}, func(ctx context.Context, in *requestIDInput) (*requestOutput, error) {
		id, err := parseAccessRequestID(in.ID)
		if err != nil {
			return nil, err
		}

		req, err := b.State.GetAccessRequest(id)
		if err != nil {
			return nil, mapError("loading access request", err)
		}

		err = requireRequestAccess(ctx, req)
		if err != nil {
			return nil, err
		}

		out := &requestOutput{}
		out.Body.Request = accessRequestFrom(req)

		return out, nil
	})
}

func registerAccessRequestWrites(api huma.API, b Backend) {
	huma.Register(api, audited(huma.Operation{
		OperationID: "createAccessRequest",
		Method:      http.MethodPost,
		Path:        "/api/v1/access-request",
		Summary:     "Request access",
		Description: "Files the caller's ask to join a requestable group for a while, for one of " +
			"their machines or for every machine they own. The credential must belong to a user.",
		Tags:     []string{tagAccessControl},
		Security: bearerAuth,
	}, "access_request.create", "access_request", ""), func(
		ctx context.Context, in *requestBodyInput,
	) (*requestOutput, error) {
		p := caller(ctx)
		if !p.HasUser() {
			return nil, mapError("filing access request", types.ErrAccessRequestNoUser)
		}

		groupID, err := parseGroupID(in.Body.GroupID)
		if err != nil {
			return nil, err
		}

		req := types.AccessRequest{
			UserID:   p.UserID,
			GroupID:  groupID,
			Reason:   in.Body.Reason,
			Duration: time.Duration(in.Body.DurationSeconds) * time.Second,
		}

		if in.Body.NodeID != "" {
			nodeID, parseErr := parseNodeID(in.Body.NodeID)
			if parseErr != nil {
				return nil, parseErr
			}

			req.NodeID = &nodeID
		}

		created, err := b.State.CreateAccessRequest(req)
		if err != nil {
			return nil, mapError("filing access request", err)
		}

		audit.Target(ctx, "access_request", created.ID.String(), "")
		audit.Detail(ctx, "groupId", in.Body.GroupID)
		audit.Detail(ctx, "durationSeconds", in.Body.DurationSeconds)

		out := &requestOutput{}
		out.Body.Request = accessRequestFrom(created)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "approveAccessRequest",
		Method:      http.MethodPost,
		Path:        "/api/v1/access-request/{id}/approve",
		Summary:     "Approve access request",
		Description: "Adds the membership for the requested duration, or the one given, " +
			"and rebuilds the policy. Nobody approves their own request.",
		Tags:     []string{tagAccessControl},
		Security: bearerAuth,
	}, scope.PolicyFile), "access_request.approve", "access_request", "id"), func(
		ctx context.Context, in *decisionInput,
	) (*requestOutput, error) {
		id, err := parseAccessRequestID(in.ID)
		if err != nil {
			return nil, err
		}

		req, c, err := b.State.ApproveAccessRequest(id, decisionFrom(ctx, b, in.Body))
		if err != nil {
			return nil, mapError("approving access request", err)
		}

		b.Change(c)

		if req.ExpiresAt != nil {
			audit.Detail(ctx, "expiresAt", req.ExpiresAt.Format(time.RFC3339))
		}

		out := &requestOutput{}
		out.Body.Request = accessRequestFrom(req)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "denyAccessRequest",
		Method:      http.MethodPost,
		Path:        "/api/v1/access-request/{id}/deny",
		Summary:     "Deny access request",
		Tags:        []string{tagAccessControl},
		Security:    bearerAuth,
	}, scope.PolicyFile), "access_request.deny", "access_request", "id"), func(
		ctx context.Context, in *decisionInput,
	) (*requestOutput, error) {
		id, err := parseAccessRequestID(in.ID)
		if err != nil {
			return nil, err
		}

		req, err := b.State.DenyAccessRequest(id, decisionFrom(ctx, b, in.Body))
		if err != nil {
			return nil, mapError("denying access request", err)
		}

		out := &requestOutput{}
		out.Body.Request = accessRequestFrom(req)

		return out, nil
	})

	huma.Register(api, audited(huma.Operation{
		OperationID: "cancelAccessRequest",
		Method:      http.MethodDelete,
		Path:        "/api/v1/access-request/{id}",
		Summary:     "Cancel or delete access request",
		Description: "The requester withdraws a pending request. A caller with policy_file " +
			"withdraws any pending request, or deletes a decided one from the record.",
		Tags:     []string{tagAccessControl},
		Security: bearerAuth,
	}, "access_request.cancel", "access_request", "id"), func(
		ctx context.Context, in *requestIDInput,
	) (*emptyOutput, error) {
		id, err := parseAccessRequestID(in.ID)
		if err != nil {
			return nil, err
		}

		err = cancelOrDelete(ctx, b, id)
		if err != nil {
			return nil, err
		}

		return &emptyOutput{}, nil
	})
}

// cancelOrDelete withdraws a pending request for its requester or a
// policy holder, and deletes a decided one for a policy holder.
func cancelOrDelete(ctx context.Context, b Backend, id types.AccessRequestID) error {
	req, err := b.State.GetAccessRequest(id)
	if err != nil {
		return mapError("loading access request", err)
	}

	p := caller(ctx)

	switch {
	case p.Allows(scope.PolicyFile) && !req.Pending():
		err = b.State.DeleteAccessRequest(id)
	case p.Allows(scope.PolicyFile) || (p.HasUser() && p.UserID == req.UserID):
		_, err = b.State.CancelAccessRequest(id, deciderName(ctx, b))
	default:
		return huma.Error404NotFound("access request not found")
	}

	if err != nil {
		return mapError("cancelling access request", err)
	}

	return nil
}

func decisionFrom(ctx context.Context, b Backend, body AccessDecisionBody) state.AccessDecision {
	p := caller(ctx)

	return state.AccessDecision{
		DeciderUserID: p.UserID,
		DecidedBy:     deciderName(ctx, b),
		Note:          body.Note,
		Duration:      time.Duration(body.DurationSeconds) * time.Second,
	}
}
