package capture

import (
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

// These tests run the capture on a real tun device, the kind tailscaled
// writes the tailnet's packets into. Run them with
// flowd/testdata/netns-test.sh.

func needsKernel(t *testing.T) {
	t.Helper()

	if os.Getenv("FLOWD_NETNS_TEST") == "" || os.Geteuid() != 0 {
		t.Skip("needs root in a privileged Linux container; run flowd/testdata/netns-test.sh")
	}
}

// tunDevice creates a tun device that takes the offload header, as
// tailscaled's does, with the given offloads, and brings it up. Packets
// written to it arrive on the device as if from the tailnet.
func tunDevice(t *testing.T, name string, offloads int) *os.File {
	t.Helper()

	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR|unix.O_CLOEXEC, 0)
	require.NoError(t, err)

	tun := os.NewFile(uintptr(fd), name)

	t.Cleanup(func() { _ = tun.Close() })

	ifr, err := unix.NewIfreq(name)
	require.NoError(t, err)

	ifr.SetUint16(unix.IFF_TUN | unix.IFF_NO_PI | unix.IFF_VNET_HDR)
	require.NoError(t, unix.IoctlIfreq(fd, unix.TUNSETIFF, ifr))

	if offloads != 0 {
		err = unix.IoctlSetInt(fd, unix.TUNSETOFFLOAD, offloads)
		if err != nil {
			t.Skipf("the kernel's tun has no UDP segmentation offload: %v", err)
		}
	}

	out, err := exec.CommandContext(t.Context(), "ip", "link", "set", name, "up").CombinedOutput()
	require.NoError(t, err, "%s", out)

	return tun
}

// vnetHeader is a virtio_net_hdr: none for a plain packet, UDP
// segmentation for a merged one.
func vnetHeader(gsoSize, hdrLen int) []byte {
	hdr := make([]byte, vnetHdrLen)
	if gsoSize == 0 {
		return hdr
	}

	hdr[0] = 1 // the checksum is left to the receiver
	hdr[vnetGSOType] = vnetGSOUDPL4
	binary.NativeEndian.PutUint16(hdr[2:], uint16(hdrLen+udpHeader))
	binary.NativeEndian.PutUint16(hdr[vnetGSOSize:], uint16(gsoSize))
	binary.NativeEndian.PutUint16(hdr[6:], uint16(hdrLen))
	binary.NativeEndian.PutUint16(hdr[8:], 6) // the UDP checksum field

	return hdr
}

// TestRunReturnsWhileFlooded: the capture used to read until the socket
// was empty before looking at its context, so a node sending faster than
// the processor reads (the review's QUIC flood) kept it running after the
// agent asked it to stop, and the agent's shutdown hung.
func TestRunReturnsWhileFlooded(t *testing.T) {
	needsKernel(t)

	const name = "flt-flood"

	tun := tunDevice(t, name, 0)
	pkt := append(vnetHeader(0, 0), ipv4(node, remote, 17, udp(5000, quicVector(t)))...)

	floodCtx, stopFlood := context.WithCancel(t.Context())
	defer stopFlood()

	go func() {
		for floodCtx.Err() == nil {
			_, _ = tun.Write(pkt)
		}
	}()

	ctx, cancel := context.WithCancel(t.Context())
	seen := make(chan struct{}, 1)
	done := make(chan error, 1)

	go func() {
		done <- Run(ctx, name, func([]byte, time.Time) {
			select {
			case seen <- struct{}{}:
			default:
			}

			// A processor slower than the flood.
			start := time.Now()
			for time.Since(start) < 50*time.Microsecond {
				runtime.Gosched()
			}
		})
	}()

	select {
	case <-seen:
	case <-time.After(5 * time.Second):
		t.Fatal("no packet captured")
	}

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Run kept reading after its context ended")
	}
}

// TestRunSplitsMergedDatagrams writes a real ClientHello's Initials into
// a tun device as one UDP segmentation-offloaded packet, the way
// tailscaled merges a burst of datagrams, and checks that the capture
// hands each datagram over on its own and that the hello is read.
func TestRunSplitsMergedDatagrams(t *testing.T) {
	needsKernel(t)

	for _, tc := range []struct {
		name   string
		v6     bool
		hdrLen int
	}{
		{name: "flt-gro4", hdrLen: ipv4MinHeader},
		{name: "flt-gro6", v6: true, hdrLen: ipv6Header},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tun := tunDevice(t, tc.name, unix.TUN_F_CSUM|unix.TUN_F_USO4|unix.TUN_F_USO6)

			build := func(port uint16, payload []byte) []byte {
				if tc.v6 {
					return ipv6(node6, remote6, 17, udp(port, payload))
				}

				return ipv4(node, remote, 17, udp(port, payload))
			}

			captured := make(chan []byte, 64)

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan error, 1)

			go func() {
				done <- Run(ctx, tc.name, func(pkt []byte, _ time.Time) { captured <- slices.Clone(pkt) })
			}()

			t.Cleanup(func() {
				cancel()
				require.NoError(t, <-done)
			})

			// The socket is bound once a probe comes through.
			probe := append(vnetHeader(0, 0), build(1, quicVector(t))...)

			for waiting := true; waiting; {
				_, err := tun.Write(probe)
				require.NoError(t, err)

				select {
				case <-captured:
					waiting = false
				case <-time.After(100 * time.Millisecond):
				}
			}

			var joined []byte
			for _, d := range quicDatagrams(t, "offload.example") {
				joined = append(joined, d...)
			}

			_, err := tun.Write(append(vnetHeader(1200, tc.hdrLen), build(7000, joined)...))
			require.NoError(t, err)

			var got []Hello

			p := NewProcessor(func(h Hello) { got = append(got, h) })

			var sizes []int

			for len(sizes) < 3 {
				select {
				case pkt := <-captured:
					_, hdrLen, ok := ipHeader(pkt)
					require.True(t, ok)

					if binary.BigEndian.Uint16(pkt[hdrLen:]) != 7000 {
						continue // a late probe
					}

					sizes = append(sizes, len(pkt)-hdrLen-udpHeader)
					p.Packet(pkt, time.Now())
				case <-time.After(5 * time.Second):
					t.Fatalf("captured %v, want three datagrams", sizes)
				}
			}

			assert.Equal(t, []int{1200, 1200, 1200}, sizes)
			require.Len(t, got, 1)
			assert.Equal(t, "offload.example", got[0].Hello.ServerName)
		})
	}
}
