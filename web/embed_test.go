package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func serve(t *testing.T, method, target string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, target, http.NoBody))

	return rec
}

func TestHandlerRedirectsBarePrefix(t *testing.T) {
	t.Parallel()

	rec := serve(t, http.MethodGet, "/admin")

	assert.Equal(t, http.StatusMovedPermanently, rec.Code)
	assert.Equal(t, Prefix, rec.Header().Get("Location"))
}

func TestHandlerRejectsWrites(t *testing.T) {
	t.Parallel()

	rec := serve(t, http.MethodPost, "/admin/")

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "GET, HEAD", rec.Header().Get("Allow"))
}

func TestHandlerSetsPolicy(t *testing.T) {
	t.Parallel()

	rec := serve(t, http.MethodGet, "/admin/")

	assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "script-src 'self'")
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
}

// TestHandlerServesConsole covers the two shapes a binary can have: with
// the console built into dist/, every unknown path falls back to the SPA
// entry and hashed assets are immutable; without it, the placeholder
// explains how to build it.
func TestHandlerServesConsole(t *testing.T) {
	t.Parallel()

	if !Built() {
		rec := serve(t, http.MethodGet, "/admin/machines/42")
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Contains(t, rec.Body.String(), "make web")
		assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))

		return
	}

	index := serve(t, http.MethodGet, "/admin/")
	require.Equal(t, http.StatusOK, index.Code)
	assert.Equal(t, "no-cache", index.Header().Get("Cache-Control"))
	assert.Contains(t, index.Body.String(), `id="root"`)

	deep := serve(t, http.MethodGet, "/admin/machines/42?x=1")
	assert.Equal(t, http.StatusOK, deep.Code)
	assert.Equal(t, index.Body.String(), deep.Body.String())

	// Find one hashed asset through the entry point and fetch it.
	body := index.Body.String()
	start := strings.Index(body, `src="/admin/assets/`)
	require.NotEqual(t, -1, start, "index.html references no script under assets/")
	rest := body[start+len(`src="`):]
	asset, _, found := strings.Cut(rest, `"`)
	require.True(t, found)

	script := serve(t, http.MethodGet, asset)
	assert.Equal(t, http.StatusOK, script.Code)
	assert.Equal(t, cacheForever, script.Header().Get("Cache-Control"))
	assert.Contains(t, script.Header().Get("Content-Type"), "javascript")

	missing := serve(t, http.MethodGet, "/admin/assets/nope.js")
	assert.Equal(t, http.StatusOK, missing.Code, "unknown asset paths fall back to the SPA entry")
	assert.NotEqual(t, cacheForever, missing.Header().Get("Cache-Control"))
}
