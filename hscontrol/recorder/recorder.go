// Package recorder is the embedded SSH session recorder: an HTTP service
// that speaks the tsrecorder upload protocol, writes each session to an
// asciinema file and indexes it in the database. The server runs it on a
// tsnet node so that tagged SSH policy can name it; see
// docs/ref/ssh-recording.md.
package recorder

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/rs/zerolog/log"
	"tailscale.com/sessionrecording"
)

const (
	// RecordPath is the v1 upload endpoint the tailscale SSH server
	// posts a session to.
	RecordPath = "/record"

	// Port is the port a recorder listens on.
	Port = types.SSHRecorderPort

	// headerLimit bounds the asciinema header line.
	headerLimit = 64 << 10

	// sweepInterval is how often retention runs.
	sweepInterval = time.Hour

	// dirMode and fileMode keep recordings readable by the server only.
	dirMode  = 0o700
	fileMode = 0o600

	nameRandomBytes = 4
)

var (
	// ErrHeaderTooLong is returned for a header line over headerLimit.
	ErrHeaderTooLong = errors.New("recording header too long")
	// ErrNoDir is returned for an upload or a download when the server
	// has no recordings directory.
	ErrNoDir = errors.New("recording directory not configured")
)

// Store is the database side of the recorder.
type Store interface {
	CreateSSHRecording(rec types.SSHRecording) (types.SSHRecording, error)
	FinishSSHRecording(id types.SSHRecordingID, size int64, complete bool) error
	GetSSHRecording(id types.SSHRecordingID) (types.SSHRecording, error)
	ListSSHRecordings(before types.SSHRecordingID, limit int) ([]types.SSHRecording, error)
	DeleteSSHRecording(id types.SSHRecordingID) error
	ListSSHRecordingsBefore(cutoff time.Time) ([]types.SSHRecording, error)
}

// NodeLookup finds the node behind a tailnet address; the recorder uses
// it to name the node a session ran on.
type NodeLookup func(addr netip.Addr) (types.NodeView, bool)

// Recorder receives session uploads and keeps the files.
type Recorder struct {
	dir       string
	retention time.Duration
	store     Store
	nodes     NodeLookup
}

// New builds a recorder writing to dir. The directory is created when
// the first session arrives, so a server that only reads old
// recordings never touches it; without one, uploads and downloads
// fail with [ErrNoDir].
func New(dir string, retention time.Duration, store Store, nodes NodeLookup) *Recorder {
	if nodes == nil {
		nodes = func(netip.Addr) (types.NodeView, bool) { return types.NodeView{}, false }
	}

	return &Recorder{dir: dir, retention: retention, store: store, nodes: nodes}
}

// Handler is the upload service. It answers the v2 probe with 404 so
// that clients fall back to the v1 streaming upload, which it accepts.
func (r *Recorder) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("HEAD /v2/record", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "v2 uploads not supported", http.StatusNotFound)
	})
	mux.HandleFunc("HEAD /v2/event", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "events not supported", http.StatusNotFound)
	})
	mux.HandleFunc("POST "+RecordPath, r.record)

	return mux
}

// Server wraps the handler in a server with no read or write deadline,
// because a session upload lasts as long as the session.
func (r *Recorder) Server() *http.Server {
	return &http.Server{
		Handler:           r.Handler(),
		ReadHeaderTimeout: types.HTTPTimeout,
	}
}

// writeStream writes the header line and then the stream, returning how
// many bytes reached the file.
func writeStream(file io.Writer, header []byte, stream io.Reader) (int64, error) {
	n, err := file.Write(header)
	if err != nil {
		return int64(n), fmt.Errorf("writing recording header: %w", err)
	}

	copied, err := io.Copy(file, stream)
	if err != nil {
		return int64(n) + copied, fmt.Errorf("writing recording stream: %w", err)
	}

	return int64(n) + copied, nil
}

// fileName is the session's file: the start time and a random suffix so
// that two sessions in the same second do not collide.
func fileName() string {
	suffix := make([]byte, nameRandomBytes)
	_, _ = rand.Read(suffix)

	return time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(suffix) + ".cast"
}

// remoteAddr is the address part of a host:port remote, or the zero
// address when it is not one.
func remoteAddr(remote string) netip.Addr {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}

	addr, err := netip.ParseAddr(strings.TrimSpace(host))
	if err != nil {
		return netip.Addr{}
	}

	return addr.Unmap()
}

// Get reads one recording's index row.
func (r *Recorder) Get(id types.SSHRecordingID) (types.SSHRecording, error) {
	return r.store.GetSSHRecording(id)
}

// List pages through recordings, newest first.
func (r *Recorder) List(before types.SSHRecordingID, limit int) ([]types.SSHRecording, error) {
	return r.store.ListSSHRecordings(before, limit)
}

// Open returns the file of a recording for download.
func (r *Recorder) Open(rec types.SSHRecording) (io.ReadCloser, error) {
	if r.dir == "" {
		return nil, ErrNoDir
	}

	file, err := os.Open(r.path(rec))
	if err != nil {
		return nil, fmt.Errorf("opening recording %d: %w", rec.ID, err)
	}

	return file, nil
}

// Delete removes a recording's row and file. A missing file is not an
// error, so a recording whose file was cleaned by hand can still go.
func (r *Recorder) Delete(id types.SSHRecordingID) error {
	rec, err := r.store.GetSSHRecording(id)
	if err != nil {
		return err
	}

	err = r.store.DeleteSSHRecording(id)
	if err != nil {
		return err
	}

	if r.dir == "" {
		return nil
	}

	err = os.Remove(r.path(rec))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing recording file: %w", err)
	}

	return nil
}

// Sweep deletes recordings older than the retention. It is a no-op
// without one.
func (r *Recorder) Sweep() error {
	if r.retention <= 0 {
		return nil
	}

	old, err := r.store.ListSSHRecordingsBefore(time.Now().Add(-r.retention))
	if err != nil {
		return err
	}

	for _, rec := range old {
		err = r.Delete(rec.ID)
		if err != nil {
			return err
		}

		log.Info().Uint64("recording", uint64(rec.ID)).Msg("expired SSH recording deleted")
	}

	return nil
}

// RunSweeper runs Sweep at start and then on an interval until the
// context ends.
func (r *Recorder) RunSweeper(ctx context.Context) {
	if r.retention <= 0 {
		return
	}

	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()

	for {
		err := r.Sweep()
		if err != nil {
			log.Error().Err(err).Msg("sweeping SSH recordings")
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// record takes one upload: the header line names the session, the rest
// is the terminal stream, written to the file as it arrives.
func (r *Recorder) record(w http.ResponseWriter, req *http.Request) {
	body := bufio.NewReaderSize(req.Body, headerLimit)

	line, err := body.ReadSlice('\n')
	if err != nil {
		if errors.Is(err, bufio.ErrBufferFull) {
			err = ErrHeaderTooLong
		}

		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	var header sessionrecording.CastHeader

	err = json.Unmarshal(line, &header)
	if err != nil {
		http.Error(w, "invalid recording header: "+err.Error(), http.StatusBadRequest)

		return
	}

	rec, file, err := r.begin(header, req.RemoteAddr)
	if err != nil {
		log.Error().Err(err).Str("srcNode", header.SrcNode).Msg("starting SSH recording")
		http.Error(w, err.Error(), http.StatusInternalServerError)

		return
	}

	written, copyErr := writeStream(file, line, body)
	closeErr := file.Close()

	complete := copyErr == nil && closeErr == nil

	err = r.store.FinishSSHRecording(rec.ID, written, complete)
	if err != nil {
		log.Error().Err(err).Uint64("recording", uint64(rec.ID)).Msg("finishing SSH recording")
	}

	e := log.Info().
		Uint64("recording", uint64(rec.ID)).
		Str("srcNode", rec.SrcNode).
		Str("dstNode", rec.DstNode).
		Str("sshUser", rec.SSHUser).
		Int64("bytes", written).
		Bool("complete", complete)
	if copyErr != nil {
		e = e.AnErr("uploadError", copyErr)
	}

	e.Msg("SSH session recorded")

	if !complete {
		// The upload broke off; the client has gone, so the status is
		// for the log only.
		http.Error(w, "upload interrupted", http.StatusBadRequest)

		return
	}

	w.WriteHeader(http.StatusOK)
}

// begin indexes the session and opens its file.
func (r *Recorder) begin(header sessionrecording.CastHeader, remote string) (types.SSHRecording, *os.File, error) {
	rec := types.SSHRecording{
		StartedAt: time.Now().UTC(),
		SrcNode:   header.SrcNode,
		SrcNodeID: string(header.SrcNodeID),
		SrcUser:   header.SrcNodeUser,
		SSHUser:   header.SSHUser,
		LocalUser: header.LocalUser,
		Command:   header.Command,
		Path:      fileName(),
	}

	if header.Timestamp > 0 {
		rec.StartedAt = time.Unix(header.Timestamp, 0).UTC()
	}

	if node, ok := r.nodes(remoteAddr(remote)); ok {
		rec.DstNodeID = node.ID()
		rec.DstNode = node.GivenName()
	}

	if r.dir == "" {
		return types.SSHRecording{}, nil, ErrNoDir
	}

	err := os.MkdirAll(r.dir, dirMode)
	if err != nil {
		return types.SSHRecording{}, nil, fmt.Errorf("creating recordings directory: %w", err)
	}

	file, err := os.OpenFile(filepath.Join(r.dir, rec.Path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return types.SSHRecording{}, nil, fmt.Errorf("creating recording file: %w", err)
	}

	stored, err := r.store.CreateSSHRecording(rec)
	if err != nil {
		_ = file.Close()
		_ = os.Remove(filepath.Join(r.dir, rec.Path))

		return types.SSHRecording{}, nil, fmt.Errorf("indexing recording: %w", err)
	}

	return stored, file, nil
}

// path is the recording's file, kept inside the directory whatever the
// stored name says.
func (r *Recorder) path(rec types.SSHRecording) string {
	return filepath.Join(r.dir, filepath.Base(rec.Path))
}
