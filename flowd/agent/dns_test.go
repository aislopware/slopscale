package agent

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"codeberg.org/miekg/dns"
	"github.com/aislopware/slopscale/flowd/names"
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

	require.Never(t, func() bool {
		st, l := dnsStatus(a)

		return st.Error != "" || !slices.Equal(listen, l)
	}, time.Second, 200*time.Millisecond)

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

// TestDNSAnswerClearsAFailedProbe: the upstream was not up when the
// resolver first probed it (the end-to-end test starts them together).
// The failure used to stay in every report until the next probe, 30 s
// later, although clients were being answered; an answer to a client now
// proves the resolver at once.
func TestDNSAnswerClearsAFailedProbe(t *testing.T) {
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
		dnsCheckEvery: time.Hour,
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

		return st.Error != "" && len(l) == 1
	}, 10*time.Second, 20*time.Millisecond)

	on.Store(true)
	ask(t, listen[0], "up.example.")

	require.Eventually(t, func() bool {
		st, _ := dnsStatus(a)

		return st.Enabled && st.Error == ""
	}, 2*time.Second, 20*time.Millisecond)
}

// TestDNSRecordsOnlyTheExitNodeUsers answers every asker, but records the
// questions, and names flows from the answers, only of the nodes the
// server lists as using the gateway as their exit node, following each
// response at once. The list is never saved.
func TestDNSRecordsOnlyTheExitNodeUsers(t *testing.T) {
	stateDir := t.TempDir()
	local := &fakeLocal{ips: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}

	a, err := newAgent(t.Context(), Options{
		Local:      local,
		Server:     "http://127.0.0.1:1",
		StateDir:   stateDir,
		SpoolBytes: 1 << 20,
		Logger:     slog.New(slog.DiscardHandler),
		dnsPort:    freePort(t),
		allowDNS:   func(netip.Addr) bool { return true },
	})
	require.NoError(t, err)

	cfg := traffic.Config{DNS: true, Upstreams: []string{upstream(t)}}
	a.applyConfig(cfg)

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
		_, listen = dnsStatus(a)

		return len(listen) == 1
	}, 5*time.Second, 20*time.Millisecond)

	self := netip.MustParseAddr("127.0.0.1")
	flow := names.Conn{
		Src:   netip.AddrPortFrom(self, 40000),
		Dst:   netip.MustParseAddrPort("192.0.2.44:443"),
		Proto: 6,
	}
	recorded := func(name string) ([]traffic.Query, string) {
		t.Helper()

		ask(t, listen[0], name)

		_, queries, _ := a.table.Drain(math.MaxInt64)
		host, _ := a.resolver.Lookup(flow, time.Now())

		return queries, host
	}

	queries, host := recorded("before.example.")
	assert.Empty(t, queries, "nobody uses the gateway as their exit node, nobody is recorded")
	assert.Empty(t, host, "and no answer names a flow")

	withSelf := cfg
	withSelf.LogSources = []netip.Addr{netip.MustParseAddr("100.64.0.9"), self}
	a.applyConfig(withSelf)

	queries, host = recorded("during.example.")
	require.Len(t, queries, 1)
	assert.Equal(t, self, queries[0].Src)
	assert.Equal(t, "during.example", queries[0].Name)
	assert.Equal(t, "during.example", host)

	raw, err := os.ReadFile(filepath.Join(stateDir, configFile))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "logSources", "the list is never saved")

	a.applyConfig(cfg)

	queries, _ = recorded("after.example.")
	assert.Empty(t, queries, "a node that dropped the exit node is recorded no more")
}
