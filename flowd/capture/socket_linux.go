package capture

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/bpf"
	"golang.org/x/sys/unix"
)

const (
	pollTimeoutMillis = 500
	receiveBuffer     = 4 << 20
	// readBatch is the most packets read before the context is checked
	// again, so a socket that never drains cannot keep Run from returning.
	readBatch = 256

	// A virtio_net_hdr precedes each packet read from a socket with
	// PACKET_VNET_HDR; its second byte is the offload type and the one at
	// vnetGSOSize the segment size, in host byte order.
	vnetHdrLen    = 10
	vnetGSOType   = 1
	vnetGSOSize   = 4
	vnetGSOECN    = 0x80
	vnetGSOUDPL4  = 5
	arphrdNone    = 0xfffe
	sysfsNetClass = "/sys/class/net/"
)

// Run captures the packets arriving on iface (from the tailnet nodes, not
// those the gateway sends to them) that pass [filterProgram], handing
// each to onPacket, until ctx ends. A UDP packet the tunnel merged from
// several datagrams (tailscaled's receive offload) is handed over one
// datagram at a time.
func Run(ctx context.Context, iface string, onPacket func(pkt []byte, now time.Time)) error {
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return fmt.Errorf("finding interface %s: %w", iface, err)
	}

	fd, vnet, err := open(ifi)
	if err != nil {
		return err
	}
	defer unix.Close(fd)

	buf := make([]byte, vnetHdrLen+snapLen)
	scratch := make([]byte, 0, snapLen)
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}} //nolint:gosec // file descriptors fit in int32

	for ctx.Err() == nil {
		n, err := unix.Poll(fds, pollTimeoutMillis)
		if errors.Is(err, unix.EINTR) || n == 0 {
			continue
		}

		if err != nil {
			return fmt.Errorf("polling the capture socket: %w", err)
		}

		for range readBatch {
			size, from, err := unix.Recvfrom(fd, buf, unix.MSG_DONTWAIT)
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
				break
			}

			// A packet whose offload the header cannot describe is
			// dropped with EINVAL; the next one reads normally.
			if errors.Is(err, unix.EINVAL) {
				continue
			}

			if err != nil {
				return fmt.Errorf("reading the capture socket: %w", err)
			}

			if ll, ok := from.(*unix.SockaddrLinklayer); ok && ll.Pkttype == unix.PACKET_OUTGOING {
				continue
			}

			deliver(buf[:size], vnet, scratch, onPacket)
		}
	}

	return nil
}

// deliver hands a packet read from the socket to onPacket, split into its
// datagrams when the offload header says it was merged from several.
func deliver(pkt []byte, vnet bool, scratch []byte, onPacket func([]byte, time.Time)) {
	now := time.Now()

	if !vnet {
		onPacket(pkt, now)

		return
	}

	if len(pkt) < vnetHdrLen {
		return
	}

	hdr, pkt := pkt[:vnetHdrLen], pkt[vnetHdrLen:]
	if hdr[vnetGSOType]&^vnetGSOECN != vnetGSOUDPL4 {
		onPacket(pkt, now)

		return
	}

	segSize := int(binary.NativeEndian.Uint16(hdr[vnetGSOSize:]))
	splitUDP(pkt, segSize, scratch, func(seg []byte) { onPacket(seg, now) })
}

// open returns a packet socket on the interface with the filter attached,
// and whether its packets carry the offload header. The socket is created
// for no protocol, so it receives nothing until it is bound, after the
// filter is in place.
//
// On a tunnel device, which has no link-layer header, a raw socket reads
// the same bytes as a datagram socket and can also carry the offload
// header (only raw sockets can); elsewhere a datagram socket strips the
// link-layer header the filter does not expect.
func open(ifi *net.Interface) (int, bool, error) {
	tunnel := linkType(ifi.Name) == arphrdNone

	typ := unix.SOCK_DGRAM
	if tunnel {
		typ = unix.SOCK_RAW
	}

	fd, err := unix.Socket(unix.AF_PACKET, typ|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		return -1, false, fmt.Errorf("opening a packet socket (needs CAP_NET_RAW): %w", err)
	}

	vnet := tunnel && unix.SetsockoptInt(fd, unix.SOL_PACKET, unix.PACKET_VNET_HDR, 1) == nil

	err = attachFilter(fd)
	if err == nil {
		// The forced size goes past net.core.rmem_max with CAP_NET_ADMIN,
		// which the agent has for connection tracking.
		if unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUFFORCE, receiveBuffer) != nil {
			_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, receiveBuffer)
		}

		err = unix.Bind(fd, &unix.SockaddrLinklayer{Protocol: htons(uint16(unix.ETH_P_ALL)), Ifindex: ifi.Index})
		if err != nil {
			err = fmt.Errorf("binding the packet socket: %w", err)
		}
	}

	if err != nil {
		_ = unix.Close(fd)

		return -1, false, err
	}

	return fd, vnet, nil
}

// linkType is the interface's ARPHRD_ type, or -1 when it cannot be read.
func linkType(name string) int {
	raw, err := os.ReadFile(sysfsNetClass + name + "/type")
	if err != nil {
		return -1
	}

	typ, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return -1
	}

	return typ
}

func attachFilter(fd int) error {
	prog, err := filterProgram()
	if err != nil {
		return err
	}

	raw, err := bpf.Assemble(prog)
	if err != nil {
		return fmt.Errorf("assembling the capture filter: %w", err)
	}

	filter := make([]unix.SockFilter, len(raw))
	for i, ins := range raw {
		filter[i] = unix.SockFilter{Code: ins.Op, Jt: ins.Jt, Jf: ins.Jf, K: ins.K}
	}

	//nolint:gosec // the program has fewer than 64 instructions
	fprog := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}

	err = unix.SetsockoptSockFprog(fd, unix.SOL_SOCKET, unix.SO_ATTACH_FILTER, &fprog)
	if err != nil {
		return fmt.Errorf("attaching the capture filter: %w", err)
	}

	return nil
}

// htons converts to network byte order, as packet sockets want protocols.
func htons(v uint16) uint16 {
	var b [2]byte

	binary.BigEndian.PutUint16(b[:], v)

	return binary.NativeEndian.Uint16(b[:])
}
