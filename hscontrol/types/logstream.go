package types

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

// LogStreamID identifies a log stream in the log_streams table.
type LogStreamID uint64

// String renders the ID in base 10.
func (id LogStreamID) String() string {
	return strconv.FormatUint(uint64(id), 10)
}

// LogStream is a sink the audit log is shipped to, the way Tailscale's
// log streaming works: every event the server records is posted to the
// sink in batches, in the shape the destination expects.
type LogStream struct {
	ID   LogStreamID
	Name string
	// Destination decides the payload shape and the auth header.
	Destination LogStreamDestination
	URL         string
	// Token is the sink's credential; the operator sees it only when
	// setting it.
	Token string
	// Enabled is whether events are shipped; a disabled stream keeps its
	// settings and counters.
	Enabled bool
	// CreatedBy is the user that created the stream; zero for a
	// credential without one.
	CreatedBy UserID
	CreatedAt time.Time
	UpdatedAt time.Time
	// LastDeliveryAt and LastDeliveryStatus report the newest batch; the
	// status is the HTTP status, or the error text when none came.
	LastDeliveryAt     *time.Time
	LastDeliveryStatus string
	// Delivered and Dropped count entries over the stream's life: those
	// a sink accepted and those given up on, after retries or because
	// the queue was full.
	Delivered uint64
	Dropped   uint64
}

// Host is the URL's host, which is safe to log and audit.
func (l LogStream) Host() string {
	u, err := url.Parse(l.URL)
	if err != nil {
		return ""
	}

	return u.Host
}

// SameSink reports whether two records ship to the same place the same
// way, so a reload can keep the stream's queue.
func (l LogStream) SameSink(o LogStream) bool {
	return l.ID == o.ID && l.Destination == o.Destination && l.URL == o.URL && l.Token == o.Token &&
		l.Enabled == o.Enabled
}

// LogStreamDelivery is how one batch went.
type LogStreamDelivery struct {
	StreamID LogStreamID
	// Entries is how many events the batch carried.
	Entries int
	// Dropped is how many events were lost before the batch because the
	// queue was full; they are counted on the stream with the batch.
	Dropped int
	// Status is the HTTP status, or the error text when no response came.
	Status string
	// OK is whether the sink answered 2xx in the end.
	OK bool
	// Attempts counts the requests made, retries included.
	Attempts int
	Duration time.Duration
	At       time.Time
}

// LogStreamDestination names the kind of sink, which decides how a batch
// is encoded and authenticated.
type LogStreamDestination string

// The destinations the server encodes for; see docs/ref/log-streaming.md.
const (
	// LogStreamHTTP posts a JSON array of entries with a bearer token:
	// Cribl, Panther, a Vector or Fluent Bit HTTP source, or anything
	// that takes JSON.
	LogStreamHTTP LogStreamDestination = "http"
	// LogStreamSplunk posts HTTP Event Collector events.
	LogStreamSplunk LogStreamDestination = "splunk"
	// LogStreamElastic posts a bulk request to an index.
	LogStreamElastic LogStreamDestination = "elastic"
	// LogStreamDatadog posts to the logs intake with an API key.
	LogStreamDatadog LogStreamDestination = "datadog"
	// LogStreamAxiom posts to a dataset's ingest endpoint.
	LogStreamAxiom LogStreamDestination = "axiom"
	// LogStreamLoki posts to the push API.
	LogStreamLoki LogStreamDestination = "loki"
)

// LogStreamDestinations lists every destination the server accepts, in
// display order.
var LogStreamDestinations = []LogStreamDestination{
	LogStreamHTTP,
	LogStreamSplunk,
	LogStreamElastic,
	LogStreamDatadog,
	LogStreamAxiom,
	LogStreamLoki,
}

// TokenRequired reports whether the destination cannot work without a
// credential.
func (d LogStreamDestination) TokenRequired() bool {
	return d == LogStreamSplunk || d == LogStreamDatadog || d == LogStreamAxiom
}

// Errors returned by the log stream validation.
var (
	ErrLogStreamNotFound           = errors.New("log stream not found")
	ErrLogStreamNameInvalid        = errors.New("log stream name must be 1 to 100 characters")
	ErrLogStreamURLInvalid         = errors.New("log stream URL must be an http or https URL")
	ErrLogStreamDestinationUnknown = errors.New("unknown log stream destination")
	ErrLogStreamTokenRequired      = errors.New("the destination needs a token")
	ErrLogStreamTokenLong          = errors.New("log stream token must be at most 4096 characters")
)

const (
	maxLogStreamNameRunes  = 100
	maxLogStreamTokenRunes = 4096
)

// ValidateLogStream checks the fields the operator controls.
func ValidateLogStream(l LogStream) error {
	name := strings.TrimSpace(l.Name)
	if name == "" || len([]rune(name)) > maxLogStreamNameRunes {
		return ErrLogStreamNameInvalid
	}

	if !slices.Contains(LogStreamDestinations, l.Destination) {
		return fmt.Errorf("%w: %q", ErrLogStreamDestinationUnknown, l.Destination)
	}

	u, err := url.Parse(l.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%w: %q", ErrLogStreamURLInvalid, l.URL)
	}

	if l.Destination.TokenRequired() && l.Token == "" {
		return fmt.Errorf("%w: %s", ErrLogStreamTokenRequired, l.Destination)
	}

	if len([]rune(l.Token)) > maxLogStreamTokenRunes {
		return ErrLogStreamTokenLong
	}

	return nil
}
