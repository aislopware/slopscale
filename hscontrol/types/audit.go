package types

import "time"

// ActorKind is how the actor of an audit event authenticated.
type ActorKind string

const (
	// ActorLocal is the unix socket: the CLI on the server host.
	ActorLocal ActorKind = "local"
	// ActorAPIKey is an API key, with or without an owning user.
	ActorAPIKey ActorKind = "api_key"
	// ActorOAuth is an OAuth access token.
	ActorOAuth ActorKind = "oauth"
	// ActorSession is a console sign-in through the identity provider.
	ActorSession ActorKind = "session"
	// ActorSystem is the server acting on its own: a node registering, an
	// automatic approval, an expiry.
	ActorSystem ActorKind = "system"
	// ActorNode is a machine reporting what its user did on it, over the
	// control connection: leaving the tailnet, for one.
	ActorNode ActorKind = "node"
)

// AuditEvent is one entry of the audit log: who did what to which object,
// and how it went. Actor and target are copied by value so the entry stays
// readable after the user or object it names is gone.
type AuditEvent struct {
	ID        uint64
	CreatedAt time.Time

	ActorKind ActorKind
	// ActorUserID is the user behind the actor; zero for a credential
	// without one and for the system.
	ActorUserID UserID
	// ActorName is the actor's user name, or the key prefix for a
	// credential without a user, at the time of the event.
	ActorName string

	// Action names what happened, dotted, object first:
	// "user.role.set", "node.delete", "console.login".
	Action string

	TargetKind string
	TargetID   string
	TargetName string

	// Outcome is the HTTP status the request ended with; the server's own
	// events record 200.
	Outcome int

	// Detail holds action-specific fields as JSON: the new role, the key's
	// expiry, the routes approved.
	Detail map[string]any

	RemoteAddr string
}

// Succeeded reports whether the audited request was carried out.
func (e *AuditEvent) Succeeded() bool {
	return e.Outcome >= 200 && e.Outcome < 300
}

// AuditQuery selects audit events; every zero field means no filter.
type AuditQuery struct {
	// ActorUserID keeps events by one user.
	ActorUserID UserID
	// Action keeps every action starting with it, so "node." keeps every
	// node action and "node" also matches a search for a partial name.
	Action string
	// TargetKind and TargetID keep events about one object.
	TargetKind string
	TargetID   string
	// Since and Until bound CreatedAt, inclusive and exclusive.
	Since time.Time
	Until time.Time
	// Before pages backwards: only events with a smaller ID.
	Before uint64
	// Limit caps the result; the store applies its own maximum.
	Limit int
}
