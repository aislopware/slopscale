package agent

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"codeberg.org/miekg/dns"
	"github.com/aislopware/slopscale/hscontrol/traffic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// switchableUpstream answers every question while on is set and drops it
// otherwise.
func switchableUpstream(t *testing.T) (string, *atomic.Bool) {
	t.Helper()

	pc, err := new(net.ListenConfig).ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { pc.Close() })

	var on atomic.Bool
	on.Store(true)

	go func() {
		buf := make([]byte, 65535)

		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}

			q := &dns.Msg{Data: append([]byte(nil), buf[:n]...)}
			if !on.Load() || q.Unpack() != nil {
				continue
			}

			m := new(dns.Msg)
			m.ID, m.Response, m.Question = q.ID, true, q.Question

			if m.Pack() == nil {
				_, _ = pc.WriteTo(m.Data, from)
			}
		}
	}()

	return pc.LocalAddr().String(), &on
}

func dnsStatus(a *Agent) (traffic.Collector, []netip.AddrPort) {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.status.DNS, a.dnsListen
}

// TestDNSHealthMeansAnswering reports the resolver healthy only while its
// upstreams answer: a failing LocalAPI read leaves a serving resolver
// alone, one lost probe is forgiven, two in a row are not, and it recovers
// when the upstream does.
func TestDNSHealthMeansAnswering(t *testing.T) {
	addr, on := switchableUpstream(t)
	local := &fakeLocal{ips: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}

	a, err := newAgent(t.Context(), Options{
		Local:         local,
		Server:        "http://127.0.0.1:1",
		StateDir:      t.TempDir(),
		SpoolBytes:    1 << 20,
		Logger:        slog.New(slog.DiscardHandler),
		dnsPort:       freePort(t),
		allowDNS:      func(netip.Addr) bool { return true },
		dnsCheckEvery: 200 * time.Millisecond,
	})
	require.NoError(t, err)

	a.applyConfig(traffic.Config{DNS: true, Upstreams: []string{addr}})

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		a.runDNS(ctx)
		close(done)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	var listen []netip.AddrPort

	require.Eventually(t, func() bool {
		st, l := dnsStatus(a)
		listen = l

		return st.Enabled && st.Error == "" && len(l) == 1
	}, 5*time.Second, 20*time.Millisecond)

	// tailscaled cannot be asked for a while: the resolver keeps serving
	// and stays healthy.
	local.failStatus(errors.New("tailscaled is restarting"))

	for range 5 {
		time.Sleep(200 * time.Millisecond)

		st, l := dnsStatus(a)
		require.Empty(t, st.Error)
		require.Equal(t, listen, l)
	}

	local.failStatus(nil)

	// The upstream stops answering: broken after two failed probes.
	on.Store(false)

	require.Eventually(t, func() bool {
		st, _ := dnsStatus(a)

		return st.Error != ""
	}, 5*time.Second, 20*time.Millisecond)

	st, l := dnsStatus(a)
	assert.Contains(t, st.Error, "not answering")
	assert.Equal(t, listen, l, "it keeps listening, clients' retries may still get through")

	on.Store(true)

	require.Eventually(t, func() bool {
		st, _ := dnsStatus(a)

		return st.Error == ""
	}, 5*time.Second, 20*time.Millisecond)
}

// TestDNSReportsANeverAnsweringUpstreamAtOnce starts with an upstream that
// does not answer: the resolver is broken at the first probe, it never
// proved itself.
func TestDNSReportsANeverAnsweringUpstreamAtOnce(t *testing.T) {
	addr, on := switchableUpstream(t)
	on.Store(false)

	a, err := newAgent(t.Context(), Options{
		Local:         &fakeLocal{ips: []netip.Addr{netip.MustParseAddr("127.0.0.1")}},
		Server:        "http://127.0.0.1:1",
		StateDir:      t.TempDir(),
		SpoolBytes:    1 << 20,
		Logger:        slog.New(slog.DiscardHandler),
		dnsPort:       freePort(t),
		allowDNS:      func(netip.Addr) bool { return true },
		dnsCheckEvery: 300 * time.Millisecond,
	})
	require.NoError(t, err)

	a.applyConfig(traffic.Config{DNS: true, Upstreams: []string{addr}})

	r := &dnsRunner{agent: a}
	t.Cleanup(r.stop)

	r.round(t.Context(), traffic.Config{DNS: true, Upstreams: []string{addr}})

	st, _ := dnsStatus(a)
	assert.Contains(t, st.Error, "not answering")
}
