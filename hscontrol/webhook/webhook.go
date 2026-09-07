// Package webhook delivers events to the endpoints operators register,
// the way Tailscale's webhooks work: a JSON array of events, signed with
// the endpoint's secret in a Tailscale-Webhook-Signature header, retried
// a few times when the receiver is down. Chat providers get the message
// in the shape their incoming webhooks expect instead.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/rs/zerolog/log"
)

// SignatureHeader carries the timestamp and HMAC of a delivery.
const SignatureHeader = "Tailscale-Webhook-Signature"

// Store records how a delivery to an endpoint went.
type Store interface {
	RecordWebhookDelivery(d types.WebhookDelivery) error
}

// Dispatcher holds the current endpoints and delivers events to the
// subscribed ones in the background.
type Dispatcher struct {
	store   Store
	tailnet string
	client  *http.Client
	// backoff is the wait before each retry; tests shorten it.
	backoff []time.Duration

	mu        sync.RWMutex
	endpoints []types.Webhook

	// lifeMu orders admission against shutdown: Emit and Test register
	// work under it, Close flips closed under it, so nothing is admitted
	// after Close starts waiting.
	lifeMu sync.Mutex
	closed bool
	wg     sync.WaitGroup
	ctx    context.Context //nolint:containedctx // bounds the deliveries in flight
	cancel context.CancelFunc
	// queue holds the deliveries waiting for a worker; a full queue drops
	// the delivery and records that.
	queue chan delivery
	// slots bounds the deliveries in flight across workers and tests.
	slots chan struct{}
}

// delivery is one event bound for one endpoint.
type delivery struct {
	endpoint types.Webhook
	event    types.WebhookEvent
}

const (
	deliveryTimeout = 15 * time.Second
	maxInFlight     = 16
	maxQueued       = 1024
	maxResponseRead = 4 << 10
)

// New returns a dispatcher with no endpoints; call [Dispatcher.Reload].
func New(store Store, tailnet string) *Dispatcher {
	ctx, cancel := context.WithCancel(context.Background())

	d := &Dispatcher{
		store:   store,
		tailnet: tailnet,
		client:  &http.Client{Timeout: deliveryTimeout, CheckRedirect: noRedirect},
		backoff: []time.Duration{2 * time.Second, 10 * time.Second, 30 * time.Second},
		ctx:     ctx,
		cancel:  cancel,
		queue:   make(chan delivery, maxQueued),
		slots:   make(chan struct{}, maxInFlight),
	}

	for range maxInFlight {
		d.wg.Go(d.work)
	}

	return d
}

// ErrClosed is returned when the dispatcher is shutting down.
var ErrClosed = errors.New("webhook dispatcher is closed")

// ErrQueueFull is recorded for a delivery dropped because too many were
// waiting.
var ErrQueueFull = errors.New("webhook queue is full; delivery dropped")

// ErrRedirected is returned when the receiver answered with a redirect.
var ErrRedirected = errors.New("webhook receiver redirected; deliveries are posted to the configured URL only")

// noRedirect keeps a signed payload at the URL the operator configured
// instead of following the receiver to wherever it points.
func noRedirect(*http.Request, []*http.Request) error {
	return ErrRedirected
}

// SetClient swaps the HTTP client, for tests and custom transports.
func (d *Dispatcher) SetClient(client *http.Client) {
	d.client = client
}

// SetBackoff sets the waits before each retry; an empty slice means one
// attempt.
func (d *Dispatcher) SetBackoff(backoff []time.Duration) {
	d.backoff = backoff
}

// Reload replaces the endpoints the dispatcher delivers to.
func (d *Dispatcher) Reload(endpoints []types.Webhook) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.endpoints = endpoints
}

// closeGrace is how long Close waits for deliveries in flight before it
// cuts them off.
const closeGrace = 5 * time.Second

// Close drops new deliveries, waits a little for the queued and in-flight
// ones and then cuts them off. It returns once every worker has exited.
func (d *Dispatcher) Close() {
	d.lifeMu.Lock()
	if d.closed {
		d.lifeMu.Unlock()

		return
	}

	d.closed = true
	close(d.queue)
	d.lifeMu.Unlock()

	done := make(chan struct{})

	go func() {
		d.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(closeGrace):
	}

	d.cancel()
	<-done
}

// Emit queues the event for every endpoint subscribed to its type. It
// never blocks: when the queue is full the delivery is dropped and
// recorded as such.
func (d *Dispatcher) Emit(t types.WebhookEventType, message string, data any) {
	d.mu.RLock()
	endpoints := d.endpoints
	d.mu.RUnlock()

	var event types.WebhookEvent

	for _, endpoint := range endpoints {
		if !endpoint.Subscribed(t) {
			continue
		}

		if event.Type == "" {
			event = d.event(t, message, data)
		}

		d.enqueue(delivery{endpoint: endpoint, event: event})
	}
}

// Test delivers a test event to the endpoint now and reports how it went.
// It takes one of the in-flight slots like any delivery, so a burst of
// tests cannot push past the limit, and shutdown cuts it off.
func (d *Dispatcher) Test(ctx context.Context, endpoint types.Webhook) error {
	d.lifeMu.Lock()
	if d.closed {
		d.lifeMu.Unlock()

		return ErrClosed
	}

	d.wg.Add(1)
	d.lifeMu.Unlock()

	defer d.wg.Done()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Shutdown cuts a test off like any delivery in flight.
	stop := context.AfterFunc(d.ctx, cancel) //nolint:contextcheck // d.ctx is the dispatcher's lifetime, not a request
	defer stop()

	select {
	case d.slots <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}

	defer func() { <-d.slots }()

	event := d.event(types.EventTest, "This is a test event from headscale.", nil)

	started := time.Now()
	status, err := d.deliver(ctx, endpoint, event)
	d.record(endpoint.ID, event.Type, status, err, 1, time.Since(started))

	return err
}

// work is one worker: it takes deliveries off the queue until the queue
// closes. Once shutdown has cut the context, what is left is dropped.
func (d *Dispatcher) work() {
	for task := range d.queue {
		if d.ctx.Err() != nil {
			d.record(task.endpoint.ID, task.event.Type, 0, ErrClosed, 0, 0)

			continue
		}

		d.deliverWithRetry(d.ctx, task.endpoint, task.event)
	}
}

// enqueue hands one delivery to the workers, under lifeMu so it cannot
// land on a queue that Close is closing.
func (d *Dispatcher) enqueue(task delivery) {
	d.lifeMu.Lock()
	defer d.lifeMu.Unlock()

	if d.closed {
		return
	}

	select {
	case d.queue <- task:
	default:
		d.record(task.endpoint.ID, task.event.Type, 0, ErrQueueFull, 0, 0)
		log.Warn().
			Uint64("webhook", uint64(task.endpoint.ID)).
			Str("event", string(task.event.Type)).
			Msg("webhook queue full, delivery dropped")
	}
}

func (d *Dispatcher) event(t types.WebhookEventType, message string, data any) types.WebhookEvent {
	return types.WebhookEvent{
		Timestamp: time.Now().UTC(),
		Version:   types.WebhookEventVersion,
		Type:      t,
		Tailnet:   d.tailnet,
		Message:   message,
		Data:      data,
	}
}

func (d *Dispatcher) deliverWithRetry(ctx context.Context, endpoint types.Webhook, event types.WebhookEvent) {
	var (
		status   int
		err      error
		attempts int
	)

	select {
	case d.slots <- struct{}{}:
	case <-ctx.Done():
		return
	}

	defer func() { <-d.slots }()

	started := time.Now()

	for attempt := 0; attempt <= len(d.backoff); attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(d.backoff[attempt-1]):
			case <-ctx.Done():
				return
			}
		}

		attempts++

		status, err = d.deliver(ctx, endpoint, event)
		if err == nil || !retryable(status, err) {
			break
		}
	}

	d.record(endpoint.ID, event.Type, status, err, attempts, time.Since(started))

	if err != nil {
		// The URL is a credential for the chat providers, so only its
		// host is logged.
		log.Warn().Err(err).
			Uint64("webhook", uint64(endpoint.ID)).
			Str("host", endpoint.Host()).
			Str("event", string(event.Type)).
			Msg("webhook delivery failed")
	}
}

// retryable reports whether another attempt may help: a network error
// or a server-side status. A 4xx is the receiver's verdict.
func retryable(status int, err error) bool {
	if status == 0 {
		return !errors.Is(err, context.Canceled) && !errors.Is(err, ErrRedirected)
	}

	return status >= http.StatusInternalServerError || status == http.StatusTooManyRequests
}

// ErrDeliveryRejected is returned when the receiver answered outside 2xx.
var ErrDeliveryRejected = errors.New("webhook receiver rejected the delivery")

// deliver posts the event once and returns the HTTP status it got.
func (d *Dispatcher) deliver(ctx context.Context, endpoint types.Webhook, event types.WebhookEvent) (int, error) {
	body, err := Payload(endpoint.ProviderType, event)
	if err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.URL, bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("building webhook request: %w", err)
	}

	now := time.Now()

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "headscale-webhook/1")
	req.Header.Set(SignatureHeader, Sign(endpoint.Secret, now, body))

	resp, err := d.client.Do(req)
	if err != nil {
		// A *url.Error prints the whole URL, which for the chat
		// providers is the credential; keep the cause only.
		if urlErr, ok := errors.AsType[*url.Error](err); ok {
			err = urlErr.Err
		}

		return 0, fmt.Errorf("posting webhook: %w", err)
	}
	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseRead))

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return resp.StatusCode, fmt.Errorf("%w: %s", ErrDeliveryRejected, resp.Status)
	}

	return resp.StatusCode, nil
}

func (d *Dispatcher) record(
	id types.WebhookID, eventType types.WebhookEventType, status int, err error, attempts int, took time.Duration,
) {
	if d.store == nil {
		return
	}

	text := strconv.Itoa(status)
	if status == 0 && err != nil {
		text = err.Error()
	}

	recordErr := d.store.RecordWebhookDelivery(types.WebhookDelivery{
		WebhookID: id,
		EventType: eventType,
		Status:    text,
		OK:        err == nil,
		Attempts:  attempts,
		Duration:  took,
		At:        time.Now().UTC(),
	})
	if recordErr != nil {
		log.Error().Err(recordErr).Uint64("webhook", uint64(id)).Msg("recording webhook delivery")
	}
}

// Sign computes the signature header value for a body at a time:
// "t=<unix>,v1=<hex hmac-sha256(secret, "<unix>.<body>")>".
func Sign(secret string, at time.Time, body []byte) string {
	ts := strconv.FormatInt(at.Unix(), 10)

	return "t=" + ts + ",v1=" + mac(secret, ts, body)
}

// Verify checks a signature header against the body, allowing the
// timestamp to be at most tolerance old or ahead. Receivers use it.
func Verify(secret, header string, body []byte, now time.Time, tolerance time.Duration) bool {
	var ts, v1 string

	for part := range strings.SplitSeq(header, ",") {
		if rest, ok := strings.CutPrefix(part, "t="); ok {
			ts = rest
		} else if rest, ok := strings.CutPrefix(part, "v1="); ok {
			v1 = rest
		}
	}

	unix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}

	if age := now.Sub(time.Unix(unix, 0)); age > tolerance || age < -tolerance {
		return false
	}

	return hmac.Equal([]byte(v1), []byte(mac(secret, ts, body)))
}

func mac(secret, ts string, body []byte) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(ts))
	h.Write([]byte("."))
	h.Write(body)

	return hex.EncodeToString(h.Sum(nil))
}

// Payload renders the event for the provider: the signed JSON array for
// the generic kind, and the message alone in the shape the chat service's
// incoming webhook expects otherwise.
func Payload(provider types.WebhookProvider, event types.WebhookEvent) ([]byte, error) {
	var body any

	switch provider {
	case types.WebhookProviderGeneric:
		body = []types.WebhookEvent{event}
	case types.WebhookProviderSlack, types.WebhookProviderMattermost, types.WebhookProviderGoogleChat:
		body = map[string]string{"text": event.Message}
	case types.WebhookProviderDiscord:
		body = map[string]string{"content": event.Message}
	default:
		return nil, fmt.Errorf("%w: %q", types.ErrWebhookProviderUnknown, provider)
	}

	out, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encoding webhook payload: %w", err)
	}

	return out, nil
}
