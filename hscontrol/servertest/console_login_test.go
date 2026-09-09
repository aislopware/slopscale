package servertest_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/aislopware/slopscale/hscontrol"
	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/web"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// consoleCall makes a request the way the console's browser does: the
// session cookie from the jar, JSON, and the same-origin fetch header.
func consoleCall(
	t *testing.T,
	client *http.Client,
	method, target string,
	body any,
	headers map[string]string,
) (int, map[string]any) {
	t.Helper()

	var reqBody io.Reader = http.NoBody

	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)

		reqBody = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(t.Context(), method, target, reqBody)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var decoded map[string]any

	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &decoded)
	}

	return resp.StatusCode, decoded
}

func sessionCookie(t *testing.T, client *http.Client, srv *servertest.TestServer) *http.Cookie {
	t.Helper()

	u, err := url.Parse(srv.URL)
	require.NoError(t, err)

	for _, c := range client.Jar.Cookies(u) {
		if c.Name == types.SessionCookieName {
			return c
		}
	}

	return nil
}

// TestConsoleLogin walks a browser through a console sign-in: the console
// sends it to /oidc/login, the identity provider bounces it back, and it
// lands in the console holding a session cookie that authenticates the
// API, is bound to the same-origin rule, shows up in the audit log and
// ends on sign-out.
//
//nolint:tparallel // the subtests drive one browser session in order.
func TestConsoleLogin(t *testing.T) {
	t.Parallel()

	srv, _ := newOIDCServer(t, nil,
		oidcUser("alice", "alice@example.com", true),
		oidcUser("alice", "alice@example.com", true),
	)

	browser := srv.HTTPClient(t)

	status, body := consoleCall(t, browser, http.MethodGet, srv.URL+"/api/v1/auth/console", nil, nil)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, hscontrol.ConsoleLoginPath, field(t, body, "oidc", "loginPath"))
	require.Equal(t, "single sign-on", field(t, body, "oidc", "provider"), "a loopback issuer gets a generic name")

	status, _ = consoleCall(t, browser, http.MethodGet, srv.URL+"/api/v1/whoami", nil, nil)
	require.Equal(t, http.StatusUnauthorized, status, "no cookie yet")

	status, page := browse(t, browser, srv.URL+hscontrol.ConsoleLoginPath+"?redirect="+
		url.QueryEscape(web.Prefix+"machines"))
	require.Equal(t, http.StatusOK, status, "sign-in did not land in the console:\n%s", page)

	require.NotNil(t, sessionCookie(t, browser, srv), "the callback set no session cookie")

	user := soleUser(t, srv)
	assert.Equal(t, types.RoleOwner, user.Role, "the first user of an empty server owns it")

	t.Run("the cookie authenticates the api as the user", func(t *testing.T) {
		status, body := consoleCall(t, browser, http.MethodGet, srv.URL+"/api/v1/whoami", nil, nil)
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, "session", body["kind"])
		assert.Equal(t, "owner", body["role"])
		assert.Equal(t, "alice", field(t, body, "user", "name"))
	})

	t.Run("a cookie without the browser's same-origin word cannot write", func(t *testing.T) {
		status, _ := consoleCall(t, browser, http.MethodPost, srv.URL+"/api/v1/user",
			map[string]string{"name": "mallory"}, map[string]string{"Sec-Fetch-Site": "cross-site"})
		assert.Equal(t, http.StatusForbidden, status)

		status, _ = consoleCall(t, browser, http.MethodGet, srv.URL+"/api/v1/whoami", nil,
			map[string]string{"Sec-Fetch-Site": "cross-site"})
		assert.Equal(t, http.StatusForbidden, status)
	})

	t.Run("writes and the sign-in are audited", func(t *testing.T) {
		status, _ := consoleCall(t, browser, http.MethodPost, srv.URL+"/api/v1/user",
			map[string]string{"name": "bob"}, nil)
		require.Equal(t, http.StatusOK, status)

		status, body := consoleCall(t, browser, http.MethodGet, srv.URL+"/api/v1/audit", nil, nil)
		require.Equal(t, http.StatusOK, status)

		events, ok := body["events"].([]any)
		require.True(t, ok)
		require.GreaterOrEqual(t, len(events), 2)

		newest, ok := events[0].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "user.create", newest["action"])
		assert.Equal(t, "session", newest["actorKind"])
		assert.Equal(t, "alice", newest["actorName"])
		assert.Equal(t, "user", newest["targetKind"])
		assert.Equal(t, "bob", newest["targetName"])
		assert.InDelta(t, http.StatusOK, newest["outcome"], 0)

		oldest, ok := events[len(events)-1].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "console.login", oldest["action"])
		assert.Equal(t, "alice", oldest["actorName"])

		status, body = consoleCall(t, browser, http.MethodGet, srv.URL+"/api/v1/audit?action=console.", nil, nil)
		require.Equal(t, http.StatusOK, status)
		assert.Len(t, body["events"], 1, "the prefix filter keeps only console actions")
	})

	t.Run("sign-out ends the session and clears the cookie", func(t *testing.T) {
		status, _ := consoleCall(t, browser, http.MethodDelete, srv.URL+"/api/v1/auth/session", nil, nil)
		require.Equal(t, http.StatusNoContent, status)

		assert.Nil(t, sessionCookie(t, browser, srv), "the response cleared the cookie")

		status, _ = consoleCall(t, browser, http.MethodGet, srv.URL+"/api/v1/whoami", nil, nil)
		assert.Equal(t, http.StatusUnauthorized, status)
	})

	t.Run("a foreign redirect lands on the console root", func(t *testing.T) {
		client := srv.HTTPClient(t)
		client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
			if req.URL.Path == web.Prefix {
				return http.ErrUseLastResponse
			}

			return nil
		}

		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet,
			srv.URL+hscontrol.ConsoleLoginPath+"?redirect=https://evil.example/", http.NoBody)
		require.NoError(t, err)

		resp, err := client.Do(req)
		require.NoError(t, err)
		resp.Body.Close()

		assert.Equal(t, http.StatusSeeOther, resp.StatusCode)
		assert.Equal(t, web.Prefix, resp.Header.Get("Location"))

		var cookie *http.Cookie

		for _, c := range resp.Cookies() {
			if c.Name == types.SessionCookieName {
				cookie = c
			}
		}

		require.NotNil(t, cookie, "the callback response carries the session cookie")
		assert.True(t, cookie.HttpOnly, "the console must not be able to read the token")
		assert.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
		assert.Equal(t, "/", cookie.Path)
		assert.Positive(t, cookie.MaxAge)
	})
}

// TestConsoleLoginWaitsForApproval pins that a user who signs in while
// users approval is on gets no session until an administrator admits them.
func TestConsoleLoginWaitsForApproval(t *testing.T) {
	t.Parallel()

	srv, _ := newOIDCServer(t, nil, oidcUser("carol", "carol@example.com", true))

	_, err := srv.State().SetSetting(types.SettingUsersApprovalOn, true)
	require.NoError(t, err)

	browser := srv.HTTPClient(t)

	status, page := browse(t, browser, srv.URL+hscontrol.ConsoleLoginPath)
	assert.Equal(t, http.StatusForbidden, status, page)
	assert.Contains(t, page, "waiting for an administrator")
	assert.Contains(t, page, "Waiting for approval")
	assert.Nil(t, sessionCookie(t, browser, srv))
}

// TestConsoleLoginAdminUsers pins oidc.admin_users: an address on the list
// signs in as an admin even when someone else already owns the tailnet,
// the match ignores case, a listed owner is left alone, and an unlisted
// user stays a member.
func TestConsoleLoginAdminUsers(t *testing.T) {
	t.Parallel()

	srv, _ := newOIDCServer(t,
		func(cfg *types.OIDCConfig) { cfg.AdminUsers = []string{"Dave@Example.com", "owner@example.com"} },
		oidcUser("owner", "owner@example.com", true),
		oidcUser("dave", "dave@example.com", true),
		oidcUser("erin", "erin@example.com", true),
	)

	for _, tc := range []struct {
		name string
		role types.Role
	}{
		{"owner", types.RoleOwner},
		{"dave", types.RoleAdmin},
		{"erin", types.RoleMember},
	} {
		browser := srv.HTTPClient(t)

		status, page := browse(t, browser, srv.URL+hscontrol.ConsoleLoginPath)
		require.Equal(t, http.StatusOK, status, page)

		status, body := consoleCall(t, browser, http.MethodGet, srv.URL+"/api/v1/whoami", nil, nil)
		require.Equal(t, http.StatusOK, status)
		assert.Equal(t, tc.name, field(t, body, "user", "name"))
		assert.Equal(t, tc.role.String(), body["role"], "role of %s", tc.name)
	}

	events, err := srv.State().ListAuditEvents(types.AuditQuery{Action: "user.role.set"})
	require.NoError(t, err)
	require.Len(t, events, 1, "only dave was promoted")
	assert.Equal(t, types.ActorSystem, events[0].ActorKind)
	assert.Equal(t, "dave", events[0].TargetName)
	assert.Equal(t, "oidc.admin_users", events[0].Detail["source"])
}
