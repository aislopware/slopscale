package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/egress"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain allows deliveries to loopback: these tests post to httptest
// receivers on this host, which the egress guard refuses by default.
func TestMain(m *testing.M) {
	egress.SetDefault(egress.Policy{AllowLoopback: true})

	os.Exit(m.Run())
}

type memStore struct {
	mu      sync.Mutex
	records map[types.WebhookID]types.WebhookDelivery
}

func (m *memStore) RecordWebhookDelivery(d types.WebhookDelivery) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.records == nil {
		m.records = map[types.WebhookID]types.WebhookDelivery{}
	}

	m.records[d.WebhookID] = d

	return nil
}

func (m *memStore) status(id types.WebhookID) string {
	return m.record(id).Status
}

func (m *memStore) record(id types.WebhookID) types.WebhookDelivery {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.records[id]
}

func TestSignVerify(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_700_000_000, 0)
	body := []byte(`[{"type":"test"}]`)
	header := Sign("s3cret", now, body)

	assert.True(t, Verify("s3cret", header, body, now.Add(time.Minute), 5*time.Minute))
	assert.False(t, Verify("other", header, body, now, 5*time.Minute), "wrong secret")
	assert.False(t, Verify("s3cret", header, []byte("tampered"), now, 5*time.Minute), "wrong body")
	assert.False(t, Verify("s3cret", header, body, now.Add(time.Hour), 5*time.Minute), "too old")
	assert.False(t, Verify("s3cret", "garbage", body, now, 5*time.Minute))
}

func TestPayloadPerProvider(t *testing.T) {
	t.Parallel()

	event := types.WebhookEvent{Type: types.EventNodeCreated, Message: "Node laptop joined.", Version: 1}
	endpoint := func(p types.WebhookProvider, url string) types.Webhook {
		return types.Webhook{ProviderType: p, URL: url}
	}

	generic, err := Payload(endpoint(types.WebhookProviderGeneric, "https://x/hook"), event)
	require.NoError(t, err)

	var events []types.WebhookEvent
	require.NoError(t, json.Unmarshal(generic.Body, &events))
	require.Len(t, events, 1)
	assert.Equal(t, types.EventNodeCreated, events[0].Type)
	assert.Equal(t, "https://x/hook", generic.URL)
	assert.Equal(t, "application/json", generic.ContentType)

	slack, err := Payload(endpoint(types.WebhookProviderSlack, "https://x/hook"), event)
	require.NoError(t, err)
	assert.JSONEq(t, `{"text":"Node laptop joined."}`, string(slack.Body))

	teams, err := Payload(endpoint(types.WebhookProviderTeams, "https://x/hook"), event)
	require.NoError(t, err)
	assert.JSONEq(t, `{"text":"Node laptop joined."}`, string(teams.Body))

	discord, err := Payload(endpoint(types.WebhookProviderDiscord, "https://x/hook"), event)
	require.NoError(t, err)
	assert.JSONEq(t,
		`{"content":"Node laptop joined.","allowed_mentions":{"parse":[]}}`,
		string(discord.Body))

	telegram, err := Payload(
		endpoint(types.WebhookProviderTelegram, "https://api.telegram.org/bot123:abc/sendMessage?chat_id=-42"), event,
	)
	require.NoError(t, err)
	assert.JSONEq(t, `{"chat_id":"-42","text":"Node laptop joined."}`, string(telegram.Body))
	assert.Equal(t, "https://api.telegram.org/bot123:abc/sendMessage", telegram.URL, "the chat moved into the body")

	ntfy, err := Payload(endpoint(types.WebhookProviderNtfy, "https://ntfy.sh/ops"), event)
	require.NoError(t, err)
	assert.Equal(t, "Node laptop joined.", string(ntfy.Body))
	assert.Equal(t, "text/plain; charset=utf-8", ntfy.ContentType)

	_, err = Payload(endpoint(types.WebhookProviderEmail, "mailto:ops@example.com"), event)
	require.ErrorIs(t, err, types.ErrWebhookProviderUnknown)

	_, err = Payload(endpoint("pager", "https://x"), event)
	require.ErrorIs(t, err, types.ErrWebhookProviderUnknown)
}

type memMailer struct {
	mu   sync.Mutex
	sent []sentMail
	err  error
}

type sentMail struct {
	to      []string
	subject string
	body    string
}

func (m *memMailer) Send(_ context.Context, to []string, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return m.err
	}

	m.sent = append(m.sent, sentMail{to: to, subject: subject, body: body})

	return nil
}

func (m *memMailer) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.sent)
}

func TestEmailEndpointGoesThroughTheMailer(t *testing.T) {
	t.Parallel()

	store := &memStore{}
	d := New(store, "example.ts.net")
	t.Cleanup(d.Close)

	endpoint := types.Webhook{
		ID: 7, ProviderType: types.WebhookProviderEmail, URL: "mailto:ops@example.com, sec@example.com",
	}

	err := d.Test(t.Context(), endpoint)
	require.ErrorIs(t, err, ErrNoMailer, "no mail server configured")
	assert.Equal(t, ErrNoMailer.Error(), store.status(7), "headscale's own words about its own state")

	mailer := &memMailer{}
	d.SetMailer(mailer)

	require.NoError(t, d.Test(t.Context(), endpoint))
	assert.Equal(t, "sent", store.status(7), "a sent mail has no HTTP status")
	assert.True(t, store.record(7).OK)

	require.Equal(t, 1, mailer.count())
	assert.Equal(t, []string{"ops@example.com", "sec@example.com"}, mailer.sent[0].to)
	assert.Equal(t, "[example.ts.net] This is a test event from headscale.", mailer.sent[0].subject)
	assert.Contains(t, mailer.sent[0].body, "Event: test")

	// A permanent rejection is not retried.
	rejecting := &memMailer{err: fmt.Errorf("%w: 550 no such user", ErrMailRejected)}
	d.SetMailer(rejecting)
	d.SetBackoff([]time.Duration{time.Millisecond, time.Millisecond})

	d.Reload([]types.Webhook{{
		ID: 8, ProviderType: types.WebhookProviderEmail, URL: "mailto:nobody@example.com",
		Subscriptions: []types.WebhookEventType{types.EventNodeCreated},
	}})
	d.Emit(types.EventNodeCreated, "Node laptop joined.", nil)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, 1, store.record(8).Attempts)
	}, 5*time.Second, 10*time.Millisecond)
	assert.False(t, store.record(8).OK)
	assert.Equal(t, "rejected", store.status(8), "the mail server's reply stays in the log")
}

func TestMailMessage(t *testing.T) {
	t.Parallel()

	from := &mail.Address{Name: "Headscale", Address: "hs@example.com"}
	msg := message(from, []string{"a@example.com"}, "Đăng nhập", "body")

	assert.Contains(t, msg, "From: \"Headscale\" <hs@example.com>\r\n")
	assert.Contains(t, msg, "To: a@example.com\r\n")
	assert.Contains(t, msg, "Subject: =?utf-8?q?")
	assert.True(t, strings.HasSuffix(msg, "\r\n\r\nbody"))
}

func TestEmitDeliversToSubscribedWithRetry(t *testing.T) {
	t.Parallel()

	var (
		hits  atomic.Int32
		other atomic.Int32
		body  atomic.Pointer[[]byte]
		sig   atomic.Pointer[string]
	)

	// The first attempt fails with a 503, the second succeeds.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/other" {
			other.Add(1)
			w.WriteHeader(http.StatusOK)

			return
		}

		if hits.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		raw, _ := io.ReadAll(r.Body)
		body.Store(&raw)

		header := r.Header.Get(SignatureHeader)
		sig.Store(&header)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	store := &memStore{}
	d := New(store, "example.ts.net")
	d.SetBackoff([]time.Duration{10 * time.Millisecond})
	d.Reload([]types.Webhook{
		{
			ID:            1,
			URL:           srv.URL + "/hook",
			Secret:        "s3cret",
			Subscriptions: []types.WebhookEventType{types.EventNodeCreated},
		},
		{ID: 2, URL: srv.URL + "/other", Secret: "x", Subscriptions: []types.WebhookEventType{types.EventUserCreated}},
	})

	d.Emit(types.EventNodeCreated, "Node laptop joined.", map[string]string{"nodeId": "7"})
	d.Close()

	assert.Equal(t, int32(2), hits.Load(), "one retry after the 503")
	assert.Equal(t, int32(0), other.Load(), "the unsubscribed endpoint is left alone")
	assert.Equal(t, "200", store.status(1))
	assert.Equal(t, 2, store.record(1).Attempts)
	assert.True(t, store.record(1).OK)
	assert.Equal(t, types.EventNodeCreated, store.record(1).EventType)

	require.NotNil(t, body.Load())
	require.NotNil(t, sig.Load())
	assert.True(t, Verify("s3cret", *sig.Load(), *body.Load(), time.Now(), time.Minute))

	var events []types.WebhookEvent
	require.NoError(t, json.Unmarshal(*body.Load(), &events))
	require.Len(t, events, 1)
	assert.Equal(t, "example.ts.net", events[0].Tailnet)
	assert.Equal(t, map[string]any{"nodeId": "7"}, events[0].Data)
}

func TestTestRecordsRejection(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)

	store := &memStore{}
	d := New(store, "example.ts.net")
	t.Cleanup(d.Close)

	err := d.Test(t.Context(), types.Webhook{ID: 3, URL: srv.URL, Secret: "s"})
	require.ErrorIs(t, err, ErrDeliveryRejected)
	assert.Equal(t, "400", store.status(3))
	assert.False(t, store.record(3).OK)
	assert.Equal(t, 1, store.record(3).Attempts)

	// A receiver that cannot be reached is recorded coarsely: the dial
	// error names the address the server resolved, which the operator's
	// status field does not repeat.
	err = d.Test(t.Context(), types.Webhook{ID: 4, URL: "http://127.0.0.1:1", Secret: "s"})
	require.Error(t, err)
	assert.Equal(t, "unreachable", store.status(4))
}

// TestDeliveryDoesNotFollowRedirects keeps the signed payload at the
// configured URL: a receiver that redirects is recorded as a failure.
func TestDeliveryDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()

	var followed atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			followed.Add(1)
			w.WriteHeader(http.StatusOK)

			return
		}

		http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(srv.Close)

	store := &memStore{}
	d := New(store, "example.ts.net")
	t.Cleanup(d.Close)

	err := d.Test(t.Context(), types.Webhook{ID: 9, URL: srv.URL + "/hook", Secret: "s"})
	require.Error(t, err)
	assert.Equal(t, int32(0), followed.Load(), "the redirect target is never posted to")
	assert.False(t, store.record(9).OK)
	assert.Contains(t, store.status(9), "redirect")
	assert.False(t, retryable(0, err), "a redirect is the receiver's verdict, not a transient failure")
}

// TestTransportErrorsHideTheURL keeps a chat provider's credential, which
// is the URL path, out of the recorded status.
func TestTransportErrorsHideTheURL(t *testing.T) {
	t.Parallel()

	store := &memStore{}
	d := New(store, "example.ts.net")
	t.Cleanup(d.Close)

	err := d.Test(t.Context(), types.Webhook{ID: 5, URL: "http://127.0.0.1:1/services/T0/B0/secret", Secret: "s"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "secret")
	assert.NotContains(t, store.status(5), "secret")
	assert.NotContains(t, store.status(5), "127.0.0.1:1/")
}

// TestCloseRefusesNewWork checks that shutdown drains the queue once
// and admits nothing afterwards.
func TestCloseRefusesNewWork(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	store := &memStore{}
	d := New(store, "example.ts.net")
	d.Reload([]types.Webhook{
		{ID: 1, URL: srv.URL, Secret: "s", Subscriptions: []types.WebhookEventType{types.EventNodeCreated}},
	})

	for range 5 {
		d.Emit(types.EventNodeCreated, "Node joined.", nil)
	}

	d.Close()
	assert.Equal(t, int32(5), hits.Load(), "queued deliveries finish before Close returns")

	d.Emit(types.EventNodeCreated, "Node joined.", nil)
	require.ErrorIs(t, d.Test(t.Context(), types.Webhook{ID: 1, URL: srv.URL, Secret: "s"}), ErrClosed)
	assert.Equal(t, int32(5), hits.Load(), "nothing is delivered after Close")

	d.Close()
}

// TestQueueFullDropsAndRecords bounds the backlog: with every worker
// stuck on a slow receiver and the queue full, a delivery is dropped and
// the endpoint's status says so.
func TestQueueFullDropsAndRecords(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	store := &memStore{}
	d := New(store, "example.ts.net")
	d.Reload([]types.Webhook{
		{ID: 1, URL: srv.URL, Secret: "s", Subscriptions: []types.WebhookEventType{types.EventNodeCreated}},
	})

	for range maxInFlight + maxQueued + 1 {
		d.Emit(types.EventNodeCreated, "Node joined.", nil)
	}

	assert.Eventually(t, func() bool {
		return store.status(1) == ErrQueueFull.Error()
	}, 5*time.Second, 10*time.Millisecond, "the overflow is recorded")

	close(release)
	d.Close()
}
