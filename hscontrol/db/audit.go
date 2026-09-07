package db

import (
	"encoding/json/v2"
	"fmt"
	"strings"
	"time"

	jet "github.com/go-jet/jet/v2/sqlite"
	"github.com/juanfont/headscale/gen/jet/table"
	"github.com/juanfont/headscale/hscontrol/types"
)

const (
	// auditDefaultLimit is the page size when a query asks for none.
	auditDefaultLimit = 50
	// auditMaxLimit caps a page so one request cannot pull the whole log.
	auditMaxLimit = 500
)

// auditEventRow is a row of the audit_events table.
type auditEventRow struct {
	ID          uint64 `sql:"primary_key"`
	CreatedAt   time.Time
	ActorKind   string
	ActorUserID *uint64
	ActorName   *string
	Action      string
	TargetKind  *string
	TargetID    *string
	TargetName  *string
	Outcome     int64
	Detail      *string
	RemoteAddr  *string
}

type auditEventRecord struct {
	Event auditEventRow `alias:"audit_events"`
}

func (r *auditEventRow) event() (*types.AuditEvent, error) {
	e := &types.AuditEvent{
		ID:         r.ID,
		CreatedAt:  r.CreatedAt,
		ActorKind:  types.ActorKind(r.ActorKind),
		ActorName:  deref(r.ActorName),
		Action:     r.Action,
		TargetKind: deref(r.TargetKind),
		TargetID:   deref(r.TargetID),
		TargetName: deref(r.TargetName),
		Outcome:    int(r.Outcome),
		RemoteAddr: deref(r.RemoteAddr),
	}

	if r.ActorUserID != nil {
		e.ActorUserID = types.UserID(*r.ActorUserID)
	}

	if r.Detail != nil && hasJSONValue(*r.Detail) {
		err := json.Unmarshal([]byte(*r.Detail), &e.Detail)
		if err != nil {
			return nil, fmt.Errorf("decoding audit detail of event %d: %w", r.ID, err)
		}
	}

	return e, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

// RecordAuditEvent appends e to the audit log and fills in its ID and
// CreatedAt when unset.
func (hsdb *HSDatabase) RecordAuditEvent(e *types.AuditEvent) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}

	row := auditEventRow{
		CreatedAt:  e.CreatedAt,
		ActorKind:  string(e.ActorKind),
		ActorName:  nullable(e.ActorName),
		Action:     e.Action,
		TargetKind: nullable(e.TargetKind),
		TargetID:   nullable(e.TargetID),
		TargetName: nullable(e.TargetName),
		Outcome:    int64(e.Outcome),
		RemoteAddr: nullable(e.RemoteAddr),
	}

	if e.ActorUserID != 0 {
		id := uint64(e.ActorUserID)
		row.ActorUserID = &id
	}

	if len(e.Detail) > 0 {
		detail, err := json.Marshal(e.Detail)
		if err != nil {
			return fmt.Errorf("encoding audit detail: %w", err)
		}

		row.Detail = nullable(string(detail))
	}

	var inserted idRow

	err := hsdb.ex.query(
		table.AuditEvents.INSERT(table.AuditEvents.MutableColumns).MODEL(&row).
			RETURNING(table.AuditEvents.ID.AS("id_row.id")),
		&inserted,
	)
	if err != nil {
		return fmt.Errorf("recording audit event: %w", err)
	}

	e.ID = inserted.ID

	return nil
}

// ListAuditEvents returns the events matching q, newest first, at most
// q.Limit of them (default and maximum apply). Paging is by ID: pass the
// last event's ID as q.Before for the next page.
func (hsdb *HSDatabase) ListAuditEvents(q types.AuditQuery) ([]types.AuditEvent, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = auditDefaultLimit
	}

	if limit > auditMaxLimit {
		limit = auditMaxLimit
	}

	where := jet.Bool(true)

	if q.ActorUserID != 0 {
		where = where.AND(table.AuditEvents.ActorUserID.EQ(jet.Uint64(uint64(q.ActorUserID))))
	}

	if q.Action != "" {
		if strings.HasSuffix(q.Action, ".") {
			// A literal prefix match; LIKE would need dialect-specific
			// escaping of the wildcards.
			where = where.AND(jet.RawBool(
				"substr(audit_events.action, 1, #n) = #p",
				jet.RawArgs{"#n": len(q.Action), "#p": q.Action},
			))
		} else {
			where = where.AND(table.AuditEvents.Action.EQ(jet.String(q.Action)))
		}
	}

	if q.TargetKind != "" {
		where = where.AND(table.AuditEvents.TargetKind.EQ(jet.String(q.TargetKind)))
	}

	if q.TargetID != "" {
		where = where.AND(table.AuditEvents.TargetID.EQ(jet.String(q.TargetID)))
	}

	if !q.Since.IsZero() {
		where = where.AND(table.AuditEvents.CreatedAt.GT_EQ(jet.TimestampExp(timeArg(q.Since.UTC()))))
	}

	if !q.Until.IsZero() {
		where = where.AND(table.AuditEvents.CreatedAt.LT(jet.TimestampExp(timeArg(q.Until.UTC()))))
	}

	if q.Before != 0 {
		where = where.AND(table.AuditEvents.ID.LT(jet.Uint64(q.Before)))
	}

	var records []auditEventRecord

	err := hsdb.ex.query(
		jet.SELECT(table.AuditEvents.AllColumns).FROM(table.AuditEvents).
			WHERE(where).ORDER_BY(table.AuditEvents.ID.DESC()).LIMIT(int64(limit)),
		&records,
	)
	if err != nil {
		return nil, fmt.Errorf("listing audit events: %w", err)
	}

	events := make([]types.AuditEvent, 0, len(records))

	for i := range records {
		e, err := records[i].Event.event()
		if err != nil {
			return nil, err
		}

		events = append(events, *e)
	}

	return events, nil
}

// DeleteAuditEventsBefore drops events older than cutoff, returning how
// many went; the retention reaper in app.go calls it.
func (hsdb *HSDatabase) DeleteAuditEventsBefore(cutoff time.Time) (int64, error) {
	return hsdb.ex.exec(
		table.AuditEvents.DELETE().WHERE(table.AuditEvents.CreatedAt.LT(jet.TimestampExp(timeArg(cutoff.UTC())))),
	)
}
