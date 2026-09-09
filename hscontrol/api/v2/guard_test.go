package apiv2

import (
	"net/http"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/api/principal"
	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// selfEnforcedKeyOps are the authenticated operations that intentionally declare
// NO static scope because they multiplex on keyType and authorize inside the
// handler via requireKeyScope (see keys.go). Every other authenticated operation
// must declare a scope.
var selfEnforcedKeyOps = map[string]bool{
	"POST /api/v2/tailnet/{tailnet}/keys":           true,
	"GET /api/v2/tailnet/{tailnet}/keys":            true,
	"GET /api/v2/tailnet/{tailnet}/keys/{keyId}":    true,
	"DELETE /api/v2/tailnet/{tailnet}/keys/{keyId}": true,
}

// TestEveryAuthenticatedOperationDeclaresScope is the structural guarantee that no
// v2 operation ships unprotected: any operation that requires authentication
// (non-empty Security) must either declare a required scope via requireScope, or
// be one of the keyType-multiplexed keys operations that self-enforce. A new
// operation added without scope protection fails this test.
func TestEveryAuthenticatedOperationDeclaresScope(t *testing.T) {
	api := NewAPI(chi.NewMux(), Backend{})

	for path, item := range api.OpenAPI().Paths {
		for method, op := range humaOperations(item) {
			if op == nil || len(op.Security) == 0 {
				continue // unregistered method or a public operation
			}

			key := method + " " + path
			if selfEnforcedKeyOps[key] {
				continue
			}

			if _, ok := principal.RequiredScope(op); !ok {
				t.Errorf("operation %q is authenticated but declares no required scope; "+
					"wrap it in principal.RequireScope, or add it to selfEnforcedKeyOps if it "+
					"authorizes inside the handler", key)
			}
		}
	}
}

func humaOperations(item *huma.PathItem) map[string]*huma.Operation {
	return map[string]*huma.Operation{
		"GET":    item.Get,
		"POST":   item.Post,
		"PUT":    item.Put,
		"DELETE": item.Delete,
		"PATCH":  item.Patch,
	}
}

// TestEveryWritingOperationIsAudited guarantees no v2 operation that changes
// state ships without an audit action (audit.Declare).
func TestEveryWritingOperationIsAudited(t *testing.T) {
	api := NewAPI(chi.NewMux(), Backend{})

	for path, item := range api.OpenAPI().Paths {
		for method, op := range humaOperations(item) {
			if op == nil || method == "GET" {
				continue
			}

			if _, ok := audit.Action(op); !ok {
				t.Errorf("operation %q writes but declares no audit action; wrap it in audit.Declare",
					method+" "+path)
			}
		}
	}
}

// nonHumaRoutes are the routes mounted on the v2 router outside huma, each
// with the reason it may be. Such a route runs neither the scope middleware
// nor the audit middleware, so the guards above cannot see it: it has to
// authenticate and record for itself.
var nonHumaRoutes = map[string]string{
	"POST /api/v2/oauth/token": "RFC 6749 form request and error body; authenticates the client " +
		"itself and records its own audit event (see oauth.go)",
	"GET /api/v2/docs":             "the rendered API documentation, public",
	"GET /api/v2/openapi.json":     "the generated OpenAPI 3.1 document, public",
	"GET /api/v2/openapi.yaml":     "the generated OpenAPI 3.1 document, public",
	"GET /api/v2/openapi-3.0.json": "the generated OpenAPI 3.0 document, public",
	"GET /api/v2/openapi-3.0.yaml": "the generated OpenAPI 3.0 document, public",
}

// TestEveryNonHumaRouteIsDeclared guarantees no route slips onto the v2
// router outside huma unnoticed: anything the router serves that is not a
// registered operation must be listed in nonHumaRoutes with the reason it
// escapes the scope and audit guards.
func TestEveryNonHumaRouteIsDeclared(t *testing.T) {
	t.Parallel()

	mux, api := Handler(Backend{})

	operations := map[string]bool{}

	for path, item := range api.OpenAPI().Paths {
		for method, op := range humaOperations(item) {
			if op != nil {
				operations[method+" "+path] = true
			}
		}
	}

	err := chi.Walk(mux, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		key := method + " " + route
		if operations[key] {
			return nil
		}

		if reason, ok := nonHumaRoutes[key]; !ok || reason == "" {
			t.Errorf("route %q is served outside huma, so it runs neither the scope nor the audit "+
				"middleware; register it as an operation, or add it to nonHumaRoutes with the "+
				"reason it does not need them", key)
		}

		return nil
	})
	require.NoError(t, err)
}

// TestSelfEnforcingOperationsAreAuthenticated pins the other half of the
// scope guard: an operation on the self-enforcing list is excused from
// declaring a scope, not from authentication. Without Security the
// middleware attaches no principal, and the handler's own check would then
// run against [principal.None], which holds nothing.
func TestSelfEnforcingOperationsAreAuthenticated(t *testing.T) {
	t.Parallel()

	api := NewAPI(chi.NewMux(), Backend{})

	seen := map[string]bool{}

	for path, item := range api.OpenAPI().Paths {
		for method, op := range humaOperations(item) {
			key := method + " " + path
			if op == nil || !selfEnforcedKeyOps[key] {
				continue
			}

			seen[key] = true

			assert.NotEmpty(t, op.Security,
				"self-enforcing operation %q must still require authentication", key)
		}
	}

	for key := range selfEnforcedKeyOps {
		assert.True(t, seen[key], "selfEnforcedKeyOps names %q, which no operation registers", key)
	}
}
