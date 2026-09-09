package egress_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/juanfont/headscale/hscontrol/egress"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGuardCheckAddr(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		addr    string
		policy  egress.Policy
		blocked bool
	}{
		{name: "public", addr: "93.184.216.34"},
		{name: "public v6", addr: "2606:2800:220:1:248:1893:25c8:1946"},
		{name: "loopback v4", addr: "127.0.0.1", blocked: true},
		{name: "loopback v6", addr: "::1", blocked: true},
		{name: "loopback mapped", addr: "::ffff:127.0.0.1", blocked: true},
		{name: "loopback allowed", addr: "127.0.0.1", policy: egress.Policy{AllowLoopback: true}},
		{name: "metadata", addr: "169.254.169.254", blocked: true},
		{name: "metadata mapped", addr: "::ffff:169.254.169.254", blocked: true},
		{name: "link-local v6", addr: "fe80::1", blocked: true},
		{name: "unspecified v4", addr: "0.0.0.0", blocked: true},
		{name: "unspecified v6", addr: "::", blocked: true},
		{
			// Loopback stays allowed while the private ranges are refused.
			name:   "unspecified allowed loopback",
			addr:   "0.0.0.0",
			policy: egress.Policy{AllowLoopback: true}, blocked: true,
		},
		{name: "private", addr: "10.0.0.1"},
		{name: "private denied", addr: "10.0.0.1", policy: egress.Policy{DenyPrivate: true}, blocked: true},
		{name: "tailnet denied", addr: "100.64.0.1", policy: egress.Policy{DenyPrivate: true}, blocked: true},
		{name: "unique local denied", addr: "fc00::1", policy: egress.Policy{DenyPrivate: true}, blocked: true},
		{name: "public with deny private", addr: "93.184.216.34", policy: egress.Policy{DenyPrivate: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := egress.New(tt.policy).CheckAddr(netip.MustParseAddr(tt.addr))
			if tt.blocked {
				require.ErrorIs(t, err, egress.ErrBlocked)

				return
			}

			require.NoError(t, err)
		})
	}
}

func TestGuardCheckHostLeavesNamesToTheDial(t *testing.T) {
	t.Parallel()

	guard := egress.New(egress.Policy{})

	require.NoError(t, guard.CheckHost("example.com"))
	require.NoError(t, guard.CheckHost("example.com:8443"))
	require.ErrorIs(t, guard.CheckHost("127.0.0.1:9"), egress.ErrBlocked)
	require.ErrorIs(t, guard.CheckHost("[::1]:9"), egress.ErrBlocked)
	require.ErrorIs(t, guard.CheckHost("[::1]"), egress.ErrBlocked)
	require.ErrorIs(t, guard.CheckHost("[::ffff:127.0.0.1]"), egress.ErrBlocked)
	require.ErrorIs(t, guard.CheckHost("169.254.169.254"), egress.ErrBlocked)
}

func TestDialContextRefusesBlockedAddress(t *testing.T) {
	// Not parallel: the default policy is process-wide.
	egress.SetDefault(egress.Policy{})
	t.Cleanup(func() { egress.SetDefault(egress.Policy{AllowLoopback: true}) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	client := &http.Client{Transport: egress.Transport()}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, http.NoBody)
	require.NoError(t, err)

	resp, err := client.Do(req) //nolint:bodyclose // the dial fails, so there is no body
	require.Error(t, err)
	assert.Nil(t, resp)
	require.ErrorIs(t, err, egress.ErrBlocked)

	egress.SetDefault(egress.Policy{AllowLoopback: true})

	req, err = http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL, http.NoBody)
	require.NoError(t, err)

	resp, err = client.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
