// Package spool keeps reports on disk until the server acknowledges them,
// so an outage of the server, the network or the agent itself costs
// nothing but delay, up to a size bound.
package spool

import (
	"bytes"
	"cmp"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/aislopware/slopscale/hscontrol/traffic"
	"github.com/klauspost/compress/zstd"
)

const (
	stateFile   = "state.json"
	reportExt   = ".json.zst"
	instanceLen = 16
	seqDigits   = 20
)

var (
	// ErrEmpty is returned by [Spool.Oldest] when nothing is spooled.
	ErrEmpty = errors.New("spool is empty")
	// ErrTooLarge is returned by [Decode] for a report over
	// [traffic.MaxReportBytes].
	ErrTooLarge = errors.New("report exceeds the size limit")
)

// state is what survives a restart besides the reports.
type state struct {
	Instance string `json:"instance"`
	NextSeq  uint64 `json:"nextSeq"`
	// Dropped counts entries of reports discarded before delivery, not
	// yet carried by a report.
	Dropped uint64 `json:"dropped"`
}

// Spool is a bounded queue of encoded reports in a directory. It is safe
// for concurrent use.
type Spool struct {
	dir      string
	maxBytes int64

	mu    sync.Mutex
	state state
	files []spooled // oldest first
	size  int64
}

type spooled struct {
	seq  uint64
	size int64
}

// Open opens or creates the spool in dir, keeping at most maxBytes of
// reports.
func Open(dir string, maxBytes int64) (*Spool, error) {
	err := os.MkdirAll(dir, 0o700)
	if err != nil {
		return nil, fmt.Errorf("creating the spool directory: %w", err)
	}

	s := &Spool{dir: dir, maxBytes: maxBytes}

	err = s.load()
	if err != nil {
		return nil, err
	}

	return s, nil
}

// Instance is the spool's random instance id, stable across restarts.
func (s *Spool) Instance() string {
	return s.state.Instance
}

// Len is the number of spooled reports.
func (s *Spool) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return len(s.files)
}

// Enqueue stamps r with the instance, the next sequence number and the
// count of entries dropped so far, and spools it. When the spool is then
// over its bound, the oldest reports are discarded and their entries
// counted for the next report.
func (s *Spool) Enqueue(r *traffic.Report) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	r.Instance = s.state.Instance
	r.Seq = s.state.NextSeq
	r.Dropped += s.state.Dropped

	body, err := Encode(r)
	if err != nil {
		return err
	}

	err = writeAtomic(s.path(r.Seq), body)
	if err != nil {
		return err
	}

	s.files = append(s.files, spooled{seq: r.Seq, size: int64(len(body))})
	s.size += int64(len(body))
	s.state.NextSeq++
	s.state.Dropped = 0

	for s.size > s.maxBytes && len(s.files) > 1 {
		err = s.discardOldestLocked()
		if err != nil {
			return err
		}
	}

	return s.saveState()
}

// Oldest returns the oldest spooled report's sequence number and encoded
// body, or [ErrEmpty].
func (s *Spool) Oldest() (uint64, []byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for len(s.files) > 0 {
		seq := s.files[0].seq

		body, err := os.ReadFile(s.path(seq))
		if err == nil {
			return seq, body, nil
		}

		if !errors.Is(err, os.ErrNotExist) {
			return 0, nil, fmt.Errorf("reading spooled report %d: %w", seq, err)
		}

		s.size -= s.files[0].size
		s.files = s.files[1:]
	}

	return 0, nil, ErrEmpty
}

// Ack removes every report up to and including seq.
func (s *Spool) Ack(seq uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for len(s.files) > 0 && s.files[0].seq <= seq {
		err := os.Remove(s.path(s.files[0].seq))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("removing acknowledged report %d: %w", s.files[0].seq, err)
		}

		s.size -= s.files[0].size
		s.files = s.files[1:]
	}

	return nil
}

// Reject discards the report seq the server refused for good, counting its
// entries as dropped.
func (s *Spool) Reject(seq uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.files) == 0 || s.files[0].seq != seq {
		return nil
	}

	err := s.discardOldestLocked()
	if err != nil {
		return err
	}

	return s.saveState()
}

func (s *Spool) load() error {
	raw, err := os.ReadFile(filepath.Join(s.dir, stateFile))

	switch {
	case errors.Is(err, os.ErrNotExist):
		var id [instanceLen]byte

		_, _ = rand.Read(id[:])
		s.state = state{Instance: hex.EncodeToString(id[:]), NextSeq: 1}
	case err != nil:
		return fmt.Errorf("reading the spool state: %w", err)
	default:
		err = json.Unmarshal(raw, &s.state)
		if err != nil || s.state.Instance == "" {
			return fmt.Errorf("spool state %s is corrupt: %w", filepath.Join(s.dir, stateFile), err)
		}
	}

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return fmt.Errorf("listing the spool: %w", err)
	}

	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, reportExt) {
			continue
		}

		seq, err := strconv.ParseUint(strings.TrimSuffix(name, reportExt), 10, 64)
		if err != nil {
			continue
		}

		info, err := e.Info()
		if err != nil {
			return fmt.Errorf("reading %s: %w", name, err)
		}

		s.files = append(s.files, spooled{seq: seq, size: info.Size()})
		s.size += info.Size()
		s.state.NextSeq = max(s.state.NextSeq, seq+1)
	}

	slices.SortFunc(s.files, func(a, b spooled) int { return cmp.Compare(a.seq, b.seq) })

	return s.saveState()
}

// discardOldestLocked removes the oldest report, counting its entries and
// the drops it carried as dropped.
func (s *Spool) discardOldestLocked() error {
	oldest := s.files[0]

	s.state.Dropped += entriesIn(s.path(oldest.seq))

	err := os.Remove(s.path(oldest.seq))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("discarding spooled report %d: %w", oldest.seq, err)
	}

	s.files = s.files[1:]
	s.size -= oldest.size

	return nil
}

// entriesIn counts the entries of a spooled report and the drops it
// carried; an unreadable report counts nothing.
func entriesIn(path string) uint64 {
	body, err := os.ReadFile(path)
	if err != nil {
		return 0
	}

	r, err := Decode(body)
	if err != nil {
		return 0
	}

	return uint64(len(r.Flows)+len(r.Queries)) + r.Dropped
}

func (s *Spool) path(seq uint64) string {
	return filepath.Join(s.dir, fmt.Sprintf("%0*d%s", seqDigits, seq, reportExt))
}

func (s *Spool) saveState() error {
	raw, err := json.Marshal(s.state)
	if err != nil {
		return fmt.Errorf("encoding the spool state: %w", err)
	}

	return writeAtomic(filepath.Join(s.dir, stateFile), raw)
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}

	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}

	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}

	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}

	if err != nil {
		_ = os.Remove(tmp.Name())

		return fmt.Errorf("writing %s: %w", path, err)
	}

	return nil
}

// Encode returns a report as the zstd-compressed JSON body the server
// takes.
func Encode(r *traffic.Report) ([]byte, error) {
	var buf bytes.Buffer

	enc, err := zstd.NewWriter(&buf, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, fmt.Errorf("creating the encoder: %w", err)
	}

	err = json.NewEncoder(enc).Encode(r)
	if err != nil {
		_ = enc.Close()

		return nil, fmt.Errorf("encoding the report: %w", err)
	}

	err = enc.Close()
	if err != nil {
		return nil, fmt.Errorf("compressing the report: %w", err)
	}

	return buf.Bytes(), nil
}

// Decode reverses [Encode], bounded by [traffic.MaxReportBytes].
func Decode(body []byte) (*traffic.Report, error) {
	dec, err := zstd.NewReader(bytes.NewReader(body), zstd.WithDecoderMaxMemory(traffic.MaxReportBytes))
	if err != nil {
		return nil, fmt.Errorf("creating the decoder: %w", err)
	}
	defer dec.Close()

	raw, err := io.ReadAll(io.LimitReader(dec, traffic.MaxReportBytes+1))
	if err != nil {
		return nil, fmt.Errorf("decompressing the report: %w", err)
	}

	if len(raw) > traffic.MaxReportBytes {
		return nil, ErrTooLarge
	}

	var r traffic.Report

	err = json.Unmarshal(raw, &r)
	if err != nil {
		return nil, fmt.Errorf("decoding the report: %w", err)
	}

	return &r, nil
}
