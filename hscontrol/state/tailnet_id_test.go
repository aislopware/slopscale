package state

import (
	"regexp"
	"testing"

	hsdb "github.com/aislopware/slopscale/hscontrol/db"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
)

var tailnetIDShape = regexp.MustCompile(`^T[0-9A-Za-z]{10}CNTRL$`)

// TestTailnetIDIsMadeOnceAndKept pins that a fresh database gets a tailnet
// ID in the hosted control plane's shape on the first start and that every
// later start reads the same one back.
func TestTailnetIDIsMadeOnceAndKept(t *testing.T) {
	t.Parallel()

	cfg := persistTestConfig(t.TempDir() + "/slopscale.db")

	first, err := NewState(cfg)
	require.NoError(t, err)

	id := first.TailnetID()
	assert.Regexp(t, tailnetIDShape, string(id))
	require.NoError(t, first.Close())

	second, err := NewState(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })

	assert.Equal(t, id, second.TailnetID(), "a restart keeps the tailnet ID")

	other := newRoleTestState(t)
	assert.NotEqual(t, id, other.TailnetID(), "every server makes its own")
}

// TestTailnetIDKeepsAStoredOne pins that a database that already holds an
// ID, from an earlier start or from another server on the same database,
// keeps it rather than getting a second one.
func TestTailnetIDKeepsAStoredOne(t *testing.T) {
	t.Parallel()

	cfg := persistTestConfig(t.TempDir() + "/slopscale.db")

	db, err := hsdb.NewSlopscaleDatabase(cfg)
	require.NoError(t, err)

	stored, err := db.EnsureTailnetID("TstoredByAnotherCNTRL")
	require.NoError(t, err)
	require.Equal(t, "TstoredByAnotherCNTRL", stored)

	// A second server racing to store its own gets the first one back.
	stored, err = db.EnsureTailnetID(newTailnetID())
	require.NoError(t, err)
	assert.Equal(t, "TstoredByAnotherCNTRL", stored)
	require.NoError(t, db.Close())

	s, err := NewState(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	assert.Equal(t, tailcfg.StableTailnetID("TstoredByAnotherCNTRL"), s.TailnetID())
}

// TestTailnetIDIsNotASwitch pins that the settings API cannot overwrite
// the ID: it is not one of the tailnet-wide switches.
func TestTailnetIDIsNotASwitch(t *testing.T) {
	t.Parallel()

	s := newRoleTestState(t)
	id := s.TailnetID()

	_, err := s.SetSetting(types.SettingTailnetID, true)
	require.ErrorIs(t, err, ErrUnknownSetting)
	assert.Equal(t, id, s.TailnetID())

	stored, err := s.db.LoadTailnetID()
	require.NoError(t, err)
	assert.Equal(t, string(id), stored)
}

func TestNewTailnetIDShape(t *testing.T) {
	t.Parallel()

	seen := make(map[string]bool)

	for range 1000 {
		id := newTailnetID()
		assert.Regexp(t, tailnetIDShape, id)
		assert.False(t, seen[id], "duplicate %s", id)
		seen[id] = true
	}

	var nilState *State
	assert.True(t, nilState.TailnetID().IsZero())
}
