package apiv1

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/api/principal"
	"github.com/juanfont/headscale/hscontrol/audit"
	"github.com/juanfont/headscale/hscontrol/types"
)

func init() {
	registrations = append(registrations, registerSession)
}

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
		Tags: []string{"Auth"},
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
		Tags:          []string{"Auth"},
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
