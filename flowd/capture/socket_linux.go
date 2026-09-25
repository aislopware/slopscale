package capture

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"

	"golang.org/x/net/bpf"
	"golang.org/x/sys/unix"
)

const (
	pollTimeoutMillis = 500
	receiveBuffer     = 4 << 20
)

// Run captures the packets arriving on iface (from the tailnet nodes, not
// those the gateway sends to them) that pass [filterProgram], handing
// each to onPacket, until ctx ends.
func Run(ctx context.Context, iface string, onPacket func(pkt []byte, now time.Time)) error {
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return fmt.Errorf("finding interface %s: %w", iface, err)
	}

	fd, err := open(ifi.Index)
	if err != nil {
		return err
	}
	defer unix.Close(fd)

	buf := make([]byte, snapLen)
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}} //nolint:gosec // file descriptors fit in int32

	for ctx.Err() == nil {
		n, err := unix.Poll(fds, pollTimeoutMillis)
		if errors.Is(err, unix.EINTR) || n == 0 {
			continue
		}

		if err != nil {
			return fmt.Errorf("polling the capture socket: %w", err)
		}

		for {
			size, from, err := unix.Recvfrom(fd, buf, unix.MSG_DONTWAIT)
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
				break
			}

			if err != nil {
				return fmt.Errorf("reading the capture socket: %w", err)
			}

			if ll, ok := from.(*unix.SockaddrLinklayer); ok && ll.Pkttype == unix.PACKET_OUTGOING {
				continue
			}

			onPacket(buf[:size], time.Now())
		}
	}

	return nil
}

// open returns a packet socket on the interface with the filter attached.
// The socket is created for no protocol, so it receives nothing until it
// is bound, after the filter is in place.
func open(ifindex int) (int, error) {
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, 0)
	if err != nil {
		return -1, fmt.Errorf("opening a packet socket (needs CAP_NET_RAW): %w", err)
	}

	err = attachFilter(fd)
	if err == nil {
		_ = unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_RCVBUF, receiveBuffer)

		err = unix.Bind(fd, &unix.SockaddrLinklayer{Protocol: htons(uint16(unix.ETH_P_ALL)), Ifindex: ifindex})
		if err != nil {
			err = fmt.Errorf("binding the packet socket: %w", err)
		}
	}

	if err != nil {
		_ = unix.Close(fd)

		return -1, err
	}

	return fd, nil
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
