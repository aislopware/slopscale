package state

import (
	"github.com/aislopware/slopscale/hscontrol/types"
)

// A federated identity is workload identity federation: a CI job or a
// cloud workload exchanges the OIDC JWT its own platform signs for a
// slopscale access token, so nothing has to store a slopscale secret. It
// shares the oauth_clients table with client-credentials clients and mints
// the same short-lived tokens; only how it authenticates differs. The JWT
// verification itself lives with the token endpoint in hscontrol/api/v2.

// CreateFederatedIdentity stores a federated identity and returns it.
func (s *State) CreateFederatedIdentity(spec types.FederatedIdentitySpec) (*types.OAuthClient, error) {
	return s.db.CreateFederatedIdentity(spec)
}

// UpdateFederatedIdentity replaces a federated identity's scopes, tags,
// description and trust conditions.
func (s *State) UpdateFederatedIdentity(
	clientID string,
	spec types.FederatedIdentitySpec,
) (*types.OAuthClient, error) {
	return s.db.UpdateFederatedIdentity(clientID, spec)
}
