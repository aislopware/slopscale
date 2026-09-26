package capture

import (
	"errors"
	"fmt"

	"github.com/aislopware/slopscale/flowd/sni"
	"golang.org/x/net/bpf"
)

// snapLen keeps whole packets, including segmentation-offloaded ones up
// to 64 KiB and more.
const snapLen = 256 << 10

// helloSnap keeps a TCP packet's headers and a ClientHello of the largest
// size read. A segment that continues a hello is no longer than this, so
// a longer one (bulk data the tunnel merged) never leaves the kernel, and
// a packet that starts a hello is cut to it.
const helloSnap = sni.MaxHelloSize + 1<<10

// Filter offsets and values, for raw IP packets starting at offset 0 (a
// tunnel device, or a packet socket of type SOCK_DGRAM on any device).
const (
	offVersion      = 0
	offV4Frag       = 6
	offV4Proto      = 9
	offV6Next       = 6
	offTCPDataOff   = 12
	offTCPFlags     = 13
	offV6TCPDataOff = ipv6Header + offTCPDataOff
	offV6TCPFlags   = ipv6Header + offTCPFlags
	offV6UDPFirst   = ipv6Header + udpHeader
	versionMask     = 0xf0
	version4        = 0x40
	version6        = 0x60
	tlsHandshake    = 0x16
	tlsMajor        = 0x03
	tcpFlagPSH      = 0x08
	quicLongFixed   = 0xc0
	dataOffShift    = 4
	dataOffToBytes  = 2
)

// asmOp is an instruction with symbolic jump targets, which [assemble]
// turns into the relative skips classic BPF wants.
type asmOp struct {
	label  string
	ins    bpf.Instruction
	jt, jf string
}

// errBadJump is returned for a jump classic BPF cannot express.
var errBadJump = errors.New("bad jump")

const (
	next   = "next"
	accept = "accept"
	hello  = "hello"
	reject = "reject"
)

// filterProgram accepts the packets that may carry a client's TLS
// ClientHello or QUIC Initial:
//
//   - unfragmented TCP with payload starting a TLS handshake record, or
//     with PSH set and no longer than a hello (the last segment of a
//     ClientHello split over two);
//   - UDP whose payload starts with a QUIC long header;
//   - IPv6 with extension headers before the transport header, which the
//     processor walks.
//
// Everything else (bulk data, ACKs, other protocols) never leaves the
// kernel. A stateless filter cannot tell the middle segments of a hello
// split over three or more from bulk data, so those hellos are read only
// when the tunnel merged the segments (tailscaled's receive offload merges
// a burst), as it does for most.
func filterProgram() ([]bpf.Instruction, error) {
	prog := []asmOp{
		{ins: bpf.LoadAbsolute{Off: offVersion, Size: 1}},
		{ins: bpf.ALUOpConstant{Op: bpf.ALUOpAnd, Val: versionMask}},
		{ins: bpf.JumpIf{Cond: bpf.JumpEqual, Val: version4}, jt: "v4", jf: next},
		{ins: bpf.JumpIf{Cond: bpf.JumpEqual, Val: version6}, jt: "v6", jf: reject},

		{label: "v4", ins: bpf.LoadAbsolute{Off: offV4Frag, Size: 2}},
		{ins: bpf.JumpIf{Cond: bpf.JumpBitsSet, Val: ipv4FragMask}, jt: reject, jf: next},
		{ins: bpf.LoadAbsolute{Off: offV4Proto, Size: 1}},
		{ins: bpf.JumpIf{Cond: bpf.JumpEqual, Val: protoTCP}, jt: "v4tcp", jf: next},
		{ins: bpf.JumpIf{Cond: bpf.JumpEqual, Val: protoUDP}, jt: "v4udp", jf: reject},

		{label: "v4tcp", ins: bpf.LoadMemShift{Off: offVersion}},
		{ins: bpf.LoadIndirect{Off: offTCPDataOff, Size: 1}},
		{ins: bpf.ALUOpConstant{Op: bpf.ALUOpShiftRight, Val: dataOffShift}},
		{ins: bpf.ALUOpConstant{Op: bpf.ALUOpShiftLeft, Val: dataOffToBytes}},
		{ins: bpf.ALUOpX{Op: bpf.ALUOpAdd}},
		{ins: bpf.TAX{}},
		{ins: bpf.LoadExtension{Num: bpf.ExtLen}},
		{ins: bpf.JumpIfX{Cond: bpf.JumpGreaterThan}, jt: next, jf: reject},
		{ins: bpf.LoadIndirect{Off: 0, Size: 1}},
		{ins: bpf.JumpIf{Cond: bpf.JumpEqual, Val: tlsHandshake}, jt: next, jf: "v4psh"},
		{ins: bpf.LoadIndirect{Off: 1, Size: 1}},
		{ins: bpf.JumpIf{Cond: bpf.JumpEqual, Val: tlsMajor}, jt: hello, jf: next},
		{label: "v4psh", ins: bpf.LoadMemShift{Off: offVersion}},
		{ins: bpf.LoadIndirect{Off: offTCPFlags, Size: 1}},
		{ins: bpf.JumpIf{Cond: bpf.JumpBitsSet, Val: tcpFlagPSH}, jt: "tail", jf: reject},

		{label: "v4udp", ins: bpf.LoadMemShift{Off: offVersion}},
		{ins: bpf.LoadIndirect{Off: udpHeader, Size: 1}},
		{ins: bpf.ALUOpConstant{Op: bpf.ALUOpAnd, Val: quicLongFixed}},
		{ins: bpf.JumpIf{Cond: bpf.JumpEqual, Val: quicLongFixed}, jt: accept, jf: reject},

		{label: "v6", ins: bpf.LoadAbsolute{Off: offV6Next, Size: 1}},
		{ins: bpf.JumpIf{Cond: bpf.JumpEqual, Val: protoTCP}, jt: "v6tcp", jf: next},
		{ins: bpf.JumpIf{Cond: bpf.JumpEqual, Val: protoUDP}, jt: "v6udp", jf: next},
		{ins: bpf.JumpIf{Cond: bpf.JumpEqual, Val: ipv6HopByHop}, jt: accept, jf: next},
		{ins: bpf.JumpIf{Cond: bpf.JumpEqual, Val: ipv6Routing}, jt: accept, jf: next},
		{ins: bpf.JumpIf{Cond: bpf.JumpEqual, Val: ipv6DestOptions}, jt: accept, jf: reject},

		{label: "v6tcp", ins: bpf.LoadAbsolute{Off: offV6TCPDataOff, Size: 1}},
		{ins: bpf.ALUOpConstant{Op: bpf.ALUOpShiftRight, Val: dataOffShift}},
		{ins: bpf.ALUOpConstant{Op: bpf.ALUOpShiftLeft, Val: dataOffToBytes}},
		{ins: bpf.ALUOpConstant{Op: bpf.ALUOpAdd, Val: ipv6Header}},
		{ins: bpf.TAX{}},
		{ins: bpf.LoadExtension{Num: bpf.ExtLen}},
		{ins: bpf.JumpIfX{Cond: bpf.JumpGreaterThan}, jt: next, jf: reject},
		{ins: bpf.LoadIndirect{Off: 0, Size: 1}},
		{ins: bpf.JumpIf{Cond: bpf.JumpEqual, Val: tlsHandshake}, jt: next, jf: "v6psh"},
		{ins: bpf.LoadIndirect{Off: 1, Size: 1}},
		{ins: bpf.JumpIf{Cond: bpf.JumpEqual, Val: tlsMajor}, jt: hello, jf: next},
		{label: "v6psh", ins: bpf.LoadAbsolute{Off: offV6TCPFlags, Size: 1}},
		{ins: bpf.JumpIf{Cond: bpf.JumpBitsSet, Val: tcpFlagPSH}, jt: "tail", jf: reject},

		{label: "v6udp", ins: bpf.LoadAbsolute{Off: offV6UDPFirst, Size: 1}},
		{ins: bpf.ALUOpConstant{Op: bpf.ALUOpAnd, Val: quicLongFixed}},
		{ins: bpf.JumpIf{Cond: bpf.JumpEqual, Val: quicLongFixed}, jt: accept, jf: reject},

		{label: "tail", ins: bpf.LoadExtension{Num: bpf.ExtLen}},
		{ins: bpf.JumpIf{Cond: bpf.JumpGreaterThan, Val: helloSnap}, jt: reject, jf: hello},

		{label: accept, ins: bpf.RetConstant{Val: snapLen}},
		{label: hello, ins: bpf.RetConstant{Val: helloSnap}},
		{label: reject, ins: bpf.RetConstant{Val: 0}},
	}

	return assemble(prog)
}

// assemble resolves the symbolic jumps. Classic BPF only jumps forward.
func assemble(prog []asmOp) ([]bpf.Instruction, error) {
	labels := make(map[string]int, len(prog))

	for i, op := range prog {
		if op.label != "" {
			labels[op.label] = i
		}
	}

	skip := func(from int, target string) (uint8, error) {
		if target == "" || target == next {
			return 0, nil
		}

		to, ok := labels[target]
		if !ok || to <= from || to-from-1 > 255 {
			return 0, fmt.Errorf("%w from %d to %q", errBadJump, from, target)
		}

		return uint8(to - from - 1), nil //nolint:gosec // checked above
	}

	out := make([]bpf.Instruction, len(prog))

	for i, op := range prog {
		jt, err := skip(i, op.jt)
		if err != nil {
			return nil, err
		}

		jf, err := skip(i, op.jf)
		if err != nil {
			return nil, err
		}

		switch ins := op.ins.(type) {
		case bpf.JumpIf:
			ins.SkipTrue, ins.SkipFalse = jt, jf
			out[i] = ins
		case bpf.JumpIfX:
			ins.SkipTrue, ins.SkipFalse = jt, jf
			out[i] = ins
		default:
			out[i] = op.ins
		}
	}

	return out, nil
}
