package apiv1

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/audit"
	"github.com/juanfont/headscale/hscontrol/scope"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/util"
)

func init() {
	registrations = append(registrations, registerOAuthClients)
}

// OAuthClient is a client-credentials client for the v2 API. The secret is
// returned once, by the create call, and never stored in the clear.
type OAuthClient struct {
	ClientID    string   `json:"clientId"`
	Description string   `json:"description"`
	Scopes      []string `doc:"Scopes the client may grant its tokens." json:"scopes" nullable:"false"`
	Tags        []string `doc:"Tags the client may put on its tokens."  json:"tags"   nullable:"false"`
	// UserID records who created the client; null when the socket did.
	UserID    *string    `doc:"Creating user id; null for the socket." format:"uint64" json:"userId" nullable:"true"`
	CreatedAt *time.Time `json:"createdAt"                             nullable:"true"`
}

// CreateOAuthClientRequestBody is what a new client is made from.
type CreateOAuthClientRequestBody struct {
	Description string `doc:"What the client is for." json:"description,omitempty"`
	// Scopes bound every token the client mints; each must be within the
	// caller's own grant, so a limited credential cannot mint a wider client.
	Scopes []string `doc:"Scopes the client may grant; at least one." json:"scopes" minItems:"1"`
	// Tags are required with devices:core or auth_keys, because such a client
	// mints tagged, tailnet-owned credentials.
	Tags []string `doc:"Tags the client may put on its tokens." json:"tags,omitempty"`
}

type (
	listOAuthClientsOutput struct {
		Body struct {
			OAuthClients []OAuthClient `json:"oauthClients" nullable:"false"`
		}
	}
	createOAuthClientInput struct {
		Body CreateOAuthClientRequestBody
	}
	createOAuthClientOutput struct {
		Body struct {
			OAuthClient OAuthClient `json:"oauthClient"`
			// ClientSecret is shown here and nowhere else.
			ClientSecret string `json:"clientSecret"`
		}
	}
	revokeOAuthClientInput struct {
		ClientID string `path:"clientId"`
	}
	revokeOAuthClientOutput struct {
		Body struct{}
	}
)

func registerOAuthClients(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "listOAuthClients",
		Method:      http.MethodGet,
		Path:        "/api/v1/oauth-client",
		Summary:     "List OAuth clients",
		Description: "Every client that can mint v2 API tokens; revoked clients are gone.",
		Tags:        []string{"OAuthClients"},
		Security:    bearerAuth,
	}, scope.OAuthKeysRead), func(_ context.Context, _ *struct{}) (*listOAuthClientsOutput, error) {
		clients, err := b.State.ListOAuthClients()
		if err != nil {
			return nil, huma.Error500InternalServerError("listing oauth clients", err)
		}

		out := &listOAuthClientsOutput{}
		out.Body.OAuthClients = make([]OAuthClient, 0, len(clients))

		for i := range clients {
			if clients[i].Revoked == nil {
				out.Body.OAuthClients = append(out.Body.OAuthClients, oauthClientFromStored(&clients[i]))
			}
		}

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "createOAuthClient",
		Method:      http.MethodPost,
		Path:        "/api/v1/oauth-client",
		Summary:     "Create OAuth client",
		Description: "The client secret is in this response only. Scopes may not exceed the caller's own.",
		Tags:        []string{"OAuthClients"},
		Security:    bearerAuth,
	}, scope.OAuthKeys), "oauthclient.create", "", ""), func(
		ctx context.Context, in *createOAuthClientInput,
	) (*createOAuthClientOutput, error) {
		return createOAuthClient(ctx, b, in)
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "revokeOAuthClient",
		Method:      http.MethodDelete,
		Path:        "/api/v1/oauth-client/{clientId}",
		Summary:     "Revoke OAuth client",
		Description: "Deletes the client and every access token it issued.",
		Tags:        []string{"OAuthClients"},
		Security:    bearerAuth,
	}, scope.OAuthKeys), "oauthclient.revoke", "oauthclient", "clientId"), func(
		_ context.Context, in *revokeOAuthClientInput,
	) (*revokeOAuthClientOutput, error) {
		err := b.State.RevokeOAuthClient(in.ClientID)
		if err != nil {
			return nil, mapError("revoking oauth client", err)
		}

		return &revokeOAuthClientOutput{}, nil
	})
}

func createOAuthClient(
	ctx context.Context,
	b Backend,
	in *createOAuthClientInput,
) (*createOAuthClientOutput, error) {
	body := in.Body

	if scope.RequiresTags(scope.Parse(body.Scopes)) && len(body.Tags) == 0 {
		return nil, huma.Error400BadRequest("tags are required when scopes include devices:core or auth_keys")
	}

	err := validateTags(body.Tags)
	if err != nil {
		return nil, err
	}

	p := caller(ctx)

	for _, s := range body.Scopes {
		if !p.Allows(scope.Scope(s)) {
			return nil, huma.Error403Forbidden(
				"client may not be granted scope " + s + " beyond the creating credential",
			)
		}
	}

	var creator *uint

	if p.HasUser() {
		u := uint(p.UserID)
		creator = &u
	}

	audit.Detail(ctx, "scopes", emptyIfNil(body.Scopes))
	audit.Detail(ctx, "tags", emptyIfNil(body.Tags))

	secret, client, err := b.State.CreateOAuthClient(body.Scopes, body.Tags, body.Description, creator)
	if err != nil {
		return nil, mapError("creating oauth client", err)
	}

	// The secret is never audited; the client id names the object.
	audit.Target(ctx, "oauthclient", client.ClientID, body.Description)

	out := &createOAuthClientOutput{}
	out.Body.OAuthClient = oauthClientFromStored(client)
	out.Body.ClientSecret = secret

	return out, nil
}

func oauthClientFromStored(client *types.OAuthClient) OAuthClient {
	out := OAuthClient{
		ClientID:    client.ClientID,
		Description: client.Description,
		Scopes:      emptyIfNil(client.Scopes),
		Tags:        emptyIfNil(client.Tags),
		CreatedAt:   client.CreatedAt,
	}

	if client.UserID != nil {
		id := strconv.FormatUint(uint64(*client.UserID), util.Base10)
		out.UserID = &id
	}

	return out
}
