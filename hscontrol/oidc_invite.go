package hscontrol

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/rs/zerolog/log"
)

// InviteTokenParam is the query parameter an invite link carries, both on
// the console's sign-in page and on [ConsoleLoginPath].
const InviteTokenParam = "invite"

// resolveInvite finds the invite a login is consuming: the one whose token
// the link carried, or the one waiting for the login's verified email. A
// token that was revoked, already used or has expired refuses the sign-in
// rather than silently signing the person in without the role they were
// promised; no invite at all is not an error.
func (a *AuthProviderOIDC) resolveInvite(
	authInfo *AuthInfo,
	claims *types.OIDCClaims,
) (*types.UserInvite, error) {
	if authInfo.InviteToken != "" {
		invite, err := a.h.state.PendingUserInviteByToken(authInfo.InviteToken)
		if err != nil {
			return nil, err
		}

		return &invite, nil
	}

	// The email path has no token to prove anything, so the provider must
	// vouch for the address: an unverified email would let anyone claim
	// someone else's invitation and its role.
	if claims.Email == "" || !claims.EmailVerified {
		return nil, nil //nolint:nilnil // no invite is not an error
	}

	invite, err := a.h.state.PendingUserInviteByEmail(claims.Email)
	if err != nil {
		if errors.Is(err, types.ErrInviteNotFound) {
			return nil, nil //nolint:nilnil // no invite is not an error
		}

		return nil, err
	}

	return &invite, nil
}

// userFromClaims resolves the login's user, consuming invite when the
// login is the first one of a new user.
func (a *AuthProviderOIDC) userFromClaims(
	claims *types.OIDCClaims,
	invite *types.UserInvite,
) (*types.User, error) {
	if invite != nil {
		existing, err := a.lookupUser(claims)
		if err != nil {
			return nil, err
		}

		// Only a first login consumes an invite: an account that exists
		// keeps the role and groups it has, and the invite stays pending.
		if existing == nil {
			return a.acceptInvite(claims, *invite)
		}

		log.Info().Str("email", invite.Email).
			Msg("the invited address already has an account; the invite stays pending")
	}

	user, _, err := a.createOrUpdateUserFromClaim(claims)

	return user, err
}

// acceptInvite creates the invited user: approved whatever the users
// approval setting says, with the invited role and groups.
func (a *AuthProviderOIDC) acceptInvite(
	claims *types.OIDCClaims,
	invite types.UserInvite,
) (*types.User, error) {
	var user types.User

	user.FromClaim(claims, a.cfg.EmailVerifiedRequired)

	created, changes, err := a.h.state.AcceptUserInvite(invite, user)
	if err != nil {
		return nil, err
	}

	a.h.Change(changes...)

	audit.Record(a.h.state, &types.AuditEvent{
		Action:     "user.invite.accept",
		TargetKind: "user",
		TargetID:   strconv.FormatUint(uint64(created.ID), 10),
		TargetName: created.Name,
		Detail: map[string]any{
			"invite": strconv.FormatUint(uint64(invite.ID), 10),
			"email":  invite.Email,
			"role":   string(invite.Role),
		},
	})

	return created, nil
}

// renderInviteRefused tells the person in front of the browser why their
// invitation link did not work.
func renderInviteRefused(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, types.ErrInviteExpired):
		renderConsoleRefused(writer, http.StatusGone, "Invitation expired",
			"This invitation has expired. Ask an administrator to send you a new one.")
	case errors.Is(err, types.ErrInviteAccepted):
		renderConsoleRefused(writer, http.StatusConflict, "Invitation already used",
			"This invitation has already been accepted. Sign in without the invitation link.")
	case errors.Is(err, types.ErrInviteNotFound):
		renderConsoleRefused(writer, http.StatusNotFound, "Invitation not found",
			"This invitation no longer exists. Ask an administrator to send you a new one.")
	default:
		log.Error().Err(err).Msg("resolving the invite of a sign-in")
		renderConsoleRefused(writer, http.StatusInternalServerError, "Sign-in failed",
			"The invitation could not be read. Try again, or ask an administrator for help.")
	}
}
