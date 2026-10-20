package hscontrol

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"log" //nolint:depguard // Pebble logs through a *log.Logger
	"math/big"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/dnsprovider/dnstest"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/caddyserver/certmagic"
	"github.com/letsencrypt/pebble/v2/ca"
	"github.com/letsencrypt/pebble/v2/db"
	"github.com/letsencrypt/pebble/v2/va"
	"github.com/letsencrypt/pebble/v2/wfe"
	"github.com/mholt/acmez/v3/acme"
	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	acmeTestZone = "example.com."
	acmeTestHost = "slopscale.example.com"
)

// testACME is an in-process ACME CA (Pebble) with an authoritative DNS
// server for acmeTestZone. The DNS server answers TXT from provider, so a
// DNS-01 record published through it is what the CA validates.
type testACME struct {
	directory string
	resolver  string
	roots     *x509.CertPool
	provider  *dnstest.Provider
}

// startTestACME validates HTTP-01 on httpPort and TLS-ALPN-01 on tlsPort
// of 127.0.0.1, where acmeTestHost resolves to.
func startTestACME(t *testing.T, httpPort, tlsPort int) *testACME {
	t.Helper()

	// Validate at once and deterministically.
	t.Setenv("PEBBLE_VA_NOSLEEP", "1")
	t.Setenv("PEBBLE_WFE_NONCEREJECT", "0")
	t.Setenv("PEBBLE_AUTHZREUSE", "0")

	provider := &dnstest.Provider{}
	resolver := startZoneServer(t, provider)

	logger := log.New(os.Stderr, "pebble ", log.LstdFlags)
	store := db.NewMemoryStore()
	authority := ca.New(logger, store, "", "ecdsa", 0, 1, map[string]ca.Profile{
		"default": {Description: "default"},
	})
	validator := va.New(logger, httpPort, tlsPort, false, resolver, store)
	frontend := wfe.New(logger, store, validator, authority, []string{"pebble.letsencrypt.org"}, false, false, 0, 0)

	srv := httptest.NewTLSServer(frontend.Handler())
	t.Cleanup(srv.Close)

	directoryRoots := x509.NewCertPool()
	directoryRoots.AddCert(srv.Certificate())

	prev := acmeTrustedRoots
	acmeTrustedRoots = directoryRoots

	t.Cleanup(func() { acmeTrustedRoots = prev })

	roots := x509.NewCertPool()
	roots.AddCert(authority.GetRootCert(0).Cert)

	return &testACME{
		directory: srv.URL + wfe.DirectoryPath,
		resolver:  resolver,
		roots:     roots,
		provider:  provider,
	}
}

// startZoneServer serves acmeTestZone over UDP and TCP on one port:
// certmagic finds the zone over UDP, Pebble validates over TCP.
func startZoneServer(t *testing.T, provider *dnstest.Provider) string {
	t.Helper()

	handler := dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		m.Authoritative = true

		q := r.Question[0]
		hdr := dns.RR_Header{Name: q.Name, Class: dns.ClassINET, Rrtype: q.Qtype, Ttl: 60}

		switch {
		case !strings.HasSuffix(strings.ToLower(q.Name), acmeTestZone):
			m.Rcode = dns.RcodeRefused
		case q.Qtype == dns.TypeSOA && strings.EqualFold(q.Name, acmeTestZone):
			m.Answer = []dns.RR{&dns.SOA{
				Hdr: hdr, Ns: "ns." + acmeTestZone, Mbox: "hostmaster." + acmeTestZone,
				Serial: 1, Refresh: 60, Retry: 60, Expire: 60, Minttl: 60,
			}}
		case q.Qtype == dns.TypeA:
			m.Answer = []dns.RR{&dns.A{Hdr: hdr, A: net.IPv4(127, 0, 0, 1)}}
		case q.Qtype == dns.TypeTXT:
			for _, v := range provider.Values(strings.ToLower(q.Name)) {
				m.Answer = append(m.Answer, &dns.TXT{Hdr: hdr, Txt: []string{v}})
			}
		}

		_ = w.WriteMsg(m)
	})

	var lc net.ListenConfig

	pc, err := lc.ListenPacket(t.Context(), "udp", "127.0.0.1:0")
	require.NoError(t, err)

	ln, err := lc.Listen(t.Context(), "tcp", pc.LocalAddr().String())
	require.NoError(t, err)

	for _, srv := range []*dns.Server{
		{PacketConn: pc, Handler: handler},
		{Listener: ln, Handler: handler},
	} {
		go func() { _ = srv.ActivateAndServe() }()

		t.Cleanup(func() { _ = srv.Shutdown() })
	}

	return pc.LocalAddr().String()
}

func freePort(t *testing.T) int {
	t.Helper()

	var lc net.ListenConfig

	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	_, port, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	require.NoError(t, ln.Close())

	p, err := strconv.Atoi(port)
	require.NoError(t, err)

	return p
}

// TestDNS01SolverWait pins that the server's own certificate waits for
// the value certmagic published, at the name it published it.
func TestDNS01SolverWait(t *testing.T) {
	provider := &dnstest.Provider{}

	var waited []string

	s := dns01Solver{
		DNS01Solver: &certmagic.DNS01Solver{
			DNSProvider: provider,
			Resolvers:   []string{startZoneServer(t, provider)},
		},
		wait: func(_ context.Context, fqdn, value string) error {
			waited = []string{fqdn, value}

			return nil
		},
	}
	ch := acme.Challenge{
		Type:             acme.ChallengeTypeDNS01,
		KeyAuthorization: "token.thumbprint",
		Identifier:       acme.Identifier{Type: "dns", Value: acmeTestHost},
	}

	require.NoError(t, s.Present(t.Context(), ch))
	require.NoError(t, s.Wait(t.Context(), ch))

	fqdn := "_acme-challenge." + acmeTestHost + "."

	require.Len(t, waited, 2)
	assert.Equal(t, fqdn, waited[0])
	assert.Equal(t, provider.Values(fqdn), waited[1:])
}

// acmeServer builds a server getting its certificate from the test CA
// with challenge, listening on tlsPort.
func acmeServer(t *testing.T, acmeCA *testACME, challenge string, httpPort, tlsPort int) *Slopscale {
	t.Helper()

	return &Slopscale{cfg: &types.Config{
		ServerURL: "https://" + acmeTestHost,
		Addr:      net.JoinHostPort("127.0.0.1", strconv.Itoa(tlsPort)),
		TLS: types.TLSConfig{ACME: types.ACMEConfig{
			Hostname:      acmeTestHost,
			ChallengeType: challenge,
			HTTPListen:    net.JoinHostPort("127.0.0.1", strconv.Itoa(httpPort)),
			URL:           acmeCA.directory,
			CacheDir:      t.TempDir(),
		}},
		HTTPSCerts: types.HTTPSCertsConfig{
			Provider:  acmeCA.provider,
			Resolvers: []string{acmeCA.resolver},
		},
	}}
}

// serveTLS starts the server's TLS the way [Slopscale.Serve] does: the
// main listener and the HTTP-01 server bound, then certificate
// management started.
func serveTLS(t *testing.T, h *Slopscale) {
	t.Helper()

	bundle, err := h.getTLSSettings(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { bundle.shutdownACME(t.Context()) })

	ln, err := tls.Listen("tcp", h.cfg.Addr, bundle.config)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			go func() {
				defer conn.Close()

				_ = conn.(*tls.Conn).HandshakeContext(t.Context()) //nolint:forcetypeassert // tls listener
			}()
		}
	}()

	go func() { _ = bundle.serveACME() }()

	require.NoError(t, bundle.manage(t.Context()))
}

// servedCert dials the server until a handshake verified against roots
// succeeds and returns the leaf it served.
func servedCert(t *testing.T, addr string, roots *x509.CertPool) *x509.Certificate {
	t.Helper()

	var leaf *x509.Certificate

	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		d := tls.Dialer{Config: &tls.Config{ServerName: acmeTestHost, RootCAs: roots, MinVersion: tls.VersionTLS12}}

		conn, err := d.DialContext(t.Context(), "tcp", addr)
		if !assert.NoError(c, err) {
			return
		}

		leaf = conn.(*tls.Conn).ConnectionState().PeerCertificates[0] //nolint:forcetypeassert // tls dialer
		_ = conn.Close()
	}, 30*time.Second, 200*time.Millisecond, "the server should serve a certificate the roots trust")

	return leaf
}

// TestACMETLSSettings obtains the server's own certificate from Pebble
// with each challenge type, serving it the way [Slopscale.Serve] does.
func TestACMETLSSettings(t *testing.T) {
	for _, challenge := range []string{
		types.DNS01ChallengeType,
		types.HTTP01ChallengeType,
		types.TLSALPN01ChallengeType,
	} {
		t.Run(challenge, func(t *testing.T) {
			httpPort, tlsPort := freePort(t), freePort(t)
			acmeCA := startTestACME(t, httpPort, tlsPort)
			h := acmeServer(t, acmeCA, challenge, httpPort, tlsPort)

			serveTLS(t, h)
			servedCert(t, h.cfg.Addr, acmeCA.roots)

			if challenge == types.DNS01ChallengeType {
				assert.NotEmpty(t, acmeCA.provider.Calls(), "DNS-01 must publish through the DNS provider")
				assert.EventuallyWithT(t, func(c *assert.CollectT) {
					assert.Empty(c, acmeCA.provider.Values("_acme-challenge."+acmeTestHost+"."))
				}, 10*time.Second, 100*time.Millisecond, "challenge record should be removed")
			}
		})
	}
}

// writeAutocertCert writes a certificate for host, valid until notAfter,
// where autocert's DirCache kept it: key then chain in one PEM file named
// after the host. It returns the certificate.
func writeAutocertCert(t *testing.T, dir, host string, notAfter time.Time) *x509.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)

	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)

	data := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	data = append(data, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	require.NoError(t, os.WriteFile(filepath.Join(dir, host), data, 0o600))

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	return cert
}

// TestACMEKeepsAutocertCertificate proves a server upgraded from
// autocert serves the certificate autocert left in the cache directory
// instead of ordering a new one.
func TestACMEKeepsAutocertCertificate(t *testing.T) {
	httpPort, tlsPort := freePort(t), freePort(t)
	acmeCA := startTestACME(t, httpPort, tlsPort)
	h := acmeServer(t, acmeCA, types.DNS01ChallengeType, httpPort, tlsPort)

	old := writeAutocertCert(t, h.cfg.TLS.ACME.CacheDir, acmeTestHost, time.Now().Add(80*24*time.Hour))

	roots := x509.NewCertPool()
	roots.AddCert(old)

	serveTLS(t, h)

	got := servedCert(t, h.cfg.Addr, roots)
	assert.Equal(t, old.SerialNumber, got.SerialNumber, "the autocert certificate is served")
	assert.Empty(t, acmeCA.provider.Calls(), "no new certificate is ordered")
}

// TestImportAutocertCertSkips proves a cached certificate that would not
// serve the host is left alone, so certmagic orders a fresh one, and an
// existing certmagic certificate is never overwritten.
func TestImportAutocertCertSkips(t *testing.T) {
	const issuerKey = "acme.example.net-directory"

	imported := func(t *testing.T, write func(dir string)) bool {
		t.Helper()

		dir := t.TempDir()
		write(dir)

		storage := &certmagic.FileStorage{Path: dir}
		importAutocertCert(t.Context(), storage, issuerKey, dir, acmeTestHost)

		return storage.Exists(t.Context(), certmagic.StorageKeys.SiteCert(issuerKey, acmeTestHost))
	}

	assert.True(t, imported(t, func(dir string) {
		writeAutocertCert(t, dir, acmeTestHost, time.Now().Add(time.Hour))
	}), "a valid certificate is imported")

	assert.False(t, imported(t, func(string) {}), "no cache file")

	assert.False(t, imported(t, func(dir string) {
		writeAutocertCert(t, dir, acmeTestHost, time.Now().Add(-time.Minute))
	}), "expired")

	assert.False(t, imported(t, func(dir string) {
		writeAutocertCert(t, dir, "other.example.com", time.Now().Add(time.Hour))
		require.NoError(t, os.Rename(filepath.Join(dir, "other.example.com"), filepath.Join(dir, acmeTestHost)))
	}), "for another name")

	assert.False(t, imported(t, func(dir string) {
		require.NoError(t, os.WriteFile(filepath.Join(dir, acmeTestHost), []byte("not pem"), 0o600))
	}), "garbage")

	// A certificate certmagic already has wins over the autocert one.
	dir := t.TempDir()
	storage := &certmagic.FileStorage{Path: dir}
	key := certmagic.StorageKeys.SiteCert(issuerKey, acmeTestHost)
	require.NoError(t, storage.Store(t.Context(), key, []byte("certmagic's")))
	writeAutocertCert(t, dir, acmeTestHost, time.Now().Add(time.Hour))

	importAutocertCert(t.Context(), storage, issuerKey, dir, acmeTestHost)

	got, err := storage.Load(t.Context(), key)
	require.NoError(t, err)
	assert.Equal(t, "certmagic's", string(got))
}
