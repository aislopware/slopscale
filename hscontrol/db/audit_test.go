package db

import (
	"errors"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
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

		// The console's action box searches by partial name, so a prefix
		// without the dot matches too.
		partial, err := db.ListAuditEvents(types.AuditQuery{Action: "node"})
		require.NoError(t, err)
		assert.Len(t, partial, 2)

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

func TestAuditExport(t *testing.T) {
	t.Parallel()

	db, err := newSQLiteTestDB()
	require.NoError(t, err)

	base := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)

	// More than one batch, so the export's paging is exercised.
	const total = auditExportBatch + 250

	for i := range total {
		action := "node.delete"
		if i%2 == 0 {
			action = "user.role.set"
		}

		require.NoError(t, db.RecordAuditEvent(&types.AuditEvent{
			CreatedAt:  base.Add(time.Duration(i) * time.Second),
			ActorKind:  types.ActorSession,
			Action:     action,
			TargetKind: "node",
			TargetID:   strconv.Itoa(i),
			Outcome:    200,
			Detail:     map[string]any{"index": float64(i)},
		}))
	}

	t.Run("oldest first across batches", func(t *testing.T) {
		t.Parallel()

		var ids []uint64

		require.NoError(t, db.ExportAuditEvents(types.AuditQuery{}, func(e *types.AuditEvent) error {
			ids = append(ids, e.ID)

			return nil
		}))

		require.Len(t, ids, total)
		assert.True(t, slices.IsSorted(ids), "the export runs oldest first")
	})

	t.Run("filters like the list", func(t *testing.T) {
		t.Parallel()

		count := 0

		require.NoError(t, db.ExportAuditEvents(
			types.AuditQuery{Action: "node.delete"},
			func(e *types.AuditEvent) error {
				count++

				assert.Equal(t, "node.delete", e.Action)
				assert.NotEmpty(t, e.Detail)

				return nil
			},
		))

		assert.Equal(t, total/2, count)
	})

	t.Run("stops at the limit", func(t *testing.T) {
		t.Parallel()

		count := 0

		require.NoError(t, db.ExportAuditEvents(types.AuditQuery{Limit: 7}, func(_ *types.AuditEvent) error {
			count++

			return nil
		}))

		assert.Equal(t, 7, count)
	})

	t.Run("stops on the writer's error", func(t *testing.T) {
		t.Parallel()

		errStop := errors.New("stop")
		count := 0

		err := db.ExportAuditEvents(types.AuditQuery{}, func(_ *types.AuditEvent) error {
			count++

			return errStop
		})
		require.ErrorIs(t, err, errStop)
		assert.Equal(t, 1, count)
	})
}

func TestAuditExportRowsCap(t *testing.T) {
	t.Parallel()

	assert.Equal(t, auditExportMaxRows, auditExportRows(0), "no limit asks for the maximum")
	assert.Equal(t, auditExportMaxRows, auditExportRows(-1))
	assert.Equal(t, auditExportMaxRows, auditExportRows(auditExportMaxRows+1), "the maximum is a cap")
	assert.Equal(t, 10, auditExportRows(10))
}
