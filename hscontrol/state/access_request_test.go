package state

import (
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The approvers an email endpoint resolves to are whoever may decide a
// request right now, so a role change is the only thing needed to change
// who is mailed.
func TestApproverEmailsFollowTheRoles(t *testing.T) {
	t.Parallel()

	s := newRoleTestState(t)

	owner, err := s.GetUserByID(types.UserID(createUserWithRole(t, s, "owner", "").ID))
	require.NoError(t, err)
	require.Equal(t, types.RoleOwner, owner.Role, "the first user owns the tailnet")

	admin := createUserWithRole(t, s, "admin", types.RoleAdmin)
	member := createUserWithRole(t, s, "member", types.RoleMember)

	assert.Empty(t, approvers(t, s), "nobody has an address yet")

	setEmail(t, s, owner, "owner@example.com")
	setEmail(t, s, admin, "admin@example.com")
	setEmail(t, s, member, "member@example.com")

	assert.ElementsMatch(t, []string{"owner@example.com", "admin@example.com"}, approvers(t, s),
		"a member may not decide a request and is not asked to")

	_, _, err = s.SetUserRole(actor(owner), types.UserID(member.ID), types.RoleAdmin)
	require.NoError(t, err)

	assert.ElementsMatch(t,
		[]string{"owner@example.com", "admin@example.com", "member@example.com"},
		approvers(t, s), "the promotion alone adds them")

	// An address a mail server would reject ends the whole message, so the
	// approver carrying it is left out rather than sent.
	setEmail(t, s, admin, "not an address")

	assert.ElementsMatch(t, []string{"owner@example.com", "member@example.com"}, approvers(t, s),
		"the other approvers are still mailed")
}

func approvers(t *testing.T, s *State) []string {
	t.Helper()

	emails, err := s.ApproverEmails()
	require.NoError(t, err)

	return emails
}

func setEmail(t *testing.T, s *State, u *types.User, email string) {
	t.Helper()

	_, _, err := s.UpdateUser(types.UserID(u.ID), func(user *types.User) error {
		user.Email = email

		return nil
	})
	require.NoError(t, err)
}
