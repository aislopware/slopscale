package types

import (
	"errors"
	"fmt"
	"slices"
)

// Role is a user's administrative role: what the user may do through the
// admin API and the management console. The vocabulary mirrors Tailscale's
// user roles minus billing-admin, which has no meaning on a self-hosted
// server, so the Tailscale-compatible API and its clients see familiar
// values. A role never affects what the user's nodes can reach; that is
// the policy's job, which may address roles through autogroup:admin and
// friends.
type Role string

const (
	// RoleOwner is the single account owner. Only the owner may transfer
	// ownership, and the owner cannot be demoted or deleted by anyone else.
	RoleOwner Role = "owner"
	// RoleAdmin may do everything the owner may, except transfer or remove
	// the owner.
	RoleAdmin Role = "admin"
	// RoleNetworkAdmin manages the policy, DNS and route approval, and
	// reads everything else.
	RoleNetworkAdmin Role = "network-admin"
	// RoleITAdmin manages users, devices, keys and feature settings, and
	// reads the policy and routes; it cannot approve routes.
	RoleITAdmin Role = "it-admin"
	// RoleAuditor reads everything and changes nothing.
	RoleAuditor Role = "auditor"
	// RoleMember has no admin access. Every user starts here.
	RoleMember Role = "member"
)

// ErrInvalidRole is returned for a role outside [Roles].
var ErrInvalidRole = errors.New("invalid role")

// Roles returns every role, most to least privileged.
func Roles() []Role {
	return []Role{RoleOwner, RoleAdmin, RoleNetworkAdmin, RoleITAdmin, RoleAuditor, RoleMember}
}

// ParseRole validates s as a role. The empty string is [RoleMember], which
// is what a user row from before roles existed reads back as.
func ParseRole(s string) (Role, error) {
	if s == "" {
		return RoleMember, nil
	}

	role := Role(s)
	if !role.Valid() {
		return "", fmt.Errorf("%w: %q, want one of %v", ErrInvalidRole, s, Roles())
	}

	return role, nil
}

// Valid reports whether r is one of [Roles].
func (r Role) Valid() bool {
	return slices.Contains(Roles(), r)
}

// String returns the role as stored and shown; an empty role reads as
// [RoleMember].
func (r Role) String() string {
	if r == "" {
		return string(RoleMember)
	}

	return string(r)
}

// IsConsole reports whether the role grants any admin access at all, which
// is every role but member.
func (r Role) IsConsole() bool {
	return r.Valid() && r != RoleMember && r != ""
}

// IsAdmin reports whether the role is owner or admin, the two roles that
// may do everything, including assign roles.
func (r Role) IsAdmin() bool {
	return r == RoleOwner || r == RoleAdmin
}
