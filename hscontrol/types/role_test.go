package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRole(t *testing.T) {
	t.Parallel()

	for _, role := range Roles() {
		got, err := ParseRole(string(role))
		require.NoError(t, err)
		assert.Equal(t, role, got)
	}

	got, err := ParseRole("")
	require.NoError(t, err)
	assert.Equal(t, RoleMember, got, "a pre-roles row reads as member")

	_, err = ParseRole("billing-admin")
	require.ErrorIs(t, err, ErrInvalidRole)

	_, err = ParseRole("Owner")
	require.ErrorIs(t, err, ErrInvalidRole, "roles are lowercase")
}

func TestRolePredicates(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "member", Role("").String())
	assert.False(t, Role("").IsConsole())
	assert.False(t, Role("").IsAdmin())
	assert.False(t, RoleMember.IsConsole())
	assert.True(t, RoleAuditor.IsConsole())
	assert.False(t, RoleAuditor.IsAdmin())
	assert.True(t, RoleAdmin.IsAdmin())
	assert.True(t, RoleOwner.IsAdmin())
	assert.False(t, Role("root").Valid())
}
