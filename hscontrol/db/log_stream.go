package db

import (
	"fmt"
	"time"

	"github.com/aislopware/slopscale/gen/jet/table"
	"github.com/aislopware/slopscale/hscontrol/types"
	jet "github.com/go-jet/jet/v2/sqlite"
)

// logStreamRow is a row of the log_streams table; see schema.sql.
type logStreamRow struct {
	ID                 uint64 `sql:"primary_key"`
	Name               string
	Destination        string
	URL                string
	Token              string
	Enabled            bool
	CreatedBy          *uint64
	CreatedAt          *time.Time
	UpdatedAt          *time.Time
	LastDeliveryAt     *time.Time
	LastDeliveryStatus string
	Delivered          int64
	Dropped            int64
}

type logStreamRecord struct {
	Stream logStreamRow `alias:"log_streams"`
}

func (r logStreamRow) logStream() types.LogStream {
	l := types.LogStream{
		ID:                 types.LogStreamID(r.ID),
		Name:               r.Name,
		Destination:        types.LogStreamDestination(r.Destination),
		URL:                r.URL,
		Token:              r.Token,
		Enabled:            r.Enabled,
		LastDeliveryAt:     r.LastDeliveryAt,
		LastDeliveryStatus: r.LastDeliveryStatus,
		Delivered:          uint64(max(r.Delivered, 0)),
		Dropped:            uint64(max(r.Dropped, 0)),
	}

	if r.CreatedBy != nil {
		l.CreatedBy = types.UserID(*r.CreatedBy)
	}

	if r.CreatedAt != nil {
		l.CreatedAt = *r.CreatedAt
	}

	if r.UpdatedAt != nil {
		l.UpdatedAt = *r.UpdatedAt
	}

	return l
}

func logStreamRowFrom(l types.LogStream) logStreamRow {
	row := logStreamRow{
		Name:        l.Name,
		Destination: string(l.Destination),
		URL:         l.URL,
		Token:       l.Token,
		Enabled:     l.Enabled,
	}

	if l.CreatedBy != 0 {
		id := uint64(l.CreatedBy)
		row.CreatedBy = &id
	}

	return row
}

// ListLogStreams reads every log stream in ID order, tokens included.
func (hsdb *HSDatabase) ListLogStreams() ([]types.LogStream, error) {
	var records []logStreamRecord

	err := hsdb.ex.query(
		jet.SELECT(table.LogStreams.AllColumns).FROM(table.LogStreams).ORDER_BY(table.LogStreams.ID.ASC()),
		&records,
	)
	if err != nil {
		return nil, fmt.Errorf("listing log streams: %w", err)
	}

	out := make([]types.LogStream, 0, len(records))
	for _, r := range records {
		out = append(out, r.Stream.logStream())
	}

	return out, nil
}

// GetLogStream reads one log stream.
func (hsdb *HSDatabase) GetLogStream(id types.LogStreamID) (types.LogStream, error) {
	return getLogStream(hsdb, id)
}

func getLogStream(q Querier, id types.LogStreamID) (types.LogStream, error) {
	var records []logStreamRecord

	err := q.executor().query(
		jet.SELECT(table.LogStreams.AllColumns).FROM(table.LogStreams).
			WHERE(table.LogStreams.ID.EQ(jet.Uint64(uint64(id)))),
		&records,
	)
	if err != nil {
		return types.LogStream{}, fmt.Errorf("reading log stream %d: %w", id, err)
	}

	if len(records) == 0 {
		return types.LogStream{}, types.ErrLogStreamNotFound
	}

	return records[0].Stream.logStream(), nil
}

// CreateLogStream stores a log stream and returns it with its ID.
func (hsdb *HSDatabase) CreateLogStream(l types.LogStream) (types.LogStream, error) {
	return Write(hsdb, func(tx *Tx) (types.LogStream, error) {
		row := logStreamRowFrom(l)

		now := time.Now().UTC()
		row.CreatedAt = &now
		row.UpdatedAt = &now

		var inserted idRow

		err := tx.executor().query(
			table.LogStreams.INSERT(
				table.LogStreams.Name, table.LogStreams.Destination, table.LogStreams.URL, table.LogStreams.Token,
				table.LogStreams.Enabled, table.LogStreams.CreatedBy, table.LogStreams.CreatedAt,
				table.LogStreams.UpdatedAt,
			).MODEL(&row).
				RETURNING(table.LogStreams.ID.AS("id_row.id")),
			&inserted,
		)
		if err != nil {
			return types.LogStream{}, fmt.Errorf("creating log stream: %w", err)
		}

		return getLogStream(tx, types.LogStreamID(inserted.ID))
	})
}

// UpdateLogStream replaces the name, destination, URL and enabled flag of
// the stream, and the token when one is given; an empty token keeps the
// stored one, so an edit never needs the credential again.
func (hsdb *HSDatabase) UpdateLogStream(l types.LogStream) (types.LogStream, error) {
	return Write(hsdb, func(tx *Tx) (types.LogStream, error) {
		row := logStreamRowFrom(l)

		stmt := table.LogStreams.UPDATE(
			table.LogStreams.Name, table.LogStreams.Destination, table.LogStreams.URL,
			table.LogStreams.Enabled, table.LogStreams.UpdatedAt,
		).SET(row.Name, row.Destination, row.URL, row.Enabled, time.Now().UTC())

		if l.Token != "" {
			stmt = table.LogStreams.UPDATE(
				table.LogStreams.Name, table.LogStreams.Destination, table.LogStreams.URL,
				table.LogStreams.Enabled, table.LogStreams.UpdatedAt, table.LogStreams.Token,
			).SET(row.Name, row.Destination, row.URL, row.Enabled, time.Now().UTC(), row.Token)
		}

		affected, err := tx.executor().exec(stmt.WHERE(table.LogStreams.ID.EQ(jet.Uint64(uint64(l.ID)))))
		if err != nil {
			return types.LogStream{}, fmt.Errorf("updating log stream %d: %w", l.ID, err)
		}

		if affected == 0 {
			return types.LogStream{}, types.ErrLogStreamNotFound
		}

		return getLogStream(tx, l.ID)
	})
}

// RecordLogStreamDelivery stores how a batch went on the stream: the
// newest status and the counters. It is called from the shipping
// goroutines, so it never fails loudly.
func (hsdb *HSDatabase) RecordLogStreamDelivery(d types.LogStreamDelivery) error {
	delivered, dropped := int64(0), int64(d.Dropped)
	if d.OK {
		delivered = int64(d.Entries)
	} else {
		dropped += int64(d.Entries)
	}

	_, err := hsdb.ex.exec(
		table.LogStreams.UPDATE(
			table.LogStreams.LastDeliveryAt, table.LogStreams.LastDeliveryStatus,
			table.LogStreams.Delivered, table.LogStreams.Dropped,
		).SET(
			d.At.UTC(), d.Status,
			table.LogStreams.Delivered.ADD(jet.Int64(delivered)),
			table.LogStreams.Dropped.ADD(jet.Int64(dropped)),
		).WHERE(table.LogStreams.ID.EQ(jet.Uint64(uint64(d.StreamID)))),
	)
	if err != nil {
		return fmt.Errorf("recording log stream %d delivery: %w", d.StreamID, err)
	}

	return nil
}

// DeleteLogStream removes the stream.
func (hsdb *HSDatabase) DeleteLogStream(id types.LogStreamID) error {
	return hsdb.Write(func(tx *Tx) error {
		affected, err := tx.executor().exec(
			table.LogStreams.DELETE().WHERE(table.LogStreams.ID.EQ(jet.Uint64(uint64(id)))),
		)
		if err != nil {
			return fmt.Errorf("deleting log stream %d: %w", id, err)
		}

		if affected == 0 {
			return types.ErrLogStreamNotFound
		}

		return nil
	})
}
