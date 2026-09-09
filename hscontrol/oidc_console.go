package hscontrol

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/juanfont/headscale/hscontrol/audit"
	hsdb "github.com/juanfont/headscale/hscontrol/db"
	"github.com/juanfont/headscale/hscontrol/templates"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/web"
	"github.com/rs/zerolog/log"
)

// ConsoleLoginPath starts a console sign-in through the identity provider.
// The console links to it with ?redirect=<console path> and the browser
// comes back signed in at that path.
const ConsoleLoginPath = "/oidc/login"

// ConsoleProvider is what the console shows on its sign-in button.
type ConsoleProvider struct {
	// Name is the provider's display name: "Google" for accounts.google.com,
	// otherwise the issuer's host name, or "single sign-on" when the issuer
	// is an IP address or localhost (a development provider).
	Name string
}

// consoleProviderNames maps well-known issuer hosts to the name users
// know them by.
var consoleProviderNames = map[string]string{
	"accounts.google.com":       "Google",
	"login.microsoftonline.com": "Microsoft",
	"github.com":                "GitHub",
	"gitlab.com":                "GitLab",
	"auth0.com":                 "Auth0",
}

// ConsoleProvider describes the provider for the console's sign-in page.
func (a *AuthProviderOIDC) ConsoleProvider() ConsoleProvider {
	host := a.cfg.Issuer

	u, err := url.Parse(a.cfg.Issuer)
	if err == nil && u.Host != "" {
		host = u.Host
	}

	for known, name := range consoleProviderNames {
		if host == known || strings.HasSuffix(host, "."+known) {
			return ConsoleProvider{Name: name}
		}
	}

	hostname, _, err := net.SplitHostPort(host)
	if err == nil {
		host = hostname
	}

	if host == "localhost" || net.ParseIP(host) != nil {
		return ConsoleProvider{Name: "single sign-on"}
	}

	return ConsoleProvider{Name: host}
}

// ConsoleLoginHandler serves [ConsoleLoginPath]: it sends the browser to
// the identity provider and remembers where in the console to land. An
// invite link's token rides in ?invite and is kept server-side under the
// OIDC state, so the callback can consume it once the identity is known.
func (a *AuthProviderOIDC) ConsoleLoginHandler(writer http.ResponseWriter, req *http.Request) {
	a.startAuth(writer, req, AuthInfo{
		Console:     true,
		Redirect:    consoleRedirect(req.URL.Query().Get("redirect")),
		InviteToken: req.URL.Query().Get(InviteTokenParam),
	})
}

// consoleRedirect keeps a redirect inside the console: a path under the
// console's prefix, never another origin or a protocol-relative URL.
func consoleRedirect(raw string) string {
	if raw == "" || !strings.HasPrefix(raw, web.Prefix) || strings.HasPrefix(raw, "//") {
		return web.Prefix
	}

	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" {
		return web.Prefix
	}

	return u.RequestURI()
}

// handleConsoleCallback finishes a console sign-in: the identity is
// verified and the user exists, so open a session, hand the browser its
// cookie and send it back into the console.
func (a *AuthProviderOIDC) handleConsoleCallback(
	writer http.ResponseWriter,
	req *http.Request,
	authInfo *AuthInfo,
	user *types.User,
) {
	if user.ApprovedAt == nil {
		// The identity is verified, so the reason can be said plainly.
		renderConsoleRefused(writer, http.StatusForbidden, "Waiting for approval",
			"Your account has been created and is waiting for an administrator's approval. "+
				"Try again once you have been approved.")

		return
	}

	token, session, err := a.h.state.CreateSession(types.UserID(user.ID), hsdb.SessionClient{
		RemoteAddr: sessionRemoteAddr(req.RemoteAddr),
		UserAgent:  req.UserAgent(),
	})
	if err != nil {
		httpUserError(writer, NewHTTPError(http.StatusInternalServerError, "could not open a session", err))

		return
	}

	//nolint:gosec // Secure follows the server URL scheme; plain http is for local development only.
	http.SetCookie(writer, &http.Cookie{
		Name:     types.SessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(time.Until(session.ExpiresAt).Seconds()),
		HttpOnly: true,
		Secure:   a.cookiesSecure(),
		SameSite: http.SameSiteLaxMode,
	})

	audit.Record(a.h.state, &types.AuditEvent{
		ActorKind:   types.ActorSession,
		ActorUserID: types.UserID(user.ID),
		ActorName:   user.Name,
		Action:      "console.login",
		TargetKind:  "session",
		TargetID:    strconv.FormatUint(session.ID, 10),
		RemoteAddr:  req.RemoteAddr,
	})

	log.Info().Str("user", user.Name).Uint64("session.id", session.ID).Msg("console sign-in")

	//nolint:gosec // consoleRedirect confined the target to the console prefix when the flow started.
	http.Redirect(writer, req, authInfo.Redirect, http.StatusSeeOther)
}

// sessionRemoteAddr is the address a session records: the host part of
// the request's remote address, which the trusted-proxy middleware has
// already resolved to the real client. The port says nothing about who
// signed in.
func sessionRemoteAddr(remote string) string {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return remote
	}

	return host
}

// renderConsoleRefused shows the sign-in error page with a message meant
// for the person in front of the browser.
func renderConsoleRefused(writer http.ResponseWriter, code int, heading, message string) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(code)

	page := templates.AuthError(templates.AuthErrorResult{
		Title:   "Headscale - " + heading,
		Heading: heading,
		Message: message,
	})

	_, err := writer.Write([]byte(page.Render()))
	if err != nil {
		log.Error().Err(err).Msg("failed to write console sign-in error page")
	}
}

// promoteConfiguredAdmin makes a member whose email is listed in
// oidc.admin_users an admin. It runs on every login so an address added to
// the configuration takes effect the next time that person signs in, and
// it never demotes: the owner and anyone already above member keep their
// role. isConfiguredAdmin holds the email to the verification the
// configuration demands.
func (a *AuthProviderOIDC) promoteConfiguredAdmin(user *types.User, claims *types.OIDCClaims) (*types.User, error) {
	if user.Role != types.RoleMember || !a.isConfiguredAdmin(claims) {
		return user, nil
	}

	promoted, c, err := a.h.state.SetUserRole(nil, types.UserID(user.ID), types.RoleAdmin)
	if err != nil {
		return nil, fmt.Errorf("promoting %s to admin: %w", user.Name, err)
	}

	a.h.Change(c)

	audit.Record(a.h.state, &types.AuditEvent{
		Action:     "user.role.set",
		TargetKind: "user",
		TargetID:   strconv.FormatUint(uint64(promoted.ID), 10),
		TargetName: promoted.Name,
		Detail:     map[string]any{"role": types.RoleAdmin.String(), "source": "oidc.admin_users"},
	})

	log.Info().Str("user", promoted.Name).Msg("promoted to admin by oidc.admin_users")

	return promoted, nil
}

// isConfiguredAdmin reports whether the login's email is in oidc.admin_users.
// An unverified email grants nothing while oidc.email_verified_required is
// on: the address alone is a claim anyone can make at a provider that does
// not check it.
func (a *AuthProviderOIDC) isConfiguredAdmin(claims *types.OIDCClaims) bool {
	if claims.Email == "" {
		return false
	}

	if !claims.EmailVerified && types.FlexibleBoolean(a.cfg.EmailVerifiedRequired) {
		return false
	}

	for _, email := range a.cfg.AdminUsers {
		if strings.EqualFold(email, claims.Email) {
			return true
		}
	}

	return false
}

// syncConfiguredGroups mirrors the login's groups claim into headscale
// groups when oidc.groups.sync is on. It runs on every login, so a person
// added to or removed from a group at the identity provider gets or loses
// the group's access the next time they sign in.
func (a *AuthProviderOIDC) syncConfiguredGroups(user *types.User, claims *types.OIDCClaims) error {
	if !a.cfg.Groups.Sync {
		return nil
	}

	names := a.cfg.Groups.SyncedNames(claims.Groups)

	c, err := a.h.state.SyncUserGroups(types.UserID(user.ID), names)
	if err != nil {
		return fmt.Errorf("syncing groups of %s: %w", user.Name, err)
	}

	if c.IsEmpty() {
		return nil
	}

	a.h.Change(c)

	audit.Record(a.h.state, &types.AuditEvent{
		Action:     "group.sync",
		TargetKind: "user",
		TargetID:   strconv.FormatUint(uint64(user.ID), 10),
		TargetName: user.Name,
		Detail:     map[string]any{"groups": names, "source": "oidc.groups"},
	})

	log.Info().Str("user", user.Name).Strs("groups", names).Msg("groups synced from the identity provider")

	return nil
}
