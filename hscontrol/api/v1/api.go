// Package apiv1 is the code-first Huma implementation of the Headscale v1 API.
// Handlers are a thin adapter over hscontrol/state; Huma emits the OpenAPI 3.1
// spec from the Go definitions (see Spec), and that spec drives the client.
//
// It depends only on the domain layer (hscontrol/state, hscontrol/types) via
// Backend, never on the hscontrol server package, so a future hscontrol/api/v2
// can sit beside it without either importing the other.
package apiv1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/juanfont/headscale/hscontrol/api/principal"
	"github.com/juanfont/headscale/hscontrol/audit"
	"github.com/juanfont/headscale/hscontrol/recorder"
	"github.com/juanfont/headscale/hscontrol/scope"
	"github.com/juanfont/headscale/hscontrol/state"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/types/change"
)

// Backend is the dependency surface the v1 API needs from the control plane:
// the state layer, the change-notification sink that distributes updates to
// connected nodes, and the config (only Policy.Mode and Policy.Path are read).
type Backend struct {
	State  *state.State
	Change func(...change.Change)
	Cfg    *types.Config

	// ConsoleLogin describes sign-in through the identity provider for
	// the admin console; nil when the server has no OIDC provider.
	ConsoleLogin *ConsoleLogin

	// Recorder indexes and serves SSH session recordings.
	Recorder *recorder.Recorder
}

// ConsoleLogin is how the console starts a sign-in through the identity
// provider.
type ConsoleLogin struct {
	// Provider is the display name for the sign-in button ("Google").
	Provider string
	// Path is where the browser is sent, relative to the server URL; it
	// takes ?redirect=<console path>.
	Path string
}

// NewAPI builds the v1 Huma API on the given chi router and registers every
// operation. Auth is enforced by a Huma middleware driven by each operation's
// declared bearer security and required scope (see authMiddleware);
// locally-trusted requests bypass it via WithLocalTrust.
func NewAPI(router chi.Router, backend Backend) huma.API {
	config := huma.DefaultConfig("Headscale API", "v1")
	config.Info.Description = "Headscale control server API."

	// Version the OpenAPI/docs routes under /api/v1 so a future v2 owns its own.
	// These register as plain mux routes, not operations, so they never appear
	// in the emitted spec or client.
	config.OpenAPIPath = "/api/v1/openapi"
	config.DocsPath = "/api/v1/docs"

	// The v1 API does not emit "$schema".
	config.SchemasPath = ""

	// Drop the default schema-link create hook: it injects a "$schema" property
	// and Link header into every response, which the v1 contract omits.
	config.CreateHooks = nil

	config.Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearer": {
			Type:   "http",
			Scheme: "bearer",
		},
	}

	api := humachi.New(router, config)

	// Must run before register: Huma snapshots the middleware chain at operation
	// registration, so a middleware added afterwards would silently never run.
	api.UseMiddleware(authMiddleware(api, backend))
	// After authentication, so the audit sees the principal.
	api.UseMiddleware(audit.Middleware(backend.State))

	register(api, backend)

	return api
}

// bearerAuth is the security requirement applied to every operation: all
// /api/v1 routes require an API key.
var bearerAuth = []map[string][]string{{"bearer": {}}}

// registrations is populated by each resource file's init(), so adding a
// resource group means adding a file rather than editing a shared point. Huma
// sorts the emitted spec, so init order does not affect output.
var registrations []func(huma.API, Backend)

// register wires up every operation contributed by the resource files.
func register(api huma.API, b Backend) {
	for _, fn := range registrations {
		fn(api, b)
	}
}

// Spec emits the OpenAPI 3.1 document. The zero Backend is safe because
// handlers are registered but never invoked during emission.
func Spec() ([]byte, error) {
	api := NewAPI(chi.NewMux(), Backend{})

	yaml, err := api.OpenAPI().YAML()
	if err != nil {
		return nil, fmt.Errorf("generating OpenAPI 3.1 YAML: %w", err)
	}

	return yaml, nil
}

// Spec30 emits the document downgraded to OpenAPI 3.0.3, needed because the
// client generator (oapi-codegen v2) cannot yet read the 3.1 spec.
func Spec30() ([]byte, error) {
	api := NewAPI(chi.NewMux(), Backend{})

	yaml, err := api.OpenAPI().DowngradeYAML()
	if err != nil {
		return nil, fmt.Errorf("generating OpenAPI 3.0 YAML: %w", err)
	}

	return yaml, nil
}

// Handler builds the v1 API on a fresh mux and returns both. Callers mount the
// mux and may use mux.Match to detect which paths this API serves.
func Handler(backend Backend) (*chi.Mux, huma.API) {
	mux := chi.NewMux()
	api := NewAPI(mux, backend)

	return mux, api
}

// WithLocalTrust wraps a handler so its requests bypass API-key authentication.
// The unix socket uses it, since access to the socket is the trust boundary.
// In-process tests that exercise the mux directly use it too.
func WithLocalTrust(next http.Handler) http.Handler {
	return principal.WithLocalTrust(next)
}

// authMiddleware authenticates the caller of any operation that declares
// security and enforces the scope the operation declared, so a role-limited
// API key or an OAuth token is held to the same matrix as on v2. Locally
// trusted requests and operations without declared security pass through.
// b.State is nil only during spec emission, where no request is served.
func authMiddleware(api huma.API, b Backend) func(huma.Context, func(huma.Context)) {
	return principal.Middleware(api, b.State)
}

// caller returns the request's principal. The middleware attaches one to
// every authenticated request, and [WithLocalTrust] to a request over the
// socket; a request that carries none reaches here only through a coding
// mistake and gets [principal.None], which may do nothing.
func caller(ctx context.Context) principal.Principal {
	p, ok := principal.From(ctx)
	if !ok {
		return principal.None()
	}

	return p
}

// roleActor is the caller as the state layer's role rules see it: nil for an
// all-access credential without a user, which is bound only by the
// structural rules.
func roleActor(ctx context.Context) *state.RoleActor {
	p := caller(ctx)
	if !p.HasUser() {
		return nil
	}

	return &state.RoleActor{UserID: p.UserID, Role: p.Role}
}

// withScope declares the scope op requires; see [principal.RequireScope].
func withScope(op huma.Operation, s scope.Scope) huma.Operation {
	return principal.RequireScope(op, s)
}

// audited declares the audit action op records and, when the path names
// its object, the target kind and the path parameter carrying its ID; see
// [audit.Declare].
func audited(op huma.Operation, action, targetKind, targetParam string) huma.Operation {
	return audit.Declare(op, action, targetKind, targetParam)
}
