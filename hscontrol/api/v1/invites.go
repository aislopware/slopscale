package apiv1

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/audit"
	"github.com/juanfont/headscale/hscontrol/scope"
	"github.com/juanfont/headscale/hscontrol/state"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/web"
)

func init() {
	registrations = append(registrations, registerInvites)
}

// tagInvites groups the invitation operations in the emitted spec.
const tagInvites = "Invites"

// Invite is an invitation to join the tailnet. The token never appears
// here; it is part of the URL, which is shown once when the invite is
// created or re-sent.
type Invite struct {
	ID       string   `format:"uint64"                                  json:"id"`
	Email    string   `json:"email"`
	Role     string   `doc:"The role the invited user is created with." json:"role"`
	GroupIDs []string `json:"groupIds"                                  nullable:"false"`

	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
	CreatedBy string    `doc:"Who sent it; empty when unknown." format:"uint64" json:"createdBy,omitempty"`

	Accepted       bool       `json:"accepted"`
	AcceptedAt     *time.Time `json:"acceptedAt"                  nullable:"true"`
	AcceptedUserID string     `doc:"The user the invite created." format:"uint64" json:"acceptedUserId,omitempty"`

	Expired bool `doc:"True once the invite can no longer be accepted." json:"expired"`
}

// CreateInviteRequestBody is the body of createInvite.
type CreateInviteRequestBody struct {
	Email    string   `doc:"The invited address."                          json:"email"`
	Role     string   `doc:"owner is refused; defaults to member."         json:"role,omitempty"`
	GroupIDs []string `doc:"Groups the invited user joins."                json:"groupIds,omitempty" nullable:"false"`
	Expiry   string   `doc:"A Go duration; 720h at most, 168h by default." json:"expiry,omitempty"`
}

// ResendInviteRequestBody is the body of resendInvite; every field is
// optional.
type ResendInviteRequestBody struct {
	Expiry string `doc:"How long the new link works for; 168h by default, 720h at most." json:"expiry,omitempty"`
}

type (
	createInviteInput struct {
		Body CreateInviteRequestBody
	}

	// inviteOutput carries the invite together with the URL and the
	// outcome of the mail, which are shown once.
	inviteOutput struct {
		Body struct {
			Invite     Invite `json:"invite"`
			URL        string `doc:"The link to send; shown only here." json:"url"`
			EmailSent  bool   `doc:"Whether the link was mailed."       json:"emailSent"`
			EmailError string `doc:"Why the mail was not sent."         json:"emailError,omitempty"`
		}
	}

	listInvitesOutput struct {
		Body struct {
			Invites []Invite `json:"invites" nullable:"false"`
		}
	}

	inviteIDInput struct {
		ID string `format:"uint64" path:"id"`
	}

	resendInviteInput struct {
		ID   string `format:"uint64" path:"id"`
		Body ResendInviteRequestBody
	}

	deleteInviteOutput struct {
		Body struct{}
	}
)

func registerInvites(api huma.API, b Backend) {
	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "createInvite",
		Method:      http.MethodPost,
		Path:        "/api/v1/invite",
		Summary:     "Invite a user",
		Description: "Creates an invitation link. The first login that opens the link, or whose verified " +
			"email matches the address, creates the user approved, with the invited role and groups. " +
			"The link is returned once and mailed to the address when a mail server is configured.",
		Tags:          []string{tagInvites},
		Security:      bearerAuth,
		DefaultStatus: http.StatusCreated,
	}, scope.Users), "user.invite.create", "invite", ""), func(
		ctx context.Context, in *createInviteInput,
	) (*inviteOutput, error) {
		spec, err := inviteSpec(ctx, in.Body)
		if err != nil {
			return nil, err
		}

		// The invited role is a role grant, so the caller is held to the
		// same matrix as a direct role change.
		invite, token, err := b.State.CreateUserInvite(roleActor(ctx), spec)
		if err != nil {
			return nil, mapInviteError("creating invite", err)
		}

		audit.Target(ctx, "", formatID(uint64(invite.ID)), invite.Email)
		audit.Detail(ctx, "role", string(invite.Role))

		return inviteResult(ctx, b, invite, token), nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "listInvites",
		Method:      http.MethodGet,
		Path:        "/api/v1/invite",
		Summary:     "List invites",
		Description: "Lists every invitation, pending and accepted. The tokens are not shown; re-send an " +
			"invite to get a fresh link.",
		Tags:     []string{tagInvites},
		Security: bearerAuth,
	}, scope.UsersRead), func(_ context.Context, _ *struct{}) (*listInvitesOutput, error) {
		invites, err := b.State.ListUserInvites()
		if err != nil {
			return nil, huma.Error500InternalServerError("listing invites", err)
		}

		out := &listInvitesOutput{}
		out.Body.Invites = make([]Invite, 0, len(invites))

		now := time.Now()
		for _, invite := range invites {
			out.Body.Invites = append(out.Body.Invites, inviteFrom(invite, now))
		}

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID:   "deleteInvite",
		Method:        http.MethodDelete,
		Path:          "/api/v1/invite/{id}",
		Summary:       "Revoke an invite",
		Description:   "Deletes the invitation, so its link stops working.",
		Tags:          []string{tagInvites},
		Security:      bearerAuth,
		DefaultStatus: http.StatusOK,
	}, scope.Users), "user.invite.delete", "invite", "id"), func(
		ctx context.Context, in *inviteIDInput,
	) (*deleteInviteOutput, error) {
		id, err := parseInviteID(in.ID)
		if err != nil {
			return nil, err
		}

		invite, err := b.State.GetUserInvite(id)
		if err != nil {
			return nil, mapInviteError("revoking invite", err)
		}

		audit.Target(ctx, "", "", invite.Email)

		err = b.State.DeleteUserInvite(id)
		if err != nil {
			return nil, mapInviteError("revoking invite", err)
		}

		return &deleteInviteOutput{}, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "resendInvite",
		Method:      http.MethodPost,
		Path:        "/api/v1/invite/{id}/resend",
		Summary:     "Re-send an invite",
		Description: "Gives the invitation a new token and expiry and mails the new link. The link in " +
			"the previous mail stops working.",
		Tags:     []string{tagInvites},
		Security: bearerAuth,
	}, scope.Users), "user.invite.resend", "invite", "id"), func(
		ctx context.Context, in *resendInviteInput,
	) (*inviteOutput, error) {
		id, err := parseInviteID(in.ID)
		if err != nil {
			return nil, err
		}

		expiry, err := inviteExpiry(in.Body.Expiry)
		if err != nil {
			return nil, err
		}

		invite, token, err := b.State.ResendUserInvite(id, expiry)
		if err != nil {
			return nil, mapInviteError("re-sending invite", err)
		}

		audit.Target(ctx, "", "", invite.Email)

		return inviteResult(ctx, b, invite, token), nil
	})
}

// inviteResult renders the invite with its one-time link and mails that
// link when a mail server is configured. A mail that cannot be sent is
// reported in the response rather than failing the invite: the link works
// either way, and the administrator can pass it on by hand.
func inviteResult(ctx context.Context, b Backend, invite types.UserInvite, token string) *inviteOutput {
	out := &inviteOutput{}
	out.Body.Invite = inviteFrom(invite, time.Now())
	out.Body.URL = inviteURL(b.Cfg.ServerURL, token)

	err := b.State.SendInviteMail(ctx, invite, out.Body.URL)
	if err != nil {
		out.Body.EmailError = err.Error()

		return out
	}

	out.Body.EmailSent = true

	return out
}

// inviteURL is the link the invited person opens: the console's sign-in
// page carrying the token, which it hands to the sign-in path so the
// callback can consume the invite.
func inviteURL(serverURL, token string) string {
	return strings.TrimSuffix(serverURL, "/") + web.Prefix + "login?invite=" + token
}

// inviteSpec turns the request body into the state layer's spec.
func inviteSpec(ctx context.Context, body CreateInviteRequestBody) (state.InviteSpec, error) {
	role, err := types.ParseRole(body.Role)
	if err != nil {
		return state.InviteSpec{}, huma.Error400BadRequest("invalid role", err)
	}

	groups, err := parseGroupIDs("groupIds", body.GroupIDs)
	if err != nil {
		return state.InviteSpec{}, err
	}

	expiry, err := inviteExpiry(body.Expiry)
	if err != nil {
		return state.InviteSpec{}, err
	}

	spec := state.InviteSpec{
		Email:    body.Email,
		Role:     role,
		GroupIDs: groups,
		Expiry:   expiry,
	}

	if p := caller(ctx); p.HasUser() {
		spec.CreatedBy = p.UserID
	}

	return spec, nil
}

// inviteExpiry parses the requested lifetime; empty means the default.
func inviteExpiry(raw string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}

	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, huma.Error400BadRequest("parsing expiry", err)
	}

	return d, nil
}

func parseInviteID(s string) (types.UserInviteID, error) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, huma.Error400BadRequest("invalid invite id", err)
	}

	return types.UserInviteID(id), nil
}

// mapInviteError turns the invite errors into the statuses the API
// promises; everything else falls through to [mapError].
func mapInviteError(msg string, err error) error {
	switch {
	case errors.Is(err, types.ErrInviteNotFound):
		return huma.Error404NotFound(msg, err)
	case errors.Is(err, types.ErrInviteEmailTaken):
		return huma.Error409Conflict(msg, err)
	case errors.Is(err, types.ErrInviteAccepted):
		return huma.Error409Conflict(msg, err)
	case errors.Is(err, types.ErrInviteEmailEmpty),
		errors.Is(err, types.ErrInviteEmailInvalid),
		errors.Is(err, types.ErrInviteOwnerRole),
		errors.Is(err, types.ErrInviteExpiryRange),
		errors.Is(err, types.ErrInviteExpired):
		return huma.Error400BadRequest(msg, err)
	}

	return mapError(msg, err)
}

// inviteFrom renders an invite for the API.
func inviteFrom(invite types.UserInvite, now time.Time) Invite {
	out := Invite{
		ID:         formatID(uint64(invite.ID)),
		Email:      invite.Email,
		Role:       string(invite.Role),
		GroupIDs:   make([]string, 0, len(invite.GroupIDs)),
		ExpiresAt:  invite.ExpiresAt,
		CreatedAt:  invite.CreatedAt,
		Accepted:   invite.Accepted(),
		AcceptedAt: invite.AcceptedAt,
		Expired:    invite.Expired(now),
	}

	for _, gid := range invite.GroupIDs {
		out.GroupIDs = append(out.GroupIDs, formatID(uint64(gid)))
	}

	if invite.CreatedBy != 0 {
		out.CreatedBy = formatID(uint64(invite.CreatedBy))
	}

	if invite.AcceptedUserID != 0 {
		out.AcceptedUserID = formatID(uint64(invite.AcceptedUserID))
	}

	return out
}
