// Package capture reads the server names tailnet nodes ask for in TLS and
// QUIC handshakes they send through the gateway. The socket sees only the
// packets that can start or continue a handshake (a kernel filter drops
// the rest); the processor reassembles them per connection.
package capture

import (
	"container/list"
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

	ipv4MinHeader  = 20
	ipv6Header     = 40
	tcpMinHeader   = 20
	udpHeader      = 8
	ipVersionShift = 4
	ipVersion4     = 4
	ipVersion6     = 6
	ipv4IHLMask    = 0x0f
	ipv4FragMask   = 0x3fff // MF flag and fragment offset
	tcpOffsetShift = 4
	wordBytes      = 4

	// IPv6 extension headers that may precede the transport header, and
	// the most of them walked. A fragment's transport header is not where
	// it seems, as with IPv4.
	ipv6HopByHop    = 0
	ipv6Routing     = 43
	ipv6DestOptions = 60
	ipv6ExtUnit     = 8
	maxIPv6Ext      = 8

	// pendingTimeout is how long a handshake may take to complete, from
	// its first packet; further packets do not extend it.
	pendingTimeout = 3 * time.Second
	// maxPendingConns bounds the incomplete handshakes held, and
	// maxPendingPerSource those of one address, so one node cannot crowd
	// out the others. When full, the oldest gives way to the newest.
	maxPendingConns     = 4096
	maxPendingPerSource = 64
	// maxHeldBytes bounds what incomplete handshakes buffer altogether.
	maxHeldBytes = 16 << 20
)

// Hello is a handshake's server name on a connection.
type Hello struct {
	Conn  names.Conn
	Hello sni.Hello
}

// pending is a connection whose handshake is incomplete: a TLS stream for
// TCP, a CRYPTO stream for QUIC.
type pending struct {
	conn    names.Conn
	expires time.Time
	held    int
	elem    *list.Element

	next uint32 // TCP: the next sequence number the stream wants
	tls  sni.TLSStream

	dcid string // QUIC: the connection ID the Initials are for
	quic sni.CryptoStream
}

// Processor turns captured packets into handshakes. It is not safe for
// concurrent use.
type Processor struct {
	onHello func(Hello)
	pending map[names.Conn]*pending
	// order holds the pending connections oldest first; deadlines are
	// fixed, so this is also the order they expire in.
	order *list.List
	bySrc map[netip.Addr]int
	held  int
}

// NewProcessor returns a processor reporting each handshake to onHello.
func NewProcessor(onHello func(Hello)) *Processor {
	return &Processor{
		onHello: onHello,
		pending: make(map[names.Conn]*pending),
		order:   list.New(),
		bySrc:   make(map[netip.Addr]int),
	}
}

// Packet processes one IP packet, as the tunnel carries it.
func (p *Processor) Packet(pkt []byte, now time.Time) {
	p.expire(now)

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
	return len(p.pending)
}

// Held is how many bytes the incomplete handshakes buffer.
func (p *Processor) Held() int {
	return p.held
}

// parseIP returns the transport protocol, addresses and transport bytes of
// an unfragmented IPv4 or IPv6 packet.
func parseIP(pkt []byte) (uint8, netip.Addr, netip.Addr, []byte, bool) {
	proto, hdrLen, ok := ipHeader(pkt)
	if !ok {
		return 0, netip.Addr{}, netip.Addr{}, nil, false
	}

	var src, dst netip.Addr

	if pkt[0]>>ipVersionShift == ipVersion4 {
		// A segmentation-offloaded packet has a zero total length; the
		// captured length is then all there is.
		total := int(binary.BigEndian.Uint16(pkt[2:4]))
		if total >= hdrLen && total < len(pkt) {
			pkt = pkt[:total]
		}

		src, dst = netip.AddrFrom4([4]byte(pkt[12:16])), netip.AddrFrom4([4]byte(pkt[16:20]))
	} else {
		payloadLen := int(binary.BigEndian.Uint16(pkt[4:6]))
		if payloadLen > 0 && ipv6Header+payloadLen >= hdrLen && ipv6Header+payloadLen < len(pkt) {
			pkt = pkt[:ipv6Header+payloadLen]
		}

		src, dst = netip.AddrFrom16([16]byte(pkt[8:24])), netip.AddrFrom16([16]byte(pkt[24:40]))
	}

	return proto, src, dst, pkt[hdrLen:], true
}

// ipHeader returns the transport protocol of an unfragmented IPv4 or IPv6
// packet and where its transport header starts, past any IPv6 extension
// headers.
func ipHeader(pkt []byte) (uint8, int, bool) {
	if len(pkt) == 0 {
		return 0, 0, false
	}

	switch pkt[0] >> ipVersionShift {
	case ipVersion4:
		if len(pkt) < ipv4MinHeader {
			return 0, 0, false
		}

		// A fragment's transport header is not where it seems, and a
		// truncated header is not a header.
		ihl := int(pkt[0]&ipv4IHLMask) * wordBytes
		if ihl < ipv4MinHeader || len(pkt) < ihl || binary.BigEndian.Uint16(pkt[6:8])&ipv4FragMask != 0 {
			return 0, 0, false
		}

		return pkt[9], ihl, true
	case ipVersion6:
		if len(pkt) < ipv6Header {
			return 0, 0, false
		}

		next, off := pkt[6], ipv6Header

		for range maxIPv6Ext {
			if next != ipv6HopByHop && next != ipv6Routing && next != ipv6DestOptions {
				return next, off, true
			}

			if len(pkt) < off+2 {
				return 0, 0, false
			}

			next, off = pkt[off], off+(int(pkt[off+1])+1)*ipv6ExtUnit
		}
	}

	return 0, 0, false
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

	pend, ok := p.pending[conn]
	if !ok {
		if !sni.LooksLikeTLS(payload) {
			return
		}

		// Most hellos fit one segment and never enter the table.
		var stream sni.TLSStream

		hello, done, err := stream.Write(payload)
		if !p.finish(conn, hello, done, err) {
			pend = p.add(conn, now)
			pend.tls = stream
			pend.next = seq + uint32(len(payload)) //nolint:gosec // bounded by the packet size
			p.account(pend, stream.Held())
		}

		return
	}

	// A gap means a lost segment, and the hello cannot be completed. A
	// retransmission may repeat what was read and carry more: the new
	// part continues the stream.
	behind := pend.next - seq
	if int32(behind) < 0 { //nolint:gosec // sequence arithmetic wraps on purpose
		p.drop(pend)

		return
	}

	if int(behind) >= len(payload) {
		return
	}

	payload = payload[behind:]
	pend.next += uint32(len(payload)) //nolint:gosec // bounded by the packet size

	hello, done, err := pend.tls.Write(payload)
	if p.finish(conn, hello, done, err) {
		p.drop(pend)

		return
	}

	p.account(pend, pend.tls.Held())
}

func (p *Processor) udpDatagram(src, dst netip.Addr, dgram []byte, now time.Time) {
	if len(dgram) < udpHeader {
		return
	}

	payload := dgram[udpHeader:]
	if !sni.LooksLikeQUICInitial(payload) {
		return
	}

	initial, parseErr := sni.ParseInitials(payload)
	if parseErr != nil {
		return
	}

	conn := names.Conn{
		Src:   netip.AddrPortFrom(src, binary.BigEndian.Uint16(dgram[0:2])),
		Dst:   netip.AddrPortFrom(dst, binary.BigEndian.Uint16(dgram[2:4])),
		Proto: protoUDP,
	}

	pend, ok := p.pending[conn]
	if ok && pend.dcid != string(initial.DCID) {
		p.drop(pend)

		ok = false
	}

	if !ok {
		var stream sni.CryptoStream

		hello, done, err := stream.Add(initial.Frames)
		if !p.finish(conn, hello, done, err) {
			pend = p.add(conn, now)
			pend.quic = stream
			pend.dcid = string(initial.DCID)
			p.account(pend, stream.Held())
		}

		return
	}

	hello, done, err := pend.quic.Add(initial.Frames)
	if p.finish(conn, hello, done, err) {
		p.drop(pend)

		return
	}

	p.account(pend, pend.quic.Held())
}

// finish reports a complete handshake and whether the connection is done
// with, complete or failed.
func (p *Processor) finish(conn names.Conn, hello sni.Hello, done bool, err error) bool {
	if done {
		p.onHello(Hello{Conn: conn, Hello: hello})
	}

	return done || err != nil
}

// add holds a new incomplete handshake, making room first: the source's
// own oldest when it has too many, then the oldest of all.
func (p *Processor) add(conn names.Conn, now time.Time) *pending {
	src := conn.Src.Addr()

	if p.bySrc[src] >= maxPendingPerSource {
		for e := p.order.Front(); e != nil; e = e.Next() {
			if old, _ := e.Value.(*pending); old.conn.Src.Addr() == src {
				p.drop(old)

				break
			}
		}
	}

	if len(p.pending) >= maxPendingConns {
		p.dropOldest()
	}

	pend := &pending{conn: conn, expires: now.Add(pendingTimeout)}
	pend.elem = p.order.PushBack(pend)
	p.pending[conn] = pend
	p.bySrc[src]++

	return pend
}

// account records what a handshake buffers now, and drops the oldest
// handshakes while all of them hold too much.
func (p *Processor) account(pend *pending, held int) {
	p.held += held - pend.held
	pend.held = held

	for p.held > maxHeldBytes && p.order.Len() > 0 {
		p.dropOldest()
	}
}

func (p *Processor) dropOldest() {
	if old, ok := p.order.Front().Value.(*pending); ok {
		p.drop(old)
	}
}

func (p *Processor) drop(pend *pending) {
	p.order.Remove(pend.elem)
	delete(p.pending, pend.conn)

	src := pend.conn.Src.Addr()

	p.bySrc[src]--
	if p.bySrc[src] == 0 {
		delete(p.bySrc, src)
	}

	p.held -= pend.held
}

// expire drops the handshakes past their deadline, which are the oldest.
func (p *Processor) expire(now time.Time) {
	for e := p.order.Front(); e != nil; e = p.order.Front() {
		old, _ := e.Value.(*pending)
		if !now.After(old.expires) {
			return
		}

		p.drop(old)
	}
}
