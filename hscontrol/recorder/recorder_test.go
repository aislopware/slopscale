package recorder_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/recorder"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/sessionrecording"
	"tailscale.com/tailcfg"
)

// memStore is the recorder's index in memory.
type memStore struct {
	mu   sync.Mutex
	next uint64
	rows map[types.SSHRecordingID]types.SSHRecording
}

func newMemStore() *memStore {
	return &memStore{rows: map[types.SSHRecordingID]types.SSHRecording{}}
}

func (m *memStore) CreateSSHRecording(rec types.SSHRecording) (types.SSHRecording, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.next++
	rec.ID = types.SSHRecordingID(m.next)
	m.rows[rec.ID] = rec

	return rec, nil
}

func (m *memStore) FinishSSHRecording(id types.SSHRecordingID, size int64, complete bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.rows[id]
	if !ok {
		return types.ErrSSHRecordingNotFound
	}

	now := time.Now()
	rec.EndedAt = &now
	rec.Size = size
	rec.Complete = complete
	m.rows[id] = rec

	return nil
}

func (m *memStore) GetSSHRecording(id types.SSHRecordingID) (types.SSHRecording, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	rec, ok := m.rows[id]
	if !ok {
		return types.SSHRecording{}, types.ErrSSHRecordingNotFound
	}

	return rec, nil
}

func (m *memStore) ListSSHRecordings(before types.SSHRecordingID, limit int) ([]types.SSHRecording, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []types.SSHRecording

	for id := types.SSHRecordingID(m.next); id > 0 && len(out) < limit; id-- {
		if before != 0 && id >= before {
			continue
		}

		if rec, ok := m.rows[id]; ok {
			out = append(out, rec)
		}
	}

	return out, nil
}

func (m *memStore) DeleteSSHRecording(id types.SSHRecordingID) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.rows[id]; !ok {
		return types.ErrSSHRecordingNotFound
	}

	delete(m.rows, id)

	return nil
}

func (m *memStore) ListSSHRecordingsBefore(cutoff time.Time) ([]types.SSHRecording, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []types.SSHRecording

	for _, rec := range m.rows {
		if rec.StartedAt.Before(cutoff) {
			out = append(out, rec)
		}
	}

	return out, nil
}

var errUploadBroken = errors.New("ssh server went away")

// serve runs the recorder's handler on a loopback listener and returns
// its address, the way a client would reach it over the tailnet.
func serve(t *testing.T, rec *recorder.Recorder) netip.AddrPort {
	t.Helper()

	srv := httptest.NewServer(rec.Handler())
	t.Cleanup(srv.Close)

	return netip.MustParseAddrPort(strings.TrimPrefix(srv.URL, "http://"))
}

func dial(ctx context.Context, network, address string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, network, address)
}

func header() sessionrecording.CastHeader {
	return sessionrecording.CastHeader{
		Version:     2,
		Width:       80,
		Height:      24,
		Timestamp:   time.Date(2026, 9, 7, 8, 0, 0, 0, time.UTC).Unix(),
		SrcNode:     "laptop.example.ts.net",
		SrcNodeID:   "stable-laptop",
		SrcNodeUser: "alice@example.com",
		SSHUser:     "root",
		LocalUser:   "root",
		Command:     "ls -la",
	}
}

// TestRecordSession proves the tailscale SSH client's own recorder
// connection uploads through the recorder: the v2 probe is refused so
// the client falls back to v1, the header names the session in the
// index, the stream lands in the file byte for byte, and the node the
// upload came from is named as the destination.
func TestRecordSession(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := newMemStore()
	server := types.Node{ID: 7, GivenName: "prod-db"}
	lookup := func(addr netip.Addr) (types.NodeView, bool) {
		if addr.IsLoopback() {
			return server.View(), true
		}

		return types.NodeView{}, false
	}

	rec := recorder.New(dir, 0, store, lookup)

	addr := serve(t, rec)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	w, attempts, errc, err := sessionrecording.ConnectToRecorder(ctx, []netip.AddrPort{addr}, dial)
	require.NoError(t, err)
	require.Len(t, attempts, 1)
	assert.Equal(t, addr.String(), attempts[0].Recorder.String())

	head, err := json.Marshal(header())
	require.NoError(t, err)

	_, err = w.Write(append(head, '\n'))
	require.NoError(t, err)

	events := []string{`[0.1,"o","$ ls -la\r\n"]`, `[0.5,"o","total 0\r\n"]`}
	for _, e := range events {
		_, err = io.WriteString(w, e+"\n")
		require.NoError(t, err)
	}

	require.NoError(t, w.Close())
	require.NoError(t, <-errc)

	var stored types.SSHRecording

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		rows, listErr := store.ListSSHRecordings(0, 10)
		require.NoError(c, listErr)
		require.Len(c, rows, 1)
		require.NotNil(c, rows[0].EndedAt, "the upload is finished")

		stored = rows[0]
	}, 5*time.Second, 10*time.Millisecond)

	assert.Equal(t, "laptop.example.ts.net", stored.SrcNode)
	assert.Equal(t, "stable-laptop", stored.SrcNodeID)
	assert.Equal(t, "alice@example.com", stored.SrcUser)
	assert.Equal(t, types.NodeID(7), stored.DstNodeID)
	assert.Equal(t, "prod-db", stored.DstNode)
	assert.Equal(t, "root", stored.SSHUser)
	assert.Equal(t, "ls -la", stored.Command)
	assert.Equal(t, time.Date(2026, 9, 7, 8, 0, 0, 0, time.UTC), stored.StartedAt)
	assert.True(t, stored.Complete)

	want := string(head) + "\n" + strings.Join(events, "\n") + "\n"
	assert.Equal(t, int64(len(want)), stored.Size)

	got, err := os.ReadFile(filepath.Join(dir, stored.Path))
	require.NoError(t, err)
	assert.Equal(t, want, string(got))

	file, err := rec.Open(stored)
	require.NoError(t, err)

	opened, err := io.ReadAll(file)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	assert.Equal(t, want, string(opened))

	require.NoError(t, rec.Delete(stored.ID))

	_, err = store.GetSSHRecording(stored.ID)
	require.ErrorIs(t, err, types.ErrSSHRecordingNotFound)
	assert.NoFileExists(t, filepath.Join(dir, stored.Path))
}

// TestInterruptedUpload proves a session whose upload breaks off is
// kept, marked incomplete, with what arrived.
func TestInterruptedUpload(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := newMemStore()

	rec := recorder.New(dir, 0, store, nil)

	addr := serve(t, rec)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	w, _, errc, err := sessionrecording.ConnectToRecorder(ctx, []netip.AddrPort{addr}, dial)
	require.NoError(t, err)

	head, err := json.Marshal(header())
	require.NoError(t, err)

	_, err = w.Write(append(head, '\n'))
	require.NoError(t, err)

	_, err = io.WriteString(w, `[0.1,"o","hello"]`+"\n")
	require.NoError(t, err)

	// The server indexes the session once it has the header; wait for
	// the row so the break lands mid-upload.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		rows, listErr := store.ListSSHRecordings(0, 10)
		require.NoError(c, listErr)
		require.Len(c, rows, 1)
	}, 5*time.Second, 10*time.Millisecond)

	// The client's writer is the pipe the request body reads from;
	// failing it is what a dying SSH server does to the upload.
	pipe, ok := w.(*io.PipeWriter)
	require.True(t, ok)
	require.NoError(t, pipe.CloseWithError(errUploadBroken))

	err = <-errc
	require.Error(t, err, "the client sees its upload fail")

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		rows, listErr := store.ListSSHRecordings(0, 10)
		require.NoError(c, listErr)
		require.Len(c, rows, 1)
		require.NotNil(c, rows[0].EndedAt)
		assert.False(c, rows[0].Complete)
		assert.Positive(c, rows[0].Size)
	}, 5*time.Second, 10*time.Millisecond)
}

// TestBadHeader proves an upload without a JSON header line is refused
// and leaves nothing behind.
func TestBadHeader(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := newMemStore()

	rec := recorder.New(dir, 0, store, nil)

	srv := httptest.NewServer(rec.Handler())
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+recorder.RecordPath,
		strings.NewReader("not json\n"))
	require.NoError(t, err)

	resp, err := srv.Client().Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	rows, err := store.ListSSHRecordings(0, 10)
	require.NoError(t, err)
	assert.Empty(t, rows)

	entries, err := os.ReadDir(dir)
	if !errors.Is(err, os.ErrNotExist) {
		require.NoError(t, err)
		assert.Empty(t, entries, "no file for a refused upload")
	}
}

// TestSweep proves retention removes old recordings, rows and files,
// and keeps the rest.
func TestSweep(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := newMemStore()

	rec := recorder.New(dir, time.Hour, store, nil)

	old, err := store.CreateSSHRecording(types.SSHRecording{
		StartedAt: time.Now().Add(-2 * time.Hour), Path: "old.cast",
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "old.cast"), []byte("x"), 0o600))

	fresh, err := store.CreateSSHRecording(types.SSHRecording{
		StartedAt: time.Now(), Path: "fresh.cast",
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fresh.cast"), []byte("y"), 0o600))

	require.NoError(t, rec.Sweep())

	_, err = store.GetSSHRecording(old.ID)
	require.ErrorIs(t, err, types.ErrSSHRecordingNotFound)
	assert.NoFileExists(t, filepath.Join(dir, "old.cast"))

	_, err = store.GetSSHRecording(fresh.ID)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dir, "fresh.cast"))
}

var _ tailcfg.SSHRecordingAttempt
