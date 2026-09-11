package servertest_test

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/aislopware/slopscale/hscontrol"
	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/oauth2-proxy/mockoidc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newInviteServer starts a Slopscale server that signs users in through a
// mock identity provider and, when mail is asked for, delivers through an
// in-process SMTP server.
func newInviteServer(t *testing.T, withMail bool, users ...mockoidc.MockUser) (*servertest.TestServer, *smtpServer) {
	t.Helper()

	provider := startMockOIDC(t, users...)

	opts := []servertest.ServerOption{servertest.WithOIDC(types.OIDCConfig{
		Issuer:       provider.Issuer(),
		ClientID:     provider.ClientID,
		ClientSecret: provider.ClientSecret,
		Scope:        []string{oidc.ScopeOpenID, oidc.ScopeProfile, oidc.ScopeEmail},
	})}

	var mail *smtpServer

	if withMail {
		mail = newSMTPServer(t)

		host, port, err := net.SplitHostPort(mail.addr)
		require.NoError(t, err)

		portNum, err := strconv.Atoi(port)
		require.NoError(t, err)

		opts = append(opts, servertest.WithSMTP(types.SMTPConfig{
			Host: host, Port: portNum, From: "Slopscale <hs@example.com>", Encryption: types.SMTPNoEncryption,
		}))
	}

	return servertest.NewServer(t, opts...), mail
}

// inviteAdminKey gives a server an owner and an API key for it, which is
// what an administrator creating invites holds.
func inviteAdminKey(t *testing.T, srv *servertest.TestServer) string {
	t.Helper()

	return srv.CreateAPIKey(t, srv.CreateUser(t, "owner"))
}

// inviteToken pulls the token out of the link an invite response carries.
func inviteToken(t *testing.T, link string) string {
	t.Helper()

	u, err := url.Parse(link)
	require.NoError(t, err)

	require.Equal(t, "/console/login", u.Path, "the link points at the console's sign-in page")

	token := u.Query().Get(hscontrol.InviteTokenParam)
	require.NotEmpty(t, token, "the invite link carries no token: %s", link)

	return token
}

// createInvite posts an invitation and returns the response body.
func createInvite(
	t *testing.T,
	srv *servertest.TestServer,
	key string,
	body map[string]any,
) (int, map[string]any) {
	t.Helper()

	return apiCall(t, srv.HTTPClient(t), key, http.MethodPost, srv.URL+"/api/v1/invite", body)
}

// loginWithInvite signs a browser in through the identity provider,
// carrying an invite token when one is given, and returns the status and
// body the redirect chain ended on.
func loginWithInvite(t *testing.T, srv *servertest.TestServer, token string) (int, string) {
	t.Helper()

	target := srv.URL + hscontrol.ConsoleLoginPath
	if token != "" {
		target += "?" + hscontrol.InviteTokenParam + "=" + url.QueryEscape(token)
	}

	return browse(t, srv.HTTPClient(t), target)
}

// userByName reads back the user a login created.
func userByName(t *testing.T, srv *servertest.TestServer, name string) types.User {
	t.Helper()

	users, err := srv.State().ListAllUsers()
	require.NoError(t, err)

	for i := range users {
		if users[i].Name == name {
			return users[i]
		}
	}

	require.Failf(t, "user not found", "no user named %q", name)

	return types.User{}
}

// TestUserInviteLink walks an invitation end to end: an administrator
// creates it, the link is mailed, the invited person signs in through it
// and lands as an approved user with the invited role and group even
// though user approval is on, and the invite is then listed as accepted.
//
//nolint:tparallel // the subtests act on one invite in order.
func TestUserInviteLink(t *testing.T) {
	t.Parallel()

	srv, mail := newInviteServer(t, true, oidcUser("ada", "ada@example.com", true))
	key := inviteAdminKey(t, srv)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	// An invite admits its holder whatever the approval setting says.
	_, err := srv.State().SetSetting(types.SettingUsersApprovalOn, true)
	require.NoError(t, err)

	status, body := apiCall(t, client, key, http.MethodPost, v1+"/group", map[string]any{
		"name": "engineering",
	})
	require.Equal(t, http.StatusOK, status, "%v", body)

	groupID, ok := field(t, body, "group", "id").(string)
	require.True(t, ok)

	status, body = createInvite(t, srv, key, map[string]any{
		"email":    "Ada@Example.com",
		"role":     "admin",
		"groupIds": []string{groupID},
		"expiry":   "24h",
	})
	require.Equal(t, http.StatusCreated, status, "%v", body)

	assert.Equal(t, "ada@example.com", field(t, body, "invite", "email"), "the address is normalised")
	assert.Equal(t, "admin", field(t, body, "invite", "role"))
	assert.Equal(t, false, field(t, body, "invite", "accepted"))
	assert.Equal(t, true, body["emailSent"], "%v", body)
	assert.Equal(t, 1, mail.count(), "the link was mailed to the invited address")

	link, ok := body["url"].(string)
	require.True(t, ok)

	inviteID, ok := field(t, body, "invite", "id").(string)
	require.True(t, ok)

	t.Run("the address is refused a second invite", func(t *testing.T) {
		status, body := createInvite(t, srv, key, map[string]any{"email": "ada@example.com"})
		assert.Equal(t, http.StatusConflict, status, "%v", body)
	})

	t.Run("an invite cannot hand out ownership", func(t *testing.T) {
		status, body := createInvite(t, srv, key, map[string]any{"email": "bea@example.com", "role": "owner"})
		assert.Equal(t, http.StatusBadRequest, status, "%v", body)
	})

	t.Run("an invite cannot outlast a month", func(t *testing.T) {
		status, body := createInvite(t, srv, key,
			map[string]any{"email": "bea@example.com", "expiry": "800h"})
		assert.Equal(t, http.StatusBadRequest, status, "%v", body)
	})

	t.Run("the link signs the invited person in with the invited role", func(t *testing.T) {
		status, page := loginWithInvite(t, srv, inviteToken(t, link))
		require.Equal(t, http.StatusOK, status, "the invited sign-in did not land in the console:\n%s", page)

		ada := userByName(t, srv, "ada")
		assert.Equal(t, types.RoleAdmin, ada.Role, "the invited role is set")
		require.NotNil(t, ada.ApprovedAt, "an invited user is approved even while approval is on")

		id, err := strconv.ParseUint(groupID, 10, 64)
		require.NoError(t, err)

		group, err := srv.State().GetGroup(types.GroupID(id))
		require.NoError(t, err)
		assert.Contains(t, group.UserIDs, types.UserID(ada.ID), "the invited groups are joined")
	})

	t.Run("the invite is listed as accepted, without its token", func(t *testing.T) {
		status, body := apiCall(t, client, key, http.MethodGet, v1+"/invite", nil)
		require.Equal(t, http.StatusOK, status)

		invites, ok := body["invites"].([]any)
		require.True(t, ok)

		var found map[string]any

		for _, raw := range invites {
			entry, ok := raw.(map[string]any)
			require.True(t, ok)

			assert.NotContains(t, entry, "token", "a listed invite never carries its token")
			assert.NotContains(t, entry, "url")

			if entry["id"] == inviteID {
				found = entry
			}
		}

		require.NotNil(t, found, "the invite is missing from the list")
		assert.Equal(t, true, found["accepted"])
		assert.NotEmpty(t, found["acceptedAt"])
		assert.NotEmpty(t, found["acceptedUserId"])
	})

	t.Run("accepting is audited against the new user", func(t *testing.T) {
		events, err := srv.State().ListAuditEvents(types.AuditQuery{Action: "user.invite.accept"})
		require.NoError(t, err)
		require.Len(t, events, 1)
		assert.Equal(t, "user", events[0].TargetKind)
		assert.Equal(t, "ada", events[0].TargetName)
		assert.Equal(t, "admin", events[0].Detail["role"])
	})
}

// TestUserInviteEmailMatch pins the second way an invite is consumed: a
// first login whose verified email matches a pending invite takes it,
// even though the browser never saw the link.
func TestUserInviteEmailMatch(t *testing.T) {
	t.Parallel()

	srv, _ := newInviteServer(t, false, oidcUser("cleo", "cleo@example.com", true))
	key := inviteAdminKey(t, srv)

	status, body := createInvite(t, srv, key, map[string]any{
		"email": "cleo@example.com",
		"role":  "network-admin",
	})
	require.Equal(t, http.StatusCreated, status, "%v", body)
	assert.Equal(t, false, body["emailSent"], "no mail server is configured")
	assert.NotEmpty(t, body["emailError"], "the reason is reported, not the invite refused")

	status, page := loginWithInvite(t, srv, "")
	require.Equal(t, http.StatusOK, status, page)

	cleo := userByName(t, srv, "cleo")
	assert.Equal(t, types.RoleNetworkAdmin, cleo.Role)
	require.NotNil(t, cleo.ApprovedAt)

	invites, err := srv.State().ListUserInvites()
	require.NoError(t, err)
	require.Len(t, invites, 1)
	assert.True(t, invites[0].Accepted())
	assert.Equal(t, types.UserID(cleo.ID), invites[0].AcceptedUserID)
}

// TestUserInviteRefused pins the two links that must not work: one whose
// invite has expired and one whose invite was revoked.
//
//nolint:tparallel // the subtests take the queued identities in order.
func TestUserInviteRefused(t *testing.T) {
	t.Parallel()

	srv, _ := newInviteServer(t, false,
		oidcUser("dora", "dora@example.com", true),
		oidcUser("evan", "evan@example.com", true),
	)
	key := inviteAdminKey(t, srv)
	client := srv.HTTPClient(t)

	t.Run("an expired invite is refused with a message", func(t *testing.T) {
		// A one-nanosecond invite is over before the browser can use it.
		status, body := createInvite(t, srv, key, map[string]any{
			"email": "dora@example.com", "expiry": "1ns",
		})
		require.Equal(t, http.StatusCreated, status, "%v", body)

		link, ok := body["url"].(string)
		require.True(t, ok)

		status, page := loginWithInvite(t, srv, inviteToken(t, link))
		assert.Equal(t, http.StatusGone, status, page)
		assert.Contains(t, page, "Invitation expired")
		assert.Contains(t, page, "Ask an administrator to send you a new one")

		users, err := srv.State().ListAllUsers()
		require.NoError(t, err)
		assert.Len(t, users, 1, "only the administrator exists; the refused login created nobody")
	})

	t.Run("a revoked invite is refused", func(t *testing.T) {
		status, body := createInvite(t, srv, key, map[string]any{"email": "evan@example.com"})
		require.Equal(t, http.StatusCreated, status, "%v", body)

		link, ok := body["url"].(string)
		require.True(t, ok)

		id, ok := field(t, body, "invite", "id").(string)
		require.True(t, ok)

		status, body = apiCall(t, client, key, http.MethodDelete, srv.URL+"/api/v1/invite/"+id, nil)
		require.Equal(t, http.StatusOK, status, "%v", body)

		status, page := loginWithInvite(t, srv, inviteToken(t, link))
		assert.Equal(t, http.StatusNotFound, status, page)
		assert.Contains(t, page, "Invitation not found")

		status, _ = apiCall(t, client, key, http.MethodDelete, srv.URL+"/api/v1/invite/"+id, nil)
		assert.Equal(t, http.StatusNotFound, status, "revoking twice is a 404")
	})
}

// TestUserInviteResend pins that re-sending an invite mints a new link and
// retires the old one.
func TestUserInviteResend(t *testing.T) {
	t.Parallel()

	// The refused attempt with the retired link consumes a queued
	// identity of its own, so fay is queued twice.
	srv, mail := newInviteServer(t, true,
		oidcUser("fay", "fay@example.com", true),
		oidcUser("fay", "fay@example.com", true),
	)
	key := inviteAdminKey(t, srv)
	client := srv.HTTPClient(t)

	status, body := createInvite(t, srv, key, map[string]any{"email": "fay@example.com"})
	require.Equal(t, http.StatusCreated, status, "%v", body)

	first, ok := body["url"].(string)
	require.True(t, ok)

	id, ok := field(t, body, "invite", "id").(string)
	require.True(t, ok)

	status, body = apiCall(t, client, key, http.MethodPost, srv.URL+"/api/v1/invite/"+id+"/resend",
		map[string]any{})
	require.Equal(t, http.StatusOK, status, "%v", body)
	assert.Equal(t, true, body["emailSent"])
	assert.Equal(t, 2, mail.count(), "the new link was mailed")

	second, ok := body["url"].(string)
	require.True(t, ok)
	require.NotEqual(t, first, second, "re-sending rotates the token")

	status, page := loginWithInvite(t, srv, inviteToken(t, first))
	assert.Equal(t, http.StatusNotFound, status, page)
	assert.Contains(t, page, "Invitation not found", "the link in the previous mail stops working")

	status, page = loginWithInvite(t, srv, inviteToken(t, second))
	require.Equal(t, http.StatusOK, status, page)

	fay := userByName(t, srv, "fay")
	assert.Equal(t, types.RoleMember, fay.Role, "the default role is member")
}
