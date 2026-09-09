package apiv1

import (
	"context"
	"net/http"

	"github.com/aislopware/slopscale/hscontrol/api/principal"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/danielgtaylor/huma/v2"
)

func init() {
	registrations = append(registrations, registerWhoami)
}

// Whoami describes the caller: how it authenticated, who it is and what it
// may do. The admin console reads it once after login to shape its menus.
type Whoami struct {
	// Kind is "local", "api_key", "oauth" or "session".
	Kind string `doc:"How the caller authenticated: local, api_key, oauth or session." json:"kind"`
	// User is the credential's owner; absent for a legacy key, a token or the socket.
	User *User `json:"user,omitempty"`
	// Role is the owner's role; "member" for a credential without a user.
	Role string `json:"role"`
	// AllAccess reports whether the caller passes every scope.
	AllAccess bool `json:"allAccess"`
	// Scopes the caller holds; empty when allAccess.
	Scopes []string `json:"scopes" nullable:"false"`
	// Permissions is every known scope with whether the caller holds it.
	Permissions map[string]bool `json:"permissions"`
}

type whoamiOutput struct {
	Body Whoami
}

func registerWhoami(api huma.API, b Backend) {
	huma.Register(api, huma.Operation{
		OperationID: "whoami",
		Method:      http.MethodGet,
		Path:        "/api/v1/whoami",
		Summary:     "Describe the caller",
		Description: "Any authenticated caller may ask who it is; no scope is required.",
		Tags:        []string{"Users"},
		Security:    bearerAuth,
	}, func(ctx context.Context, _ *struct{}) (*whoamiOutput, error) {
		p := caller(ctx)

		out := &whoamiOutput{}
		out.Body = whoamiFromPrincipal(p)

		if p.HasUser() {
			user, err := b.State.GetUserByID(p.UserID)
			if err == nil {
				u := userFromView(user.View())
				out.Body.User = &u
			}
		}

		return out, nil
	})
}

func whoamiFromPrincipal(p principal.Principal) Whoami {
	w := Whoami{
		Role:        p.Role.String(),
		AllAccess:   !p.Bounded,
		Scopes:      []string{},
		Permissions: make(map[string]bool, len(scope.Known())),
	}

	switch p.Kind {
	case principal.LocalTrust:
		w.Kind = "local"
	case principal.APIKey:
		w.Kind = "api_key"
	case principal.AccessToken:
		w.Kind = "oauth"
	case principal.Session:
		w.Kind = "session"
	}

	if p.Bounded {
		for _, s := range p.Scopes {
			w.Scopes = append(w.Scopes, string(s))
		}
	}

	for _, s := range scope.Known() {
		w.Permissions[string(s)] = p.Allows(s)
	}

	return w
}
