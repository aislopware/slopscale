package hscontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/state"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

// registerInteractive starts an interactive registration with a fresh
// machine key, as anyone who opens a Noise session can.
func registerInteractive(
	ctx context.Context,
	app *Slopscale,
	hostname string,
) (*tailcfg.RegisterResponse, error) {
	return app.handleRegister(ctx, tailcfg.RegisterRequest{
		NodeKey:  key.NewNode().Public(),
		Hostinfo: &tailcfg.Hostinfo{Hostname: hostname},
		Auth:     &tailcfg.RegisterResponseAuth{},
	}, key.NewMachine().Public())
}

func requireTooManyPending(t *testing.T, resp *tailcfg.RegisterResponse, err error) {
	t.Helper()

	require.Nil(t, resp)
	require.ErrorIs(t, err, state.ErrTooManyPendingRegistrations)

	var httpErr HTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, http.StatusTooManyRequests, httpErr.Code)
	assert.Contains(t, httpErr.Msg, "too many pending registrations from your network")
}

// TestInteractiveRegistrationLimitedPerSource drives the register handler the
// way the Noise router does: a flood of fresh machine keys from one network
// is refused with 429 once it holds its share of the auth cache, including
// on the followup path that mints a new entry, while another network's
// pending login survives and completes.
func TestInteractiveRegistrationLimitedPerSource(t *testing.T) {
	t.Parallel()

	app := createTestApp(t)

	// A bare IP, as the trusted_proxies middleware writes RemoteAddr.
	victimCtx := withRegistrationSource(t.Context(), "192.0.2.10")

	victimResp, err := registerInteractive(victimCtx, app, "victim")
	require.NoError(t, err)

	victimID, err := extractRegistrationIDFromAuthURL(victimResp.AuthURL)
	require.NoError(t, err)

	attackerCtx := withRegistrationSource(t.Context(), "198.51.100.7:41641")

	for i := range 16 {
		resp, floodErr := registerInteractive(attackerCtx, app, fmt.Sprintf("flood-%d", i))
		require.NoError(t, floodErr)
		require.NotEmpty(t, resp.AuthURL)
	}

	for i := range 100 {
		resp, refusedErr := registerInteractive(attackerCtx, app, fmt.Sprintf("refused-%d", i))
		requireTooManyPending(t, resp, refusedErr)
	}

	// A followup whose entry is gone gets a fresh one; from a full network
	// that is refused too.
	resp, err := app.handleRegister(attackerCtx, tailcfg.RegisterRequest{
		Followup: fmt.Sprintf("http://localhost:8080/register/%s", types.MustAuthID()),
		NodeKey:  key.NewNode().Public(),
	}, key.NewMachine().Public())
	requireTooManyPending(t, resp, err)

	_, ok := app.state.GetAuthCacheEntry(victimID)
	require.True(t, ok, "the victim's pending registration must survive the flood")

	user := app.state.CreateUserForTest("victim-user")
	node, _, err := app.state.HandleNodeFromAuthPath(victimID, types.UserID(user.ID), nil, util.RegisterMethodCLI)
	require.NoError(t, err)
	require.True(t, node.Valid())
	assert.Equal(t, "victim", node.Hostname())
}

// TestInteractiveRegistrationIPv6SourceIsSlash64: rotating addresses inside
// one /64 does not buy more registrations.
func TestInteractiveRegistrationIPv6SourceIsSlash64(t *testing.T) {
	t.Parallel()

	app := createTestApp(t)

	for i := range 16 {
		ctx := withRegistrationSource(t.Context(), fmt.Sprintf("[2001:db8:5:6:%x::1]:41641", i+1))

		_, err := registerInteractive(ctx, app, fmt.Sprintf("v6-%d", i))
		require.NoError(t, err)
	}

	resp, err := registerInteractive(
		withRegistrationSource(t.Context(), "[2001:db8:5:6:ffff:ffff:ffff:ffff]:41641"), app, "v6-refused")
	requireTooManyPending(t, resp, err)

	_, err = registerInteractive(withRegistrationSource(t.Context(), "[2001:db8:5:7::1]:41641"), app, "v6-next")
	require.NoError(t, err, "the next /64 has its own limit")
}

// serveRegisterFrom sends an interactive register request through the
// real Noise handler from remoteAddr, as the Noise router hands it over,
// and decodes the answer, which is always a 200.
func serveRegisterFrom(t *testing.T, app *Slopscale, remoteAddr, hostname string) tailcfg.RegisterResponse {
	t.Helper()

	payload, err := json.Marshal(tailcfg.RegisterRequest{
		Version:  tailcfg.CurrentCapabilityVersion,
		NodeKey:  key.NewNode().Public(),
		Hostinfo: &tailcfg.Hostinfo{Hostname: hostname},
		Auth:     &tailcfg.RegisterResponseAuth{},
	})
	require.NoError(t, err)

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/machine/register", bytes.NewReader(payload))
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()

	ns := &noiseServer{slopscale: app, machineKey: key.NewMachine().Public()}
	ns.RegistrationHandler(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body=%q", rec.Body.String())

	var resp tailcfg.RegisterResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	return resp
}

// TestRegistrationHandlerLimitsPerSource pins that the Noise handler hands
// the request's address to the per-source limit: without it the limit
// counts nothing.
func TestRegistrationHandlerLimitsPerSource(t *testing.T) {
	t.Parallel()

	app := createTestApp(t)

	for i := range 16 {
		resp := serveRegisterFrom(t, app, "198.51.100.7:41641", fmt.Sprintf("flood-%d", i))
		require.NotEmpty(t, resp.AuthURL, resp.Error)
	}

	refused := serveRegisterFrom(t, app, "198.51.100.7:41642", "refused")
	assert.Empty(t, refused.AuthURL)
	assert.Contains(t, refused.Error, "too many pending registrations from your network")

	other := serveRegisterFrom(t, app, "192.0.2.10:41641", "other")
	assert.NotEmpty(t, other.AuthURL, other.Error)
}

// TestRegistrationHandlerBodyLimit pins the register request's own body
// limit, tighter than other Noise requests because a pending registration
// keeps the request's Hostinfo in memory.
func TestRegistrationHandlerBodyLimit(t *testing.T) {
	t.Parallel()

	ns := &noiseServer{machineKey: key.NewMachine().Public()}

	oversized := bytes.Repeat([]byte("x"), int(registerBodyLimit)+1)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/machine/register", bytes.NewReader(oversized))
	rec := httptest.NewRecorder()

	ns.RegistrationHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}
