package apiv1

import (
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/juanfont/headscale/hscontrol/api/principal"
	"github.com/juanfont/headscale/hscontrol/audit"
)

// selfEnforcedOps are the authenticated operations that intentionally declare
// no static scope: health and whoami are open to any authenticated caller, and
// the API key operations bound a role-limited caller to its own keys inside
// the handler (apiKeyOwner, requireKeyAccess). Every other authenticated
// operation must declare a scope.
var selfEnforcedOps = map[string]bool{
	"GET /api/v1/health":             true,
	"GET /api/v1/whoami":             true,
	"DELETE /api/v1/auth/session":    true,
	"POST /api/v1/apikey":            true,
	"POST /api/v1/apikey/expire":     true,
	"GET /api/v1/apikey":             true,
	"DELETE /api/v1/apikey/{prefix}": true,

	// Sharing lets a member act on the nodes they own (requireShareAccess).
	"POST /api/v1/node/{nodeId}/share":            true,
	"DELETE /api/v1/node/{nodeId}/share/{userId}": true,
}

// TestEveryAuthenticatedOperationDeclaresScope guarantees no v1 operation
// ships reachable by a role-limited API key without a scope check: any
// operation requiring authentication must declare a scope via withScope or
// be listed as self-enforcing. A new operation added without either fails.
func TestEveryAuthenticatedOperationDeclaresScope(t *testing.T) {
	t.Parallel()

	api := NewAPI(chi.NewMux(), Backend{})

	for path, item := range api.OpenAPI().Paths {
		for method, op := range humaOperations(item) {
			if op == nil || len(op.Security) == 0 {
				continue
			}

			key := method + " " + path
			if selfEnforcedOps[key] {
				continue
			}

			if _, ok := principal.RequiredScope(op); !ok {
				t.Errorf("operation %q is authenticated but declares no required scope; "+
					"wrap it in withScope, or add it to selfEnforcedOps if it authorizes "+
					"inside the handler", key)
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

// unauditedOps are the writing operations that change nothing: they
// validate input and answer.
var unauditedOps = map[string]bool{
	"POST /api/v1/policy/check":  true,
	"POST /api/v1/posture/check": true,
}

// TestEveryWritingOperationIsAudited guarantees no v1 operation that changes
// state ships without an audit action: every non-GET operation must declare
// one via audited, or be listed as unaudited because it changes nothing.
func TestEveryWritingOperationIsAudited(t *testing.T) {
	t.Parallel()

	api := NewAPI(chi.NewMux(), Backend{})

	for path, item := range api.OpenAPI().Paths {
		for method, op := range humaOperations(item) {
			if op == nil || method == "GET" {
				continue
			}

			key := method + " " + path
			if unauditedOps[key] {
				continue
			}

			if _, ok := audit.Action(op); !ok {
				t.Errorf("operation %q writes but declares no audit action; wrap it in audited, "+
					"or add it to unauditedOps if it changes nothing", key)
			}
		}
	}
}
