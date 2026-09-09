package db

import (
	"fmt"
	"time"

	"github.com/aislopware/slopscale/gen/jet/table"
	"github.com/aislopware/slopscale/hscontrol/types"
	jet "github.com/go-jet/jet/v2/sqlite"
)

// sshRecordingRow is a row of the ssh_recordings table; see schema.sql.
type sshRecordingRow struct {
	ID        uint64 `sql:"primary_key"`
	StartedAt time.Time
	EndedAt   *time.Time
	SrcNode   string
	SrcNodeID string
	SrcUser   string
	DstNodeID *uint64
	DstNode   string
	SSHUser   string
	LocalUser string
	Command   string
	Size      int64
	Path      string
	Complete  bool
}

type sshRecordingRecord struct {
	Recording sshRecordingRow `alias:"ssh_recordings"`
}

func (r sshRecordingRow) recording() types.SSHRecording {
	out := types.SSHRecording{
		ID:        types.SSHRecordingID(r.ID),
		StartedAt: r.StartedAt,
		EndedAt:   r.EndedAt,
		SrcNode:   r.SrcNode,
		SrcNodeID: r.SrcNodeID,
		SrcUser:   r.SrcUser,
		DstNode:   r.DstNode,
		SSHUser:   r.SSHUser,
		LocalUser: r.LocalUser,
		Command:   r.Command,
		Size:      r.Size,
		Path:      r.Path,
		Complete:  r.Complete,
	}

	if r.DstNodeID != nil {
		out.DstNodeID = types.NodeID(*r.DstNodeID)
	}

	return out
}

// CreateSSHRecording stores a recording as its upload starts and returns
// it with its ID.
func (hsdb *HSDatabase) CreateSSHRecording(rec types.SSHRecording) (types.SSHRecording, error) {
	return Write(hsdb, func(tx *Tx) (types.SSHRecording, error) {
		row := sshRecordingRow{
			StartedAt: rec.StartedAt.UTC(),
			SrcNode:   rec.SrcNode,
			SrcNodeID: rec.SrcNodeID,
			SrcUser:   rec.SrcUser,
			DstNode:   rec.DstNode,
			SSHUser:   rec.SSHUser,
			LocalUser: rec.LocalUser,
			Command:   rec.Command,
			Path:      rec.Path,
		}

		if rec.DstNodeID != 0 {
			id := uint64(rec.DstNodeID)
			row.DstNodeID = &id
		}

		var inserted idRow

		err := tx.executor().query(
			table.SSHRecordings.INSERT(
				table.SSHRecordings.StartedAt, table.SSHRecordings.SrcNode, table.SSHRecordings.SrcNodeID,
				table.SSHRecordings.SrcUser, table.SSHRecordings.DstNodeID, table.SSHRecordings.DstNode,
				table.SSHRecordings.SSHUser, table.SSHRecordings.LocalUser, table.SSHRecordings.Command,
				table.SSHRecordings.Path,
			).MODEL(&row).
				RETURNING(table.SSHRecordings.ID.AS("id_row.id")),
			&inserted,
		)
		if err != nil {
			return types.SSHRecording{}, fmt.Errorf("creating SSH recording: %w", err)
		}

		return getSSHRecording(tx, types.SSHRecordingID(inserted.ID))
	})
}

// FinishSSHRecording records how the upload ended: the file's size, and
// whether the client closed it cleanly.
func (hsdb *HSDatabase) FinishSSHRecording(id types.SSHRecordingID, size int64, complete bool) error {
	_, err := hsdb.ex.exec(
		table.SSHRecordings.UPDATE(
			table.SSHRecordings.EndedAt, table.SSHRecordings.Size, table.SSHRecordings.Complete,
		).SET(time.Now().UTC(), size, complete).
			WHERE(table.SSHRecordings.ID.EQ(jet.Uint64(uint64(id)))),
	)
	if err != nil {
		return fmt.Errorf("finishing SSH recording %d: %w", id, err)
	}

	return nil
}

// GetSSHRecording reads one recording.
func (hsdb *HSDatabase) GetSSHRecording(id types.SSHRecordingID) (types.SSHRecording, error) {
	return getSSHRecording(hsdb, id)
}

func getSSHRecording(q Querier, id types.SSHRecordingID) (types.SSHRecording, error) {
	var records []sshRecordingRecord

	err := q.executor().query(
		jet.SELECT(table.SSHRecordings.AllColumns).FROM(table.SSHRecordings).
			WHERE(table.SSHRecordings.ID.EQ(jet.Uint64(uint64(id)))),
		&records,
	)
	if err != nil {
		return types.SSHRecording{}, fmt.Errorf("reading SSH recording %d: %w", id, err)
	}

	if len(records) == 0 {
		return types.SSHRecording{}, types.ErrSSHRecordingNotFound
	}

	return records[0].Recording.recording(), nil
}

// ListSSHRecordings returns recordings newest first, at most limit of
// them, those started before the given ID when before is set.
func (hsdb *HSDatabase) ListSSHRecordings(before types.SSHRecordingID, limit int) ([]types.SSHRecording, error) {
	stmt := jet.SELECT(table.SSHRecordings.AllColumns).FROM(table.SSHRecordings)

	if before != 0 {
		stmt = stmt.WHERE(table.SSHRecordings.ID.LT(jet.Uint64(uint64(before))))
	}

	var records []sshRecordingRecord

	err := hsdb.ex.query(stmt.ORDER_BY(table.SSHRecordings.ID.DESC()).LIMIT(int64(limit)), &records)
	if err != nil {
		return nil, fmt.Errorf("listing SSH recordings: %w", err)
	}

	out := make([]types.SSHRecording, 0, len(records))
	for _, r := range records {
		out = append(out, r.Recording.recording())
	}

	return out, nil
}

// DeleteSSHRecording removes the row; the caller removes the file.
func (hsdb *HSDatabase) DeleteSSHRecording(id types.SSHRecordingID) error {
	affected, err := hsdb.ex.exec(
		table.SSHRecordings.DELETE().WHERE(table.SSHRecordings.ID.EQ(jet.Uint64(uint64(id)))),
	)
	if err != nil {
		return fmt.Errorf("deleting SSH recording %d: %w", id, err)
	}

	if affected == 0 {
		return types.ErrSSHRecordingNotFound
	}

	return nil
}

// ListSSHRecordingsBefore returns the recordings that started before the
// time, oldest first, for the retention collector.
func (hsdb *HSDatabase) ListSSHRecordingsBefore(cutoff time.Time) ([]types.SSHRecording, error) {
	var records []sshRecordingRecord

	err := hsdb.ex.query(
		jet.SELECT(table.SSHRecordings.AllColumns).FROM(table.SSHRecordings).
			WHERE(table.SSHRecordings.StartedAt.LT(jet.TimestampExp(timeArg(cutoff.UTC())))).
			ORDER_BY(table.SSHRecordings.ID.ASC()),
		&records,
	)
	if err != nil {
		return nil, fmt.Errorf("listing old SSH recordings: %w", err)
	}

	out := make([]types.SSHRecording, 0, len(records))
	for _, r := range records {
		out = append(out, r.Recording.recording())
	}

	return out, nil
}
