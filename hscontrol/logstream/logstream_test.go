package logstream

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	records map[types.LogStreamID][]types.LogStreamDelivery
}

func (m *memStore) RecordLogStreamDelivery(d types.LogStreamDelivery) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.records == nil {
		m.records = map[types.LogStreamID][]types.LogStreamDelivery{}
	}

	m.records[d.StreamID] = append(m.records[d.StreamID], d)

	return nil
}

func (m *memStore) all(id types.LogStreamID) []types.LogStreamDelivery {
	m.mu.Lock()
	defer m.mu.Unlock()

	return append([]types.LogStreamDelivery(nil), m.records[id]...)
}

func (m *memStore) last(id types.LogStreamID) types.LogStreamDelivery {
	all := m.all(id)
	if len(all) == 0 {
		return types.LogStreamDelivery{}
	}

	return all[len(all)-1]
}

func event(id uint64, action string) types.AuditEvent {
	return types.AuditEvent{
		ID:          id,
		CreatedAt:   time.Date(2026, 9, 12, 9, 0, 0, int(id)*1000, time.UTC),
		ActorKind:   types.ActorAPIKey,
		ActorUserID: 3,
		ActorName:   "alice",
		Action:      action,
		TargetKind:  "user",
		TargetID:    "5",
		TargetName:  "bob",
		Outcome:     200,
		Detail:      map[string]any{"role": "admin"},
		RemoteAddr:  "203.0.113.9",
	}
}

func TestEntrySummary(t *testing.T) {
	t.Parallel()

	e := EntryFrom("example.ts.net", event(1, "user.role.set"))
	assert.Equal(t, "user.role.set by alice on user bob: 200", e.Summary())
	assert.Equal(t, "3", e.Actor.ID)
	assert.Equal(t, LogType, e.Type)

	system := EntryFrom("", types.AuditEvent{ActorKind: types.ActorSystem, Action: "node.expire", Outcome: 200})
	assert.Equal(t, "node.expire by system: 200", system.Summary())
	assert.Nil(t, system.Target)
}

func TestEncodePerDestination(t *testing.T) {
	t.Parallel()

	entries := []Entry{
		EntryFrom("example.ts.net", event(1, "user.role.set")),
		EntryFrom("example.ts.net", event(2, "node.delete")),
	}
	stream := func(d types.LogStreamDestination, token string) types.LogStream {
		return types.LogStream{ID: 1, Name: "siem", Destination: d, URL: "https://sink.example/x", Token: token}
	}

	t.Run("http", func(t *testing.T) {
		t.Parallel()

		req, err := Encode(stream(types.LogStreamHTTP, "tok"), entries)
		require.NoError(t, err)
		assert.Equal(t, "Authorization", req.AuthHeader)
		assert.Equal(t, "Bearer tok", req.AuthValue)
		assert.Equal(t, contentTypeJSON, req.ContentType) //nolint:testifylint // a content type, not JSON

		var got []Entry
		require.NoError(t, json.Unmarshal(req.Body, &got))
		require.Len(t, got, 2)
		assert.Equal(t, "node.delete", got[1].Action)

		plain, err := Encode(stream(types.LogStreamHTTP, ""), entries)
		require.NoError(t, err)
		assert.Empty(t, plain.AuthHeader, "no token, no header")
	})

	t.Run("splunk", func(t *testing.T) {
		t.Parallel()

		req, err := Encode(stream(types.LogStreamSplunk, "hec"), entries)
		require.NoError(t, err)
		assert.Equal(t, "Splunk hec", req.AuthValue)

		lines := ndjson(t, req.Body)
		require.Len(t, lines, 2)
		assert.Equal(t, "headscale:configuration", lines[0]["sourcetype"])
		assert.Equal(t, "example.ts.net", lines[0]["host"])
		assert.Contains(t, lines[0], "event")
	})

	t.Run("elastic", func(t *testing.T) {
		t.Parallel()

		req, err := Encode(stream(types.LogStreamElastic, "key"), entries)
		require.NoError(t, err)
		assert.Equal(t, contentTypeNDJSON, req.ContentType) //nolint:testifylint // a content type, not JSON
		assert.Equal(t, "ApiKey key", req.AuthValue)

		lines := ndjson(t, req.Body)
		require.Len(t, lines, 4, "an action line per document")
		assert.Contains(t, lines[0], "index")
		assert.Contains(t, lines[1], "@timestamp")
		assert.Equal(t, "user.role.set", lines[1]["action"])
	})

	t.Run("datadog", func(t *testing.T) {
		t.Parallel()

		req, err := Encode(stream(types.LogStreamDatadog, "dd"), entries)
		require.NoError(t, err)
		assert.Equal(t, "DD-API-KEY", req.AuthHeader)

		var logs []map[string]any
		require.NoError(t, json.Unmarshal(req.Body, &logs))
		require.Len(t, logs, 2)
		assert.Equal(t, "headscale", logs[0]["ddsource"])
		assert.Equal(t, "type:configuration,tailnet:example.ts.net,stream:siem", logs[0]["ddtags"])
		assert.Equal(t, "user.role.set by alice on user bob: 200", logs[0]["message"])
	})

	t.Run("axiom", func(t *testing.T) {
		t.Parallel()

		req, err := Encode(stream(types.LogStreamAxiom, "ax"), entries)
		require.NoError(t, err)
		assert.Equal(t, "Bearer ax", req.AuthValue)

		var events []map[string]any
		require.NoError(t, json.Unmarshal(req.Body, &events))
		require.Len(t, events, 2)
		assert.Contains(t, events[0], "_time")
	})

	t.Run("loki", func(t *testing.T) {
		t.Parallel()

		req, err := Encode(stream(types.LogStreamLoki, ""), entries)
		require.NoError(t, err)
		assert.Empty(t, req.AuthHeader)

		var push struct {
			Streams []struct {
				Stream map[string]string `json:"stream"`
				Values [][]string        `json:"values"`
			} `json:"streams"`
		}
		require.NoError(t, json.Unmarshal(req.Body, &push))
		require.Len(t, push.Streams, 1)
		assert.Equal(t, "headscale", push.Streams[0].Stream["job"])
		assert.Equal(t, "siem", push.Streams[0].Stream["stream"])
		require.Len(t, push.Streams[0].Values, 2)
		assert.Less(t, push.Streams[0].Values[0][0], push.Streams[0].Values[1][0], "nanosecond order")
		assert.Contains(t, push.Streams[0].Values[0][1], `"action":"user.role.set"`)
	})

	t.Run("unknown", func(t *testing.T) {
		t.Parallel()

		_, err := Encode(stream("s3", ""), entries)
		require.ErrorIs(t, err, types.ErrLogStreamDestinationUnknown)
	})
}

func ndjson(t *testing.T, body []byte) []map[string]any {
	t.Helper()

	var out []map[string]any

	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}

		var line map[string]any
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &line))

		out = append(out, line)
	}

	return out
}

// sink is an HTTP server that keeps the batches it got.
type sink struct {
	mu      sync.Mutex
	batches [][]Entry
	auth    []string
	fail    atomic.Int32
	srv     *httptest.Server
}

func newSink(t *testing.T) *sink {
	t.Helper()

	s := &sink{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.fail.Load() > 0 {
			s.fail.Add(-1)
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		body, _ := io.ReadAll(r.Body)

		var batch []Entry

		err := json.Unmarshal(body, &batch)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)

			return
		}

		s.mu.Lock()
		s.batches = append(s.batches, batch)
		s.auth = append(s.auth, r.Header.Get("Authorization"))
		s.mu.Unlock()

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(s.srv.Close)

	return s
}

func (s *sink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	n := 0
	for _, b := range s.batches {
		n += len(b)
	}

	return n
}

func (s *sink) batchCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.batches)
}

func TestPublishBatchesAndRetries(t *testing.T) {
	t.Parallel()

	store := &memStore{}
	target := newSink(t)
	target.fail.Store(1)

	s := New(store, "example.ts.net")
	s.SetBackoff([]time.Duration{time.Millisecond})
	s.SetBatching(10, 20*time.Millisecond)
	t.Cleanup(s.Close)

	s.Reload([]types.LogStream{
		{ID: 1, Name: "siem", Destination: types.LogStreamHTTP, URL: target.srv.URL, Token: "tok", Enabled: true},
		{ID: 2, Name: "off", Destination: types.LogStreamHTTP, URL: target.srv.URL, Enabled: false},
	})

	for i := range 3 {
		s.Publish(event(uint64(i+1), "node.delete"))
	}

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, 3, target.count())
	}, 5*time.Second, 5*time.Millisecond)

	assert.Equal(t, 1, target.batchCount(), "three events within the flush wait make one batch")
	assert.Equal(t, "Bearer tok", target.auth[0])

	last := store.last(1)
	assert.True(t, last.OK)
	assert.Equal(t, 3, last.Entries)
	assert.Equal(t, 2, last.Attempts, "the first attempt got a 503")
	assert.Equal(t, "200", last.Status)
	assert.Empty(t, store.all(2), "a disabled stream ships nothing")
}

func TestReloadKeepsUnchangedWorkersAndStopsRemoved(t *testing.T) {
	t.Parallel()

	store := &memStore{}
	target := newSink(t)

	s := New(store, "")
	s.SetBatching(10, 10*time.Millisecond)
	t.Cleanup(s.Close)

	stream := types.LogStream{ID: 1, Name: "a", Destination: types.LogStreamHTTP, URL: target.srv.URL, Enabled: true}
	s.Reload([]types.LogStream{stream})

	first := s.workers[1]

	renamed := stream
	renamed.Name = "b"
	s.Reload([]types.LogStream{renamed})
	assert.Same(t, first, s.workers[1], "a rename keeps the worker")
	assert.Equal(t, "b", first.current().Name)

	retargeted := renamed
	retargeted.Token = "new"
	s.Reload([]types.LogStream{retargeted})
	assert.NotSame(t, first, s.workers[1], "a new token restarts the worker")

	s.Reload(nil)
	assert.Empty(t, s.workers)

	s.Publish(event(1, "x"))
	assert.Never(t, func() bool { return target.count() > 0 }, 50*time.Millisecond, 5*time.Millisecond,
		"nothing listens after removal")
}

func TestFullQueueDropsAndCounts(t *testing.T) {
	t.Parallel()

	store := &memStore{}
	target := newSink(t)

	s := New(store, "")
	s.SetBatching(2, 10*time.Millisecond)
	t.Cleanup(s.Close)

	// A worker whose queue is tiny: fill it without a worker draining it
	// by publishing before the worker gets scheduled is racy, so build
	// the worker by hand with a full queue instead.
	w := &worker{
		s:      s,
		stream: types.LogStream{ID: 9, Destination: types.LogStreamHTTP, URL: target.srv.URL},
		in:     make(chan Entry, 1),
	}
	w.offer(EntryFrom("", event(1, "a")))
	w.offer(EntryFrom("", event(2, "b")))
	w.offer(EntryFrom("", event(3, "c")))
	assert.Equal(t, int64(2), w.dropped.Load())

	s.wg.Go(w.run)
	w.stop()

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, 1, target.count())
	}, 5*time.Second, 5*time.Millisecond)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		assert.Equal(c, 2, store.last(9).Dropped)
	}, 5*time.Second, 5*time.Millisecond)
	assert.Equal(t, 1, store.last(9).Entries)
}

func TestTestRecordsRejection(t *testing.T) {
	t.Parallel()

	store := &memStore{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	s := New(store, "")
	t.Cleanup(s.Close)

	stream := types.LogStream{ID: 4, Name: "x", Destination: types.LogStreamSplunk, URL: srv.URL, Token: "t"}
	err := s.Test(t.Context(), stream)
	require.ErrorIs(t, err, ErrRejected)
	assert.Equal(t, "403", store.last(4).Status)
	assert.False(t, store.last(4).OK)

	s.Close()
	require.ErrorIs(t, s.Test(t.Context(), stream), ErrClosed)
}
