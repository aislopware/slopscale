package webhook

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memStore struct {
	mu      sync.Mutex
	records map[types.WebhookID]string
}

func (m *memStore) RecordWebhookDelivery(id types.WebhookID, _ time.Time, status string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.records == nil {
		m.records = map[types.WebhookID]string{}
	}

	m.records[id] = status

	return nil
}

func (m *memStore) status(id types.WebhookID) string {
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

	generic, err := Payload(types.WebhookProviderGeneric, event)
	require.NoError(t, err)

	var events []types.WebhookEvent
	require.NoError(t, json.Unmarshal(generic, &events))
	require.Len(t, events, 1)
	assert.Equal(t, types.EventNodeCreated, events[0].Type)

	slack, err := Payload(types.WebhookProviderSlack, event)
	require.NoError(t, err)
	assert.JSONEq(t, `{"text":"Node laptop joined."}`, string(slack))

	discord, err := Payload(types.WebhookProviderDiscord, event)
	require.NoError(t, err)
	assert.JSONEq(t, `{"content":"Node laptop joined."}`, string(discord))

	_, err = Payload("pager", event)
	require.ErrorIs(t, err, types.ErrWebhookProviderUnknown)
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

	err = d.Test(t.Context(), types.Webhook{ID: 4, URL: "http://127.0.0.1:1", Secret: "s"})
	require.Error(t, err)
	assert.Contains(t, store.status(4), "connect")
}
