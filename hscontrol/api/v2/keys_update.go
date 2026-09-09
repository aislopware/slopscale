package apiv2

import (
	"context"
	"net/http"

	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/danielgtaylor/huma/v2"
)

func init() {
	registrations = append(registrations, registerKeyUpdate)
}

// UpdateKeyRequest is the PUT body, Tailscale's SetOAuthClient and
// SetFederatedIdentity in one. Like the POST it is multiplexed by keyType;
// an auth key cannot be updated, because its capabilities are fixed at
// creation.
type UpdateKeyRequest struct {
	KeyType     string   `doc:"Key kind: \"client\" (default) or \"federated\"." json:"keyType,omitempty"`
	Scopes      []string `json:"scopes,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Description string   `json:"description,omitempty"                           maxLength:"50"`

	// The trust conditions of a federated identity; see CreateKeyRequest.
	Audience         string            `json:"audience,omitempty"`
	Issuer           string            `json:"issuer,omitempty"`
	Subject          string            `json:"subject,omitempty"`
	CustomClaimRules map[string]string `json:"customClaimRules,omitempty"`
}

type setKeyByIDInput struct {
	Tailnet string `path:"tailnet"`
	KeyID   string `path:"keyId"`
	Body    UpdateKeyRequest
}

// registerKeyUpdate adds Tailscale's SetOAuthClient/SetFederatedIdentity.
// Like the other keys operations it declares no static scope: the kind,
// and so the scope, is known only once the body is parsed (see
// registerKeys).
func registerKeyUpdate(api huma.API, b Backend) {
	huma.Register(api, audit.Declare(huma.Operation{
		OperationID: "setKey",
		Method:      http.MethodPut,
		Path:        "/api/v2/tailnet/{tailnet}/keys/{keyId}",
		Summary:     "Update an OAuth client or federated identity",
		Description: "Replaces the scopes, tags, description and (for a federated identity) trust " +
			"conditions, keeping an OAuth client's secret. Requires the `oauth_keys` scope (an admin " +
			"API key is all-access).",
		Tags:     []string{"Keys", tagTailscaleCompat},
		Security: security,
		Errors: []int{
			http.StatusBadRequest,
			http.StatusUnauthorized,
			http.StatusForbidden,
			http.StatusNotFound,
		},
	}, "key.update", "key", "keyId"), func(ctx context.Context, in *setKeyByIDInput) (*keyOutput, error) {
		return handleSetKey(ctx, b, in)
	})
}

func handleSetKey(ctx context.Context, b Backend, in *setKeyByIDInput) (*keyOutput, error) {
	err := requireDefaultTailnet(in.Tailnet)
	if err != nil {
		return nil, err
	}

	if in.Body.KeyType == keyTypeFederated {
		return updateFederatedIdentity(ctx, b, in.KeyID, CreateKeyRequest{
			KeyType:          keyTypeFederated,
			Scopes:           in.Body.Scopes,
			Tags:             in.Body.Tags,
			Description:      in.Body.Description,
			Audience:         in.Body.Audience,
			Issuer:           in.Body.Issuer,
			Subject:          in.Body.Subject,
			CustomClaimRules: in.Body.CustomClaimRules,
		})
	}

	if in.Body.KeyType != "" && in.Body.KeyType != keyTypeClient {
		return nil, huma.Error400BadRequest(
			"only an OAuth client (keyType \"client\") or a federated identity " +
				"(keyType \"federated\") can be updated",
		)
	}

	err = requireKeyScope(ctx, scope.OAuthKeys)
	if err != nil {
		return nil, err
	}

	if len(in.Body.Scopes) == 0 {
		return nil, huma.Error400BadRequest("an OAuth client must declare at least one scope")
	}

	err = authorizeClientGrant(ctx, b, in.Body.Scopes, in.Body.Tags)
	if err != nil {
		return nil, err
	}

	audit.Detail(ctx, "keyType", keyTypeClient)
	audit.Detail(ctx, "scopes", emptyIfNil(in.Body.Scopes))
	audit.Detail(ctx, "tags", emptyIfNil(in.Body.Tags))

	client, err := b.State.UpdateOAuthClient(in.KeyID, in.Body.Scopes, in.Body.Tags, in.Body.Description)
	if err != nil {
		return nil, mapError("updating oauth client", err)
	}

	return &keyOutput{Body: oauthClientToKey(client, "")}, nil
}

// authorizeClientGrant enforces what an OAuth client or federated identity
// may be given: tags where the scopes demand them, no scope beyond the
// creating credential's own grant (otherwise an oauth_keys credential
// could mint an all-access client and escalate), and, for an OAuth token,
// only tags within its own grant and defined in policy. An API key keeps
// the historical syntax-only tag validation.
func authorizeClientGrant(ctx context.Context, b Backend, scopes, tags []string) error {
	// Tailscale: tags are mandatory when the scopes include devices:core or
	// auth_keys, because such a client mints tagged, tailnet-owned credentials.
	if scope.RequiresTags(scope.Parse(scopes)) && len(tags) == 0 {
		return huma.Error400BadRequest("tags are required when scopes include devices:core or auth_keys")
	}

	p := caller(ctx)

	for _, s := range scopes {
		if !p.Allows(scope.Scope(s)) {
			return huma.Error403Forbidden(
				"client may not be granted scope " + s + " beyond the creating credential",
			)
		}
	}

	tokenTags, isOAuth := principalTags(ctx)
	if !isOAuth {
		return nil
	}

	for _, tag := range tags {
		if !b.State.TagExists(tag) {
			return huma.Error400BadRequest("tag " + tag + " is not defined in policy")
		}

		if !b.State.TagOwnedByTags(tag, tokenTags) {
			return huma.Error403Forbidden(
				"client may not be granted tag " + tag + " beyond the creating token",
			)
		}
	}

	return nil
}
