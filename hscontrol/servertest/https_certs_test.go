package servertest_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/dnsprovider/dnstest"
	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
	"tailscale.com/types/netmap"
)

// TestHTTPSCertificates proves that with certificate assistance on,
// every machine's MagicDNS name is announced as a cert domain, that a
// machine can publish the DNS-01 challenge record for its own name and
// nothing else, that a resend reaches the provider once, and that the
// record goes into the audit log.
func TestHTTPSCertificates(t *testing.T) {
	t.Parallel()

	provider := &dnstest.Provider{}
	srv := servertest.NewServer(t,
		servertest.WithDNS(types.DNSConfig{MagicDNS: true, BaseDomain: "ts.example.com"}),
		servertest.WithHTTPSCerts(types.HTTPSCertsConfig{
			Enabled: true, Provider: provider, Zone: "ts.example.com",
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

	value := challengeValue("laptop")
	post := func(node *servertest.TestClient, nodeKey key.NodePublic, name, typ, value string) int {
		return postSetDNS(t, srv.URL, node, tailcfg.SetDNSRequest{
			NodeKey: nodeKey, Name: name, Type: typ, Value: value,
		})
	}

	self := laptop.Netmap().SelfNode.Key()
	name := "_acme-challenge.laptop.ts.example.com"

	assert.Equal(t, http.StatusOK, post(laptop, self, name, "TXT", value))
	assert.Equal(t, http.StatusOK, post(laptop, self, name+".", "txt", value),
		"the trailing dot and the type's case do not matter")

	assert.Equal(t, http.StatusForbidden, post(laptop, self, "_acme-challenge.LAPTOP.ts.example.com", "TXT", value),
		"the name matches exactly")
	assert.Equal(t, http.StatusForbidden, post(laptop, self, "_acme-challenge.other.ts.example.com", "TXT", value),
		"another machine's name")
	assert.Equal(t, http.StatusForbidden, post(laptop, self, "laptop.ts.example.com", "TXT", value),
		"not a challenge record")
	assert.Equal(t, http.StatusBadRequest, post(laptop, self, name, "A", value))
	assert.Equal(t, http.StatusBadRequest, post(laptop, self, name, "TXT", "v=spf1 include:example.net"),
		"not a DNS-01 value")
	assert.Equal(t, http.StatusUnauthorized,
		post(laptop, other.Netmap().SelfNode.Key(), "_acme-challenge.other.ts.example.com", "TXT", value),
		"another machine's key over this session")

	assert.Equal(t, []string{value}, provider.Values(name+"."))
	assert.Len(t, provider.Calls(), 1, "a resend and rejected requests do not reach the provider")

	status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/audit?action=node.cert_challenge", nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.Len(t, field(t, body, "events"), 2)

	// The certificate name follows a rename, lowercased; the client only
	// runs ACME for a name it finds in CertDomains.
	_, renameChange, err := srv.State().RenameNode(findNodeID(t, srv, "laptop"), "NoteBook")
	require.NoError(t, err)
	srv.App.Change(renameChange)

	laptop.WaitForCondition(t, "cert domain follows rename", 10*time.Second, func(nm *netmap.NetworkMap) bool {
		return nm != nil && slices.Equal(nm.DNS.CertDomains, []string{"notebook.ts.example.com"})
	})
	assert.Equal(t, http.StatusForbidden, post(laptop, self, name, "TXT", challengeValue("again")),
		"the old name is no longer the machine's")
}

// challengeValue is a well-formed DNS-01 value: the unpadded base64url
// SHA-256 the client sends.
func challengeValue(seed string) string {
	sum := sha256.Sum256([]byte(seed))

	return base64.RawURLEncoding.EncodeToString(sum[:])
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
		Value: challengeValue("laptop"),
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
