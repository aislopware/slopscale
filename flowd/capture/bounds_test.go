package capture

import (
	"encoding/binary"
	"net/netip"
	"testing"
	"time"

	"github.com/aislopware/slopscale/flowd/internal/quictest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/bpf"
)

// oddFrames is a datagram's worth of one-byte CRYPTO frames at odd
// offsets, never offset 0: a hello that can never complete.
func oddFrames(first, count int) []byte {
	frames := make([]byte, 0, count*32)

	for i := range count {
		frames = append(frames, quictest.Crypto(2*(first+i)+1, []byte{0})...)
	}

	return frames
}

// TestQUICFloodCannotHoldTheProcessor replays the review's flood: valid
// Initials, which anyone can build, full of one-byte CRYPTO frames that
// leave a gap each. One connection used to hold 8000 ranges and 151 KiB,
// re-sorted on every datagram. Now the stream fails at the seventeenth
// gap and nothing is held.
func TestQUICFloodCannotHoldTheProcessor(t *testing.T) {
	p := NewProcessor(func(Hello) { t.Fatal("no hello in a flood") })
	now := time.Unix(1_800_000_000, 0)

	for c := range 64 {
		dcid := []byte{1, 2, 3, 4, 5, 6, 7, byte(c)}

		for first := 0; first < 8000; first += 60 {
			initial := quictest.Initial(dcid, uint32(first), oddFrames(first, 60))
			p.Packet(ipv4(node, remote, 17, udp(uint16(6000+c), initial)), now)
		}
	}

	assert.Zero(t, p.Pending())
	assert.Zero(t, p.Held())
}

// firstHalf is the first segment of a hello whose record promises more
// than it carries, so the connection waits for the rest.
func firstHalf(size int) []byte {
	b := make([]byte, size)
	copy(b, []byte{0x16, 0x03, 0x01, 0x3f, 0xff, 0x01, 0x00, 0x3f, 0xfb})

	return b
}

// TestOneSourceCannotCrowdOutOthers: a node opened 4096 connections with
// the first half of a hello each and never finished them. Every other
// node's split hello used to be refused while the table was full; now
// the node is held to its share and the others' hellos are read.
func TestOneSourceCannotCrowdOutOthers(t *testing.T) {
	var got []Hello

	p := NewProcessor(func(h Hello) { got = append(got, h) })
	now := time.Unix(1_800_000_000, 0)

	for port := range maxPendingConns {
		p.Packet(ipv4(node, remote, 6, tcp(uint16(1024+port), 443, 1, flagACK, firstHalf(100))), now)
	}

	assert.Equal(t, maxPendingPerSource, p.Pending())

	other := netip.MustParseAddr("100.64.0.4")
	hello := clientHello(t, "fair.example")

	p.Packet(ipv4(other, remote, 6, tcp(40000, 443, 7, flagACK, hello[:1000])), now)
	p.Packet(ipv4(other, remote, 6, tcp(40000, 443, 1007, flagACK|flagPSH, hello[1000:])), now)

	require.Len(t, got, 1)
	assert.Equal(t, "fair.example", got[0].Hello.ServerName)

	// With many sources, the whole table is bounded and the newest
	// handshake gives the oldest its place.
	for i := range maxPendingConns + 10 {
		src := netip.AddrFrom4([4]byte{100, 64, byte(1 + i>>8), byte(i)})
		p.Packet(ipv4(src, remote, 6, tcp(2000, 443, 1, flagACK, firstHalf(100))), now)
	}

	assert.Equal(t, maxPendingConns, p.Pending())
}

// TestPendingDeadlineIsFixed: a node trickling one in-order byte a second
// used to keep its handshake pending forever; the deadline now runs from
// the first packet.
func TestPendingDeadlineIsFixed(t *testing.T) {
	p := NewProcessor(func(Hello) {})
	now := time.Unix(1_800_000_000, 0)

	p.Packet(ipv4(node, remote, 6, tcp(3000, 443, 1, flagACK, firstHalf(100))), now)

	for i := range 5 {
		at := now.Add(time.Duration(i+1) * time.Second)
		p.Packet(ipv4(node, remote, 6, tcp(3000, 443, uint32(101+i), flagACK|flagPSH, []byte{0})), at)
	}

	assert.Zero(t, p.Pending())
}

// TestHeldBytesAreBounded: thousands of half hellos of 15 KB each would
// hold 60 MB; the oldest give way once all of them hold maxHeldBytes.
func TestHeldBytesAreBounded(t *testing.T) {
	p := NewProcessor(func(Hello) {})
	now := time.Unix(1_800_000_000, 0)

	half := firstHalf(15_000)

	var last netip.Addr

	for i := range 4000 {
		last = netip.AddrFrom4([4]byte{100, 64, byte(1 + i>>8), byte(i)})
		p.Packet(ipv4(last, remote, 6, tcp(2000, 443, 1, flagACK, half)), now)
		require.LessOrEqual(t, p.Held(), maxHeldBytes)
	}

	assert.Less(t, p.Pending(), 4000)

	// The newest is still there: a byte more is taken, not a new hello.
	before := p.Held()
	p.Packet(ipv4(last, remote, 6, tcp(2000, 443, 1+15_000, flagACK, []byte{0})), now)
	assert.GreaterOrEqual(t, p.Held(), before)
}

// TestRetransmissionCarryingNewBytes: a retransmission that repeats part
// of what was read and carries more continues the stream; it used to be
// ignored, and the next segment then looked like a gap.
func TestRetransmissionCarryingNewBytes(t *testing.T) {
	var got []Hello

	p := NewProcessor(func(h Hello) { got = append(got, h) })
	now := time.Unix(1_800_000_000, 0)
	hello := clientHello(t, "resent.example")
	require.Greater(t, len(hello), 1200)

	var isn uint32 = 0xfffffe00 // the sequence number wraps inside the hello

	p.Packet(ipv4(node, remote, 6, tcp(4000, 443, isn, flagACK, hello[:800])), now)
	p.Packet(ipv4(node, remote, 6, tcp(4000, 443, isn+400, flagACK, hello[400:1200])), now)
	assert.Equal(t, 1, p.Pending())

	p.Packet(ipv4(node, remote, 6, tcp(4000, 443, isn+1200, flagACK|flagPSH, hello[1200:])), now)
	require.Len(t, got, 1)
	assert.Equal(t, "resent.example", got[0].Hello.ServerName)
}

// ipv6Ext inserts extension headers (hop-by-hop, then destination
// options) between an IPv6 header and its transport header.
func ipv6Ext(src, dst netip.Addr, proto uint8, transport []byte) []byte {
	ext := make([]byte, 0, 24+len(transport))
	ext = append(ext,
		60, 0, 5, 2, 0, 0, 0, 0, // hop-by-hop: next is destination options, a router alert
		proto, 1, 1, 12, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // destination options, 16 bytes
	)

	return ipv6(src, dst, 0, append(ext, transport...))
}

// TestIPv6ExtensionHeaders: a hello behind IPv6 extension headers used to
// be skipped; the processor walks them to the transport header.
func TestIPv6ExtensionHeaders(t *testing.T) {
	var got []Hello

	p := NewProcessor(func(h Hello) { got = append(got, h) })

	p.Packet(ipv6Ext(node6, remote6, 6, tcp(5000, 443, 1, flagPSH, clientHello(t, "ext.example"))), time.Now())
	require.Len(t, got, 1)
	assert.Equal(t, "ext.example", got[0].Hello.ServerName)

	// The kernel filter lets it through for that.
	prog, err := filterProgram()
	require.NoError(t, err)

	vm, err := bpf.NewVM(prog)
	require.NoError(t, err)

	n, err := vm.Run(ipv6Ext(node6, remote6, 6, tcp(5000, 443, 1, flagPSH, []byte{1})))
	require.NoError(t, err)
	assert.Positive(t, n)

	// A chain longer than any real one, or cut short, is not read.
	chain := ipv6(node6, remote6, 60, nil)
	for range maxIPv6Ext + 1 {
		chain = append(chain, 60, 0, 0, 0, 0, 0, 0, 0)
	}

	_, _, ok := ipHeader(chain)
	assert.False(t, ok)

	_, _, ok = ipHeader(ipv6(node6, remote6, 0, []byte{60}))
	assert.False(t, ok)
}

// quicDatagrams returns the Initial datagrams of a real ClientHello split
// in three, each padded after its packet to 1200 bytes, as clients may.
func quicDatagrams(t *testing.T, serverName string) [][]byte {
	t.Helper()

	msg := clientHello(t, serverName)[5:] // the handshake message, without its record header
	dcid := []byte{9, 8, 7, 6, 5, 4, 3, 2}
	third := len(msg)/3 + 1

	out := make([][]byte, 0, 3)

	for i := range 3 {
		off := i * third
		pkt := quictest.Initial(dcid, uint32(i), quictest.Crypto(off, msg[off:min(off+third, len(msg))]))
		require.Less(t, len(pkt), 1200)

		out = append(out, append(pkt, make([]byte, 1200-len(pkt))...))
	}

	return out
}

// TestMergedDatagramsAreSplit: the tunnel's receive offload merges a
// burst of datagrams into one packet with one UDP header. Read as one
// datagram, the padding after the first Initial ended it and the rest of
// the hello was lost; split at the segment size, every Initial is read.
func TestMergedDatagramsAreSplit(t *testing.T) {
	for name, build := range map[string]func([]byte) []byte{
		"v4": func(b []byte) []byte { return ipv4(node, remote, 17, udp(7000, b)) },
		"v6": func(b []byte) []byte { return ipv6(node6, remote6, 17, udp(7000, b)) },
	} {
		t.Run(name, func(t *testing.T) {
			var joined []byte
			for _, d := range quicDatagrams(t, "merged.example") {
				joined = append(joined, d...)
			}

			merged := build(joined)

			var got []Hello

			p := NewProcessor(func(h Hello) { got = append(got, h) })
			p.Packet(merged, time.Now())
			assert.Empty(t, got, "read as one datagram, the hello is lost")

			var sizes []int

			splitUDP(merged, 1200, nil, func(seg []byte) {
				proto, hdrLen, ok := ipHeader(seg)
				require.True(t, ok)
				require.Equal(t, uint8(17), proto)
				assert.Equal(t, len(seg)-hdrLen, int(binary.BigEndian.Uint16(seg[hdrLen+4:])), "UDP length")

				sizes = append(sizes, len(seg)-hdrLen-udpHeader)
				p.Packet(seg, time.Now())
			})

			assert.Equal(t, []int{1200, 1200, 1200}, sizes)
			require.Len(t, got, 1)
			assert.Equal(t, "merged.example", got[0].Hello.ServerName)
		})
	}

	// Anything else passes through whole.
	var whole [][]byte

	tcpPkt := ipv4(node, remote, 6, tcp(1, 443, 1, flagPSH, []byte{1, 2, 3}))
	splitUDP(tcpPkt, 1200, nil, func(seg []byte) { whole = append(whole, seg) })
	assert.Equal(t, [][]byte{tcpPkt}, whole)
}
