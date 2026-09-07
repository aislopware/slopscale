package apiv1

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/scope"
	"github.com/juanfont/headscale/hscontrol/types"
)

func init() {
	registrations = append(registrations, registerAudit)
}

// AuditEvent is one entry of the audit log.
type AuditEvent struct {
	ID        string    `format:"uint64"  json:"id"`
	CreatedAt time.Time `json:"createdAt"`

	ActorKind   string `doc:"local, api_key, oauth, session or system."                      json:"actorKind"`
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

type listAuditInput struct {
	ActorUserID string `doc:"Keep events by this user."                query:"actorUserId"`
	Action      string `doc:"One action, or a prefix ending in a dot." query:"action"`
	TargetKind  string `query:"targetKind"`
	TargetID    string `query:"targetId"`
	Since       string `doc:"RFC 3339; events at or after this time."  format:"date-time"  query:"since"`
	Until       string `doc:"RFC 3339; events before this time."       format:"date-time"  query:"until"`
	Before      string `doc:"Page: events with an ID below this one."  format:"uint64"     query:"before"`
	Limit       int    `doc:"Page size, at most 500."                  maximum:"500"       minimum:"1"    query:"limit"`
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
		Tags:     []string{"Audit"},
		Security: bearerAuth,
	}, scope.LogsConfigurationRead), func(_ context.Context, in *listAuditInput) (*listAuditOutput, error) {
		q, err := auditQuery(in)
		if err != nil {
			return nil, err
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

func auditQuery(in *listAuditInput) (types.AuditQuery, error) {
	q := types.AuditQuery{
		Action:     in.Action,
		TargetKind: in.TargetKind,
		TargetID:   in.TargetID,
		Limit:      in.Limit,
	}

	if q.Limit == 0 {
		q.Limit = 50
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
