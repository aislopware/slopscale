package state

import (
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/db"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

// TestReauthWithNewHostnameRenamesMagicDNS proves that a machine that
// re-registers under a new hostname is reachable under it: the GivenName
// follows the hostname on the re-registration path the way it does on the
// map request path, so a re-imaged fleet does not keep stale MagicDNS names
// (aislopware/slopscale#3432). A name an administrator chose is kept.
func TestReauthWithNewHostnameRenamesMagicDNS(t *testing.T) {
	t.Parallel()

	dbPath := t.TempDir() + "/slopscale.db"
	cfg := persistTestConfig(dbPath)

	database, err := db.NewSlopscaleDatabase(cfg)
	require.NoError(t, err)

	user := database.CreateUserForTest("reimage")
	auto := database.CreateRegisteredNodeForTest(user, "host-a")
	renamed := database.CreateRegisteredNodeForTest(user, "kept-a")

	require.NoError(t, database.Close())

	s, err := NewState(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	_, _, err = s.RenameNode(renamed.ID, "chosen")
	require.NoError(t, err)

	userID := types.UserID(user.ID)
	pak, err := s.CreatePreAuthKey(&userID, true, false, nil, nil)
	require.NoError(t, err)

	reauth := func(machineKey key.MachinePublic, hostname string) types.NodeView {
		t.Helper()

		node, _, err := s.HandleNodeFromPreAuthKey(tailcfg.RegisterRequest{
			Auth:     &tailcfg.RegisterResponseAuth{AuthKey: pak.Key},
			NodeKey:  key.NewNode().Public(),
			Expiry:   time.Now().Add(24 * time.Hour),
			Hostinfo: &tailcfg.Hostinfo{Hostname: hostname},
		}, machineKey)
		require.NoError(t, err)

		return node
	}

	got := reauth(auto.MachineKey, "host-b")
	assert.Equal(t, "host-b", got.Hostname())
	assert.Equal(t, "host-b", got.GivenName(), "an auto-derived name follows the hostname")

	got = reauth(renamed.MachineKey, "kept-b")
	assert.Equal(t, "kept-b", got.Hostname())
	assert.Equal(t, "chosen", got.GivenName(), "an administrator's name survives")

	// A re-registration under a name another machine holds is bumped, not
	// duplicated.
	got = reauth(auto.MachineKey, "chosen")
	assert.Equal(t, "chosen", got.Hostname())
	assert.NotEqual(t, "chosen", got.GivenName())
	assert.Contains(t, got.GivenName(), "chosen-")
}
