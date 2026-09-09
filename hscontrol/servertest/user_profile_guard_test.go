package servertest_test

import (
	"net/http"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUserProfileUpdateGuards pins what the users scope does not buy on the
// profile endpoint: an email another user already holds, or a rewrite of an
// admin's profile by a caller who is not one. Both are steps towards taking
// an account over, because a login is matched to a user by email.
func TestUserProfileUpdateGuards(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	// The first user owns the tailnet.
	owner := srv.CreateUser(t, "guard-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	itAdmin := srv.CreateUser(t, "guard-it")
	admin := srv.CreateUser(t, "guard-admin")
	member := srv.CreateUser(t, "guard-member")

	setRole(t, srv, itAdmin, types.RoleITAdmin)
	setRole(t, srv, admin, types.RoleAdmin)

	itAdminKey := srv.CreateAPIKey(t, itAdmin)

	status, body := apiCall(t, client, ownerKey, http.MethodPatch, v1+"/user/"+userID(member),
		map[string]any{"email": "member@example.com"})
	require.Equal(t, http.StatusOK, status, body)

	status, body = apiCall(t, client, ownerKey, http.MethodPatch, v1+"/user/"+userID(admin),
		map[string]any{"email": "member@example.com"})
	assert.Equal(t, http.StatusConflict, status, "an email another user holds is refused: %v", body)

	status, body = apiCall(t, client, itAdminKey, http.MethodPatch, v1+"/user/"+userID(member),
		map[string]any{"displayName": "Member"})
	assert.Equal(t, http.StatusOK, status, "an it-admin still edits an ordinary user: %v", body)

	status, body = apiCall(t, client, itAdminKey, http.MethodPatch, v1+"/user/"+userID(admin),
		map[string]any{"email": "attacker@example.com"})
	assert.Equal(t, http.StatusForbidden, status, "an it-admin cannot rewrite an admin's profile: %v", body)

	status, body = apiCall(t, client, ownerKey, http.MethodPatch, v1+"/user/"+userID(admin),
		map[string]any{"email": "admin@example.com"})
	assert.Equal(t, http.StatusOK, status, "the owner still edits an admin: %v", body)
}

func setRole(t *testing.T, srv *servertest.TestServer, user *types.User, role types.Role) {
	t.Helper()

	_, _, err := srv.State().SetUserRole(nil, types.UserID(user.ID), role)
	require.NoError(t, err)
}
