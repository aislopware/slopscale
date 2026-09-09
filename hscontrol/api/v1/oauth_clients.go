package apiv1

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/egress"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/danielgtaylor/huma/v2"
)

func init() {
	registrations = append(registrations, registerOAuthClients)
}

// OAuthClient is a client-credentials client for the v2 API, or a federated
// identity, which is the same principal without a secret: it mints tokens by
// presenting a JWT its own issuer signed rather than one Slopscale handed out
// (see the v2 token exchange). A client's secret is returned once, by the
// create call, and never stored in the clear.
type OAuthClient struct {
	ClientID string `json:"clientId"`
	// KeyType tells the two kinds apart. A row written before federated
	// identities existed carries none and reads as a client.
	KeyType     string   `doc:"client or federated."                    enum:"client,federated" json:"keyType"`
	Description string   `json:"description"`
	Scopes      []string `doc:"Scopes the client may grant its tokens." json:"scopes"           nullable:"false"`
	Tags        []string `doc:"Tags the client may put on its tokens."  json:"tags"             nullable:"false"`
	// Issuer, Audience and Subject are the trust conditions a presented JWT
	// is held to; they are empty on a client.
	Issuer   string `doc:"Federated: the OIDC issuer that signs the presented JWT." json:"issuer"`
	Audience string `doc:"Federated: the audience the JWT must carry."              json:"audience"`
	Subject  string `doc:"Federated: the subject the JWT must equal."               json:"subject"`
	// CustomClaimRules are further claims the JWT must carry, claim name to
	// the value it must have; empty on a client.
	CustomClaimRules map[string]string `doc:"Federated: further claims the JWT must carry." json:"customClaimRules" nullable:"false"` //nolint:lll // struct tag
	// UserID records who created the client; null when the socket did.
	UserID    *string    `doc:"Creating user id; null for the socket." format:"uint64" json:"userId" nullable:"true"`
	CreatedAt *time.Time `json:"createdAt"                             nullable:"true"`
}

// CreateOAuthClientRequestBody is what a new client or federated identity is
// made from.
type CreateOAuthClientRequestBody struct {
	KeyType     string `doc:"client (the default) or federated." enum:",client,federated"     json:"keyType,omitempty"`
	Description string `doc:"What the client is for."            json:"description,omitempty"`
	// Scopes bound every token the client mints; each must be within the
	// caller's own grant, so a limited credential cannot mint a wider client.
	Scopes []string `doc:"Scopes the client may grant; at least one." json:"scopes" minItems:"1" nullable:"false"`
	// Tags are required with devices:core or auth_keys, because such a client
	// mints tagged, tailnet-owned credentials.
	Tags []string `doc:"Tags the client may put on its tokens." json:"tags,omitempty"`
	// The trust conditions of a federated identity; all three are required
	// for one, because an identity missing one would accept any issuer, any
	// workload or any audience, which is the whole guard.
	Issuer           string            `doc:"Federated: the https URL of the OIDC issuer."  json:"issuer,omitempty"`
	Audience         string            `doc:"Federated: the audience the JWT must carry."   json:"audience,omitempty"`
	Subject          string            `doc:"Federated: the subject the JWT must equal."    json:"subject,omitempty"`
	CustomClaimRules map[string]string `doc:"Federated: further claims the JWT must carry." json:"customClaimRules,omitempty"` //nolint:lll // struct tag
}

// UpdateOAuthClientRequestBody changes a client or a federated identity in
// place; an absent field keeps the value it has. The trust conditions belong
// to a federated identity, so sending one for a client is refused rather than
// quietly dropped.
type UpdateOAuthClientRequestBody struct {
	Description *string   `json:"description,omitempty"`
	Scopes      *[]string `doc:"Replaces the scopes; at least one." json:"scopes,omitempty"`
	Tags        *[]string `doc:"Replaces the tags; [] clears them." json:"tags,omitempty"`

	Issuer           *string            `doc:"Federated: the https URL of the OIDC issuer."   json:"issuer,omitempty"`
	Audience         *string            `doc:"Federated: the audience the JWT must carry."    json:"audience,omitempty"`
	Subject          *string            `doc:"Federated: the subject the JWT must equal."     json:"subject,omitempty"`
	CustomClaimRules *map[string]string `doc:"Federated: replaces the rules; {} clears them." json:"customClaimRules,omitempty"` //nolint:lll // struct tag
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
			// ClientSecret is shown here and nowhere else. A federated
			// identity has none, so the field is absent for one.
			ClientSecret string `json:"clientSecret,omitempty"`
		}
	}
	updateOAuthClientInput struct {
		ClientID string `path:"clientId"`
		Body     UpdateOAuthClientRequestBody
	}
	updateOAuthClientOutput struct {
		Body struct {
			OAuthClient OAuthClient `json:"oauthClient"`
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
		Description: "Every client and federated identity that can mint v2 API tokens; revoked ones are gone.",
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
		Description: "The client secret is in this response only, and a federated identity has none. " +
			"Scopes may not exceed the caller's own.",
		Tags:     []string{"OAuthClients"},
		Security: bearerAuth,
	}, scope.OAuthKeys), "oauthclient.create", "", ""), func(
		ctx context.Context, in *createOAuthClientInput,
	) (*createOAuthClientOutput, error) {
		return createOAuthClient(ctx, b, in)
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "updateOAuthClient",
		Method:      http.MethodPatch,
		Path:        "/api/v1/oauth-client/{clientId}",
		Summary:     "Update OAuth client",
		Description: "Changes the description, scopes, tags and, for a federated identity, its trust " +
			"conditions. The secret is untouched, so the client keeps working across an update.",
		Tags:     []string{"OAuthClients"},
		Security: bearerAuth,
	}, scope.OAuthKeys), "oauthclient.update", "oauthclient", "clientId"), func(
		ctx context.Context, in *updateOAuthClientInput,
	) (*updateOAuthClientOutput, error) {
		return updateOAuthClient(ctx, b, in)
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

	err := authorizeClientGrant(ctx, b, body.Scopes, body.Tags)
	if err != nil {
		return nil, err
	}

	var creator *uint

	if p := caller(ctx); p.HasUser() {
		u := uint(p.UserID)
		creator = &u
	}

	audit.Detail(ctx, "keyType", oauthKeyType(body.KeyType))
	audit.Detail(ctx, "scopes", emptyIfNil(body.Scopes))
	audit.Detail(ctx, "tags", emptyIfNil(body.Tags))

	out := &createOAuthClientOutput{}

	if body.KeyType == types.OAuthKeyTypeFederated {
		spec, specErr := federatedSpec(body.Issuer, body.Audience, body.Subject, body.CustomClaimRules)
		if specErr != nil {
			return nil, specErr
		}

		spec.Scopes, spec.Tags = body.Scopes, body.Tags
		spec.Description, spec.CreatorUserID = body.Description, creator

		audit.Detail(ctx, "issuer", spec.Issuer)
		audit.Detail(ctx, "subject", spec.Subject)

		identity, createErr := b.State.CreateFederatedIdentity(spec)
		if createErr != nil {
			return nil, mapError("creating federated identity", createErr)
		}

		audit.Target(ctx, "oauthclient", identity.ClientID, body.Description)

		out.Body.OAuthClient = oauthClientFromStored(identity)

		return out, nil
	}

	secret, client, err := b.State.CreateOAuthClient(body.Scopes, body.Tags, body.Description, creator)
	if err != nil {
		return nil, mapError("creating oauth client", err)
	}

	// The secret is never audited; the client id names the object.
	audit.Target(ctx, "oauthclient", client.ClientID, body.Description)

	out.Body.OAuthClient = oauthClientFromStored(client)
	out.Body.ClientSecret = secret

	return out, nil
}

func updateOAuthClient(
	ctx context.Context,
	b Backend,
	in *updateOAuthClientInput,
) (*updateOAuthClientOutput, error) {
	existing, err := b.State.GetOAuthClientByClientID(in.ClientID)
	if err != nil {
		return nil, mapError("getting oauth client", err)
	}

	body := in.Body
	if !existing.IsFederated() && (body.Issuer != nil || body.Audience != nil ||
		body.Subject != nil || body.CustomClaimRules != nil) {
		return nil, huma.Error400BadRequest(
			"issuer, audience, subject and customClaimRules belong to a federated identity, " +
				"not to an OAuth client",
		)
	}

	scopes, tags, description := existing.Scopes, existing.Tags, existing.Description

	if body.Scopes != nil {
		scopes = *body.Scopes
	}

	if body.Tags != nil {
		tags = *body.Tags
	}

	if body.Description != nil {
		description = *body.Description
	}

	if len(scopes) == 0 {
		return nil, huma.Error400BadRequest("an OAuth client must keep at least one scope")
	}

	err = authorizeClientGrant(ctx, b, scopes, tags)
	if err != nil {
		return nil, err
	}

	audit.Detail(ctx, "keyType", existing.Kind())
	audit.Detail(ctx, "scopes", emptyIfNil(scopes))
	audit.Detail(ctx, "tags", emptyIfNil(tags))

	out := &updateOAuthClientOutput{}

	if existing.IsFederated() {
		identity, updateErr := updateFederatedIdentity(ctx, b, existing, body, scopes, tags, description)
		if updateErr != nil {
			return nil, updateErr
		}

		out.Body.OAuthClient = oauthClientFromStored(identity)

		return out, nil
	}

	client, err := b.State.UpdateOAuthClient(in.ClientID, scopes, tags, description)
	if err != nil {
		return nil, mapError("updating oauth client", err)
	}

	out.Body.OAuthClient = oauthClientFromStored(client)

	return out, nil
}

// updateFederatedIdentity replaces the identity with its merged form: the
// state layer takes a whole spec, as Tailscale's SetFederatedIdentity does,
// so an absent field is filled from the stored row before the write.
func updateFederatedIdentity(
	ctx context.Context,
	b Backend,
	existing *types.OAuthClient,
	body UpdateOAuthClientRequestBody,
	scopes, tags []string,
	description string,
) (*types.OAuthClient, error) {
	issuer, audience, subject := existing.Issuer, existing.Audience, existing.Subject
	rules := existing.CustomClaimRules

	if body.Issuer != nil {
		issuer = *body.Issuer
	}

	if body.Audience != nil {
		audience = *body.Audience
	}

	if body.Subject != nil {
		subject = *body.Subject
	}

	if body.CustomClaimRules != nil {
		rules = *body.CustomClaimRules
	}

	spec, err := federatedSpec(issuer, audience, subject, rules)
	if err != nil {
		return nil, err
	}

	spec.Scopes, spec.Tags, spec.Description = scopes, tags, description

	audit.Detail(ctx, "issuer", spec.Issuer)
	audit.Detail(ctx, "subject", spec.Subject)

	identity, err := b.State.UpdateFederatedIdentity(existing.ClientID, spec)
	if err != nil {
		return nil, mapError("updating federated identity", err)
	}

	return identity, nil
}

// authorizeClientGrant enforces what a client or a federated identity may be
// given, as the v2 keys resource does: tags where the scopes demand them, no
// scope beyond the creating credential's own grant (otherwise an oauth_keys
// credential could mint an all-access client and escalate), and, for an OAuth
// token, only tags within its own grant and defined in policy. An API key
// keeps the historical syntax-only tag validation.
func authorizeClientGrant(ctx context.Context, b Backend, scopes, tags []string) error {
	if scope.RequiresTags(scope.Parse(scopes)) && len(tags) == 0 {
		return huma.Error400BadRequest("tags are required when scopes include devices:core or auth_keys")
	}

	err := validateTags(tags)
	if err != nil {
		return err
	}

	p := caller(ctx)

	for _, s := range scopes {
		if !p.Allows(scope.Scope(s)) {
			return huma.Error403Forbidden(
				"client may not be granted scope " + s + " beyond the creating credential",
			)
		}
	}

	if !p.IsOAuth() {
		return nil
	}

	for _, tag := range tags {
		if !b.State.TagExists(tag) {
			return huma.Error400BadRequest("tag " + tag + " is not defined in policy")
		}

		if !b.State.TagOwnedByTags(tag, p.Tags) {
			return huma.Error403Forbidden(
				"client may not be granted tag " + tag + " beyond the creating token",
			)
		}
	}

	return nil
}

// federatedSpec validates the trust conditions of a federated identity. All
// of them are required: one missing would let the identity accept any issuer,
// any workload or any audience.
func federatedSpec(
	issuer, audience, subject string,
	rules map[string]string,
) (types.FederatedIdentitySpec, error) {
	checked, err := federatedIssuer(issuer)
	if err != nil {
		return types.FederatedIdentitySpec{}, err
	}

	spec := types.FederatedIdentitySpec{
		Issuer:           checked,
		Audience:         strings.TrimSpace(audience),
		Subject:          strings.TrimSpace(subject),
		CustomClaimRules: rules,
	}

	if spec.Audience == "" || spec.Subject == "" {
		return types.FederatedIdentitySpec{}, huma.Error400BadRequest(
			"a federated identity needs an audience and a subject; without them any token " +
				"the issuer signs would be accepted",
		)
	}

	for name := range spec.CustomClaimRules {
		if strings.TrimSpace(name) == "" {
			return types.FederatedIdentitySpec{},
				huma.Error400BadRequest("a custom claim rule needs a claim name")
		}
	}

	return spec, nil
}

// federatedIssuer checks the issuer is an origin the server may reach. The
// discovery document and JWKS are fetched from it, so an unchecked issuer
// would turn the token endpoint into a request forgery against the server's
// own network (see hscontrol/egress).
func federatedIssuer(raw string) (string, error) {
	issuer, host, err := types.ParseFederatedIssuer(raw)
	if err != nil {
		return "", huma.Error400BadRequest(err.Error())
	}

	err = egress.Default().CheckHost(host)
	if err != nil {
		return "", huma.Error400BadRequest("issuer points at a " + err.Error())
	}

	return issuer, nil
}

// oauthKeyType fills in the default for a body that omits keyType.
func oauthKeyType(keyType string) string {
	if keyType == types.OAuthKeyTypeFederated {
		return types.OAuthKeyTypeFederated
	}

	return types.OAuthKeyTypeClient
}

func oauthClientFromStored(client *types.OAuthClient) OAuthClient {
	out := OAuthClient{
		ClientID:         client.ClientID,
		KeyType:          client.Kind(),
		Description:      client.Description,
		Scopes:           emptyIfNil(client.Scopes),
		Tags:             emptyIfNil(client.Tags),
		Issuer:           client.Issuer,
		Audience:         client.Audience,
		Subject:          client.Subject,
		CustomClaimRules: client.CustomClaimRules,
		CreatedAt:        client.CreatedAt,
	}

	if out.CustomClaimRules == nil {
		out.CustomClaimRules = map[string]string{}
	}

	if client.UserID != nil {
		id := strconv.FormatUint(uint64(*client.UserID), util.Base10)
		out.UserID = &id
	}

	return out
}
