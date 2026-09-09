package apiv2

import (
	"context"
	"strings"

	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/egress"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
)

// A federated identity (Tailscale's keyType "federated") is workload
// identity federation: a CI job or a cloud workload presents the OIDC JWT
// its own platform signs and gets a slopscale access token back, so
// nothing has to hold a slopscale secret. It is created, read and deleted
// through the keys resource like an OAuth client, and used at
// POST /api/v2/oauth/token-exchange (see token_exchange.go).

// createFederatedIdentity creates a federated identity. It is gated on the
// same oauth_keys scope and the same escalation rules as an OAuth client:
// an identity is a credential, and a JWT anybody's CI can mint must not
// buy more authority than its creator holds.
func createFederatedIdentity(ctx context.Context, b Backend, body CreateKeyRequest) (*keyOutput, error) {
	err := requireKeyScope(ctx, scope.OAuthKeys)
	if err != nil {
		return nil, err
	}

	spec, err := federatedSpec(ctx, b, body)
	if err != nil {
		return nil, err
	}

	audit.Detail(ctx, "keyType", keyTypeFederated)
	audit.Detail(ctx, "scopes", emptyIfNil(spec.Scopes))
	audit.Detail(ctx, "tags", emptyIfNil(spec.Tags))
	audit.Detail(ctx, "issuer", spec.Issuer)
	audit.Detail(ctx, "subject", spec.Subject)

	if uid, ok := ownerUser(ctx); ok {
		u := uint(uid)
		spec.CreatorUserID = &u
	}

	identity, err := b.State.CreateFederatedIdentity(spec)
	if err != nil {
		return nil, mapError("creating federated identity", err)
	}

	audit.Target(ctx, "key", identity.ClientID, "")

	// A federated identity has no secret, so nothing is shown once here:
	// the JWT its issuer signs is the credential.
	return &keyOutput{Body: oauthClientToKey(identity, "")}, nil
}

// updateFederatedIdentity replaces a federated identity's grant and trust
// conditions, Tailscale's SetFederatedIdentity.
func updateFederatedIdentity(
	ctx context.Context, b Backend, clientID string, body CreateKeyRequest,
) (*keyOutput, error) {
	err := requireKeyScope(ctx, scope.OAuthKeys)
	if err != nil {
		return nil, err
	}

	spec, err := federatedSpec(ctx, b, body)
	if err != nil {
		return nil, err
	}

	audit.Detail(ctx, "keyType", keyTypeFederated)
	audit.Detail(ctx, "scopes", emptyIfNil(spec.Scopes))
	audit.Detail(ctx, "tags", emptyIfNil(spec.Tags))
	audit.Detail(ctx, "issuer", spec.Issuer)
	audit.Detail(ctx, "subject", spec.Subject)

	identity, err := b.State.UpdateFederatedIdentity(clientID, spec)
	if err != nil {
		return nil, mapError("updating federated identity", err)
	}

	return &keyOutput{Body: oauthClientToKey(identity, "")}, nil
}

// federatedSpec validates the request and turns it into the stored spec.
// Every trust condition is required: an identity missing one would accept
// any issuer, any workload or any audience, which is the whole guard.
func federatedSpec(
	ctx context.Context, b Backend, body CreateKeyRequest,
) (types.FederatedIdentitySpec, error) {
	if len(body.Scopes) == 0 {
		return types.FederatedIdentitySpec{},
			huma.Error400BadRequest("a federated identity must declare at least one scope")
	}

	err := authorizeClientGrant(ctx, b, body.Scopes, body.Tags)
	if err != nil {
		return types.FederatedIdentitySpec{}, err
	}

	issuer, err := federatedIssuer(body.Issuer)
	if err != nil {
		return types.FederatedIdentitySpec{}, err
	}

	spec := types.FederatedIdentitySpec{
		Scopes:           body.Scopes,
		Tags:             body.Tags,
		Description:      body.Description,
		Issuer:           issuer,
		Audience:         strings.TrimSpace(body.Audience),
		Subject:          strings.TrimSpace(body.Subject),
		CustomClaimRules: body.CustomClaimRules,
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

// federatedIssuer checks the issuer is an https origin the server may
// reach. The discovery document and JWKS are fetched from it, so an
// unchecked issuer would turn the token endpoint into a request forgery
// against the server's own network (see hscontrol/egress).
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
