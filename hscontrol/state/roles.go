package state

import (
	"errors"
	"fmt"

	hsdb "github.com/juanfont/headscale/hscontrol/db"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/types/change"
	"github.com/juanfont/headscale/hscontrol/util/zlog/zf"
	"github.com/rs/zerolog/log"
)

var (
	// ErrCannotChangeOwnRole rejects a caller editing their own role.
	ErrCannotChangeOwnRole = errors.New("a user cannot change their own role")
	// ErrRoleChangeForbidden rejects a caller whose role cannot assign roles.
	ErrRoleChangeForbidden = errors.New("only the owner or an admin can change roles")
	// ErrOnlyOwnerTransfers rejects an ownership transfer by anyone but the owner.
	ErrOnlyOwnerTransfers = errors.New("only the owner can transfer ownership")
	// ErrOwnerRoleImmutable rejects demoting the owner other than by transfer.
	ErrOwnerRoleImmutable = errors.New("the owner's role only changes by transferring ownership")
	// ErrOwnerExists rejects creating a second owner.
	ErrOwnerExists = errors.New("the tailnet already has an owner")
)

// RoleActor is the authenticated user requesting a role change. A nil actor
// is the local trust boundary (the unix socket CLI), which is bound only by
// the structural rules: one owner, whose role moves by transfer.
type RoleActor struct {
	UserID types.UserID
	Role   types.Role
}

// SetUserRole assigns role to the target user and returns the updated user.
// Assigning [types.RoleOwner] transfers ownership: the previous owner becomes
// an admin in the same transaction, so the tailnet always has exactly one
// owner. Roles feed the policy autogroups, so a change is a policy change.
func (s *State) SetUserRole(
	actor *RoleActor,
	target types.UserID,
	role types.Role,
) (*types.User, change.Change, error) {
	if !role.Valid() {
		return nil, change.Change{}, fmt.Errorf("%w: %q", types.ErrInvalidRole, role)
	}

	user, err := hsdb.Write(s.db, func(tx *hsdb.Tx) (*types.User, error) {
		user, err := hsdb.GetUserByID(tx, target)
		if err != nil {
			return nil, err
		}

		err = authorizeRoleChange(actor, user, role)
		if err != nil {
			return nil, err
		}

		if role == types.RoleOwner && user.Role != types.RoleOwner {
			err = demoteOwners(tx)
			if err != nil {
				return nil, err
			}
		}

		user.Role = role

		err = hsdb.UpdateUser(tx, user)
		if err != nil {
			return nil, fmt.Errorf("updating user role: %w", err)
		}

		return user, nil
	})
	if err != nil {
		return nil, change.Change{}, err
	}

	c, err := s.updatePolicyManagerUsers()
	if err != nil {
		return user, change.Change{}, fmt.Errorf("updating policy manager after role change: %w", err)
	}

	// autogroup:owner and friends resolve through the user list, and a
	// node's is-admin capability follows its user's role, so every node's
	// map must be rebuilt even when the filter text did not move.
	if c.IsEmpty() {
		c = change.PolicyChange()
	}

	log.Info().Str(zf.UserName, user.Name).Str("role", role.String()).Msg("user role set")

	return user, c, nil
}

// authorizeRoleChange applies the role matrix to a requested change.
func authorizeRoleChange(actor *RoleActor, target *types.User, role types.Role) error {
	if target.Role == types.RoleOwner && role != types.RoleOwner {
		return ErrOwnerRoleImmutable
	}

	if actor == nil {
		return nil
	}

	switch {
	case actor.UserID == types.UserID(target.ID):
		return ErrCannotChangeOwnRole
	case !actor.Role.IsAdmin():
		return ErrRoleChangeForbidden
	case role == types.RoleOwner && actor.Role != types.RoleOwner:
		return ErrOnlyOwnerTransfers
	}

	return nil
}

// demoteOwners turns every current owner into an admin ahead of a transfer.
func demoteOwners(tx *hsdb.Tx) error {
	owners, err := hsdb.ListUsers(tx, &types.User{Role: types.RoleOwner})
	if err != nil {
		return fmt.Errorf("listing owners: %w", err)
	}

	for i := range owners {
		owners[i].Role = types.RoleAdmin

		err = hsdb.UpdateUser(tx, &owners[i])
		if err != nil {
			return fmt.Errorf("demoting previous owner: %w", err)
		}
	}

	return nil
}

// assignInitialRole settles the role of a user about to be created. The first
// user of a fresh tailnet becomes its owner, as on Tailscale, so an operator
// never starts locked out; every later user defaults to member. An explicit
// owner request is honoured only while no owner exists.
func assignInitialRole(tx *hsdb.Tx, user *types.User) error {
	if user.Role == "" {
		user.Role = types.RoleMember
	}

	if !user.Role.Valid() {
		return fmt.Errorf("%w: %q", types.ErrInvalidRole, user.Role)
	}

	users, err := hsdb.ListUsers(tx, nil)
	if err != nil {
		return fmt.Errorf("listing users: %w", err)
	}

	if len(users) == 0 {
		user.Role = types.RoleOwner

		return nil
	}

	if user.Role != types.RoleOwner {
		return nil
	}

	for i := range users {
		if users[i].Role == types.RoleOwner {
			return ErrOwnerExists
		}
	}

	return nil
}
