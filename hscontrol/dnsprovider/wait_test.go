package dnsprovider_test

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"codeberg.org/miekg/dns"
	"codeberg.org/miekg/dns/dnsutil"
	"github.com/aislopware/slopscale/hscontrol/dnsprovider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errNoSuchZone = errors.New("no such zone")

// txtServer is an authoritative server answering TXT queries from a map.
type txtServer struct {
	mu     sync.Mutex
	txts   map[string][]string
	server *dns.Server
	port   string
}

func newTXTServer(t *testing.T) *txtServer {
	t.Helper()

	s := &txtServer{txts: map[string][]string{}}

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	_, s.port, err = net.SplitHostPort(listener.Addr().String())
	require.NoError(t, err)

	s.server = &dns.Server{
		Listener: listener,
		Handler: dns.HandlerFunc(func(_ context.Context, w dns.ResponseWriter, r *dns.Msg) {
			require.NoError(t, r.Unpack())

			reply := dnsutil.SetReply(new(dns.Msg), r)

			s.mu.Lock()
			defer s.mu.Unlock()

			if len(r.Question) == 1 {
				name := r.Question[0].Header().Name
				for _, value := range s.txts[name] {
					reply.Answer = append(reply.Answer, &dns.TXT{
						Hdr: dns.Header{Name: name, Class: dns.ClassINET, TTL: 60},
						Txt: []string{value},
					})
				}
			}

			_, _ = reply.WriteTo(w)
		}),
	}

	go func() { _ = s.server.ListenAndServe() }()

	t.Cleanup(func() { s.server.Shutdown(t.Context()) })

	return s
}

func (s *txtServer) add(name, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	name = dnsutil.Fqdn(name)
	s.txts[name] = append(s.txts[name], value)
}

// TestWaitVisible proves the wait finds the zone's nameservers above
// the record's name, returns once every one serves the value among the
// others at the name, and gives up with the context.
func TestWaitVisible(t *testing.T) {
	t.Parallel()

	first := newTXTServer(t)
	second := newTXTServer(t)

	lookups := map[string][]*net.NS{
		"example.com": {{Host: "127.0.0.1"}},
	}
	// Zones outside the map fail the way an NXDOMAIN does; the walk
	// up the name treats that as "not here".
	lookupNS := func(_ context.Context, zone string) ([]*net.NS, error) {
		records, ok := lookups[zone]
		if !ok {
			return nil, errNoSuchZone
		}

		return records, nil
	}

	name := "_acme-challenge.laptop.ts.example.com"

	t.Run("no nameservers", func(t *testing.T) {
		t.Parallel()

		checker := dnsprovider.Checker{LookupNS: lookupNS, Port: first.port, Interval: 10 * time.Millisecond}
		err := checker.WaitVisible(t.Context(), "_acme-challenge.laptop.ts.example.net", "v")
		require.ErrorIs(t, err, dnsprovider.ErrNoNameservers)
	})

	t.Run("gives up with the context", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
		defer cancel()

		checker := dnsprovider.Checker{LookupNS: lookupNS, Port: first.port, Interval: 10 * time.Millisecond}
		err := checker.WaitVisible(ctx, name, "never")
		require.ErrorIs(t, err, dnsprovider.ErrRecordNotVisible)
		require.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("returns once the server has it", func(t *testing.T) {
		t.Parallel()

		// The record arrives after the wait started, next to another
		// value at the same name; the wait spans the delay.
		second.add(name, "other")

		timer := time.AfterFunc(50*time.Millisecond, func() { second.add(name, "value") })
		defer timer.Stop()

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		started := time.Now()
		checker := dnsprovider.Checker{LookupNS: lookupNS, Port: second.port, Interval: 10 * time.Millisecond}
		require.NoError(t, checker.WaitVisible(ctx, name, "value"))
		assert.GreaterOrEqual(t, time.Since(started), 50*time.Millisecond)
	})
}
