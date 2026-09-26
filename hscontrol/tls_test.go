package hscontrol

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"syscall"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func acmeTestServer(t *testing.T, listen string) *Slopscale {
	t.Helper()

	return &Slopscale{cfg: &types.Config{
		ServerURL: "https://slopscale.example.com",
		TLS: types.TLSConfig{LetsEncrypt: types.LetsEncryptConfig{
			Hostname:      "slopscale.example.com",
			Listen:        listen,
			CacheDir:      t.TempDir(),
			ChallengeType: types.HTTP01ChallengeType,
		}},
	}}
}

// TestACMEChallengeListenerBindFailure proves a taken HTTP-01 address
// fails the start with the listener named, instead of exiting the process
// from a goroutine as it used to.
func TestACMEChallengeListenerBindFailure(t *testing.T) {
	taken, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	t.Cleanup(func() { _ = taken.Close() })

	_, err = acmeTestServer(t, taken.Addr().String()).getTLSSettings(t.Context())
	require.Error(t, err)
	require.ErrorIs(t, err, syscall.EADDRINUSE)

	bindErr, ok := errors.AsType[*types.ListenerBindError](err)
	require.True(t, ok, "want a ListenerBindError, got %T", err)
	assert.Equal(t, "ACME HTTP-01 challenge", bindErr.Listener)
	assert.Equal(t, "tls_letsencrypt_listen", bindErr.ConfigKey)
	assert.Equal(t, taken.Addr().String(), bindErr.Addr)
}

// TestACMEChallengeListenerServesAndShutsDown proves the challenge listener
// is bound before serving, answers, and stops cleanly on shutdown.
func TestACMEChallengeListenerServesAndShutsDown(t *testing.T) {
	bundle, err := acmeTestServer(t, "127.0.0.1:0").getTLSSettings(t.Context())
	require.NoError(t, err)
	require.NotNil(t, bundle.config)
	require.NotNil(t, bundle.acmeListener)

	served := make(chan error, 1)

	go func() { served <- bundle.serveACME() }()

	url := "http://" + bundle.acmeListener.Addr().String() + "/.well-known/acme-challenge/unknown"
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	require.NoError(t, err)

	req.Host = "slopscale.example.com"

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)

	_, _ = io.Copy(io.Discard, resp.Body)
	require.NoError(t, resp.Body.Close())
	assert.Equal(t, http.StatusNotFound, resp.StatusCode, "an unknown token is not found")

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	bundle.shutdownACME(ctx)
	require.NoError(t, <-served, "a shutdown is not a serve error")
}

func TestTLSSettingsWithoutTLS(t *testing.T) {
	h := &Slopscale{cfg: &types.Config{ServerURL: "http://slopscale.example.com"}}

	bundle, err := h.getTLSSettings(t.Context())
	require.NoError(t, err)
	assert.Nil(t, bundle.config)
	require.NoError(t, bundle.serveACME(), "no challenge listener serves nothing")
	bundle.shutdownACME(t.Context())
}
