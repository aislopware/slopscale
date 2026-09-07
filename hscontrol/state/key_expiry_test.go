package state

import (
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyDefaultNodeExpiryWithCap(t *testing.T) {
	t.Parallel()

	s, err := NewState(persistTestConfig(t.TempDir() + "/headscale.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	s.cfg.Node.Expiry = 30 * 24 * time.Hour
	now := time.Now()
	long := now.Add(180 * 24 * time.Hour)
	short := now.Add(24 * time.Hour)

	// Without a cap the file's default only fills a missing expiry.
	node := &types.Node{Expiry: &long}
	s.applyDefaultNodeExpiry(node)
	assert.Equal(t, long, *node.Expiry, "the client's own request wins over node.expiry")

	node = &types.Node{}
	s.applyDefaultNodeExpiry(node)
	assert.WithinDuration(t, now.Add(30*24*time.Hour), *node.Expiry, time.Minute)

	// With a cap a longer request is shortened, a shorter one kept, and a
	// missing one gets the cap rather than the file's default.
	require.NoError(t, s.SetKeyExpiry(7*24*time.Hour))

	node = &types.Node{Expiry: &long}
	s.applyDefaultNodeExpiry(node)
	assert.WithinDuration(t, now.Add(7*24*time.Hour), *node.Expiry, time.Minute)

	node = &types.Node{Expiry: &short}
	s.applyDefaultNodeExpiry(node)
	assert.Equal(t, short, *node.Expiry)

	node = &types.Node{}
	s.applyDefaultNodeExpiry(node)
	assert.WithinDuration(t, now.Add(7*24*time.Hour), *node.Expiry, time.Minute)

	// Tagged nodes never expire, cap or not.
	node = &types.Node{Tags: []string{"tag:server"}, Expiry: &long}
	s.applyDefaultNodeExpiry(node)
	assert.Equal(t, long, *node.Expiry)

	// Off again, and the setting survives a reload.
	require.NoError(t, s.SetKeyExpiry(0))
	assert.Zero(t, s.Settings().KeyExpiry)
	require.ErrorIs(t, s.SetKeyExpiry(time.Minute), types.ErrKeyExpiryOutOfRange)
	require.NoError(t, s.SetKeyExpiry(24*time.Hour))

	settings, err := s.db.LoadSettings()
	require.NoError(t, err)
	assert.Equal(t, 24*time.Hour, settings.KeyExpiry)
}
