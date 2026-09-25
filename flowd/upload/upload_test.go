package upload

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aislopware/slopscale/flowd/spool"
	"github.com/aislopware/slopscale/hscontrol/traffic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeServer is a report endpoint whose answers the test scripts.
type fakeServer struct {
	mu       sync.Mutex
	statuses []int // answered in order, then 200
	applied  map[string]uint64
	tokens   []string
	reports  []*traffic.Report
	extraSeq uint64
	config   traffic.Config
	valid    func(token string) bool
}

func (f *fakeServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if r.URL.Path != traffic.ReportPath || r.Method != http.MethodPost {
		http.NotFound(w, r)

		return
	}

	token := r.Header.Get("Authorization")
	f.tokens = append(f.tokens, token)

	if f.valid != nil && !f.valid(token) {
		http.Error(w, "bad token", http.StatusUnauthorized)

		return
	}

	if len(f.statuses) > 0 {
		status := f.statuses[0]
		f.statuses = f.statuses[1:]

		http.Error(w, "scripted", status)

		return
	}

	if r.Header.Get("Content-Encoding") != "zstd" {
		http.Error(w, "want zstd", http.StatusBadRequest)

		return
	}

	body, _ := io.ReadAll(r.Body)

	report, err := spool.Decode(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	f.reports = append(f.reports, report)

	if report.Seq > f.applied[report.Instance] {
		f.applied[report.Instance] = report.Seq
	}

	_ = json.NewEncoder(w).Encode(traffic.Response{
		Seq:    f.applied[report.Instance] + f.extraSeq,
		Config: f.config,
	})
}

func jwt(exp time.Time) string {
	payload, _ := json.Marshal(map[string]int64{"exp": exp.Unix()})

	return "e30." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
}

type harness struct {
	server   *fakeServer
	spool    *spool.Spool
	uploader *Uploader
	fetches  atomic.Int32
	configs  []traffic.Config
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	h := &harness{
		server: &fakeServer{applied: map[string]uint64{}, config: traffic.Config{SNI: true, ReportInterval: 60}},
	}

	ts := httptest.NewServer(h.server)
	t.Cleanup(ts.Close)

	sp, err := spool.Open(t.TempDir(), 1<<20)
	require.NoError(t, err)

	h.spool = sp

	tokens := NewTokens(func(context.Context) (string, error) {
		n := h.fetches.Add(1)

		return fmt.Sprintf("%s#%d", jwt(time.Now().Add(time.Hour)), n), nil
	})

	h.uploader = New(ts.URL+"/", ts.Client(), tokens, sp, func(c traffic.Config) {
		h.configs = append(h.configs, c)
	}, slog.New(slog.DiscardHandler))

	return h
}

func (h *harness) enqueue(t *testing.T, n int) {
	t.Helper()

	for range n {
		require.NoError(t, h.spool.Enqueue(&traffic.Report{Version: "test"}))
	}
}

func TestDrainDeliversInOrder(t *testing.T) {
	h := newHarness(t)
	h.enqueue(t, 3)

	wait, err := h.uploader.Drain(t.Context())
	require.NoError(t, err)
	assert.Zero(t, wait)

	require.Len(t, h.server.reports, 3)

	for i, r := range h.server.reports {
		assert.Equal(t, uint64(i+1), r.Seq)
		assert.Equal(t, h.spool.Instance(), r.Instance)
	}

	assert.Zero(t, h.spool.Len())
	assert.Equal(t, int32(1), h.fetches.Load(), "the token is reused while valid")
	require.Len(t, h.configs, 3)
	assert.True(t, h.configs[0].SNI)
}

// TestServerAheadStartsANewInstance: the server has applied later reports
// of this instance than the spool ever sent, as when a gateway's state
// directory was cloned or restored. Acknowledging up to the server's
// sequence would delete reports it never got; instead the spool starts a
// new instance and everything arrives, once.
func TestServerAheadStartsANewInstance(t *testing.T) {
	h := newHarness(t)
	h.enqueue(t, 3)

	old := h.spool.Instance()
	h.server.applied[old] = 10 // the clone got there first

	_, err := h.uploader.Drain(t.Context())
	require.NoError(t, err)

	assert.NotEqual(t, old, h.spool.Instance())
	assert.Zero(t, h.spool.Len())

	var applied []uint64

	for _, r := range h.server.reports {
		if r.Instance == h.spool.Instance() {
			applied = append(applied, r.Seq)
		}
	}

	assert.Len(t, applied, 3, "every spooled report arrives under the new instance")
	assert.Equal(t, uint64(10), h.server.applied[old], "nothing more is taken for the old instance")
}

// TestServerAlwaysAheadIsNotBelieved has a server that claims a later
// sequence for any instance: the uploader starts a new instance once, then
// backs off instead of spinning.
func TestServerAlwaysAheadIsNotBelieved(t *testing.T) {
	h := newHarness(t)
	h.enqueue(t, 2)
	h.server.extraSeq = 5

	wait, err := h.uploader.Drain(t.Context())
	require.ErrorIs(t, err, errServerAhead)
	assert.Equal(t, refusedBackoff, wait)
	assert.Equal(t, 2, h.spool.Len(), "the reports stay spooled")
	assert.Len(t, h.server.reports, 2)
}

// TestRejectionIsReportedUntilDelivered remembers why the server refused a
// report until a report built after the refusal has been delivered, so the
// refusal reaches the server's view of the gateway.
func TestRejectionIsReportedUntilDelivered(t *testing.T) {
	h := newHarness(t)
	h.enqueue(t, 1)
	h.server.statuses = []int{http.StatusBadRequest}

	_, err := h.uploader.Drain(t.Context())
	require.NoError(t, err)
	assert.Contains(t, h.uploader.Rejection(), "refused report 1 (400 Bad Request): scripted")

	// The next report is built with the rejection in its status, and once
	// it is delivered the rejection is forgotten.
	h.enqueue(t, 1)

	_, err = h.uploader.Drain(t.Context())
	require.NoError(t, err)
	assert.Empty(t, h.uploader.Rejection())
}

// TestSkewFromTheServersDate reads the server's clock from the Date header
// of any response.
func TestSkewFromTheServersDate(t *testing.T) {
	h := newHarness(t)
	assert.Zero(t, h.uploader.Skew())

	sent := time.Now()
	h.uploader.observeClock(sent.Add(10*time.Minute).UTC().Format(http.TimeFormat), sent, sent)
	assert.InDelta(t, float64(10*time.Minute), float64(h.uploader.Skew()), float64(time.Second))

	h.uploader.observeClock("not a date", sent, sent)
	assert.InDelta(t, float64(10*time.Minute), float64(h.uploader.Skew()), float64(time.Second),
		"an unreadable header changes nothing")

	// A real exchange with a server whose clock agrees brings it back.
	h.enqueue(t, 1)

	_, err := h.uploader.Drain(t.Context())
	require.NoError(t, err)
	assert.InDelta(t, 0, float64(h.uploader.Skew()), float64(1500*time.Millisecond))
}

func TestUnauthorizedRefreshesTokenOnce(t *testing.T) {
	h := newHarness(t)
	h.enqueue(t, 1)
	h.server.statuses = []int{http.StatusUnauthorized}

	_, err := h.uploader.Drain(t.Context())
	require.NoError(t, err)

	assert.Equal(t, int32(2), h.fetches.Load())
	require.Len(t, h.server.tokens, 2)
	assert.NotEqual(t, h.server.tokens[0], h.server.tokens[1])

	// A server that keeps refusing gets a backoff, not a loop.
	h.enqueue(t, 1)
	h.server.valid = func(string) bool { return false }

	wait, err := h.uploader.Drain(t.Context())
	require.ErrorIs(t, err, ErrUnauthorized)
	assert.Equal(t, refusedBackoff, wait)
	assert.Equal(t, 1, h.spool.Len(), "the report stays spooled")
}

func TestForbiddenKeepsReports(t *testing.T) {
	h := newHarness(t)
	h.enqueue(t, 2)
	h.server.statuses = []int{http.StatusForbidden}

	wait, err := h.uploader.Drain(t.Context())
	require.ErrorIs(t, err, ErrForbidden)
	assert.Contains(t, err.Error(), "403", "the log names the status the operator looks up")
	assert.Equal(t, refusedBackoff, wait)
	assert.Equal(t, 2, h.spool.Len())
}

func TestRejectedReportIsDropped(t *testing.T) {
	h := newHarness(t)
	h.enqueue(t, 2)
	h.server.statuses = []int{http.StatusBadRequest}

	_, err := h.uploader.Drain(t.Context())
	require.NoError(t, err)

	require.Len(t, h.server.reports, 1)
	assert.Equal(t, uint64(2), h.server.reports[0].Seq)
	assert.Len(t, h.configs, 1, "a rejection carries no configuration")
}

// TestRunRetriesServerErrors runs the loop against a server failing twice:
// it backs off exponentially and then delivers.
func TestRunRetriesServerErrors(t *testing.T) {
	h := newHarness(t)
	h.enqueue(t, 1)
	h.server.statuses = []int{http.StatusBadGateway, http.StatusServiceUnavailable}

	var (
		mu     sync.Mutex
		sleeps []time.Duration
	)

	h.uploader.sleep = func(_ context.Context, d time.Duration) {
		mu.Lock()

		sleeps = append(sleeps, d)
		mu.Unlock()
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		h.uploader.Run(ctx)
		close(done)
	}()

	require.Eventually(t, func() bool { return h.spool.Len() == 0 }, 5*time.Second, 10*time.Millisecond)
	cancel()
	<-done

	mu.Lock()
	defer mu.Unlock()

	assert.Equal(t, []time.Duration{minBackoff, 2 * minBackoff}, sleeps)
}

// TestRunStopsWhileRetrying stops the agent while the server is failing:
// Run returns instead of retrying a cancelled request in a tight loop,
// which once logged millions of lines after a SIGTERM.
func TestRunStopsWhileRetrying(t *testing.T) {
	h := newHarness(t)
	h.enqueue(t, 1)

	for i := range 64 {
		h.server.statuses = append(h.server.statuses, http.StatusBadGateway+i%2)
	}

	var attempts atomic.Int32

	h.uploader.sleep = func(ctx context.Context, d time.Duration) {
		attempts.Add(1)
		sleepCtx(ctx, d)
	}

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		h.uploader.Run(ctx)
		close(done)
	}()

	require.Eventually(t, func() bool { return attempts.Load() > 0 }, 5*time.Second, time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run kept going after its context ended")
	}

	assert.LessOrEqual(t, attempts.Load(), int32(2), "no retries once the context ended")
}

func TestTokenExpiry(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)

	assert.Equal(t, time.Unix(1_800_000_300, 0), tokenExpiry(jwt(time.Unix(1_800_000_300, 0)), now))
	assert.Equal(t, now.Add(2*tokenMargin), tokenExpiry("not-a-jwt", now))
	assert.Equal(t, now.Add(2*tokenMargin), tokenExpiry("a.!!!.c", now))

	var fetches int

	tokens := NewTokens(func(context.Context) (string, error) {
		fetches++

		return jwt(now.Add(90 * time.Second)), nil
	})
	tokens.now = func() time.Time { return now }

	_, err := tokens.Token(t.Context())
	require.NoError(t, err)

	_, err = tokens.Token(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 1, fetches)

	// Inside the margin before expiry a new token is fetched.
	tokens.now = func() time.Time { return now.Add(31 * time.Second) }
	_, err = tokens.Token(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 2, fetches)
}
