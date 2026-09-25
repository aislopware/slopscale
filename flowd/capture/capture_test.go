package capture

import (
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"net"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/bpf"
)

const (
	flagACK = 0x10
	flagPSH = 0x08
)

func ipv4(src, dst netip.Addr, proto uint8, transport []byte) []byte {
	pkt := make([]byte, 20, 20+len(transport))
	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:], uint16(20+len(transport)))
	binary.BigEndian.PutUint16(pkt[6:], 0x4000) // DF
	pkt[8] = 64
	pkt[9] = proto
	copy(pkt[12:16], src.AsSlice())
	copy(pkt[16:20], dst.AsSlice())

	return append(pkt, transport...)
}

func ipv6(src, dst netip.Addr, proto uint8, transport []byte) []byte {
	pkt := make([]byte, 40, 40+len(transport))
	pkt[0] = 0x60
	binary.BigEndian.PutUint16(pkt[4:], uint16(len(transport)))
	pkt[6] = proto
	pkt[7] = 64
	copy(pkt[8:24], src.AsSlice())
	copy(pkt[24:40], dst.AsSlice())

	return append(pkt, transport...)
}

func tcp(sport, dport uint16, seq uint32, flags byte, payload []byte) []byte {
	seg := make([]byte, 20, 20+len(payload))
	binary.BigEndian.PutUint16(seg[0:], sport)
	binary.BigEndian.PutUint16(seg[2:], dport)
	binary.BigEndian.PutUint32(seg[4:], seq)
	seg[12] = 5 << 4
	seg[13] = flags

	return append(seg, payload...)
}

// udp builds a datagram to port 443, where QUIC goes.
func udp(sport uint16, payload []byte) []byte {
	dg := make([]byte, 8, 8+len(payload))
	binary.BigEndian.PutUint16(dg[0:], sport)
	binary.BigEndian.PutUint16(dg[2:], 443)
	binary.BigEndian.PutUint16(dg[4:], uint16(8+len(payload)))

	return append(dg, payload...)
}

func clientHello(t *testing.T, serverName string) []byte {
	t.Helper()

	client, server := net.Pipe()

	t.Cleanup(func() {
		client.Close()
		server.Close()
	})

	go func() {
		_ = tls.Client(client, &tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12}).
			HandshakeContext(t.Context())
	}()

	require.NoError(t, server.SetReadDeadline(time.Now().Add(5*time.Second)))

	buf := make([]byte, 64<<10)
	n, err := server.Read(buf)
	require.NoError(t, err)

	return buf[:n]
}

func quicVector(t *testing.T) []byte {
	t.Helper()

	raw, err := os.ReadFile("../sni/testdata/rfc9001-client-initial.hex")
	require.NoError(t, err)

	b, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	require.NoError(t, err)

	return b
}

var (
	node    = netip.MustParseAddr("100.64.0.3")
	node6   = netip.MustParseAddr("fd7a:115c:a1e0::3")
	remote  = netip.MustParseAddr("203.0.113.80")
	remote6 = netip.MustParseAddr("2001:db8::80")
)

// TestFilter runs the kernel filter in x/net/bpf's interpreter over the
// packets a tunnel carries.
func TestFilter(t *testing.T) {
	prog, err := filterProgram()
	require.NoError(t, err)

	vm, err := bpf.NewVM(prog)
	require.NoError(t, err)

	hello := clientHello(t, "a.example")
	bulk := make([]byte, 1200)

	fragment := ipv4(node, remote, 6, tcp(1, 443, 0, flagACK, hello))
	binary.BigEndian.PutUint16(fragment[6:], 0x2000) // MF

	for _, tc := range []struct {
		name   string
		pkt    []byte
		accept bool
	}{
		{name: "v4 hello", pkt: ipv4(node, remote, 6, tcp(1, 443, 0, flagACK, hello[:1000])), accept: true},
		{
			name:   "v4 hello tail with PSH",
			pkt:    ipv4(node, remote, 6, tcp(1, 443, 1000, flagACK|flagPSH, bulk[:300])),
			accept: true,
		},
		{name: "v4 bulk data", pkt: ipv4(node, remote, 6, tcp(1, 443, 5000, flagACK, bulk))},
		{name: "v4 pure ack", pkt: ipv4(node, remote, 6, tcp(1, 443, 5000, flagACK|flagPSH, nil))},
		{name: "v4 fragment", pkt: fragment},
		{name: "v4 quic initial", pkt: ipv4(node, remote, 17, udp(5000, quicVector(t))), accept: true},
		{name: "v4 quic short header", pkt: ipv4(node, remote, 17, udp(5000, append([]byte{0x40}, bulk...)))},
		{name: "v4 icmp", pkt: ipv4(node, remote, 1, bulk[:64])},
		{name: "v6 hello", pkt: ipv6(node6, remote6, 6, tcp(1, 443, 0, flagACK, hello[:1000])), accept: true},
		{name: "v6 bulk", pkt: ipv6(node6, remote6, 6, tcp(1, 443, 0, flagACK, bulk))},
		{name: "v6 psh", pkt: ipv6(node6, remote6, 6, tcp(1, 443, 0, flagPSH, bulk[:10])), accept: true},
		{name: "v6 quic initial", pkt: ipv6(node6, remote6, 17, udp(5000, quicVector(t))), accept: true},
		{name: "v6 hop-by-hop header", pkt: ipv6(node6, remote6, 0, bulk[:80])},
		{name: "garbage", pkt: []byte{0x12, 0x34}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := vm.Run(tc.pkt)
			require.NoError(t, err)

			if tc.accept {
				assert.Positive(t, n)
			} else {
				assert.Zero(t, n)
			}
		})
	}
}

// TestProcessorReassemblesSplitHello delivers a real ClientHello in two
// tunnel-sized segments with a retransmission between them.
func TestProcessorReassemblesSplitHello(t *testing.T) {
	var got []Hello

	p := NewProcessor(func(h Hello) { got = append(got, h) })
	now := time.Unix(1_800_000_000, 0)
	hello := clientHello(t, "Split.Example.org")
	require.Greater(t, len(hello), 1240)

	first, second := hello[:1240], hello[1240:]

	var isn uint32 = 0xfffffff0 // the sequence number wraps inside the hello

	p.Packet(ipv4(node, remote, 6, tcp(50000, 443, isn, flagACK, first)), now)
	p.Packet(ipv4(node, remote, 6, tcp(50000, 443, isn, flagACK, first)), now) // retransmission
	assert.Empty(t, got)
	assert.Equal(t, 1, p.Pending())

	p.Packet(ipv4(node, remote, 6, tcp(50000, 443, isn+1240, flagACK|flagPSH, second)), now)
	require.Len(t, got, 1)
	assert.Equal(t, "split.example.org", got[0].Hello.ServerName)
	assert.Equal(t, netip.MustParseAddrPort("100.64.0.3:50000"), got[0].Conn.Src)
	assert.Equal(t, netip.MustParseAddrPort("203.0.113.80:443"), got[0].Conn.Dst)
	assert.Equal(t, uint8(6), got[0].Conn.Proto)
	assert.Zero(t, p.Pending())
}

func TestProcessorDropsGapsAndStale(t *testing.T) {
	var got []Hello

	p := NewProcessor(func(h Hello) { got = append(got, h) })
	now := time.Unix(1_800_000_000, 0)
	hello := clientHello(t, "gap.example")

	// A lost segment: the next one does not continue the stream.
	p.Packet(ipv4(node, remote, 6, tcp(50001, 443, 100, flagACK, hello[:600])), now)
	p.Packet(ipv4(node, remote, 6, tcp(50001, 443, 1000, flagPSH, hello[900:])), now)
	assert.Empty(t, got)
	assert.Zero(t, p.Pending())

	// An incomplete hello is forgotten after the timeout.
	p.Packet(ipv4(node, remote, 6, tcp(50002, 443, 100, flagACK, hello[:600])), now)
	assert.Equal(t, 1, p.Pending())
	p.Packet(ipv4(node, remote, 6, tcp(1, 1, 1, flagPSH, []byte("x"))), now.Add(4*time.Second))
	assert.Zero(t, p.Pending())

	// Traffic that is not a tailnet node leaving the tailnet is ignored.
	p.Packet(ipv4(remote, node, 6, tcp(443, 50003, 1, flagACK, hello)), now)
	p.Packet(ipv4(node, netip.MustParseAddr("100.64.0.9"), 6, tcp(50004, 443, 1, flagACK, hello)), now)
	assert.Empty(t, got)

	// Garbage never panics.
	garbage := [][]byte{
		nil, {0x45}, {0x45, 0, 0, 20}, make([]byte, 20), {0x60, 1, 2}, ipv4(node, remote, 6, []byte{1, 2}),
	}

	for _, pkt := range garbage {
		p.Packet(pkt, now)
	}
}

func TestProcessorQUIC(t *testing.T) {
	var got []Hello

	p := NewProcessor(func(h Hello) { got = append(got, h) })
	now := time.Unix(1_800_000_000, 0)

	p.Packet(ipv6(node6, remote6, 17, udp(60000, quicVector(t))), now)
	require.Len(t, got, 1)
	assert.Equal(t, "example.com", got[0].Hello.ServerName)
	assert.Equal(t, uint8(17), got[0].Conn.Proto)
	assert.Equal(t, netip.AddrPortFrom(node6, 60000), got[0].Conn.Src)

	// A corrupted Initial is ignored.
	bad := quicVector(t)
	bad[len(bad)-5] ^= 1
	p.Packet(ipv4(node, remote, 17, udp(60001, bad)), now)
	assert.Len(t, got, 1)
	assert.Zero(t, p.Pending())
}

// TestProcessorOffloadedPacket handles a segmentation-offloaded capture,
// whose IPv4 total length field is zero.
func TestProcessorOffloadedPacket(t *testing.T) {
	var got []Hello

	p := NewProcessor(func(h Hello) { got = append(got, h) })
	pkt := ipv4(node, remote, 6, tcp(50005, 443, 7, flagPSH, clientHello(t, "gso.example")))
	binary.BigEndian.PutUint16(pkt[2:], 0)

	p.Packet(pkt, time.Now())
	require.Len(t, got, 1)
	assert.Equal(t, "gso.example", got[0].Hello.ServerName)
}
