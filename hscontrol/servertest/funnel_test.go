package servertest_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg/nodecap"
	"tailscale.com/tsnet"
	"tailscale.com/types/logger"
	"tailscale.com/types/netmap"
)

// funnelWait is how long the two tailnet nodes get to find each other.
const funnelWait = 90 * time.Second

// freePort picks a loopback port nothing listens on.
func freePort(t *testing.T) uint16 {
	t.Helper()

	var lc net.ListenConfig

	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	port := ln.Addr().(*net.TCPAddr).Port //nolint:forcetypeassert // a TCP listener has a TCP address

	require.NoError(t, ln.Close())

	return uint16(port)
}

func selfSignedCert(t *testing.T, name string) tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// TestFunnelEndToEnd proves a public TLS connection to the embedded
// ingress reaches a machine that turned Funnel on: the policy grants the
// machine the funnel attribute and the ingress the ingress capability,
// the ingress joins as a tagged node, the machine (a real client, over
// tsnet) opens a Funnel listener, and an HTTP request from outside the
// tailnet is answered through it. The machine's Hostinfo then marks it
// as a Funnel host on the API.
func TestFunnelEndToEnd(t *testing.T) {
	t.Parallel()

	port := freePort(t)
	dir := t.TempDir()

	srv := servertest.NewServer(t,
		servertest.WithRealListener(),
		servertest.WithEmbeddedDERP(),
		servertest.WithDNS(types.DNSConfig{MagicDNS: true, BaseDomain: "funnel.test"}),
		servertest.WithHTTPSCerts(types.HTTPSCertsConfig{
			Enabled:  true,
			Provider: types.DNSProviderCommand,
			Command:  types.CommandDNSConfig{Path: "/usr/bin/true"},
		}),
		servertest.WithFunnel(types.FunnelConfig{
			Enabled:     true,
			ListenAddrs: []string{"127.0.0.1:" + strconv.Itoa(int(port))},
			StateDir:    dir + "/ingress",
			Ports:       []uint16{port},
		}),
	)
	owner := srv.CreateUser(t, "owner")

	reloadPolicy(t, srv, `{
		"acls": [{"action": "accept", "src": ["*"], "dst": ["*:*"]}],
		"nodeAttrs": [{"target": ["*"], "attr": ["funnel"]}]
	}`)

	// A plain client watches the tailnet: the ingress must join as a
	// tagged node and the funnel node must carry the caps.
	watcher := servertest.NewClient(t, srv, "watcher", servertest.WithUser(owner))
	portsCap := types.FunnelConfig{Ports: []uint16{port}}.FunnelPortsCap()

	watcher.WaitForCondition(t, "funnel caps on the self node", 10*time.Second, func(nm *netmap.NetworkMap) bool {
		return hasCap(nm, nodecap.Funnel) && hasCap(nm, nodecap.HTTPS) && hasCap(nm, portsCap)
	})
	assert.False(t, hasCap(watcher.Netmap(), nodecap.WarnFunnelNoHTTPS), "HTTPS is on")

	srv.App.StartFunnelIngressForTest(t)

	watcher.WaitForCondition(t, "the ingress as a peer", funnelWait, func(_ *netmap.NetworkMap) bool {
		_, ok := watcher.PeerByName(types.FunnelIngressHostname)

		return ok
	})

	peer, _ := watcher.PeerByName(types.FunnelIngressHostname)
	assert.Equal(t, []string{types.FunnelIngressTag}, peer.Tags().AsSlice())

	// The funnel host is a real client.
	web := &tsnet.Server{
		Dir:        dir + "/web",
		Hostname:   "web",
		ControlURL: srv.URL,
		AuthKey:    srv.CreatePreAuthKey(t, types.UserID(owner.ID)),
		Ephemeral:  true,
		Logf:       logger.Discard,
	}
	t.Cleanup(func() { _ = web.Close() })

	ctx, cancel := context.WithTimeout(t.Context(), funnelWait)
	defer cancel()

	status, err := web.Up(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"web.funnel.test"}, status.CertDomains)

	ln, err := web.ListenFunnel("tcp", ":"+strconv.Itoa(int(port)),
		tsnet.FunnelTLSConfig(&tls.Config{
			Certificates: []tls.Certificate{selfSignedCert(t, "web.funnel.test")},
		}))
	require.NoError(t, err)

	httpSrv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.WriteString(w, "hello from "+r.Host)
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	t.Cleanup(func() { _ = httpSrv.Close() })

	go func() { _ = httpSrv.Serve(ln) }()

	// From outside the tailnet, the public address answers for the
	// machine's name.
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				ServerName:         "web.funnel.test",
				InsecureSkipVerify: true,
			},
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer

				return d.DialContext(ctx, "tcp", "127.0.0.1:"+strconv.Itoa(int(port)))
			},
		},
		Timeout: 10 * time.Second,
	}

	funnelURL := "https://web.funnel.test:" + strconv.Itoa(int(port)) + "/"

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, funnelURL, http.NoBody)
		if !assert.NoError(c, err) {
			return
		}

		resp, err := client.Do(req)
		if !assert.NoError(c, err) {
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		assert.Equal(c, http.StatusOK, resp.StatusCode)
		assert.Contains(c, string(body), "hello from web.funnel.test")
	}, funnelWait, time.Second, "the request reaches the machine through the ingress")

	// The client tells the server it has Funnel on, and the API says so.
	api := srv.HTTPClient(t)
	ownerKey := srv.CreateAPIKey(t, owner)

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		statusCode, body := apiCall(t, api, ownerKey, http.MethodGet, srv.URL+"/api/v1/node", nil)
		assert.Equal(c, http.StatusOK, statusCode)

		var found bool

		nodes, _ := body["nodes"].([]any)
		for _, n := range nodes {
			node, _ := n.(map[string]any)
			if node["givenName"] == "web" {
				found, _ = node["funnelEnabled"].(bool)
			}
		}

		assert.True(c, found, "web is marked as a Funnel host")
	}, 30*time.Second, time.Second)

	statusCode, body := apiCall(t, api, ownerKey, http.MethodGet, srv.URL+"/api/v1/server", nil)
	require.Equal(t, http.StatusOK, statusCode, body)
	assert.Equal(t, true, field(t, body, "funnelIngress"))
	assert.InDelta(t, float64(1), field(t, body, "funnelIngressNodes"), 0)
}
