package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"
	"time"

	"codeberg.org/miekg/dns"
	"github.com/aislopware/slopscale/flowd/conntrack"
	"github.com/aislopware/slopscale/flowd/names"
	"github.com/aislopware/slopscale/flowd/rollup"
	"github.com/aislopware/slopscale/flowd/spool"
	"github.com/aislopware/slopscale/hscontrol/traffic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/types/appctype"
)

// fakeLocal stands in for tailscaled's LocalAPI.
type fakeLocal struct {
	mu        sync.Mutex
	ips       []netip.Addr
	prefs     ipn.Prefs
	routeInfo appctype.RouteInfo
	tokens    int
	statusErr error
	userspace bool
}

func (f *fakeLocal) failStatus(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.statusErr = err
}

func (f *fakeLocal) IDToken(context.Context, string) (*tailcfg.TokenResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.tokens++

	return &tailcfg.TokenResponse{IDToken: "token"}, nil
}

func (f *fakeLocal) StatusWithoutPeers(context.Context) (*ipnstate.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.statusErr != nil {
		return nil, f.statusErr
	}

	return &ipnstate.Status{TUN: !f.userspace, Self: &ipnstate.PeerStatus{TailscaleIPs: f.ips}}, nil
}

func (f *fakeLocal) GetPrefs(context.Context) (*ipn.Prefs, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	p := f.prefs

	return &p, nil
}

func (f *fakeLocal) GetAppConnectorRouteInfo(context.Context) (appctype.RouteInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.routeInfo, nil
}

// reportServer is the server side of the report endpoint.
type reportServer struct {
	mu      sync.Mutex
	config  traffic.Config
	reports []*traffic.Report
	auth    []string
}

func (s *reportServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)

	report, err := spool.Decode(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)

		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.reports = append(s.reports, report)
	s.auth = append(s.auth, r.Header.Get("Authorization"))

	_ = json.NewEncoder(w).Encode(traffic.Response{Seq: report.Seq, Config: s.config})
}

func (s *reportServer) last() *traffic.Report {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.reports[len(s.reports)-1]
}

func (s *reportServer) setConfig(c traffic.Config) {
	s.mu.Lock()
	s.config = c
	s.mu.Unlock()
}

// upstream answers every A question with 192.0.2.44 for five minutes.
func upstream(t *testing.T) string {
	t.Helper()

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { pc.Close() })

	go func() {
		buf := make([]byte, 65535)

		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}

			q := &dns.Msg{Data: append([]byte(nil), buf[:n]...)}
			if q.Unpack() != nil {
				continue
			}

			m := new(dns.Msg)
			m.ID, m.Response, m.Question = q.ID, true, q.Question
			m.Answer = []dns.RR{&dns.A{
				Hdr:  dns.Header{Name: q.Question[0].Header().Name, TTL: 300, Class: dns.ClassINET},
				Addr: netip.MustParseAddr("192.0.2.44"),
			}}

			if m.Pack() == nil {
				_, _ = pc.WriteTo(m.Data, from)
			}
		}
	}()

	return pc.LocalAddr().String()
}

func freePort(t *testing.T) uint16 {
	t.Helper()

	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)

	port := pc.LocalAddr().(*net.UDPAddr).Port
	require.NoError(t, pc.Close())

	return uint16(port)
}

func ask(t *testing.T, server netip.AddrPort, name string) {
	t.Helper()

	m := dns.NewMsg(name, dns.TypeA)
	require.NoError(t, m.Pack())

	conn, err := net.Dial("udp", server.String())
	require.NoError(t, err)

	defer conn.Close()

	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))

	_, err = conn.Write(m.Data)
	require.NoError(t, err)

	buf := make([]byte, 1500)
	_, err = conn.Read(buf)
	require.NoError(t, err)
}

// TestAgentReportsAndFollowsConfig drives the agent against a report
// endpoint: a heartbeat brings the server's configuration, which turns the
// resolver on; a question through it names the destination of the next
// connection; both reach the server; turning DNS off stops the resolver;
// the configuration and the instance survive a restart.
func TestAgentReportsAndFollowsConfig(t *testing.T) {
	srv := &reportServer{config: traffic.Config{
		SNI:            false,
		DNS:            true,
		Upstreams:      []string{upstream(t)},
		ReportInterval: 15,
	}}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	local := &fakeLocal{ips: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}
	local.prefs.ControlURL = ts.URL
	stateDir := t.TempDir()

	opts := Options{
		Local:      local,
		StateDir:   stateDir,
		SpoolBytes: 1 << 20,
		Version:    "test",
		Logger:     slog.New(slog.DiscardHandler),
		dnsPort:    freePort(t),
		allowDNS:   func(netip.Addr) bool { return true },
	}

	a, err := newAgent(t.Context(), opts)
	require.NoError(t, err)

	// The first report is a heartbeat and returns the configuration.
	a.report(math.MaxInt64)
	_, err = a.uploader.Drain(t.Context())
	require.NoError(t, err)

	first := srv.last()
	assert.Equal(t, "test", first.Version)
	assert.Empty(t, first.Flows)
	assert.True(t, first.Status.Conntrack.Enabled)
	assert.Equal(t, "Bearer token", srv.auth[0])

	cfg, _ := a.current()
	require.True(t, cfg.DNS)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		a.runDNS(ctx)
		close(done)
	}()

	var listen []netip.AddrPort

	require.Eventually(t, func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()

		listen = a.dnsListen

		return len(listen) == 1
	}, 5*time.Second, 20*time.Millisecond)

	ask(t, listen[0], "Downloads.Example.net.")

	// The node then connects to the address it was given.
	a.onDelta(conntrack.Delta{
		Conn: names.Conn{
			Src:   netip.MustParseAddrPort("127.0.0.1:40000"),
			Dst:   netip.MustParseAddrPort("192.0.2.44:443"),
			Proto: 6,
		},
		Counters: rollup.Counters{TxBytes: 1000, RxBytes: 50000, TxPackets: 10, RxPackets: 40, Conns: 1},
	})

	a.report(math.MaxInt64)
	_, err = a.uploader.Drain(t.Context())
	require.NoError(t, err)

	second := srv.last()
	assert.Equal(t, listen, second.DNSListen)
	assert.True(t, second.Status.DNS.Enabled)
	assert.Empty(t, second.Status.DNS.Error)
	require.Len(t, second.Flows, 1)
	assert.Equal(t, "downloads.example.net", second.Flows[0].Host)
	assert.Equal(t, traffic.HostDNS, second.Flows[0].HostSource)
	assert.Equal(t, uint16(443), second.Flows[0].Port)
	assert.Equal(t, uint64(50000), second.Flows[0].RxBytes)
	require.Len(t, second.Queries, 1)
	assert.Equal(t, traffic.Query{
		Bucket: second.Queries[0].Bucket, Src: netip.MustParseAddr("127.0.0.1"), Name: "downloads.example.net", Count: 1,
	}, second.Queries[0])

	// The server turns DNS off: the resolver stops.
	srv.setConfig(traffic.Config{SNI: false, ReportInterval: 15})
	a.report(math.MaxInt64)
	_, err = a.uploader.Drain(t.Context())
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()

		return len(a.dnsListen) == 0
	}, 5*time.Second, 20*time.Millisecond)

	conn, err := net.Dial("udp", listen[0].String())
	require.NoError(t, err)

	_, _ = conn.Write([]byte{0, 1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(300*time.Millisecond)))

	_, err = conn.Read(make([]byte, 512))
	require.Error(t, err, "nothing answers once the resolver is off")
	conn.Close()

	cancel()
	<-done

	// A restart keeps the instance and the last configuration.
	b, err := newAgent(t.Context(), opts)
	require.NoError(t, err)
	assert.Equal(t, a.spool.Instance(), b.spool.Instance())

	cfg, _ = b.current()
	assert.False(t, cfg.DNS)
	assert.False(t, cfg.SNI)
	assert.Equal(t, 15, cfg.ReportInterval)
}

func TestReportSplitsLargeRollups(t *testing.T) {
	srv := &reportServer{config: traffic.Config{SNI: true, ReportInterval: 60}}
	ts := httptest.NewServer(srv)
	t.Cleanup(ts.Close)

	a, err := newAgent(t.Context(), Options{
		Local:      &fakeLocal{},
		Server:     ts.URL,
		StateDir:   t.TempDir(),
		SpoolBytes: 64 << 20,
	})
	require.NoError(t, err)

	src := netip.MustParseAddr("100.64.0.3")
	for i := range traffic.MaxFlowsPerReport + 5 {
		a.table.AddFlow(rollup.FlowKey{
			Bucket: 60, Src: src, Dst: netip.AddrFrom4([4]byte{198, 51, byte(i >> 8), byte(i)}), Proto: 17, Port: uint16(i),
		}, rollup.Counters{TxBytes: 1})
	}

	a.report(math.MaxInt64)
	_, err = a.uploader.Drain(t.Context())
	require.NoError(t, err)

	require.Len(t, srv.reports, 2)
	assert.Len(t, srv.reports[0].Flows, traffic.MaxFlowsPerReport)
	assert.Len(t, srv.reports[1].Flows, 5)
}

func TestServerURL(t *testing.T) {
	local := &fakeLocal{}

	_, err := serverURL(t.Context(), Options{Local: local})
	require.Error(t, err, "no control URL")

	local.prefs.ControlURL = "https://controlplane.tailscale.com"
	_, err = serverURL(t.Context(), Options{Local: local})
	require.Error(t, err, "the hosted control plane")

	local.prefs.ControlURL = "https://vpn.example.com"
	got, err := serverURL(t.Context(), Options{Local: local})
	require.NoError(t, err)
	assert.Equal(t, "https://vpn.example.com", got)

	got, err = serverURL(t.Context(), Options{Local: local, Server: "https://other.example"})
	require.NoError(t, err)
	assert.Equal(t, "https://other.example", got)
}

func TestAppConnectorNames(t *testing.T) {
	local := &fakeLocal{}
	local.prefs.AppConnector.Advertise = true
	local.routeInfo.Domains = map[string][]netip.Addr{"gitlab.example.com": {netip.MustParseAddr("198.51.100.72")}}

	a, err := newAgent(t.Context(), Options{Local: local, Server: "http://unused", StateDir: t.TempDir(), SpoolBytes: 1 << 20})
	require.NoError(t, err)

	a.refreshAppConnector(t.Context())

	host, source := a.resolver.Lookup(names.Conn{
		Src: netip.MustParseAddrPort("100.64.0.3:1"), Dst: netip.MustParseAddrPort("198.51.100.72:22"), Proto: 6,
	}, time.Now())
	assert.Equal(t, "gitlab.example.com", host)
	assert.Equal(t, traffic.HostAppConnector, source)
	assert.Equal(t, traffic.Collector{Enabled: true}, a.status.AppConnector)

	local.prefs.AppConnector.Advertise = false
	a.refreshAppConnector(t.Context())
	assert.Equal(t, traffic.Collector{}, a.status.AppConnector)
}

func TestServicePort(t *testing.T) {
	c := names.Conn{Dst: netip.MustParseAddrPort("192.0.2.1:8443")}

	for proto, want := range map[uint8]uint16{6: 8443, 17: 8443, 132: 8443, 1: 0, 58: 0, 47: 0} {
		c.Proto = proto
		assert.Equal(t, want, servicePort(c), "proto %d", proto)
	}
}

// TestSNICaptureSurvivesOtherConfigChanges: turning DNS logging on or
// changing upstreams leaves the handshake capture running, so no
// handshake falls into a restart gap; only switching SNI off stops it.
func TestSNICaptureSurvivesOtherConfigChanges(t *testing.T) {
	starts := make(chan struct{}, 10)
	stops := make(chan struct{}, 10)

	a, err := newAgent(t.Context(), Options{
		Local: &fakeLocal{}, Server: "http://unused", StateDir: t.TempDir(), SpoolBytes: 1 << 20,
		capture: func(ctx context.Context, _ string, _ func([]byte, time.Time)) error {
			starts <- struct{}{}

			<-ctx.Done()

			stops <- struct{}{}

			return ctx.Err()
		},
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		a.runSNI(ctx)
		close(done)
	}()

	receive := func(ch chan struct{}, what string) {
		t.Helper()

		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			require.FailNow(t, "the capture did not "+what)
		}
	}
	none := func(ch chan struct{}, what string) {
		t.Helper()

		select {
		case <-ch:
			require.FailNow(t, "the capture should not "+what)
		case <-time.After(200 * time.Millisecond):
		}
	}

	receive(starts, "start")

	a.applyConfig(traffic.Config{SNI: true, DNS: true, Upstreams: []string{"192.0.2.53"}})
	a.applyConfig(traffic.Config{SNI: true, DNS: false, ReportInterval: 120})
	none(stops, "stop on a change that keeps SNI on")
	none(starts, "restart on a change that keeps SNI on")

	a.applyConfig(traffic.Config{SNI: false})
	receive(stops, "stop when SNI is switched off")
	assert.Eventually(t, func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()

		return a.status.SNI == traffic.Collector{}
	}, 5*time.Second, 10*time.Millisecond, "the status should say SNI is off")

	a.applyConfig(traffic.Config{SNI: true})
	receive(starts, "start again when SNI is switched on")

	cancel()
	receive(stops, "stop with the agent")
	<-done
}

var errBoom = errors.New("boom")

// TestSupervisorRecordsAndRestarts: a failing collector is restarted and
// its error shows in the status until it runs again.
func TestSupervisorRecordsAndRestarts(t *testing.T) {
	a := &Agent{log: slog.New(slog.DiscardHandler)}

	ctx, cancel := context.WithCancel(t.Context())

	var (
		mu     sync.Mutex
		errs   []error
		starts int
	)

	a.supervise(ctx, "test", func(err error) {
		mu.Lock()
		errs = append(errs, err)
		mu.Unlock()
	}, func(context.Context) error {
		starts++
		cancel() // one run, then the agent stops

		return errBoom
	})

	assert.Equal(t, 1, starts)
	assert.Equal(t, []error{nil}, errs, "a failure while stopping is not a failure")
}
