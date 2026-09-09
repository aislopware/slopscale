package state

import (
	"github.com/aislopware/slopscale/hscontrol/types"
)

// UpdateOAuthClient replaces an OAuth client's scopes, tags and
// description, leaving its secret in place. Tailscale's SetOAuthClient is a
// wholesale update of the configuration, which the Terraform provider uses
// to change a client without replacing it.
func (s *State) UpdateOAuthClient(
	clientID string,
	scopes, tags []string,
	description string,
) (*types.OAuthClient, error) {
	return s.db.UpdateOAuthClient(clientID, scopes, tags, description)
}
