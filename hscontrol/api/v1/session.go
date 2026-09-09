package apiv1

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aislopware/slopscale/hscontrol/api/principal"
	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
)

func init() {
	registrations = append(registrations, registerSession, registerSessionManagement)
}

// tagAuth groups the sign-in operations in the emitted spec.
const tagAuth = "Auth"

// ConsoleSession is one browser signed in to the admin console.
type ConsoleSession struct {
	ID   string `format:"uint64" json:"id"`
	User User   `json:"user"`

	CreatedAt  time.Time `json:"createdAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	LastSeenAt time.Time `doc:"When it last made a request; written at most once a minute." json:"lastSeenAt"`

	RemoteAddr string `doc:"Where the browser signed in from; empty for an older session." json:"remoteAddr"`
	UserAgent  string `doc:"The browser that signed in; empty for an older session."       json:"userAgent"`

	Current bool `doc:"True for the session making this request." json:"current"`
}

type (
	listSessionsOutput struct {
		Body struct {
			Sessions []ConsoleSession `json:"sessions" nullable:"false"`
		}
	}

	endSessionByIDInput struct {
		ID string `format:"uint64" path:"id"`
	}

	endUserSessionsInput struct {
		ID string `format:"uint64" path:"id"`
	}
	endUserSessionsOutput struct {
		Body struct {
			Ended int64 `doc:"How many sessions were ended." json:"ended"`
		}
	}
)

// ConsoleAuth tells the admin console how it may sign in, before any
// credential is presented.
type ConsoleAuth struct {
	// OIDC is present when the server signs users in through an identity
	// provider.
	OIDC *ConsoleOIDC `json:"oidc,omitempty"`
}

// ConsoleOIDC describes the identity provider sign-in.
type ConsoleOIDC struct {
	// Provider is the display name for the sign-in button, such as "Google".
	Provider string `json:"provider"`
	// LoginPath is where to send the browser, relative to the server URL;
	// it takes ?redirect=<console path> to land there after sign-in.
	LoginPath string `json:"loginPath"`
}

type consoleAuthOutput struct {
	Body ConsoleAuth
}

// endSessionOutput clears the session cookie along with the row.
type endSessionOutput struct {
	SetCookie http.Cookie `header:"Set-Cookie"`
}

func registerSession(api huma.API, b Backend) {
	huma.Register(api, huma.Operation{
		OperationID: "getConsoleAuth",
		Method:      http.MethodGet,
		Path:        "/api/v1/auth/console",
		Summary:     "Describe console sign-in",
		Description: "Public: the admin console asks before showing its sign-in page which methods the " +
			"server offers. Signing in with an API key is always possible.",
		Tags: []string{tagAuth},
	}, func(_ context.Context, _ *struct{}) (*consoleAuthOutput, error) {
		out := &consoleAuthOutput{}

		if b.ConsoleLogin != nil {
			out.Body.OIDC = &ConsoleOIDC{
				Provider:  b.ConsoleLogin.Provider,
				LoginPath: b.ConsoleLogin.Path,
			}
		}

		return out, nil
	})

	huma.Register(api, audited(huma.Operation{
		OperationID: "endSession",
		Method:      http.MethodDelete,
		Path:        "/api/v1/auth/session",
		Summary:     "Sign out of the console",
		Description: "Ends the console session the request was authenticated with and clears its cookie. " +
			"Only a session may call it; an API key has nothing to end.",
		Tags:          []string{tagAuth},
		Security:      bearerAuth,
		DefaultStatus: http.StatusNoContent,
	}, "console.logout", "session", ""), func(ctx context.Context, _ *struct{}) (*endSessionOutput, error) {
		p := caller(ctx)
		if p.Kind != principal.Session {
			return nil, huma.Error400BadRequest("the caller is not signed in with a session")
		}

		err := b.State.DeleteSession(p.SessionID)
		if err != nil {
			return nil, mapError("ending session", err)
		}

		audit.Target(ctx, "", p.Credential, "")

		//nolint:gosec // Secure follows the server URL scheme; plain http is for local development only.
		return &endSessionOutput{
			SetCookie: http.Cookie{
				Name:     types.SessionCookieName,
				Value:    "",
				Path:     "/",
				MaxAge:   -1,
				HttpOnly: true,
				Secure:   strings.HasPrefix(b.Cfg.ServerURL, "https://"),
				SameSite: http.SameSiteLaxMode,
			},
		}, nil
	})
}

// sessionAudience decides whose sessions the caller may see or end: a
// caller holding the users scope reaches every session, anyone else only
// their own. A zero user ID means every session.
func sessionAudience(ctx context.Context, want scope.Scope) (types.UserID, error) {
	p := caller(ctx)
	if p.Allows(want) {
		return 0, nil
	}

	if !p.HasUser() {
		return 0, huma.Error403Forbidden("this credential may only reach its own sessions, and it has none")
	}

	return p.UserID, nil
}

// registerSessionManagement registers the operations that show and end
// console sessions. Listing and ending one session declare no scope: they
// authorize in the handler, because a member holds no scope at all and
// must still be able to see and end their own sign-ins (see
// selfEnforcedOps).
func registerSessionManagement(api huma.API, b Backend) {
	huma.Register(api, huma.Operation{
		OperationID: "listSessions",
		Method:      http.MethodGet,
		Path:        "/api/v1/auth/sessions",
		Summary:     "List console sessions",
		Description: "Lists the console sign-ins that have not expired. A caller who may manage users " +
			"sees every session; anyone else sees only their own.",
		Tags:     []string{tagAuth},
		Security: bearerAuth,
	}, func(ctx context.Context, _ *struct{}) (*listSessionsOutput, error) {
		audience, err := sessionAudience(ctx, scope.UsersRead)
		if err != nil {
			return nil, err
		}

		sessions, err := b.State.ListSessions(audience)
		if err != nil {
			return nil, huma.Error500InternalServerError("listing sessions", err)
		}

		out := &listSessionsOutput{}

		out.Body.Sessions, err = consoleSessions(b, sessions, caller(ctx).SessionID)
		if err != nil {
			return nil, huma.Error500InternalServerError("listing sessions", err)
		}

		return out, nil
	})

	huma.Register(api, audited(huma.Operation{
		OperationID: "endSessionByID",
		Method:      http.MethodDelete,
		Path:        "/api/v1/auth/sessions/{id}",
		Summary:     "End a console session",
		Description: "Ends one console sign-in, so the browser holding its cookie is signed out on its " +
			"next request. A caller who may manage users can end any session; anyone else only their own.",
		Tags:          []string{tagAuth},
		Security:      bearerAuth,
		DefaultStatus: http.StatusNoContent,
	}, "session.end", "session", "id"), func(
		ctx context.Context, in *endSessionByIDInput,
	) (*struct{}, error) {
		id, err := strconv.ParseUint(in.ID, 10, 64)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid session id", err)
		}

		audience, err := sessionAudience(ctx, scope.Users)
		if err != nil {
			return nil, err
		}

		session, err := b.State.GetSession(id)
		if err != nil {
			return nil, mapError("ending session", err)
		}

		if audience != 0 && session.UserID != audience {
			return nil, huma.Error403Forbidden("only an administrator may end another user's session")
		}

		audit.Detail(ctx, "user", formatID(uint64(session.UserID)))

		err = b.State.DeleteSession(id)
		if err != nil {
			return nil, mapError("ending session", err)
		}

		return &struct{}{}, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "endUserSessions",
		Method:      http.MethodDelete,
		Path:        "/api/v1/user/{id}/sessions",
		Summary:     "Sign a user out everywhere",
		Description: "Ends every console session of the user. Their browsers are signed out on their " +
			"next request; nothing else about the account changes.",
		Tags:     []string{tagAuth},
		Security: bearerAuth,
	}, scope.Users), "user.sessions.end", "user", "id"), func(
		ctx context.Context, in *endUserSessionsInput,
	) (*endUserSessionsOutput, error) {
		id, err := parseUserID(in.ID)
		if err != nil {
			return nil, err
		}

		user, err := b.State.GetUserByID(id)
		if err != nil {
			return nil, mapError("ending sessions", err)
		}

		audit.Target(ctx, "", "", user.Name)

		ended, err := b.State.DeleteUserSessions(id)
		if err != nil {
			return nil, huma.Error500InternalServerError("ending sessions", err)
		}

		audit.Detail(ctx, "ended", ended)

		out := &endUserSessionsOutput{}
		out.Body.Ended = ended

		return out, nil
	})
}

// consoleSessions renders sessions for the API, resolving each one's user
// once. current marks the session the request was authenticated with.
func consoleSessions(b Backend, sessions []types.Session, current uint64) ([]ConsoleSession, error) {
	users, err := b.State.ListAllUsers()
	if err != nil {
		return nil, err
	}

	byID := make(map[types.UserID]types.User, len(users))
	for i := range users {
		byID[types.UserID(users[i].ID)] = users[i]
	}

	out := make([]ConsoleSession, 0, len(sessions))

	for i := range sessions {
		s := sessions[i]

		rendered := ConsoleSession{
			ID:         formatID(s.ID),
			CreatedAt:  s.CreatedAt,
			ExpiresAt:  s.ExpiresAt,
			LastSeenAt: s.LastSeenAt,
			RemoteAddr: s.RemoteAddr,
			UserAgent:  s.UserAgent,
			Current:    s.ID == current,
		}

		if user, ok := byID[s.UserID]; ok {
			rendered.User = userFromView(user.View())
		}

		out = append(out, rendered)
	}

	return out, nil
}
