// Package capture reads the server names tailnet nodes ask for in TLS and
// QUIC handshakes they send through the gateway. The socket sees only the
// packets that can start or continue a handshake (a kernel filter drops
// the rest); the processor reassembles them per connection.
package capture

import (
	"encoding/binary"
	"net/netip"
	"time"

	"github.com/aislopware/slopscale/flowd/names"
	"github.com/aislopware/slopscale/flowd/sni"
	"github.com/aislopware/slopscale/flowd/tailnet"
)

const (
	protoTCP = 6
	protoUDP = 17

	ipv4MinHeader   = 20
	ipv6Header      = 40
	tcpMinHeader    = 20
	udpHeader       = 8
	ipVersionShift  = 4
	ipVersion4      = 4
	ipVersion6      = 6
	ipv4IHLMask     = 0x0f
	ipv4FragMask    = 0x3fff // MF flag and fragment offset
	tcpOffsetShift  = 4
	wordBytes       = 4
	pendingTimeout  = 3 * time.Second
	maxPendingConns = 4096
	sweepEvery      = time.Second
)

// Hello is a handshake's server name on a connection.
type Hello struct {
	Conn  names.Conn
	Hello sni.Hello
}

type tcpPending struct {
	stream  sni.TLSStream
	next    uint32
	expires time.Time
}

type quicPending struct {
	dcid    string
	stream  sni.CryptoStream
	expires time.Time
}

// Processor turns captured packets into handshakes. It is not safe for
// concurrent use.
type Processor struct {
	onHello   func(Hello)
	tcp       map[names.Conn]*tcpPending
	quic      map[names.Conn]*quicPending
	lastSweep time.Time
}

// NewProcessor returns a processor reporting each handshake to onHello.
func NewProcessor(onHello func(Hello)) *Processor {
	return &Processor{
		onHello: onHello,
		tcp:     make(map[names.Conn]*tcpPending),
		quic:    make(map[names.Conn]*quicPending),
	}
}

// Packet processes one IP packet, as the tunnel carries it.
func (p *Processor) Packet(pkt []byte, now time.Time) {
	p.sweep(now)

	proto, src, dst, payload, ok := parseIP(pkt)
	if !ok || !tailnet.Egress(src, dst) {
		return
	}

	switch proto {
	case protoTCP:
		p.tcpSegment(src, dst, payload, now)
	case protoUDP:
		p.udpDatagram(src, dst, payload, now)
	}
}

// Pending is the number of connections whose handshake is incomplete.
func (p *Processor) Pending() int {
	return len(p.tcp) + len(p.quic)
}

// parseIP returns the transport protocol, addresses and transport bytes of
// an unfragmented IPv4 or IPv6 packet.
func parseIP(pkt []byte) (uint8, netip.Addr, netip.Addr, []byte, bool) {
	if len(pkt) == 0 {
		return 0, netip.Addr{}, netip.Addr{}, nil, false
	}

	switch pkt[0] >> ipVersionShift {
	case ipVersion4:
		if len(pkt) < ipv4MinHeader {
			break
		}

		ihl := int(pkt[0]&ipv4IHLMask) * wordBytes
		total := int(binary.BigEndian.Uint16(pkt[2:4]))

		// A fragment's transport header is not where it seems; so are
		// a truncated header and a length the capture contradicts. A
		// segmentation-offloaded packet has a zero total length; the
		// captured length is then all there is.
		if ihl < ipv4MinHeader || len(pkt) < ihl || binary.BigEndian.Uint16(pkt[6:8])&ipv4FragMask != 0 {
			break
		}

		if total >= ihl && total < len(pkt) {
			pkt = pkt[:total]
		}

		src := netip.AddrFrom4([4]byte(pkt[12:16]))
		dst := netip.AddrFrom4([4]byte(pkt[16:20]))

		return pkt[9], src, dst, pkt[ihl:], true
	case ipVersion6:
		if len(pkt) < ipv6Header {
			break
		}

		payloadLen := int(binary.BigEndian.Uint16(pkt[4:6]))
		if payloadLen > 0 && ipv6Header+payloadLen < len(pkt) {
			pkt = pkt[:ipv6Header+payloadLen]
		}

		src := netip.AddrFrom16([16]byte(pkt[8:24]))
		dst := netip.AddrFrom16([16]byte(pkt[24:40]))

		return pkt[6], src, dst, pkt[ipv6Header:], true
	}

	return 0, netip.Addr{}, netip.Addr{}, nil, false
}

func (p *Processor) tcpSegment(src, dst netip.Addr, seg []byte, now time.Time) {
	if len(seg) < tcpMinHeader {
		return
	}

	off := int(seg[12]>>tcpOffsetShift) * wordBytes
	if off < tcpMinHeader || off > len(seg) {
		return
	}

	payload := seg[off:]
	if len(payload) == 0 {
		return
	}

	conn := names.Conn{
		Src:   netip.AddrPortFrom(src, binary.BigEndian.Uint16(seg[0:2])),
		Dst:   netip.AddrPortFrom(dst, binary.BigEndian.Uint16(seg[2:4])),
		Proto: protoTCP,
	}
	seq := binary.BigEndian.Uint32(seg[4:8])

	pending, ok := p.tcp[conn]

	switch {
	case !ok && sni.LooksLikeTLS(payload):
		if len(p.tcp) >= maxPendingConns {
			return
		}

		pending = &tcpPending{next: seq}
		p.tcp[conn] = pending
	case !ok:
		return
	case seq != pending.next:
		// A retransmission of what was already read is ignored; a gap
		// means a lost segment, and the hello cannot be completed.
		if int32(seq-pending.next) > 0 { //nolint:gosec // sequence arithmetic wraps on purpose
			delete(p.tcp, conn)
		}

		return
	}

	pending.next = seq + uint32(len(payload)) //nolint:gosec // bounded by the packet size
	pending.expires = now.Add(pendingTimeout)

	hello, done, err := pending.stream.Write(payload)
	if err != nil || done {
		delete(p.tcp, conn)
	}

	if done {
		p.onHello(Hello{Conn: conn, Hello: hello})
	}
}

func (p *Processor) udpDatagram(src, dst netip.Addr, dgram []byte, now time.Time) {
	if len(dgram) < udpHeader {
		return
	}

	payload := dgram[udpHeader:]
	if !sni.LooksLikeQUICInitial(payload) {
		return
	}

	initial, err := sni.ParseInitials(payload)
	if err != nil {
		return
	}

	conn := names.Conn{
		Src:   netip.AddrPortFrom(src, binary.BigEndian.Uint16(dgram[0:2])),
		Dst:   netip.AddrPortFrom(dst, binary.BigEndian.Uint16(dgram[2:4])),
		Proto: protoUDP,
	}

	pending, ok := p.quic[conn]
	if !ok || pending.dcid != string(initial.DCID) {
		if !ok && len(p.quic) >= maxPendingConns {
			return
		}

		pending = &quicPending{dcid: string(initial.DCID)}
		p.quic[conn] = pending
	}

	pending.expires = now.Add(pendingTimeout)

	hello, done, err := pending.stream.Add(initial.Frames)
	if err != nil || done {
		delete(p.quic, conn)
	}

	if done {
		p.onHello(Hello{Conn: conn, Hello: hello})
	}
}

func (p *Processor) sweep(now time.Time) {
	if now.Sub(p.lastSweep) < sweepEvery {
		return
	}

	p.lastSweep = now

	for k, v := range p.tcp {
		if now.After(v.expires) {
			delete(p.tcp, k)
		}
	}

	for k, v := range p.quic {
		if now.After(v.expires) {
			delete(p.quic, k)
		}
	}
}
