package state

import (
	"testing"

	hsdb "github.com/juanfont/headscale/hscontrol/db"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRoleTestState(t *testing.T) *State {
	t.Helper()

	s, err := NewState(persistTestConfig(t.TempDir() + "/headscale.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	return s
}

func createUserWithRole(t *testing.T, s *State, name string, role types.Role) *types.User {
	t.Helper()

	u, _, err := s.CreateUser(types.User{Name: name, Role: role})
	require.NoError(t, err)

	return u
}

func actor(u *types.User) *RoleActor {
	return &RoleActor{UserID: types.UserID(u.ID), Role: u.Role}
}

func roleOf(t *testing.T, s *State, u *types.User) types.Role {
	t.Helper()

	got, err := s.GetUserByID(types.UserID(u.ID))
	require.NoError(t, err)

	return got.Role
}

func TestCreateUserAssignsRoles(t *testing.T) {
	t.Parallel()

	s := newRoleTestState(t)

	first := createUserWithRole(t, s, "first", "")
	assert.Equal(t, types.RoleOwner, first.Role, "the first user owns a fresh tailnet")

	second := createUserWithRole(t, s, "second", "")
	assert.Equal(t, types.RoleMember, second.Role, "later users default to member")

	third := createUserWithRole(t, s, "third", types.RoleAuditor)
	assert.Equal(t, types.RoleAuditor, third.Role, "an explicit role is kept")

	_, _, err := s.CreateUser(types.User{Name: "second-owner", Role: types.RoleOwner})
	require.ErrorIs(t, err, ErrOwnerExists)

	_, _, err = s.CreateUser(types.User{Name: "bad", Role: "root"})
	require.ErrorIs(t, err, types.ErrInvalidRole)
}

func TestSetUserRoleRules(t *testing.T) {
	t.Parallel()

	s := newRoleTestState(t)

	owner := createUserWithRole(t, s, "owner", "")
	admin := createUserWithRole(t, s, "admin", types.RoleAdmin)
	auditor := createUserWithRole(t, s, "auditor", types.RoleAuditor)
	member := createUserWithRole(t, s, "member", "")

	uid := func(u *types.User) types.UserID { return types.UserID(u.ID) }

	_, _, err := s.SetUserRole(actor(owner), uid(member), "root")
	require.ErrorIs(t, err, types.ErrInvalidRole)

	_, _, err = s.SetUserRole(actor(auditor), uid(member), types.RoleAdmin)
	require.ErrorIs(t, err, ErrRoleChangeForbidden, "an auditor assigns nothing")

	_, _, err = s.SetUserRole(actor(admin), uid(admin), types.RoleOwner)
	require.ErrorIs(t, err, ErrCannotChangeOwnRole)

	_, _, err = s.SetUserRole(actor(admin), uid(member), types.RoleOwner)
	require.ErrorIs(t, err, ErrOnlyOwnerTransfers)

	_, _, err = s.SetUserRole(actor(admin), uid(owner), types.RoleMember)
	require.ErrorIs(t, err, ErrOwnerRoleImmutable, "the owner cannot be demoted")

	_, _, err = s.SetUserRole(nil, uid(owner), types.RoleAdmin)
	require.ErrorIs(t, err, ErrOwnerRoleImmutable, "not even by the socket")

	got, c, err := s.SetUserRole(actor(admin), uid(member), types.RoleNetworkAdmin)
	require.NoError(t, err)
	assert.Equal(t, types.RoleNetworkAdmin, got.Role)
	assert.False(t, c.IsEmpty(), "a role change is a policy change")
	assert.Equal(t, types.RoleNetworkAdmin, roleOf(t, s, member))

	got, _, err = s.SetUserRole(nil, uid(member), types.RoleMember)
	require.NoError(t, err, "the socket assigns any non-owner role")
	assert.Equal(t, types.RoleMember, got.Role)

	// Transfer: the previous owner becomes an admin, and only one owner remains.
	got, _, err = s.SetUserRole(actor(owner), uid(auditor), types.RoleOwner)
	require.NoError(t, err)
	assert.Equal(t, types.RoleOwner, got.Role)
	assert.Equal(t, types.RoleAdmin, roleOf(t, s, owner))

	owners, err := s.ListUsersWithFilter(&types.User{Role: types.RoleOwner})
	require.NoError(t, err)
	require.Len(t, owners, 1)
	assert.Equal(t, auditor.ID, owners[0].ID)

	// The old owner is now an admin, so it may no longer hand out ownership.
	_, _, err = s.SetUserRole(actor(&types.User{ID: owner.ID, Role: types.RoleAdmin}), uid(member), types.RoleOwner)
	require.ErrorIs(t, err, ErrOnlyOwnerTransfers)

	// The socket may transfer ownership too.
	_, _, err = s.SetUserRole(nil, uid(member), types.RoleOwner)
	require.NoError(t, err)
	assert.Equal(t, types.RoleAdmin, roleOf(t, s, auditor))
}

func TestDeleteUserKeepsOwner(t *testing.T) {
	t.Parallel()

	s := newRoleTestState(t)

	owner := createUserWithRole(t, s, "owner", "")
	member := createUserWithRole(t, s, "member", "")

	_, err := s.DeleteUser(types.UserID(owner.ID))
	require.ErrorIs(t, err, hsdb.ErrCannotDeleteOwner)

	_, err = s.DeleteUser(types.UserID(member.ID))
	require.NoError(t, err)
}
