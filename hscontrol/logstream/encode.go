package logstream

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
)

// LogType is what the entries are; the server has one log, the audit
// log, which Tailscale calls the configuration log.
const LogType = "configuration"

// Entry is one audit event as a sink receives it.
type Entry struct {
	Time    time.Time `json:"time"`
	Type    string    `json:"type"`
	Tailnet string    `json:"tailnet,omitempty"`
	// ID is the event's ID in the audit log, so a sink can find it again.
	ID     uint64 `json:"id,omitempty"`
	Action string `json:"action"`
	Actor  Party  `json:"actor"`
	Target *Party `json:"target,omitempty"`
	// Outcome is the HTTP status the request ended with.
	Outcome    int            `json:"outcome"`
	Detail     map[string]any `json:"detail,omitempty"`
	RemoteAddr string         `json:"remoteAddr,omitempty"`
}

// Party is who acted or what was acted on.
type Party struct {
	Kind string `json:"kind,omitempty"`
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// EntryFrom renders an audit event for the sinks.
func EntryFrom(tailnet string, e types.AuditEvent) Entry {
	entry := Entry{
		Time:    e.CreatedAt.UTC(),
		Type:    LogType,
		Tailnet: tailnet,
		ID:      e.ID,
		Action:  e.Action,
		Actor: Party{
			Kind: string(e.ActorKind),
			Name: e.ActorName,
		},
		Outcome:    e.Outcome,
		Detail:     e.Detail,
		RemoteAddr: e.RemoteAddr,
	}

	if e.ActorUserID != 0 {
		entry.Actor.ID = strconv.FormatUint(uint64(e.ActorUserID), 10)
	}

	if e.TargetKind != "" || e.TargetID != "" || e.TargetName != "" {
		entry.Target = &Party{Kind: e.TargetKind, ID: e.TargetID, Name: e.TargetName}
	}

	return entry
}

// Summary is the entry as one line, for the sinks that want a message
// next to the fields: "user.role.set by alice on user bob: 200".
func (e Entry) Summary() string {
	var b strings.Builder

	b.WriteString(e.Action)

	if e.Actor.Name != "" {
		b.WriteString(" by " + e.Actor.Name)
	} else if e.Actor.Kind != "" {
		b.WriteString(" by " + e.Actor.Kind)
	}

	if e.Target != nil {
		name := e.Target.Name
		if name == "" {
			name = e.Target.ID
		}

		if name != "" {
			b.WriteString(" on " + strings.TrimSpace(e.Target.Kind+" "+name))
		}
	}

	b.WriteString(": " + strconv.Itoa(e.Outcome))

	return b.String()
}

// fields is the entry as a map, for the destinations that put their own
// keys next to the event's.
func (e Entry) fields() map[string]any {
	out := map[string]any{
		"time":    e.Time,
		"type":    e.Type,
		"action":  e.Action,
		"actor":   e.Actor,
		"outcome": e.Outcome,
	}

	if e.Tailnet != "" {
		out["tailnet"] = e.Tailnet
	}

	if e.ID != 0 {
		out["id"] = e.ID
	}

	if e.Target != nil {
		out["target"] = e.Target
	}

	if len(e.Detail) > 0 {
		out["detail"] = e.Detail
	}

	if e.RemoteAddr != "" {
		out["remoteAddr"] = e.RemoteAddr
	}

	return out
}

// Request is one batch as it is posted: the body, its type and the
// header that carries the credential.
type Request struct {
	ContentType string
	Body        []byte
	// AuthHeader and AuthValue are the credential header; empty when the
	// stream has no token.
	AuthHeader string
	AuthValue  string
}

const (
	contentTypeJSON   = "application/json"
	contentTypeNDJSON = "application/x-ndjson"
)

// Encode renders a batch for the stream's destination.
func Encode(stream types.LogStream, entries []Entry) (Request, error) {
	var (
		req Request
		err error
	)

	switch stream.Destination {
	case types.LogStreamHTTP:
		req, err = encodeJSON(entries)
		req.AuthHeader, req.AuthValue = bearer(stream.Token)
	case types.LogStreamSplunk:
		req, err = encodeSplunk(entries)
		req.AuthHeader, req.AuthValue = "Authorization", "Splunk "+stream.Token
	case types.LogStreamElastic:
		req, err = encodeElastic(entries)

		if stream.Token != "" {
			req.AuthHeader, req.AuthValue = "Authorization", "ApiKey "+stream.Token
		}
	case types.LogStreamDatadog:
		req, err = encodeDatadog(stream, entries)
		req.AuthHeader, req.AuthValue = "DD-API-KEY", stream.Token
	case types.LogStreamAxiom:
		req, err = encodeAxiom(entries)
		req.AuthHeader, req.AuthValue = bearer(stream.Token)
	case types.LogStreamLoki:
		req, err = encodeLoki(stream, entries)
		req.AuthHeader, req.AuthValue = bearer(stream.Token)
	default:
		return Request{}, fmt.Errorf("%w: %q", types.ErrLogStreamDestinationUnknown, stream.Destination)
	}

	if err != nil {
		return Request{}, err
	}

	return req, nil
}

func bearer(token string) (string, string) {
	if token == "" {
		return "", ""
	}

	return "Authorization", "Bearer " + token
}

func marshal(v any) (Request, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return Request{}, fmt.Errorf("encoding log batch: %w", err)
	}

	return Request{ContentType: contentTypeJSON, Body: body}, nil
}

// encodeJSON is the array of entries.
func encodeJSON(entries []Entry) (Request, error) {
	return marshal(entries)
}

// encodeSplunk is HTTP Event Collector events, one JSON object after
// another, which the collector accepts in one request.
func encodeSplunk(entries []Entry) (Request, error) {
	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)

	for _, e := range entries {
		event := map[string]any{
			"time":       float64(e.Time.UnixNano()) / float64(time.Second),
			"source":     "slopscale",
			"sourcetype": "slopscale:" + e.Type,
			"event":      e,
		}

		if e.Tailnet != "" {
			event["host"] = e.Tailnet
		}

		err := enc.Encode(event)
		if err != nil {
			return Request{}, fmt.Errorf("encoding log batch: %w", err)
		}
	}

	return Request{ContentType: contentTypeJSON, Body: buf.Bytes()}, nil
}

// encodeElastic is a bulk request: an index action line, then the
// document, per entry. The URL names the index.
func encodeElastic(entries []Entry) (Request, error) {
	var buf bytes.Buffer

	enc := json.NewEncoder(&buf)

	for _, e := range entries {
		doc := e.fields()
		doc["@timestamp"] = e.Time

		for _, v := range []any{map[string]any{"index": map[string]any{}}, doc} {
			err := enc.Encode(v)
			if err != nil {
				return Request{}, fmt.Errorf("encoding log batch: %w", err)
			}
		}
	}

	return Request{ContentType: contentTypeNDJSON, Body: buf.Bytes()}, nil
}

// encodeDatadog is the logs intake array: Datadog's reserved attributes
// next to the entry's fields.
func encodeDatadog(stream types.LogStream, entries []Entry) (Request, error) {
	logs := make([]map[string]any, 0, len(entries))

	for _, e := range entries {
		doc := e.fields()
		doc["ddsource"] = "slopscale"
		doc["service"] = "slopscale"
		doc["ddtags"] = "type:" + e.Type + tag("tailnet", e.Tailnet) + tag("stream", stream.Name)
		doc["message"] = e.Summary()
		doc["timestamp"] = e.Time.UnixMilli()

		logs = append(logs, doc)
	}

	return marshal(logs)
}

func tag(key, value string) string {
	if value == "" {
		return ""
	}

	return "," + key + ":" + value
}

// encodeAxiom is the ingest array with Axiom's _time field.
func encodeAxiom(entries []Entry) (Request, error) {
	events := make([]map[string]any, 0, len(entries))

	for _, e := range entries {
		doc := e.fields()
		doc["_time"] = e.Time

		events = append(events, doc)
	}

	return marshal(events)
}

// encodeLoki is the push API's streams: one stream with the labels, its
// values the nanosecond timestamp and the entry as a JSON line.
func encodeLoki(stream types.LogStream, entries []Entry) (Request, error) {
	labels := map[string]string{"job": "slopscale", "type": LogType}
	if stream.Name != "" {
		labels["stream"] = stream.Name
	}

	values := make([][]string, 0, len(entries))

	for _, e := range entries {
		if e.Tailnet != "" {
			labels["tailnet"] = e.Tailnet
		}

		line, err := json.Marshal(e)
		if err != nil {
			return Request{}, fmt.Errorf("encoding log batch: %w", err)
		}

		values = append(values, []string{strconv.FormatInt(e.Time.UnixNano(), 10), string(line)})
	}

	return marshal(map[string]any{
		"streams": []map[string]any{{"stream": labels, "values": values}},
	})
}
