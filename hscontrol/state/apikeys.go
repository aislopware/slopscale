package state

import (
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
)

// RotateAPIKey mints a new secret for an existing API key and returns the new
// key string, which is shown once. The key keeps its id, owner, scopes and
// description; a nil expiration keeps the one it has. The previous secret is
// refused from the moment this returns.
func (s *State) RotateAPIKey(key *types.APIKey, expiration *time.Time) (string, error) {
	return s.db.RotateAPIKey(key, expiration)
}
