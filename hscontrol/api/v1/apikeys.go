package apiv1

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
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
	huma.Register(api, huma.Operation{
		OperationID: "createApiKey",
		Method:      http.MethodPost,
		Path:        "/api/v1/apikey",
		Summary:     "Create API key",
		Description: "Any authenticated caller may mint a key for itself; a key for another user, or " +
			"a legacy key without a user, needs the owner, an admin or the socket.",
		Tags:     []string{"ApiKeys"},
		Security: bearerAuth,
	}, func(ctx context.Context, in *createAPIKeyInput) (*createAPIKeyOutput, error) {
		// A missing expiration is a key that never expires. The gRPC handler
		// defaulted it to the zero time, which minted a key that was expired
		// before it was printed; the CLI always sends one, so nothing relied
		// on that.
		userID, err := apiKeyOwner(ctx, b, in.Body.UserID)
		if err != nil {
			return nil, err
		}

		keyStr, _, err := b.State.CreateAPIKeyForUser(in.Body.Expiration, userID)
		if err != nil {
			return nil, huma.Error500InternalServerError("creating api key", err)
		}

		out := &createAPIKeyOutput{}
		out.Body.APIKey = keyStr

		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "expireApiKey",
		Method:      http.MethodPost,
		Path:        "/api/v1/apikey/expire",
		Summary:     "Expire API key",
		Tags:        []string{"ApiKeys"},
		Security:    bearerAuth,
	}, func(ctx context.Context, in *expireAPIKeyInput) (*expireAPIKeyOutput, error) {
		key, err := lookupAPIKey(b, in.Body.ID, in.Body.Prefix)
		if err != nil {
			return nil, err
		}

		err = requireKeyAccess(ctx, key)
		if err != nil {
			return nil, err
		}

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

	huma.Register(api, huma.Operation{
		OperationID: "deleteApiKey",
		Method:      http.MethodDelete,
		Path:        "/api/v1/apikey/{prefix}",
		Summary:     "Delete API key",
		Tags:        []string{"ApiKeys"},
		Security:    bearerAuth,
	}, func(ctx context.Context, in *deleteAPIKeyInput) (*deleteAPIKeyOutput, error) {
		key, err := lookupAPIKey(b, in.ID, in.Prefix)
		if err != nil {
			return nil, err
		}

		err = requireKeyAccess(ctx, key)
		if err != nil {
			return nil, err
		}

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

	return out
}

// apiKeyOwner resolves the user a new key should belong to and checks the
// caller may mint it: an all-access caller (socket, legacy key, owner, admin)
// may mint for anyone or for nobody; every other caller only for itself.
func apiKeyOwner(ctx context.Context, b Backend, rawUserID string) (*types.UserID, error) {
	p := caller(ctx)

	if rawUserID == "" {
		if !p.Bounded {
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
