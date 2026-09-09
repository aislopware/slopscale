package apiv1

import (
	"testing"

	"github.com/aislopware/slopscale/hscontrol/api/principal"
	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// selfEnforcedOps are the authenticated operations that intentionally declare
// no static scope: health and whoami are open to any authenticated caller, and
// the API key operations bound a role-limited caller to its own keys inside
// the handler (apiKeyOwner, requireKeyAccess). Every other authenticated
// operation must declare a scope.
var selfEnforcedOps = map[string]bool{
	"GET /api/v1/health":                  true,
	"GET /api/v1/whoami":                  true,
	"DELETE /api/v1/auth/session":         true,
	"POST /api/v1/apikey":                 true,
	"POST /api/v1/apikey/expire":          true,
	"POST /api/v1/apikey/{prefix}/rotate": true,
	"GET /api/v1/apikey":                  true,
	"DELETE /api/v1/apikey/{prefix}":      true,

	// Sharing lets a member act on the nodes they own (requireShareAccess).
	"POST /api/v1/node/{nodeId}/share":            true,
	"DELETE /api/v1/node/{nodeId}/share/{userId}": true,

	// Access requests let a member file and follow their own asks
	// (requireRequestAccess); deciding declares the policy scope.
	"GET /api/v1/access-request/options": true,
	"GET /api/v1/access-request":         true,
	"GET /api/v1/access-request/{id}":    true,
	"POST /api/v1/access-request":        true,
	"DELETE /api/v1/access-request/{id}": true,

	// A member holds no scope but must still see and end their own
	// console sign-ins (sessionAudience).
	"GET /api/v1/auth/sessions":         true,
	"DELETE /api/v1/auth/sessions/{id}": true,

	// A browser SSH session mints a key for the caller's own user; the
	// SSH policy decides what that user may reach. The username hints the
	// terminal offers follow the same visibility rule.
	"POST /api/v1/ssh-session":                true,
	"GET /api/v1/node/{nodeId}/ssh-usernames": true,
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

// TestDebugNodeOperationIsGatedByConfig proves POST /api/v1/debug/node, which
// mints a node from key material the caller hands it, is registered only when
// the config asks for it: a production server neither serves it nor describes
// it. The spec generator builds the API with no config and keeps every
// operation. With the endpoint off the guards above still hold, so the
// operation is absent rather than unguarded.
func TestDebugNodeOperationIsGatedByConfig(t *testing.T) {
	t.Parallel()

	const debugNode = "/api/v1/debug/node"

	off := NewAPI(chi.NewMux(), Backend{Cfg: &types.Config{}})
	assert.Nil(t, off.OpenAPI().Paths[debugNode], "off unless the config asks for it")

	on := NewAPI(chi.NewMux(), Backend{
		Cfg: &types.Config{Debug: types.DebugConfig{NodeAPIEnabled: true}},
	})
	require.NotNil(t, on.OpenAPI().Paths[debugNode])
	require.NotNil(t, on.OpenAPI().Paths[debugNode].Post)
	assert.NotEmpty(t, on.OpenAPI().Paths[debugNode].Post.Security, "and still authenticated")

	spec := NewAPI(chi.NewMux(), Backend{})
	assert.NotNil(t, spec.OpenAPI().Paths[debugNode], "the spec describes every operation")

	for path, item := range off.OpenAPI().Paths {
		for method, op := range humaOperations(item) {
			if op == nil {
				continue
			}

			key := method + " " + path

			if len(op.Security) > 0 && !selfEnforcedOps[key] {
				_, ok := principal.RequiredScope(op)
				assert.True(t, ok, "operation %q is authenticated but declares no scope", key)
			}

			if method != "GET" && !unauditedOps[key] {
				_, ok := audit.Action(op)
				assert.True(t, ok, "operation %q writes but declares no audit action", key)
			}
		}
	}
}

// unauditedOps are the writing operations that change nothing: they
// validate input and answer.
var unauditedOps = map[string]bool{
	"POST /api/v1/policy/check":  true,
	"POST /api/v1/posture/check": true,
	// A credential check reaches the provider and stores nothing.
	"POST /api/v1/posture-integrations/check": true,
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
			if op == nil || !selfEnforcedOps[key] {
				continue
			}

			seen[key] = true

			assert.NotEmpty(t, op.Security,
				"self-enforcing operation %q must still require authentication", key)
		}
	}

	for key := range selfEnforcedOps {
		assert.True(t, seen[key], "selfEnforcedOps names %q, which no operation registers", key)
	}
}
