package hscontrol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/capver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

var errTestUnexpected = errors.New("unexpected failure")

// TestHandleVerifyRequest_OversizedBodyRejected verifies that the
// /verify handler refuses POST bodies larger than [verifyBodyLimit].
// The [http.MaxBytesReader] is applied in [Slopscale.VerifyHandler], so
// the test wraps the body the same way.
func TestHandleVerifyRequest_OversizedBodyRejected(t *testing.T) {
	t.Parallel()

	body := strings.Repeat("x", int(verifyBodyLimit)+128)
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		"/verify",
		strings.NewReader(body),
	)
	req.Body = http.MaxBytesReader(rec, req.Body, verifyBodyLimit)

	h := &Slopscale{}

	err := h.handleVerifyRequest(req, &bytes.Buffer{})
	require.Error(t, err, "oversized verify body must be rejected")

	httpErr, ok := errors.AsType[HTTPError](err)
	require.True(t, ok, "error must be an HTTPError, got: %T (%v)", err, err)

	assert.Equal(t, http.StatusRequestEntityTooLarge, httpErr.Code,
		"oversized body must surface 413")
}

// TestVerifyHandler_SuccessSetsJSONContentType verifies that a successful
// POST to /verify advertises Content-Type: application/json. The header
// must be set before the JSON body is written, otherwise the implicit
// WriteHeader on first Write locks in a sniffed content type and the
// later Header().Set becomes a no-op.
func TestVerifyHandler_SuccessSetsJSONContentType(t *testing.T) {
	t.Parallel()

	h := createTestApp(t)

	reqBody, err := json.Marshal(tailcfg.DERPAdmitClientRequest{
		NodePublic: key.NewNode().Public(),
	})
	require.NoError(t, err)

	// A real HTTP server is required to observe the bug: the first body
	// Write triggers an implicit WriteHeader that snapshots the header
	// map, so a Content-Type set afterwards never reaches the wire.
	// An httptest.ResponseRecorder does not snapshot, so it would hide
	// the defect.
	srv := httptest.NewServer(http.HandlerFunc(h.VerifyHandler))
	defer srv.Close()

	httpReq, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		srv.URL+"/verify",
		bytes.NewReader(reqBody),
	)
	require.NoError(t, err)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(httpReq)
	require.NoError(t, err)

	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"),
		"successful /verify response must advertise application/json")
}

// TestKeyHandler_UnsupportedCapVerDoesNotLeakKey reproduces
// https://github.com/juanfont/headscale/issues/3380. The /key handler
// must gate key disclosure on the same floor the Noise handshake
// enforces (capver.MinSupportedCapabilityVersion). A capability version
// below that floor can never complete a handshake, so it must be
// rejected rather than handed the server's Noise public key, which would
// otherwise serve only as a fingerprint / version-boundary oracle.
func TestKeyHandler_UnsupportedCapVerDoesNotLeakKey(t *testing.T) {
	t.Parallel()

	noise := key.NewMachine()
	h := &Slopscale{noisePrivateKey: &noise}

	unsupported := capver.MinSupportedCapabilityVersion - 1

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		fmt.Sprintf("/key?v=%d", unsupported),
		http.NoBody,
	)

	h.KeyHandler(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code,
		"a client below the supported floor must be rejected")
	assert.NotContains(t, rec.Body.String(), noise.Public().String(),
		"must not disclose Noise public key to a client below the supported floor")

	// A supported client still receives the key.
	recOK := httptest.NewRecorder()
	reqOK := httptest.NewRequestWithContext(
		t.Context(),
		http.MethodGet,
		fmt.Sprintf("/key?v=%d", capver.MinSupportedCapabilityVersion),
		http.NoBody,
	)

	h.KeyHandler(recOK, reqOK)

	assert.Equal(t, http.StatusOK, recOK.Code)
	assert.Contains(t, recOK.Body.String(), noise.Public().String(),
		"a supported client must receive the Noise public key")
}

func TestHttpUserError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		err            error
		wantCode       int
		wantContains   string
		wantNotContain string
	}{
		{
			name:           "forbidden_renders_authorization_message",
			err:            NewHTTPError(http.StatusForbidden, "csrf token mismatch", nil),
			wantCode:       http.StatusForbidden,
			wantContains:   "You are not authorized. Please contact your administrator.",
			wantNotContain: "csrf token mismatch",
		},
		{
			name:           "unauthorized_renders_authorization_message",
			err:            NewHTTPError(http.StatusUnauthorized, "unauthorised domain", nil),
			wantCode:       http.StatusUnauthorized,
			wantContains:   "You are not authorized. Please contact your administrator.",
			wantNotContain: "unauthorised domain",
		},
		{
			name:           "gone_renders_session_expired",
			err:            NewHTTPError(http.StatusGone, "login session expired, try again", nil),
			wantCode:       http.StatusGone,
			wantContains:   "Your session has expired. Please try again.",
			wantNotContain: "login session expired",
		},
		{
			name: "gone_with_user_message_renders_specific_guidance",
			err: newHTTPUserError(
				http.StatusGone,
				"registration link already used or expired",
				"This link has already been used or has expired.",
				nil,
			),
			wantCode:       http.StatusGone,
			wantContains:   "This link has already been used or has expired.",
			wantNotContain: "registration link already used or expired",
		},
		{
			name:           "bad_request_renders_generic_retry",
			err:            NewHTTPError(http.StatusBadRequest, "state not found", nil),
			wantCode:       http.StatusBadRequest,
			wantContains:   "The request could not be processed. Please try again.",
			wantNotContain: "state not found",
		},
		{
			name:         "plain_error_renders_500",
			err:          errTestUnexpected,
			wantCode:     http.StatusInternalServerError,
			wantContains: "Something went wrong. Please try again later.",
		},
		{
			name:         "html_structure_present",
			err:          NewHTTPError(http.StatusGone, "session expired", nil),
			wantCode:     http.StatusGone,
			wantContains: "<!DOCTYPE html>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			httpUserError(rec, tt.err)

			assert.Equal(t, tt.wantCode, rec.Code)
			assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
			assert.Contains(t, rec.Body.String(), tt.wantContains)

			if tt.wantNotContain != "" {
				assert.NotContains(t, rec.Body.String(), tt.wantNotContain)
			}
		})
	}
}

// TestRouterMethodNotAllowedIncludesAllow pins that the router answers a
// wrong method with 405 and names the methods it does accept, which chi
// only does for routes registered per method.
func TestRouterMethodNotAllowedIncludesAllow(t *testing.T) {
	t.Parallel()

	h := createTestApp(t)
	router := h.createRouter(http.NotFoundHandler(), http.NotFoundHandler())

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/health", http.NoBody)

	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, []string{http.MethodGet}, rec.Header().Values("Allow"))
}
