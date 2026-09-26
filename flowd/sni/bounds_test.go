package sni

import (
	"crypto/tls"
	"math/rand/v2"
	"testing"

	"github.com/aislopware/slopscale/flowd/internal/quictest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTLSStreamReadsOnlyTheHello: a client offering early data writes its
// ClientHello, a ChangeCipherSpec and early data at once (RFC 8446 D.4),
// so they share a segment. The hello is complete and is read; what follows
// used to fail the stream as not TLS, or as too large past 16 KiB.
func TestTLSStreamReadsOnlyTheHello(t *testing.T) {
	hello := captureClientHello(t, &tls.Config{ServerName: "early.example.com", MinVersion: tls.VersionTLS12})
	ccs := []byte{0x14, 0x03, 0x03, 0x00, 0x01, 0x01}
	early := []byte{0x17, 0x03, 0x03, 0x00, 0x02, 0xaa, 0xbb}

	large := make([]byte, 20_000)
	for i := 0; i+recordHeaderLen <= len(large); i += 5005 {
		copy(large[i:], []byte{0x17, 0x03, 0x03, 0x13, 0x88})
	}

	for name, tail := range map[string][]byte{
		"change cipher spec and early data": append(append([]byte(nil), ccs...), early...),
		"20 KB of early data":               large,
	} {
		t.Run(name, func(t *testing.T) {
			var stream TLSStream

			got, done, err := stream.Write(append(append([]byte(nil), hello...), tail...))
			require.NoError(t, err)
			require.True(t, done)
			assert.Equal(t, "early.example.com", got.ServerName)
			assert.Less(t, stream.Held(), 4*len(hello), "the tail is not buffered")
		})
	}

	// The same, one byte at a time.
	var stream TLSStream

	in := append(append([]byte(nil), hello...), ccs...)
	for i, b := range in {
		got, done, err := stream.Write([]byte{b})
		require.NoError(t, err)

		if done {
			assert.Equal(t, len(hello)-1, i, "done at the hello's last byte")
			assert.Equal(t, "early.example.com", got.ServerName)

			return
		}
	}

	t.Fatal("the hello was never complete")
}

// TestCryptoStreamRefusesAFlood: anyone can build valid Initials, so a
// node sending one-byte CRYPTO frames at odd offsets, never offset 0,
// used to make the stream hold thousands of ranges and re-sort them on
// every frame. The stream now fails once the gaps outnumber any client's
// reordering, and a real hello shuffled into as many pieces still arrives.
func TestCryptoStreamRefusesAFlood(t *testing.T) {
	var flood CryptoStream

	var err error

	frames := 0

	for i := range 8000 {
		_, _, err = flood.Add([]CryptoFrame{{Offset: uint64(2*i + 1), Data: []byte{0}}})
		if err != nil {
			break
		}

		frames++
	}

	require.ErrorIs(t, err, ErrFragmented)
	assert.Equal(t, maxCryptoRanges, frames)
	assert.LessOrEqual(t, flood.Held(), 64)

	initial, err := ParseInitials(readVector(t, "rfc9001-client-initial.hex"))
	require.NoError(t, err)

	data := initial.Frames[0].Data
	size := len(data)/(maxCryptoRanges+1) + 1

	var pieces []CryptoFrame
	for off := 0; off < len(data); off += size {
		pieces = append(pieces, CryptoFrame{Offset: uint64(off), Data: data[off:min(off+size, len(data))]})
	}

	require.Len(t, pieces, maxCryptoRanges+1)

	for seed := range uint64(50) {
		shuffled := append([]CryptoFrame(nil), pieces...)
		rand.New(rand.NewPCG(seed, seed)).Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})

		var (
			stream CryptoStream
			hello  Hello
			done   bool
		)

		for _, f := range shuffled {
			// Every frame twice: a duplicate never counts as a gap.
			hello, done, err = stream.Add([]CryptoFrame{f, f})
			require.NoError(t, err)
		}

		require.True(t, done)
		assert.Equal(t, "example.com", hello.ServerName)
	}
}

// TestCoalescedInitialsShareTheirKeys: the Initials coalesced in one
// datagram share a connection ID, so their keys are derived once, not
// once per packet (about 40 times for a 1280-byte datagram of small
// packets, or 2000 for a 64 KB offloaded one).
func TestCoalescedInitialsShareTheirKeys(t *testing.T) {
	derived := 0
	deriveKeys = func(version uint32, dcid []byte) (initialKeys, error) {
		derived++

		return deriveInitialKeys(version, dcid)
	}

	t.Cleanup(func() { deriveKeys = deriveInitialKeys })

	dcid := []byte{1, 2, 3, 4, 5, 6, 7, 8}

	datagram := make([]byte, 0, 40*64)
	for i := range 40 {
		datagram = append(datagram, quictest.Initial(dcid, uint32(i), quictest.Crypto(i, []byte{byte(i)}))...)
	}

	initial, err := ParseInitials(datagram)
	require.NoError(t, err)
	assert.Len(t, initial.Frames, 40)
	assert.Equal(t, 1, derived)
}
