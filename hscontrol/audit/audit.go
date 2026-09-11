// Package audit records who changed what through the admin API. Every
// writing operation declares an action with [Declare]; [Middleware] turns
// each such request into one [types.AuditEvent] once the handler has run,
// with the caller from the principal, the outcome from the response status
// and whatever the handler added through [Target] and [Detail]. Events the
// server raises on its own, such as a console sign-in, go through [Record].
package audit

import (
	"context"
	"net"
	"net/http"

	"github.com/aislopware/slopscale/hscontrol/api/principal"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/zerolog/log"
)

// Sink stores events and names the users behind them. *state.State
// satisfies it.
type Sink interface {
	RecordAuditEvent(e *types.AuditEvent) error
	GetUserByID(id types.UserID) (*types.User, error)
}

const (
	actionMetaKey = "slopscale.audit.action"
	targetMetaKey = "slopscale.audit.target"
)

// target is how an operation names its object: the kind, and the path
// parameter carrying its ID.
type target struct {
	kind  string
	param string
}

// Declare records op's audit action, dotted and object first
// ("user.role.set"), and optionally the kind of its target with the path
// parameter that carries the target's ID. The action is also published in
// the OpenAPI document as x-audit-action.
func Declare(op huma.Operation, action, targetKind, targetParam string) huma.Operation {
	if op.Metadata == nil {
		op.Metadata = map[string]any{}
	}

	op.Metadata[actionMetaKey] = action

	if targetKind != "" {
		op.Metadata[targetMetaKey] = target{kind: targetKind, param: targetParam}
	}

	if op.Extensions == nil {
		op.Extensions = map[string]any{}
	}

	op.Extensions["x-audit-action"] = action

	return op
}

// Action returns the action op declared, if any.
func Action(op *huma.Operation) (string, bool) {
	if op == nil || op.Metadata == nil {
		return "", false
	}

	a, ok := op.Metadata[actionMetaKey].(string)

	return a, ok
}

type contextKey struct{}

// From returns the event being built for the request, or nil when the
// operation is not audited.
func From(ctx context.Context) *types.AuditEvent {
	e, _ := ctx.Value(contextKey{}).(*types.AuditEvent)

	return e
}

// Target names the object a request acted on. Handlers call it when the
// path parameter alone does not identify the object (a key created by
// the request, a node found by name) or to add the object's name.
func Target(ctx context.Context, kind, id, name string) {
	e := From(ctx)
	if e == nil {
		return
	}

	if kind != "" {
		e.TargetKind = kind
	}

	if id != "" {
		e.TargetID = id
	}

	if name != "" {
		e.TargetName = name
	}
}

// Detail adds one action-specific field to the request's event.
func Detail(ctx context.Context, key string, value any) {
	e := From(ctx)
	if e == nil {
		return
	}

	if e.Detail == nil {
		e.Detail = map[string]any{}
	}

	e.Detail[key] = value
}

// Middleware records one event per declared operation after the handler
// ran. Unauthenticated attempts are not recorded: they carry no actor and
// are already logged, and recording them would let anyone fill the log.
func Middleware(sink Sink) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		action, ok := Action(ctx.Operation())
		if !ok {
			next(ctx)

			return
		}

		e := &types.AuditEvent{
			Action:     action,
			RemoteAddr: remoteIP(ctx.RemoteAddr()),
		}

		if t, isTarget := ctx.Operation().Metadata[targetMetaKey].(target); isTarget {
			e.TargetKind = t.kind
			e.TargetID = ctx.Param(t.param)
		}

		next(huma.WithValue(ctx, contextKey{}, e))

		p, ok := principal.From(ctx.Context())
		if !ok {
			return
		}

		e.Outcome = ctx.Status()
		if e.Outcome == 0 {
			e.Outcome = http.StatusOK
		}

		if e.Outcome == http.StatusUnauthorized {
			return
		}

		fillActor(sink, e, p)

		Record(sink, e)
	}
}

// Record stores e, filling in the actor's name for a system event, and
// logs rather than fails when the store refuses it: an audit failure must
// not undo the change it describes.
func Record(sink Sink, e *types.AuditEvent) {
	if e.ActorKind == "" {
		e.ActorKind = types.ActorSystem
	}

	if e.Outcome == 0 {
		e.Outcome = http.StatusOK
	}

	if e.ActorUserID != 0 && e.ActorName == "" {
		e.ActorName = userName(sink, e.ActorUserID)
	}

	e.RemoteAddr = remoteIP(e.RemoteAddr)

	err := sink.RecordAuditEvent(e)
	if err != nil {
		log.Error().Err(err).Str("action", e.Action).Msg("recording audit event")
	}
}

func fillActor(sink Sink, e *types.AuditEvent, p principal.Principal) {
	switch p.Kind {
	case principal.LocalTrust:
		e.ActorKind = types.ActorLocal
	case principal.APIKey:
		e.ActorKind = types.ActorAPIKey
	case principal.AccessToken:
		e.ActorKind = types.ActorOAuth
	case principal.Session:
		e.ActorKind = types.ActorSession
	}

	e.ActorUserID = p.UserID
	e.ActorName = p.Credential

	if p.HasUser() {
		if name := userName(sink, p.UserID); name != "" {
			e.ActorName = name
		}
	}
}

func userName(sink Sink, id types.UserID) string {
	user, err := sink.GetUserByID(id)
	if err != nil {
		return ""
	}

	return user.AuditName()
}

// remoteIP strips the port from a remote address; the port says nothing
// about who called.
func remoteIP(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}

	return host
}
