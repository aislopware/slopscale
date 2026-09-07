package apiv1

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/audit"
	"github.com/juanfont/headscale/hscontrol/scope"
	"github.com/juanfont/headscale/hscontrol/types"
)

func init() {
	registrations = append(registrations, registerAPIKeys)
}

// ApiKey is the v1 ApiKey message. Timestamps are pointers so a nil source is
// emitted as JSON null, matching protojson's unset Timestamp (e.g. lastSeen on
// a fresh key).
//
//nolint:staticcheck,revive // ST1003: name is the OpenAPI schema name
type ApiKey struct {
	ID         string     `format:"uint64"   json:"id"`
	Prefix     string     `json:"prefix"`
	Expiration *time.Time `json:"expiration" nullable:"true"`
	CreatedAt  *time.Time `json:"createdAt"  nullable:"true"`
	LastSeen   *time.Time `json:"lastSeen"   nullable:"true"`
	// UserID is the owning user, whose role bounds the key; null for a legacy
	// all-access key.
	UserID *string `doc:"Owning user id; null for a legacy key." format:"uint64" json:"userId" nullable:"true"`
	// Scopes narrow the key below its owner's role; empty is the whole role.
	Scopes      []string `doc:"Scopes the key is limited to; empty means its owner's whole role." json:"scopes" nullable:"false"` //nolint:lll // struct tag
	Description string   `json:"description"`
}

// CreateApiKeyRequestBody is the v1.CreateApiKeyRequest body.
//
//nolint:staticcheck,revive // ST1003: name is the OpenAPI schema name
type CreateApiKeyRequestBody struct {
	Expiration *time.Time `json:"expiration,omitempty"`
	// UserID makes the key belong to a user, bounding it by the user's role.
	// A caller that is itself a user may only mint keys for that user unless
	// it is the owner or an admin; only those, or the socket, may mint a key
	// without a user.
	UserID string `doc:"Owning user id; empty for a legacy all-access key." format:"uint64" json:"userId,omitempty"`
	// Scopes limit the key to some operations, within what the caller and
	// the owner's role may do; a scope the caller cannot delegate is
	// dropped. Empty keeps the owner's whole role.
	Scopes      []string `doc:"Scopes to limit the key to; empty keeps the owner's whole role." json:"scopes,omitempty"`      //nolint:lll // struct tag
	Description string   `doc:"What the key is for."                                            json:"description,omitempty"` //nolint:lll // struct tag
}

// ExpireApiKeyRequestBody is the v1.ExpireApiKeyRequest body.
//
//nolint:staticcheck,revive // ST1003: name is the OpenAPI schema name
type ExpireApiKeyRequestBody struct {
	Prefix string `json:"prefix,omitempty"`
	ID     string `format:"uint64"         json:"id,omitempty"`
}

type (
	createAPIKeyInput struct {
		Body CreateApiKeyRequestBody
	}
	createAPIKeyOutput struct {
		Body struct {
			APIKey string `json:"apiKey"`
		}
	}
)

type (
	expireAPIKeyInput struct {
		Body ExpireApiKeyRequestBody
	}
	expireAPIKeyOutput struct {
		Body struct{}
	}
)

type (
	listAPIKeysOutput struct {
		Body struct {
			APIKeys []ApiKey `json:"apiKeys" nullable:"false"`
		}
	}
)

type (
	deleteAPIKeyInput struct {
		Prefix string `path:"prefix"`
		ID     string `format:"uint64" query:"id"`
	}
	deleteAPIKeyOutput struct {
		Body struct{}
	}
)

func registerAPIKeys(api huma.API, b Backend) {
	huma.Register(api, audited(huma.Operation{
		OperationID: "createApiKey",
		Method:      http.MethodPost,
		Path:        "/api/v1/apikey",
		Summary:     "Create API key",
		Description: "Any authenticated caller may mint a key for itself; a key for another user, or " +
			"a legacy key without a user, needs the owner, an admin or the socket.",
		Tags:     []string{"ApiKeys"},
		Security: bearerAuth,
	}, "apikey.create", "apikey", ""), func(ctx context.Context, in *createAPIKeyInput) (*createAPIKeyOutput, error) {
		return createAPIKey(ctx, b, in)
	})

	huma.Register(api, audited(huma.Operation{
		OperationID: "expireApiKey",
		Method:      http.MethodPost,
		Path:        "/api/v1/apikey/expire",
		Summary:     "Expire API key",
		Tags:        []string{"ApiKeys"},
		Security:    bearerAuth,
	}, "apikey.expire", "apikey", ""), func(ctx context.Context, in *expireAPIKeyInput) (*expireAPIKeyOutput, error) {
		key, err := lookupAPIKey(b, in.Body.ID, in.Body.Prefix)
		if err != nil {
			return nil, err
		}

		err = requireKeyAccess(ctx, key)
		if err != nil {
			return nil, err
		}

		audit.Target(ctx, "", key.Prefix, "")

		err = b.State.ExpireAPIKey(key)
		if err != nil {
			return nil, huma.Error500InternalServerError("expiring api key", err)
		}

		return &expireAPIKeyOutput{}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listApiKeys",
		Method:      http.MethodGet,
		Path:        "/api/v1/apikey",
		Summary:     "List API keys",
		Tags:        []string{"ApiKeys"},
		Security:    bearerAuth,
	}, func(ctx context.Context, _ *struct{}) (*listAPIKeysOutput, error) {
		keys, err := b.State.ListAPIKeys()
		if err != nil {
			return nil, huma.Error500InternalServerError("listing api keys", err)
		}

		// A caller without admin authority sees only its own keys.
		if p := caller(ctx); p.Bounded {
			keys = slices.DeleteFunc(keys, func(k types.APIKey) bool {
				return k.UserID == nil || types.UserID(*k.UserID) != p.UserID
			})
		}

		// Match the gRPC handler's ascending-ID ordering.
		slices.SortFunc(keys, func(a, b types.APIKey) int {
			return cmp.Compare(a.ID, b.ID)
		})

		out := &listAPIKeysOutput{}

		out.Body.APIKeys = make([]ApiKey, len(keys))
		for i := range keys {
			out.Body.APIKeys[i] = apiKeyFromState(&keys[i])
		}

		return out, nil
	})

	huma.Register(api, audited(huma.Operation{
		OperationID: "deleteApiKey",
		Method:      http.MethodDelete,
		Path:        "/api/v1/apikey/{prefix}",
		Summary:     "Delete API key",
		Tags:        []string{"ApiKeys"},
		Security:    bearerAuth,
	}, "apikey.delete", "apikey", "prefix"), func(
		ctx context.Context, in *deleteAPIKeyInput,
	) (*deleteAPIKeyOutput, error) {
		key, err := lookupAPIKey(b, in.ID, in.Prefix)
		if err != nil {
			return nil, err
		}

		err = requireKeyAccess(ctx, key)
		if err != nil {
			return nil, err
		}

		// The path may carry a placeholder prefix when the key is addressed
		// by id; name the key that was actually deleted.
		audit.Target(ctx, "", key.Prefix, "")

		err = b.State.DestroyAPIKey(*key)
		if err != nil {
			return nil, huma.Error500InternalServerError("deleting api key", err)
		}

		return &deleteAPIKeyOutput{}, nil
	})
}

// lookupAPIKey resolves an API key by id or prefix; exactly one must be
// supplied. An empty or zero id counts as "no id". Unknown id/prefix maps to
// 404 via mapError.
func lookupAPIKey(b Backend, idStr, prefix string) (*types.APIKey, error) {
	id, err := parseAPIKeyID(idStr)
	if err != nil {
		return nil, err
	}

	hasID := id != 0
	hasPrefix := prefix != ""

	switch {
	case hasID && hasPrefix:
		return nil, huma.Error400BadRequest("provide either id or prefix, not both")
	case hasID:
		key, err := b.State.GetAPIKeyByID(id)
		if err != nil {
			return nil, mapError("getting api key", err)
		}

		return key, nil
	case hasPrefix:
		key, err := b.State.GetAPIKey(prefix)
		if err != nil {
			return nil, mapError("getting api key", err)
		}

		return key, nil
	default:
		return nil, huma.Error400BadRequest("must provide id or prefix")
	}
}

// parseAPIKeyID decodes the optional uint64 id. Empty maps to zero; non-numeric
// is rejected with 400.
func parseAPIKeyID(s string) (uint64, error) {
	if s == "" {
		return 0, nil
	}

	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, huma.Error400BadRequest("invalid api key id", err)
	}

	return id, nil
}

// apiKeyFromState converts a domain API key into the v1 response shape, masking
// the prefix so the secret is never returned.
func apiKeyFromState(k *types.APIKey) ApiKey {
	out := ApiKey{
		ID:         formatID(k.ID),
		Prefix:     apiKeyMaskedPrefix(k.Prefix),
		Expiration: k.Expiration,
		CreatedAt:  k.CreatedAt,
		LastSeen:   k.LastSeen,
	}

	if k.UserID != nil {
		uid := formatID(uint64(*k.UserID))
		out.UserID = &uid
	}

	out.Scopes = emptyIfNil(k.Scopes)
	out.Description = k.Description

	return out
}

// apiKeyScopes validates the scopes a new key asks for and narrows them
// to what the caller may delegate, so a key never outgrows its minter. An
// unknown scope is a client error rather than a silent drop. A caller
// whose credential carries its own scopes and names none inherits them:
// an empty list on a stored key means the owner's whole role (or, without
// an owner, everything), which such a caller may not hand out.
func apiKeyScopes(ctx context.Context, requested []string) ([]string, error) {
	p := caller(ctx)

	if len(requested) == 0 {
		if !p.Scoped {
			return nil, nil
		}

		if len(p.Scopes) == 0 {
			return nil, huma.Error403Forbidden("this credential has no scopes to delegate")
		}

		return scopeStrings(p.Scopes), nil
	}

	known := scope.Known()
	wanted := make([]scope.Scope, 0, len(requested))

	for _, raw := range requested {
		s := scope.Scope(strings.TrimSpace(raw))
		if !slices.Contains(known, s) {
			return nil, huma.Error400BadRequest("unknown scope " + strconv.Quote(string(s)))
		}

		if !slices.Contains(wanted, s) {
			wanted = append(wanted, s)
		}
	}

	narrowed := p.Narrow(wanted)
	if len(narrowed) == 0 {
		return nil, huma.Error403Forbidden("none of the requested scopes may be delegated by this caller")
	}

	return scopeStrings(narrowed), nil
}

// scopeStrings renders scopes for storage.
func scopeStrings(scopes []scope.Scope) []string {
	out := make([]string, 0, len(scopes))
	for _, s := range scopes {
		out = append(out, string(s))
	}

	return out
}

// apiKeyOwner resolves the user a new key should belong to and checks the
// caller may mint it: an all-access caller (socket, legacy key, owner, admin)
// may mint for anyone or for nobody; every other caller only for itself.
func apiKeyOwner(ctx context.Context, b Backend, rawUserID string) (*types.UserID, error) {
	p := caller(ctx)

	if rawUserID == "" {
		// A scoped key without an owner is bounded by its scopes alone;
		// what it mints has no owner either and is narrowed below.
		if !p.Bounded || !p.HasUser() {
			return nil, nil //nolint:nilnil // no owner is a legacy all-access key
		}

		uid := p.UserID

		return &uid, nil
	}

	id, err := parseUserID(rawUserID)
	if err != nil {
		return nil, err
	}

	if p.Bounded && id != p.UserID {
		return nil, huma.Error403Forbidden("only the owner or an admin may create keys for other users")
	}

	_, err = b.State.GetUserByID(id)
	if err != nil {
		return nil, mapError("looking up key owner", err)
	}

	return &id, nil
}

// requireKeyAccess lets a bounded caller manage only its own keys. Not
// found, rather than forbidden, so the key's existence is not revealed.
func requireKeyAccess(ctx context.Context, key *types.APIKey) error {
	p := caller(ctx)
	if !p.Bounded {
		return nil
	}

	if key.UserID == nil || types.UserID(*key.UserID) != p.UserID {
		return huma.Error404NotFound("api key not found")
	}

	return nil
}

// apiKeyMaskedPrefix reproduces the unexported types.APIKey.maskedPrefix.
func apiKeyMaskedPrefix(prefix string) string {
	if len(prefix) == types.NewAPIKeyPrefixLength {
		return "hskey-api-" + prefix + "-***"
	}

	return prefix + "***"
}

// createAPIKey mints a key within the caller's authority: the owner it
// may mint for and the scopes it may delegate.
func createAPIKey(ctx context.Context, b Backend, in *createAPIKeyInput) (*createAPIKeyOutput, error) {
	// An API key carries scopes but not tags, so a token bounded to some
	// tags could launder its way to auth keys for any tag through one.
	// Tokens mint tokens (through their client), not API keys.
	if caller(ctx).IsOAuth() {
		return nil, huma.Error403Forbidden("an OAuth access token cannot mint API keys")
	}

	// A missing expiration is a key that never expires. The gRPC handler
	// defaulted it to the zero time, which minted a key that was expired
	// before it was printed; the CLI always sends one, so nothing relied
	// on that.
	userID, err := apiKeyOwner(ctx, b, in.Body.UserID)
	if err != nil {
		return nil, err
	}

	scopes, err := apiKeyScopes(ctx, in.Body.Scopes)
	if err != nil {
		return nil, err
	}

	keyStr, apiKey, err := b.State.CreateScopedAPIKey(
		in.Body.Expiration, userID, scopes, strings.TrimSpace(in.Body.Description),
	)
	if err != nil {
		return nil, huma.Error500InternalServerError("creating api key", err)
	}

	// The prefix names the key; the key itself is a secret.
	audit.Target(ctx, "", apiKey.Prefix, "")

	if userID != nil {
		audit.Detail(ctx, "userId", formatID(uint64(*userID)))
	}

	if len(scopes) > 0 {
		audit.Detail(ctx, "scopes", scopes)
	}

	if in.Body.Expiration != nil {
		audit.Detail(ctx, "expiration", in.Body.Expiration.Format(time.RFC3339))
	}

	out := &createAPIKeyOutput{}
	out.Body.APIKey = keyStr

	return out, nil
}
