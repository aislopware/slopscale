package state

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	hsdb "github.com/aislopware/slopscale/hscontrol/db"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/aislopware/slopscale/hscontrol/util/zlog/zf"
	"github.com/aislopware/slopscale/hscontrol/webhook"
	"github.com/rs/zerolog/log"
)

// InviteSpec is an invitation an administrator asks for.
type InviteSpec struct {
	Email    string
	Role     types.Role
	GroupIDs []types.GroupID
	// Expiry is how long the link works for; zero means
	// [types.InviteDefaultExpiry].
	Expiry time.Duration
	// CreatedBy is the administrator sending it, zero for a credential
	// that belongs to nobody.
	CreatedBy types.UserID
}

// ListUserInvites returns every invite, pending and accepted.
func (s *State) ListUserInvites() ([]types.UserInvite, error) {
	return s.db.ListUserInvites()
}

// GetUserInvite reads one invite.
func (s *State) GetUserInvite(id types.UserInviteID) (types.UserInvite, error) {
	return s.db.GetUserInvite(id)
}

// CreateUserInvite stores an invitation and returns it with the token its
// link carries. The token is returned once; only its hash is stored. actor
// is the administrator asking for it, nil for the local trust boundary.
func (s *State) CreateUserInvite(actor *RoleActor, spec InviteSpec) (types.UserInvite, string, error) {
	email := hsdb.NormaliseInviteEmail(spec.Email)

	expiry, err := s.validateInvite(actor, email, spec)
	if err != nil {
		return types.UserInvite{}, "", err
	}

	token, hash, err := hsdb.NewInviteToken()
	if err != nil {
		return types.UserInvite{}, "", err
	}

	invite, err := s.db.CreateUserInvite(types.UserInvite{
		Email:     email,
		Role:      spec.Role,
		GroupIDs:  spec.GroupIDs,
		ExpiresAt: time.Now().Add(expiry),
		CreatedBy: spec.CreatedBy,
	}, hash)
	if err != nil {
		return types.UserInvite{}, "", err
	}

	log.Info().Str("email", email).Str("role", string(invite.Role)).Msg("user invited")

	return invite, token, nil
}

// validateInvite checks the request and settles the expiry: the role must
// exist, must not be owner and must be one the sender could assign, the
// groups must exist, and the address must be free of both a user and a
// pending invite.
func (s *State) validateInvite(actor *RoleActor, email string, spec InviteSpec) (time.Duration, error) {
	if email == "" {
		return 0, types.ErrInviteEmailEmpty
	}

	// The address is mailed a link and becomes the invited user's login, so
	// it must be one bare mailbox: no display name, no list, no header.
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Name != "" || addr.Address != email {
		return 0, fmt.Errorf("%w: %q", types.ErrInviteEmailInvalid, email)
	}

	if !spec.Role.Valid() {
		return 0, fmt.Errorf("%w: %q", types.ErrInvalidRole, spec.Role)
	}

	if spec.Role == types.RoleOwner {
		return 0, types.ErrInviteOwnerRole
	}

	err = authorizeInviteRole(actor, spec.Role)
	if err != nil {
		return 0, err
	}

	expiry := spec.Expiry
	if expiry == 0 {
		expiry = types.InviteDefaultExpiry
	}

	if expiry <= 0 || expiry > types.InviteMaxExpiry {
		return 0, types.ErrInviteExpiryRange
	}

	for _, gid := range spec.GroupIDs {
		_, err = s.GetGroup(gid)
		if err != nil {
			return 0, err
		}
	}

	err = s.requireInviteEmailFree(email)
	if err != nil {
		return 0, err
	}

	return expiry, nil
}

// authorizeInviteRole holds an invitation to a role its sender could assign
// directly: an invite is a role grant that arrives later, and the invited
// person may sign in with an identity of their own choosing, so a caller who
// cannot promote anybody must not be able to invite an admin. A nil actor is
// the local trust boundary.
func authorizeInviteRole(actor *RoleActor, role types.Role) error {
	if actor == nil || role == "" || role == types.RoleMember || actor.Role.IsAdmin() {
		return nil
	}

	return ErrRoleChangeForbidden
}

// requireInviteEmailFree refuses an address that already belongs to a user
// or to a pending invite.
func (s *State) requireInviteEmailFree(email string) error {
	users, err := s.ListAllUsers()
	if err != nil {
		return fmt.Errorf("listing users: %w", err)
	}

	for i := range users {
		if strings.EqualFold(strings.TrimSpace(users[i].Email), email) {
			return fmt.Errorf("%w: %s", types.ErrInviteEmailTaken, email)
		}
	}

	_, err = s.db.GetPendingUserInviteByEmail(email, time.Now())
	if err == nil {
		return fmt.Errorf("%w: %s", types.ErrInviteEmailTaken, email)
	}

	if !errors.Is(err, types.ErrInviteNotFound) {
		return err
	}

	return nil
}

// DeleteUserInvite revokes an invite, so its link stops working.
func (s *State) DeleteUserInvite(id types.UserInviteID) error {
	return s.db.DeleteUserInvite(id)
}

// ResendUserInvite gives a pending invite a fresh token and expiry and
// returns both, so the mail can be sent again. The link in the previous
// mail stops working.
func (s *State) ResendUserInvite(
	id types.UserInviteID,
	expiry time.Duration,
) (types.UserInvite, string, error) {
	if expiry == 0 {
		expiry = types.InviteDefaultExpiry
	}

	if expiry <= 0 || expiry > types.InviteMaxExpiry {
		return types.UserInvite{}, "", types.ErrInviteExpiryRange
	}

	invite, err := s.db.GetUserInvite(id)
	if err != nil {
		return types.UserInvite{}, "", err
	}

	if invite.Accepted() {
		return types.UserInvite{}, "", types.ErrInviteAccepted
	}

	token, hash, err := hsdb.NewInviteToken()
	if err != nil {
		return types.UserInvite{}, "", err
	}

	rotated, err := s.db.RotateUserInviteToken(id, hash, time.Now().Add(expiry))
	if err != nil {
		return types.UserInvite{}, "", err
	}

	return rotated, token, nil
}

// PendingUserInviteByToken resolves an invite link's token to the invite
// it stands for, refusing one that was revoked, already accepted or has
// expired.
func (s *State) PendingUserInviteByToken(token string) (types.UserInvite, error) {
	invite, err := s.db.GetUserInviteByToken(token)
	if err != nil {
		return types.UserInvite{}, err
	}

	if invite.Accepted() {
		return types.UserInvite{}, types.ErrInviteAccepted
	}

	if invite.Expired(time.Now()) {
		return types.UserInvite{}, types.ErrInviteExpired
	}

	return invite, nil
}

// PendingUserInviteByEmail returns the invite waiting for an address, so a
// first login whose verified email matches consumes it without carrying
// the link's token.
func (s *State) PendingUserInviteByEmail(email string) (types.UserInvite, error) {
	return s.db.GetPendingUserInviteByEmail(email, time.Now())
}

// AcceptUserInvite creates the user behind a first login that carries an
// invite. The user is approved whatever the users approval setting says,
// because an administrator already vouched for the address, and gets the
// invited role and groups. The invite is marked accepted last, so a
// failure leaves it usable.
func (s *State) AcceptUserInvite(
	invite types.UserInvite,
	user types.User,
) (*types.User, []change.Change, error) {
	user.ApprovedAt = new(time.Now().UTC())

	created, c, err := s.createUser(user)
	if err != nil {
		return nil, nil, err
	}

	changes := []change.Change{c}

	roleChange, err := s.applyInvitedRole(invite, created)
	if err != nil {
		return nil, nil, err
	}

	if roleChange != nil {
		created = roleChange.user
		changes = append(changes, roleChange.change)
	}

	changes = append(changes, s.applyInvitedGroups(invite, types.UserID(created.ID))...)

	err = s.db.AcceptUserInvite(invite.ID, types.UserID(created.ID), time.Now())
	if err != nil {
		return nil, nil, err
	}

	log.Info().Str(zf.UserName, created.Name).Str("email", invite.Email).Msg("invite accepted")

	return created, changes, nil
}

// invitedRole is the outcome of applying an invite's role.
type invitedRole struct {
	user   *types.User
	change change.Change
}

// applyInvitedRole gives the new user the invited role through the same
// path an administrator uses. The first user of an empty tailnet is its
// owner and stays so: ownership is not something an invite hands out.
func (s *State) applyInvitedRole(invite types.UserInvite, user *types.User) (*invitedRole, error) {
	if invite.Role == "" || invite.Role == user.Role || user.Role == types.RoleOwner {
		return nil, nil //nolint:nilnil // no role change is not an error
	}

	updated, c, err := s.SetUserRole(nil, types.UserID(user.ID), invite.Role)
	if err != nil {
		return nil, fmt.Errorf("setting the invited role: %w", err)
	}

	return &invitedRole{user: updated, change: c}, nil
}

// applyInvitedGroups adds the new user to the invite's groups. A group
// deleted since the invite was sent is skipped: the person is already
// signed in, and refusing the login would leave them with no way in.
func (s *State) applyInvitedGroups(invite types.UserInvite, userID types.UserID) []change.Change {
	var changes []change.Change

	for _, gid := range invite.GroupIDs {
		_, c, err := s.AddGroupUser(gid, userID, nil)
		if err != nil {
			log.Warn().Err(err).Uint64("group.id", uint64(gid)).
				Msg("invited group could not be joined; the user signed in without it")

			continue
		}

		changes = append(changes, c)
	}

	return changes
}

// SendInviteMail sends the invite link to the invited address through the
// configured mail server, or returns [webhook.ErrNoMailer] when there is
// none. The caller reports a failure to the administrator rather than
// failing the invite: the link works whether or not the mail arrived.
func (s *State) SendInviteMail(ctx context.Context, invite types.UserInvite, link string) error {
	if s.mailer == nil {
		return webhook.ErrNoMailer
	}

	tailnet := tailnetName(s.cfg)

	subject := "You have been invited to " + tailnet
	if tailnet == "" {
		subject = "You have been invited to a Slopscale tailnet"
	}

	var body strings.Builder

	body.WriteString(subject + ".\r\n\r\n")
	body.WriteString("Open this link to sign in and finish setting up your account:\r\n\r\n")
	body.WriteString(link + "\r\n\r\n")
	body.WriteString("The invitation expires on " + invite.ExpiresAt.UTC().Format(time.RFC1123) + ".\r\n")

	err := s.mailer.Send(ctx, []string{invite.Email}, subject, body.String())
	if err != nil {
		return fmt.Errorf("sending the invite mail: %w", err)
	}

	return nil
}
