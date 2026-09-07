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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/rs/zerolog/log"
)

// SignatureHeader carries the timestamp and HMAC of a delivery.
const SignatureHeader = "Tailscale-Webhook-Signature"

// Store records how the newest delivery to an endpoint went.
type Store interface {
	RecordWebhookDelivery(id types.WebhookID, at time.Time, status string) error
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

	wg     sync.WaitGroup
	closed atomic.Bool
	ctx    context.Context //nolint:containedctx // bounds the deliveries in flight
	cancel context.CancelFunc
	// slots bounds the deliveries in flight.
	slots chan struct{}
}

const (
	deliveryTimeout = 15 * time.Second
	maxInFlight     = 16
	maxResponseRead = 4 << 10
)

// New returns a dispatcher with no endpoints; call [Dispatcher.Reload].
func New(store Store, tailnet string) *Dispatcher {
	ctx, cancel := context.WithCancel(context.Background())

	return &Dispatcher{
		store:   store,
		tailnet: tailnet,
		client:  &http.Client{Timeout: deliveryTimeout},
		backoff: []time.Duration{2 * time.Second, 10 * time.Second, 30 * time.Second},
		ctx:     ctx,
		cancel:  cancel,
		slots:   make(chan struct{}, maxInFlight),
	}
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

// Close drops new deliveries, waits a little for the ones in flight and
// then cuts them off.
func (d *Dispatcher) Close() {
	d.closed.Store(true)

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

// Emit delivers the event to every endpoint subscribed to its type, in
// the background. It never blocks the caller beyond the in-flight bound.
func (d *Dispatcher) Emit(t types.WebhookEventType, message string, data any) {
	d.mu.RLock()
	endpoints := d.endpoints
	d.mu.RUnlock()

	if d.closed.Load() {
		return
	}

	event := d.event(t, message, data)

	for _, endpoint := range endpoints {
		if !endpoint.Subscribed(t) {
			continue
		}

		d.wg.Go(func() {
			select {
			case d.slots <- struct{}{}:
			case <-d.ctx.Done():
				return
			}

			defer func() { <-d.slots }()

			d.deliverWithRetry(d.ctx, endpoint, event)
		})
	}
}

// Test delivers a test event to the endpoint now and reports how it went.
func (d *Dispatcher) Test(ctx context.Context, endpoint types.Webhook) error {
	event := d.event(types.EventTest, "This is a test event from headscale.", nil)

	status, err := d.deliver(ctx, endpoint, event)
	d.record(endpoint.ID, status, err)

	return err
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
		status int
		err    error
	)

	for attempt := 0; attempt <= len(d.backoff); attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(d.backoff[attempt-1]):
			case <-ctx.Done():
				return
			}
		}

		status, err = d.deliver(ctx, endpoint, event)
		if err == nil || !retryable(status, err) {
			break
		}
	}

	d.record(endpoint.ID, status, err)

	if err != nil {
		log.Warn().Err(err).
			Str("url", endpoint.URL).
			Str("event", string(event.Type)).
			Msg("webhook delivery failed")
	}
}

// retryable reports whether another attempt may help: a network error
// or a server-side status. A 4xx is the receiver's verdict.
func retryable(status int, err error) bool {
	if status == 0 {
		return !errors.Is(err, context.Canceled)
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
		return 0, fmt.Errorf("posting webhook: %w", err)
	}
	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseRead))

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return resp.StatusCode, fmt.Errorf("%w: %s", ErrDeliveryRejected, resp.Status)
	}

	return resp.StatusCode, nil
}

func (d *Dispatcher) record(id types.WebhookID, status int, err error) {
	if d.store == nil {
		return
	}

	text := strconv.Itoa(status)
	if status == 0 && err != nil {
		text = err.Error()
	}

	recordErr := d.store.RecordWebhookDelivery(id, time.Now().UTC(), text)
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
