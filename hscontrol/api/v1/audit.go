package apiv1

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/scope"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/rs/zerolog/log"
)

func init() {
	registrations = append(registrations, registerAudit, registerAuditExport)
}

const (
	tagAudit = "Audit"
	// auditDefaultPage is the list page size when a request asks for none.
	auditDefaultPage = 50
)

// AuditEvent is one entry of the audit log.
type AuditEvent struct {
	ID        string    `format:"uint64"  json:"id"`
	CreatedAt time.Time `json:"createdAt"`

	ActorKind   string `doc:"local, api_key, oauth, session, node or system."                json:"actorKind"`
	ActorUserID string `doc:"The user behind the actor; empty for a credential without one." json:"actorUserId"`
	ActorName   string `doc:"The actor's user name, or the credential's prefix."             json:"actorName"`

	Action string `doc:"What happened, dotted and object first: user.role.set, node.delete." json:"action"`

	TargetKind string `json:"targetKind"`
	TargetID   string `json:"targetId"`
	TargetName string `json:"targetName"`

	Outcome int            `doc:"The HTTP status the request ended with." json:"outcome"`
	Detail  map[string]any `doc:"Action-specific fields."                 json:"detail"  nullable:"false"`

	RemoteAddr string `json:"remoteAddr"`
}

// AuditFilters are the query filters the list and the export share. It is
// exported because huma skips unexported embedded fields, which would drop
// every filter from both operations.
type AuditFilters struct {
	ActorUserID string `doc:"Keep events by this user."                query:"actorUserId"`
	Action      string `doc:"One action, or a prefix ending in a dot." query:"action"`
	TargetKind  string `query:"targetKind"`
	TargetID    string `query:"targetId"`
	Since       string `doc:"RFC 3339; events at or after this time."  format:"date-time"  query:"since"`
	Until       string `doc:"RFC 3339; events before this time."       format:"date-time"  query:"until"`
	Before      string `doc:"Page: events with an ID below this one."  format:"uint64"     query:"before"`
}

type listAuditInput struct {
	AuditFilters

	Limit int `doc:"Page size, at most 500." maximum:"500" minimum:"1" query:"limit"`
}

type listAuditOutput struct {
	Body struct {
		Events []AuditEvent `json:"events" nullable:"false"`
		// NextBefore is the cursor for the next page, empty on the last one.
		NextBefore string `json:"nextBefore"`
	}
}

func registerAudit(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "listAuditEvents",
		Method:      http.MethodGet,
		Path:        "/api/v1/audit",
		Summary:     "List audit events",
		Description: "Newest first. Every writing API request and the server's own sign-in events are " +
			"recorded; page with before=<last id>.",
		Tags:     []string{tagAudit},
		Security: bearerAuth,
	}, scope.LogsConfigurationRead), func(_ context.Context, in *listAuditInput) (*listAuditOutput, error) {
		q, err := auditQuery(&in.AuditFilters)
		if err != nil {
			return nil, err
		}

		q.Limit = in.Limit
		if q.Limit == 0 {
			q.Limit = auditDefaultPage
		}

		events, err := b.State.ListAuditEvents(q)
		if err != nil {
			return nil, mapError("listing audit events", err)
		}

		out := &listAuditOutput{}
		out.Body.Events = make([]AuditEvent, 0, len(events))

		for i := range events {
			out.Body.Events = append(out.Body.Events, auditEventFromType(&events[i]))
		}

		if q.Limit > 0 && len(events) == q.Limit {
			out.Body.NextBefore = out.Body.Events[len(events)-1].ID
		}

		return out, nil
	})
}

// auditQuery turns the shared filters into a store query; the caller sets
// the limit it wants.
func auditQuery(in *AuditFilters) (types.AuditQuery, error) {
	q := types.AuditQuery{
		Action:     in.Action,
		TargetKind: in.TargetKind,
		TargetID:   in.TargetID,
	}

	if in.ActorUserID != "" {
		id, err := parseUserID(in.ActorUserID)
		if err != nil {
			return q, err
		}

		q.ActorUserID = id
	}

	if in.Before != "" {
		before, err := strconv.ParseUint(in.Before, 10, 64)
		if err != nil {
			return q, huma.Error400BadRequest("invalid before cursor", err)
		}

		q.Before = before
	}

	var err error

	q.Since, err = parseTime(in.Since, "since")
	if err != nil {
		return q, err
	}

	q.Until, err = parseTime(in.Until, "until")
	if err != nil {
		return q, err
	}

	return q, nil
}

func parseTime(raw, name string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}

	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}, huma.Error400BadRequest("invalid "+name+" timestamp", err)
	}

	return t, nil
}

func auditEventFromType(e *types.AuditEvent) AuditEvent {
	out := AuditEvent{
		ID:         strconv.FormatUint(e.ID, 10),
		CreatedAt:  e.CreatedAt,
		ActorKind:  string(e.ActorKind),
		ActorName:  e.ActorName,
		Action:     e.Action,
		TargetKind: e.TargetKind,
		TargetID:   e.TargetID,
		TargetName: e.TargetName,
		Outcome:    e.Outcome,
		Detail:     e.Detail,
		RemoteAddr: e.RemoteAddr,
	}

	if e.ActorUserID != 0 {
		out.ActorUserID = strconv.FormatUint(uint64(e.ActorUserID), 10)
	}

	if out.Detail == nil {
		out.Detail = map[string]any{}
	}

	return out
}

const (
	// auditExportCSV and auditExportJSON are the formats an export is
	// written in, with the content type each is served as.
	auditExportCSV      = "csv"
	auditExportCSVMedia = "text/csv"
	auditExportCSVType  = auditExportCSVMedia + "; charset=utf-8"
	auditExportJSON     = "json"
	auditExportJSONType = "application/json"

	// auditExportMax is the number of events an export writes at most;
	// hscontrol/db enforces it, this only says so.
	auditExportMax = "100000"
)

type exportAuditInput struct {
	AuditFilters

	Format string `doc:"File format, csv by default." enum:"csv,json" query:"format"`
}

// registerAuditExport adds the download, which streams the matching events
// as a file rather than a JSON page.
func registerAuditExport(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "exportAuditEvents",
		Method:      http.MethodGet,
		Path:        "/api/v1/audit/export",
		Summary:     "Export audit events",
		Description: "The events matching the same filters as the list, oldest first, as a file " +
			"download. At most " + auditExportMax + " events: when more match, the export stops there " +
			"and the newest are left out, so narrow since and until to reach them.",
		Tags:     []string{tagAudit},
		Security: bearerAuth,
		Responses: map[string]*huma.Response{
			"200": {
				Description: "The events as a file.",
				Content: map[string]*huma.MediaType{
					auditExportCSVMedia: {Schema: &huma.Schema{Type: "string", Format: "binary"}},
					auditExportJSONType: {Schema: &huma.Schema{Type: "string", Format: "binary"}},
				},
			},
		},
	}, scope.LogsConfigurationRead), func(_ context.Context, in *exportAuditInput) (*huma.StreamResponse, error) {
		q, err := auditQuery(&in.AuditFilters)
		if err != nil {
			return nil, err
		}

		format := in.Format
		if format == "" {
			format = auditExportCSV
		}

		contentType := auditExportCSVType
		if format == auditExportJSON {
			contentType = auditExportJSONType
		}

		return &huma.StreamResponse{Body: func(ctx huma.Context) {
			ctx.SetHeader("Content-Type", contentType)
			ctx.SetHeader("Content-Disposition",
				`attachment; filename="`+auditExportFilename(q, format)+`"`)
			ctx.SetStatus(http.StatusOK)

			err := writeAuditExport(ctx.BodyWriter(), format, func(fn func(*types.AuditEvent) error) error {
				return b.State.ExportAuditEvents(q, fn)
			})
			if err != nil {
				log.Error().Err(err).Str("format", format).Msg("exporting audit events")
			}
		}}, nil
	})
}

// auditExporter hands every matching event to fn, oldest first.
type auditExporter func(fn func(*types.AuditEvent) error) error

// writeAuditExport streams the events in the chosen format.
func writeAuditExport(w io.Writer, format string, export auditExporter) error {
	if format == auditExportJSON {
		return writeAuditJSON(w, export)
	}

	return writeAuditCSV(w, export)
}

// auditCSVHeader names the columns, in the order auditCSVRow writes them.
var auditCSVHeader = []string{
	"id", "time", "action", "actorKind", "actorUserId", "actorName",
	"targetKind", "targetId", "targetName", "outcome", "remoteAddr", "detail",
}

func writeAuditCSV(w io.Writer, export auditExporter) error {
	out := csv.NewWriter(w)

	err := out.Write(auditCSVHeader)
	if err != nil {
		return fmt.Errorf("writing audit export header: %w", err)
	}

	err = export(func(e *types.AuditEvent) error {
		row, rowErr := auditCSVRow(e)
		if rowErr != nil {
			return rowErr
		}

		return out.Write(row)
	})
	if err != nil {
		return fmt.Errorf("writing audit export: %w", err)
	}

	out.Flush()

	err = out.Error()
	if err != nil {
		return fmt.Errorf("writing audit export: %w", err)
	}

	return nil
}

// auditCSVRow renders one event; the detail is one cell of JSON.
func auditCSVRow(e *types.AuditEvent) ([]string, error) {
	detail := ""

	if len(e.Detail) > 0 {
		raw, err := json.Marshal(e.Detail)
		if err != nil {
			return nil, fmt.Errorf("encoding detail of audit event %d: %w", e.ID, err)
		}

		detail = string(raw)
	}

	actorUserID := ""
	if e.ActorUserID != 0 {
		actorUserID = strconv.FormatUint(uint64(e.ActorUserID), 10)
	}

	return []string{
		strconv.FormatUint(e.ID, 10),
		e.CreatedAt.UTC().Format(time.RFC3339),
		e.Action,
		string(e.ActorKind),
		actorUserID,
		e.ActorName,
		e.TargetKind,
		e.TargetID,
		e.TargetName,
		strconv.Itoa(e.Outcome),
		e.RemoteAddr,
		detail,
	}, nil
}

// writeAuditJSON writes the events as one array, in the shape the list
// endpoint returns them.
func writeAuditJSON(w io.Writer, export auditExporter) error {
	out := bufio.NewWriter(w)
	first := true

	_, _ = out.WriteString("[")

	err := export(func(e *types.AuditEvent) error {
		raw, err := json.Marshal(auditEventFromType(e))
		if err != nil {
			return fmt.Errorf("encoding audit event %d: %w", e.ID, err)
		}

		if !first {
			_, _ = out.WriteString(",")
		}

		first = false

		// bufio keeps the first write error; Flush reports it.
		_, _ = out.Write(raw)

		return nil
	})
	if err != nil {
		return fmt.Errorf("writing audit export: %w", err)
	}

	_, _ = out.WriteString("]\n")

	err = out.Flush()
	if err != nil {
		return fmt.Errorf("writing audit export: %w", err)
	}

	return nil
}

// auditExportFilename names the download after the window it covers: the
// filters when they bound it, the log's start and now when they do not.
func auditExportFilename(q types.AuditQuery, format string) string {
	from := "start"
	if !q.Since.IsZero() {
		from = auditExportStamp(q.Since)
	}

	to := auditExportStamp(time.Now())
	if !q.Until.IsZero() {
		to = auditExportStamp(q.Until)
	}

	return "audit-" + from + "-" + to + "." + format
}

func auditExportStamp(t time.Time) string {
	return t.UTC().Format("20060102T150405Z")
}
