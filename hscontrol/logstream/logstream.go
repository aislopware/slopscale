// Package logstream ships the audit log to the sinks operators register,
// the way Tailscale's log streaming works: every event is queued per
// stream, batched, encoded for the destination and posted, with a few
// retries when the sink is down. A sink that stays down loses the batch
// and the loss is counted on the stream; the log itself is never held
// back for a sink.
package logstream

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aislopware/slopscale/hscontrol/egress"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/rs/zerolog/log"
)

// Store records how a batch went.
type Store interface {
	RecordLogStreamDelivery(d types.LogStreamDelivery) error
}

// Streamer holds one worker per enabled stream and hands every event to
// all of them.
type Streamer struct {
	store   Store
	tailnet string
	client  *http.Client
	// backoff is the wait before each retry; tests shorten it.
	backoff []time.Duration
	// flush is how long a worker waits for more entries before it ships
	// a batch smaller than batchSize.
	flush     time.Duration
	batchSize int

	mu      sync.Mutex
	workers map[types.LogStreamID]*worker
	closed  bool
	wg      sync.WaitGroup
	ctx     context.Context //nolint:containedctx // bounds the batches in flight
	cancel  context.CancelFunc
}

const (
	deliveryTimeout = 15 * time.Second
	defaultFlush    = 2 * time.Second
	defaultBatch    = 100
	queueSize       = 4096
	maxResponseRead = 4 << 10
	closeGrace      = 5 * time.Second
)

// New returns a streamer with no streams; call [Streamer.Reload].
func New(store Store, tailnet string) *Streamer {
	ctx, cancel := context.WithCancel(context.Background())

	return &Streamer{
		store:   store,
		tailnet: tailnet,
		// Sink URLs are operator input, so batches dial through the egress
		// guard; see hscontrol/egress.
		client:    &http.Client{Timeout: deliveryTimeout, CheckRedirect: noRedirect, Transport: egress.Transport()},
		backoff:   []time.Duration{2 * time.Second, 10 * time.Second, 30 * time.Second},
		flush:     defaultFlush,
		batchSize: defaultBatch,
		workers:   map[types.LogStreamID]*worker{},
		ctx:       ctx,
		cancel:    cancel,
	}
}

// ErrClosed is returned when the streamer is shutting down.
var ErrClosed = errors.New("log streamer is closed")

// ErrRedirected is returned when the sink answered with a redirect.
var ErrRedirected = errors.New("sink redirected; batches are posted to the configured URL only")

// ErrRejected is returned when the sink answered outside 2xx.
var ErrRejected = errors.New("sink rejected the batch")

func noRedirect(*http.Request, []*http.Request) error {
	return ErrRedirected
}

// SetClient swaps the HTTP client, for tests and custom transports.
func (s *Streamer) SetClient(client *http.Client) {
	s.client = client
}

// SetBackoff sets the waits before each retry; an empty slice means one
// attempt.
func (s *Streamer) SetBackoff(backoff []time.Duration) {
	s.backoff = backoff
}

// SetBatching sets how many entries a batch holds at most and how long a
// worker waits to fill one.
func (s *Streamer) SetBatching(size int, flush time.Duration) {
	s.batchSize = size
	s.flush = flush
}

// Reload replaces the streams. A stream whose sink is unchanged keeps
// its worker and queue; a changed one gets a fresh worker once the old
// one has shipped what it holds; a disabled or removed one is stopped.
func (s *Streamer) Reload(streams []types.LogStream) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return
	}

	keep := map[types.LogStreamID]bool{}

	for _, stream := range streams {
		if !stream.Enabled {
			continue
		}

		keep[stream.ID] = true

		if w, ok := s.workers[stream.ID]; ok && w.stream.SameSink(stream) {
			w.rename(stream.Name)

			continue
		}

		if w, ok := s.workers[stream.ID]; ok {
			w.stop()
		}

		s.workers[stream.ID] = s.start(stream)
	}

	for id, w := range s.workers {
		if !keep[id] {
			w.stop()
			delete(s.workers, id)
		}
	}
}

// Publish queues the event for every stream. It never blocks: a full
// queue drops the event and counts it against the stream.
func (s *Streamer) Publish(e types.AuditEvent) {
	entry := EntryFrom(s.tailnet, e)

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, w := range s.workers {
		w.offer(entry)
	}
}

// Test ships one synthetic entry to the stream now and reports how it
// went. Shutdown cuts it off like any batch.
func (s *Streamer) Test(ctx context.Context, stream types.LogStream) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()

		return ErrClosed
	}

	s.wg.Add(1)
	s.mu.Unlock()

	defer s.wg.Done()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	stop := context.AfterFunc(s.ctx, cancel) //nolint:contextcheck // s.ctx is the streamer's lifetime, not a request
	defer stop()

	entry := EntryFrom(s.tailnet, types.AuditEvent{
		CreatedAt: time.Now().UTC(),
		ActorKind: types.ActorSystem,
		Action:    "logstream.test",
		Outcome:   http.StatusOK,
		Detail:    map[string]any{"stream": stream.Name},
	})

	started := time.Now()
	status, err := s.post(ctx, stream, []Entry{entry})
	s.record(stream.ID, 1, 0, status, err, 1, time.Since(started))

	if err != nil {
		// The reported status is coarse, so the cause is logged here.
		log.Debug().Err(err).
			Uint64("stream", uint64(stream.ID)).
			Str("host", stream.Host()).
			Msg("log stream test delivery failed")
	}

	return err
}

// Close stops taking events, gives the workers a little time to ship
// what they hold and then cuts them off.
func (s *Streamer) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()

		return
	}

	s.closed = true

	for id, w := range s.workers {
		w.stop()
		delete(s.workers, id)
	}
	s.mu.Unlock()

	done := make(chan struct{})

	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(closeGrace):
	}

	s.cancel()
	<-done
}

// worker is one stream's queue and shipping loop.
type worker struct {
	s      *Streamer
	stream types.LogStream
	in     chan Entry
	// dropped counts entries lost to a full queue since the last batch.
	dropped atomic.Int64
	// nameMu guards the name, which a reload may change while the worker
	// runs; the rest of the stream is fixed for the worker's life.
	nameMu sync.Mutex
}

func (s *Streamer) start(stream types.LogStream) *worker {
	w := &worker{s: s, stream: stream, in: make(chan Entry, queueSize)}

	s.wg.Go(w.run)

	return w
}

// stop closes the queue; the worker ships what is left and exits.
func (w *worker) stop() {
	close(w.in)
}

func (w *worker) rename(name string) {
	w.nameMu.Lock()
	defer w.nameMu.Unlock()

	w.stream.Name = name
}

func (w *worker) current() types.LogStream {
	w.nameMu.Lock()
	defer w.nameMu.Unlock()

	return w.stream
}

func (w *worker) offer(entry Entry) {
	select {
	case w.in <- entry:
	default:
		if w.dropped.Add(1) == 1 {
			log.Warn().Uint64("stream", uint64(w.stream.ID)).Msg("log stream queue full, dropping entries")
		}
	}
}

// run ships batches until the queue closes.
func (w *worker) run() {
	for {
		batch, more := w.collect()
		if len(batch) > 0 {
			w.ship(batch)
		}

		if !more {
			return
		}
	}
}

// collect blocks for the first entry, then takes what arrives within
// the flush wait, up to the batch size. more is false once the queue is
// closed and drained.
func (w *worker) collect() ([]Entry, bool) {
	first, ok := <-w.in
	if !ok {
		return nil, false
	}

	batch := []Entry{first}
	timer := time.NewTimer(w.s.flush)

	defer timer.Stop()

	for len(batch) < w.s.batchSize {
		select {
		case entry, ok := <-w.in:
			if !ok {
				return batch, false
			}

			batch = append(batch, entry)
		case <-timer.C:
			return batch, true
		}
	}

	return batch, true
}

// ship posts one batch with retries and records the outcome.
func (w *worker) ship(batch []Entry) {
	ctx := w.s.ctx
	if ctx.Err() != nil {
		w.s.record(w.stream.ID, len(batch), int(w.dropped.Swap(0)), 0, ErrClosed, 0, 0)

		return
	}

	stream := w.current()

	var (
		status   int
		err      error
		attempts int
	)

	started := time.Now()

	for attempt := 0; attempt <= len(w.s.backoff); attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(w.s.backoff[attempt-1]):
			case <-ctx.Done():
				w.s.record(
					stream.ID,
					len(batch),
					int(w.dropped.Swap(0)),
					status,
					ErrClosed,
					attempts,
					time.Since(started),
				)

				return
			}
		}

		attempts++

		status, err = w.s.post(ctx, stream, batch)
		if err == nil || !retryable(status, err) {
			break
		}
	}

	w.s.record(stream.ID, len(batch), int(w.dropped.Swap(0)), status, err, attempts, time.Since(started))

	if err != nil {
		log.Warn().Err(err).
			Uint64("stream", uint64(stream.ID)).
			Str("host", stream.Host()).
			Int("entries", len(batch)).
			Msg("log stream delivery failed")
	}
}

// retryable reports whether another attempt may help: a network error
// or a server-side status. A 4xx is the sink's verdict.
func retryable(status int, err error) bool {
	if status == 0 {
		return !errors.Is(err, context.Canceled) && !errors.Is(err, ErrRedirected)
	}

	return status >= http.StatusInternalServerError || status == http.StatusTooManyRequests
}

// deliveryStatus is what the operator is told about one batch, stored as the
// stream's last status. A transport error is collapsed: its text names the
// address the server resolved and dialed, which is the server's view of its
// own network, and the whole error goes to the log instead.
func deliveryStatus(status int, err error) string {
	switch {
	case status != 0:
		return strconv.Itoa(status)

	case err == nil:
		return "sent"

	case errors.Is(err, egress.ErrBlocked):
		return "rejected"

	case errors.Is(err, ErrRedirected), errors.Is(err, ErrClosed):
		// slopscale's own words about its own state, with nothing of the
		// sink's network in them.
		return err.Error()

	default:
		return "unreachable"
	}
}

// post sends one batch once and returns the HTTP status it got.
func (s *Streamer) post(ctx context.Context, stream types.LogStream, batch []Entry) (int, error) {
	payload, err := Encode(stream, batch)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, stream.URL, bytes.NewReader(payload.Body))
	if err != nil {
		return 0, fmt.Errorf("building log stream request: %w", err)
	}

	req.Header.Set("Content-Type", payload.ContentType)
	req.Header.Set("User-Agent", "slopscale-logstream/1")

	if payload.AuthHeader != "" {
		req.Header.Set(payload.AuthHeader, payload.AuthValue)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		// A *url.Error prints the whole URL, which may carry a
		// credential; keep the cause only.
		if urlErr, ok := errors.AsType[*url.Error](err); ok {
			err = urlErr.Err
		}

		return 0, fmt.Errorf("posting log batch: %w", err)
	}
	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseRead))

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return resp.StatusCode, fmt.Errorf("%w: %s", ErrRejected, resp.Status)
	}

	return resp.StatusCode, nil
}

func (s *Streamer) record(
	id types.LogStreamID, entries, dropped, status int, err error, attempts int, took time.Duration,
) {
	if s.store == nil {
		return
	}

	recordErr := s.store.RecordLogStreamDelivery(types.LogStreamDelivery{
		StreamID: id,
		Entries:  entries,
		Dropped:  dropped,
		Status:   deliveryStatus(status, err),
		OK:       err == nil,
		Attempts: attempts,
		Duration: took,
		At:       time.Now().UTC(),
	})
	if recordErr != nil {
		log.Error().Err(recordErr).Uint64("stream", uint64(id)).Msg("recording log stream delivery")
	}
}
