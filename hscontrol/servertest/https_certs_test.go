package servertest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/servertest"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
	"tailscale.com/types/netmap"
)

// TestHTTPSCertificates proves that with certificate assistance on,
// every machine's MagicDNS name is announced as a cert domain, that a
// machine can publish the DNS-01 challenge record for its own name and
// nothing else, and that the record goes through the configured
// provider and into the audit log.
func TestHTTPSCertificates(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	records := filepath.Join(dir, "records")
	script := filepath.Join(dir, "set-txt.sh")
	require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\necho \"$1 $2\" >> \""+records+"\"\n"), 0o700))

	srv := servertest.NewServer(t,
		servertest.WithDNS(types.DNSConfig{MagicDNS: true, BaseDomain: "ts.example.com"}),
		servertest.WithHTTPSCerts(types.HTTPSCertsConfig{
			Enabled: true, Provider: types.DNSProviderCommand, Command: types.CommandDNSConfig{Path: script},
		}),
	)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "certs-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	laptop := servertest.NewClient(t, srv, "laptop", servertest.WithUser(owner))
	other := servertest.NewClient(t, srv, "other", servertest.WithUser(owner))

	laptop.WaitForCondition(t, "its cert domain", 10*time.Second, func(nm *netmap.NetworkMap) bool {
		return nm.DNS.CertDomains != nil
	})
	assert.Equal(t, []string{"laptop.ts.example.com"}, laptop.Netmap().DNS.CertDomains)

	other.WaitForCondition(t, "a netmap", 10*time.Second, func(nm *netmap.NetworkMap) bool {
		return nm != nil && nm.SelfNode.Valid()
	})

	post := func(node *servertest.TestClient, nodeKey key.NodePublic, name, typ string) int {
		return postSetDNS(t, srv.URL, node, tailcfg.SetDNSRequest{
			NodeKey: nodeKey, Name: name, Type: typ, Value: "challenge-token",
		})
	}

	self := laptop.Netmap().SelfNode.Key()

	assert.Equal(t, http.StatusOK, post(laptop, self, "_acme-challenge.laptop.ts.example.com", "TXT"))
	assert.Equal(t, http.StatusOK, post(laptop, self, "_acme-challenge.laptop.ts.example.com.", "txt"),
		"case and the trailing dot do not matter")

	assert.Equal(t, http.StatusForbidden, post(laptop, self, "_acme-challenge.other.ts.example.com", "TXT"),
		"another machine's name")
	assert.Equal(t, http.StatusForbidden, post(laptop, self, "laptop.ts.example.com", "TXT"),
		"not a challenge record")
	assert.Equal(t, http.StatusBadRequest, post(laptop, self, "_acme-challenge.laptop.ts.example.com", "A"))
	assert.Equal(t, http.StatusUnauthorized,
		post(laptop, other.Netmap().SelfNode.Key(), "_acme-challenge.other.ts.example.com", "TXT"),
		"another machine's key over this session")

	got, err := os.ReadFile(records)
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat("_acme-challenge.laptop.ts.example.com challenge-token\n", 2), string(got),
		"the name reaches the provider normalised")

	status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/audit?action=node.cert_challenge", nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.Len(t, field(t, body, "events"), 2)
}

// TestHTTPSCertificatesOff proves a server without certificate
// assistance announces no cert domains and turns set-dns away.
func TestHTTPSCertificatesOff(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t, servertest.WithDNS(types.DNSConfig{MagicDNS: true, BaseDomain: "ts.example.com"}))
	owner := srv.CreateUser(t, "certs-off-owner")
	laptop := servertest.NewClient(t, srv, "laptop", servertest.WithUser(owner))

	laptop.WaitForCondition(t, "a netmap", 10*time.Second, func(nm *netmap.NetworkMap) bool {
		return nm != nil && nm.SelfNode.Valid()
	})
	assert.Empty(t, laptop.Netmap().DNS.CertDomains)

	status := postSetDNS(t, srv.URL, laptop, tailcfg.SetDNSRequest{
		NodeKey: laptop.Netmap().SelfNode.Key(), Name: "_acme-challenge.laptop.ts.example.com", Type: "TXT",
		Value: "token",
	})
	assert.Equal(t, http.StatusNotImplemented, status)
}

// postSetDNS sends a set-dns request over the node's Noise connection,
// as tailscaled does during an ACME DNS-01 challenge.
func postSetDNS(t *testing.T, serverURL string, node *servertest.TestClient, req tailcfg.SetDNSRequest) int {
	t.Helper()

	body, err := json.Marshal(req)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	target := strings.Replace(serverURL+"/machine/set-dns", "http://", "https://", 1)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	require.NoError(t, err)

	resp, err := node.Direct().DoNoiseRequest(httpReq)
	require.NoError(t, err)

	defer resp.Body.Close()

	return resp.StatusCode
}
