package db

import (
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditEvents(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	base := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)

	record := func(offset time.Duration, actor types.UserID, action, targetKind, targetID string) uint64 {
		t.Helper()

		e := &types.AuditEvent{
			CreatedAt:   base.Add(offset),
			ActorKind:   types.ActorSession,
			ActorUserID: actor,
			ActorName:   "alice",
			Action:      action,
			TargetKind:  targetKind,
			TargetID:    targetID,
			Outcome:     200,
			Detail:      map[string]any{"role": "admin"},
			RemoteAddr:  "127.0.0.1",
		}
		require.NoError(t, db.RecordAuditEvent(e))
		require.NotZero(t, e.ID)

		return e.ID
	}

	record(0, 1, "user.role.set", "user", "2")
	record(time.Minute, 1, "node.delete", "node", "7")
	record(2*time.Minute, 2, "node.approve", "node", "7")
	last := record(3*time.Minute, 2, "console.login", "", "")

	t.Run("newest first with the detail round-tripped", func(t *testing.T) {
		t.Parallel()

		events, err := db.ListAuditEvents(types.AuditQuery{})
		require.NoError(t, err)
		require.Len(t, events, 4)
		assert.Equal(t, last, events[0].ID)
		assert.Equal(t, "console.login", events[0].Action)
		assert.Equal(t, map[string]any{"role": "admin"}, events[0].Detail)
		assert.Equal(t, types.ActorSession, events[0].ActorKind)
		assert.Equal(t, types.UserID(2), events[0].ActorUserID)
		assert.Empty(t, events[0].TargetKind)
		assert.True(t, events[0].Succeeded())
	})

	t.Run("filters", func(t *testing.T) {
		t.Parallel()

		byActor, err := db.ListAuditEvents(types.AuditQuery{ActorUserID: 1})
		require.NoError(t, err)
		assert.Len(t, byActor, 2)

		byPrefix, err := db.ListAuditEvents(types.AuditQuery{Action: "node."})
		require.NoError(t, err)
		assert.Len(t, byPrefix, 2)

		exact, err := db.ListAuditEvents(types.AuditQuery{Action: "node.delete"})
		require.NoError(t, err)
		require.Len(t, exact, 1)
		assert.Equal(t, "7", exact[0].TargetID)

		byTarget, err := db.ListAuditEvents(types.AuditQuery{TargetKind: "node", TargetID: "7"})
		require.NoError(t, err)
		assert.Len(t, byTarget, 2)

		window, err := db.ListAuditEvents(types.AuditQuery{
			Since: base.Add(time.Minute),
			Until: base.Add(3 * time.Minute),
		})
		require.NoError(t, err)
		assert.Len(t, window, 2, "since is inclusive, until exclusive")
	})

	t.Run("pages by id", func(t *testing.T) {
		t.Parallel()

		page, err := db.ListAuditEvents(types.AuditQuery{Limit: 3})
		require.NoError(t, err)
		require.Len(t, page, 3)

		rest, err := db.ListAuditEvents(types.AuditQuery{Limit: 3, Before: page[2].ID})
		require.NoError(t, err)
		require.Len(t, rest, 1)
		assert.Equal(t, "user.role.set", rest[0].Action)
	})
}

func TestAuditRetention(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	base := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)

	for i := range 4 {
		require.NoError(t, db.RecordAuditEvent(&types.AuditEvent{
			CreatedAt: base.Add(time.Duration(i) * time.Minute),
			ActorKind: types.ActorSystem,
			Action:    "node.register",
			Outcome:   200,
		}))
	}

	// The cutoff is deliberately in a non-UTC zone: SQLite compares bound
	// times as text, so the executor must bind it in UTC.
	cutoff := base.Add(90 * time.Second).In(time.FixedZone("plus7", 7*3600))

	dropped, err := db.DeleteAuditEventsBefore(cutoff)
	require.NoError(t, err)
	assert.Equal(t, int64(2), dropped)

	left, err := db.ListAuditEvents(types.AuditQuery{})
	require.NoError(t, err)
	assert.Len(t, left, 2)
}
