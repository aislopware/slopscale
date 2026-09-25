package names

import (
	"net/netip"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/traffic"
	"github.com/stretchr/testify/assert"
)

func TestLookupPrecedence(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	laptop := netip.MustParseAddr("100.64.0.3")
	phone := netip.MustParseAddr("100.64.0.6")
	dst := netip.MustParseAddr("203.0.113.10")

	conn := Conn{
		Src:   netip.AddrPortFrom(laptop, 51234),
		Dst:   netip.AddrPortFrom(dst, 443),
		Proto: 6,
	}

	r := NewResolver()

	host, source := r.Lookup(conn, now)
	assert.Empty(t, host)
	assert.Empty(t, source)

	// Another node's answer is the weakest evidence.
	r.PutDNS(phone, "shared.example", []netip.Addr{dst}, time.Minute, now)
	host, source = r.Lookup(conn, now)
	assert.Equal(t, "shared.example", host)
	assert.Equal(t, traffic.HostDNSShared, source)

	// The public name of an encrypted hello beats it.
	r.PutSNI(conn, "cloudflare-ech.com", true, now)
	host, source = r.Lookup(conn, now)
	assert.Equal(t, "cloudflare-ech.com", host)
	assert.Equal(t, traffic.HostECH, source)

	// The app connector's domain beats both.
	r.SetAppConnector(map[string][]netip.Addr{"b.connector.example": {dst}, "a.connector.example": {dst}})
	host, source = r.Lookup(conn, now)
	assert.Equal(t, "a.connector.example", host)
	assert.Equal(t, traffic.HostAppConnector, source)

	// The node's own DNS answer beats the connector.
	r.PutDNS(laptop, "mine.example", []netip.Addr{dst}, time.Minute, now)
	host, source = r.Lookup(conn, now)
	assert.Equal(t, "mine.example", host)
	assert.Equal(t, traffic.HostDNS, source)

	// And the connection's own clear-text handshake beats everything.
	r.PutSNI(conn, "files.example", false, now)
	host, source = r.Lookup(conn, now)
	assert.Equal(t, "files.example", host)
	assert.Equal(t, traffic.HostSNI, source)
}

func TestLookupExpiry(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	laptop := netip.MustParseAddr("100.64.0.3")
	dst := netip.MustParseAddr("198.51.100.7")
	conn := Conn{Src: netip.AddrPortFrom(laptop, 40000), Dst: netip.AddrPortFrom(dst, 443), Proto: 6}

	r := NewResolver()

	// A 30 s TTL still attributes for the five-minute floor.
	r.PutDNS(laptop, "short.example", []netip.Addr{dst}, 30*time.Second, now)
	host, _ := r.Lookup(conn, now.Add(4*time.Minute))
	assert.Equal(t, "short.example", host)

	host, _ = r.Lookup(conn, now.Add(6*time.Minute))
	assert.Empty(t, host)

	// A day-long TTL is capped at an hour.
	r.PutDNS(laptop, "long.example", []netip.Addr{dst}, 24*time.Hour, now)
	host, _ = r.Lookup(conn, now.Add(59*time.Minute))
	assert.Equal(t, "long.example", host)

	host, _ = r.Lookup(conn, now.Add(61*time.Minute))
	assert.Empty(t, host)

	// Sweeps drop what expired.
	r.PutSNI(conn, "gone.example", false, now)
	r.PutDNS(laptop, "later.example", nil, time.Minute, now.Add(2*time.Hour))
	assert.Empty(t, r.sni)
	assert.Empty(t, r.bySrc)
}

func TestMappedAddressesMatch(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	r := NewResolver()

	r.PutDNS(netip.MustParseAddr("100.64.0.3"), "v4.example",
		[]netip.Addr{netip.MustParseAddr("::ffff:192.0.2.1")}, time.Minute, now)

	host, source := r.Lookup(Conn{
		Src: netip.MustParseAddrPort("100.64.0.3:1000"),
		Dst: netip.MustParseAddrPort("192.0.2.1:443"),
	}, now)
	assert.Equal(t, "v4.example", host)
	assert.Equal(t, traffic.HostDNS, source)
}
