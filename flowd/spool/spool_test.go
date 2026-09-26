package spool

import (
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/traffic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func report(flows int) *traffic.Report {
	r := &traffic.Report{Version: "test"}

	for i := range flows {
		r.Flows = append(r.Flows, traffic.Flow{
			Bucket: 60, Src: netip.MustParseAddr("100.64.0.3"),
			Dst:   netip.AddrFrom4([4]byte{203, 0, 113, byte(i)}),
			Proto: 6, Port: uint16(i), TxBytes: uint64(i),
		})
	}

	return r
}

func TestSequenceAndAck(t *testing.T) {
	dir := t.TempDir()

	s, err := Open(dir, 1<<20)
	require.NoError(t, err)

	instance := s.Instance()
	require.Len(t, instance, 32)

	for range 3 {
		require.NoError(t, s.Enqueue(report(2)))
	}

	seq, body, err := s.Oldest()
	require.NoError(t, err)
	assert.Equal(t, uint64(1), seq)

	decoded, err := Decode(body)
	require.NoError(t, err)
	assert.Equal(t, instance, decoded.Instance)
	assert.Equal(t, uint64(1), decoded.Seq)
	assert.Len(t, decoded.Flows, 2)

	require.NoError(t, s.Ack(2))

	seq, _, err = s.Oldest()
	require.NoError(t, err)
	assert.Equal(t, uint64(3), seq)

	// A restart keeps the instance, the queue and the sequence.
	s, err = Open(dir, 1<<20)
	require.NoError(t, err)
	assert.Equal(t, instance, s.Instance())
	assert.Equal(t, 1, s.Len())

	r := report(1)
	require.NoError(t, s.Enqueue(r))
	assert.Equal(t, uint64(4), r.Seq)

	require.NoError(t, s.Ack(10))

	_, _, err = s.Oldest()
	require.ErrorIs(t, err, ErrEmpty)
}

// TestBoundDropsOldest fills a small spool: the oldest reports go, their
// entries are counted, and the count rides on the next report.
func TestBoundDropsOldest(t *testing.T) {
	dir := t.TempDir()

	first := report(200)
	body, err := Encode(first)
	require.NoError(t, err)

	// Room for about two reports.
	s, err := Open(dir, int64(len(body))*5/2)
	require.NoError(t, err)

	for range 4 {
		require.NoError(t, s.Enqueue(report(200)))
	}

	assert.Equal(t, 2, s.Len())

	seq, _, err := s.Oldest()
	require.NoError(t, err)
	assert.Equal(t, uint64(3), seq, "the oldest reports were discarded")

	// Report 1 went while report 3 was spooled, so report 4 says so;
	// report 2 went while report 4 was spooled, so the next one does.
	require.NoError(t, s.Ack(3))
	_, body4, err := s.Oldest()
	require.NoError(t, err)

	fourth, err := Decode(body4)
	require.NoError(t, err)
	assert.Equal(t, uint64(200), fourth.Dropped)

	next := report(1)
	require.NoError(t, s.Enqueue(next))
	assert.Equal(t, uint64(200), next.Dropped)

	// The count survives a restart when no report carried it yet.
	require.NoError(t, s.Enqueue(report(200)))
	require.NoError(t, s.Enqueue(report(200)))
	s, err = Open(dir, int64(len(body))*5/2)
	require.NoError(t, err)

	after := report(0)
	require.NoError(t, s.Enqueue(after))
	assert.Positive(t, after.Dropped)
}

func TestRejectCountsEntries(t *testing.T) {
	s, err := Open(t.TempDir(), 1<<20)
	require.NoError(t, err)

	require.NoError(t, s.Enqueue(report(5)))
	require.NoError(t, s.Enqueue(report(1)))

	// Rejecting anything but the oldest is a no-op.
	require.NoError(t, s.Reject(2))
	assert.Equal(t, 2, s.Len())

	require.NoError(t, s.Reject(1))
	assert.Equal(t, 1, s.Len())

	next := report(0)
	require.NoError(t, s.Enqueue(next))
	assert.Equal(t, uint64(5), next.Dropped)
}

// TestCorruptStateStartsANewInstance: an unreadable state no longer stops
// the agent for good. The spool starts a new instance, keeps the reports
// already spooled (stamped with the old one) and numbers after them.
func TestCorruptStateStartsANewInstance(t *testing.T) {
	dir := t.TempDir()

	s, err := Open(dir, 1<<20)
	require.NoError(t, err)

	old := s.Instance()

	require.NoError(t, s.Enqueue(report(2)))
	require.NoError(t, os.WriteFile(filepath.Join(dir, stateFile), []byte("{"), 0o600))

	s, err = Open(dir, 1<<20)
	require.NoError(t, err)
	require.Error(t, s.Recovered())
	assert.NotEqual(t, old, s.Instance())
	assert.FileExists(t, filepath.Join(dir, stateFile+".corrupt"))

	seq, body, err := s.Oldest()
	require.NoError(t, err)
	assert.Equal(t, uint64(1), seq)

	kept, err := Decode(body)
	require.NoError(t, err)
	assert.Equal(t, old, kept.Instance)

	next := report(0)
	require.NoError(t, s.Enqueue(next))
	assert.Equal(t, uint64(2), next.Seq, "numbering continues past the kept report")

	s, err = Open(dir, 1<<20)
	require.NoError(t, err)
	require.NoError(t, s.Recovered(), "the new state is readable")

	// A state without an instance is as unusable as one that is not JSON.
	require.NoError(t, os.WriteFile(filepath.Join(dir, stateFile), []byte(`{"nextSeq":7}`), 0o600))

	s, err = Open(dir, 1<<20)
	require.NoError(t, err)
	require.Error(t, s.Recovered())
}

// TestOversizedReportCountsAsDropped discards a report over the server's
// size limit: its entries count as dropped, where they used to count as
// nothing because the spool could not read its own file back.
func TestOversizedReportCountsAsDropped(t *testing.T) {
	s, err := Open(t.TempDir(), 1<<30)
	require.NoError(t, err)

	big := report(0)
	big.Version = strings.Repeat("v", traffic.MaxReportBytes)

	for i := range 7 {
		big.Flows = append(big.Flows, traffic.Flow{Bucket: 60, Port: uint16(i)})
	}

	require.NoError(t, s.Enqueue(big))
	require.NoError(t, s.Reject(1))

	next := report(0)
	require.NoError(t, s.Enqueue(next))
	assert.Equal(t, uint64(7), next.Dropped)
}

// TestRestampRenumbersUnderANewInstance keeps every spooled report, in
// order, under a new instance and later sequence numbers.
func TestRestampRenumbersUnderANewInstance(t *testing.T) {
	dir := t.TempDir()

	s, err := Open(dir, 1<<20)
	require.NoError(t, err)

	old := s.Instance()

	for n := range 3 {
		require.NoError(t, s.Enqueue(report(n+1)))
	}

	require.NoError(t, s.Restamp())
	assert.NotEqual(t, old, s.Instance())
	assert.Equal(t, 3, s.Len())

	for want := range 3 {
		seq, body, readErr := s.Oldest()
		require.NoError(t, readErr)
		assert.Equal(t, uint64(4+want), seq)

		r, decodeErr := Decode(body)
		require.NoError(t, decodeErr)
		assert.Equal(t, s.Instance(), r.Instance)
		assert.Equal(t, seq, r.Seq)
		assert.Len(t, r.Flows, want+1)

		require.NoError(t, s.Ack(seq))
	}

	// The new instance survives a restart.
	reopened, err := Open(dir, 1<<20)
	require.NoError(t, err)
	assert.Equal(t, s.Instance(), reopened.Instance())
	assert.Equal(t, uint64(7), reopened.NextSeq())
}

func TestDecodeRejects(t *testing.T) {
	_, err := Decode([]byte("not zstd"))
	require.Error(t, err)

	// A small body that inflates past the report limit is refused.
	big := &traffic.Report{Version: strings.Repeat("a", traffic.MaxReportBytes)}
	body, err := Encode(big)
	require.NoError(t, err)
	assert.Less(t, len(body), 1<<20)

	_, err = Decode(body)
	require.Error(t, err)
}
