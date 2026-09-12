package web

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testServerURL carries a trailing slash to check the handler drops it
// before it joins a path to it.
const testServerURL = "https://hs.example.test/"

func serve(t *testing.T, method, target string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	Handler(testServerURL).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, target, http.NoBody))

	return rec
}

func TestHandlerRedirectsBarePrefix(t *testing.T) {
	t.Parallel()

	rec := serve(t, http.MethodGet, "/console")

	assert.Equal(t, http.StatusMovedPermanently, rec.Code)
	assert.Equal(t, Prefix, rec.Header().Get("Location"))
}

func TestHandlerRejectsWrites(t *testing.T) {
	t.Parallel()

	rec := serve(t, http.MethodPost, "/console/")

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	assert.Equal(t, "GET, HEAD", rec.Header().Get("Allow"))
}

func TestHandlerSetsPolicy(t *testing.T) {
	t.Parallel()

	rec := serve(t, http.MethodGet, "/console/")

	assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "script-src 'self' 'wasm-unsafe-eval'")
	assert.Contains(t, rec.Header().Get("Content-Security-Policy"), "connect-src 'self' wss:")
	assert.Contains(t, rec.Header().Get("Content-Type"), "text/html")
}

// TestHandlerServesConsole covers the two shapes a binary can have: with
// the console built into dist/, every unknown path falls back to the SPA
// entry and hashed assets are immutable; without it, the placeholder
// explains how to build it.
func TestHandlerServesConsole(t *testing.T) {
	t.Parallel()

	if !Built() {
		rec := serve(t, http.MethodGet, "/console/machines/42")
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
		assert.Contains(t, rec.Body.String(), "make web")
		assert.Equal(t, "no-cache", rec.Header().Get("Cache-Control"))

		return
	}

	index := serve(t, http.MethodGet, "/console/")
	require.Equal(t, http.StatusOK, index.Code)
	assert.Equal(t, "no-cache", index.Header().Get("Cache-Control"))
	assert.Contains(t, index.Body.String(), `id="root"`)
	assert.Contains(t, index.Body.String(), `property="og:image" content="https://hs.example.test/opengraph.png"`,
		"the social card names the server's own address")
	assert.NotContains(t, index.Body.String(), serverURLPlaceholder)

	deep := serve(t, http.MethodGet, "/console/machines/42?x=1")
	assert.Equal(t, http.StatusOK, deep.Code)
	assert.Equal(t, index.Body.String(), deep.Body.String())

	// Find one hashed asset through the entry point and fetch it.
	body := index.Body.String()
	start := strings.Index(body, `src="/console/assets/`)
	require.NotEqual(t, -1, start, "index.html references no script under assets/")
	rest := body[start+len(`src="`):]
	asset, _, found := strings.Cut(rest, `"`)
	require.True(t, found)

	script := serve(t, http.MethodGet, asset)
	assert.Equal(t, http.StatusOK, script.Code)
	assert.Equal(t, cacheForever, script.Header().Get("Cache-Control"))
	assert.Contains(t, script.Header().Get("Content-Type"), "javascript")

	missing := serve(t, http.MethodGet, "/console/assets/nope.js")
	assert.Equal(t, http.StatusOK, missing.Code, "unknown asset paths fall back to the SPA entry")
	assert.NotEqual(t, cacheForever, missing.Header().Get("Cache-Control"))
}

// TestHandlerAdmitsInlineScriptByHash checks the policy carries the hash of
// the colour-mode script in the served page, so the browser runs it before
// the first paint instead of blocking it.
func TestHandlerAdmitsInlineScriptByHash(t *testing.T) {
	t.Parallel()

	if !Built() {
		t.Skip("console not built")
	}

	index := serve(t, http.MethodGet, "/console/")
	require.Equal(t, http.StatusOK, index.Code)

	scripts := inlineScripts(index.Body.String())
	require.Len(t, scripts, 1, "index.html carries exactly the colour-mode script inline")
	assert.Contains(t, scripts[0], "prefers-color-scheme")

	sum := sha256.Sum256([]byte(scripts[0]))
	want := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"

	_, directives, found := strings.Cut(index.Header().Get("Content-Security-Policy"), "script-src")
	require.True(t, found)

	directive, _, _ := strings.Cut(directives, ";")
	assert.Equal(t, " 'self' 'wasm-unsafe-eval' "+want, directive)
}

func TestInlineScriptsSkipExternal(t *testing.T) {
	t.Parallel()

	page := `<head><script>a()</script><script src="/x.js"></script><script>
b()
</script></head>`

	assert.Equal(t, []string{"a()", "\nb()\n"}, inlineScripts(page))
	assert.Empty(t, inlineScripts(`<script src="/x.js"></script>`))
	assert.Empty(t, inlineScripts(`<script>unterminated`))
}

func TestRootHandlerSendsToConsole(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	RootHandler(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", http.NoBody))

	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, Prefix, rec.Header().Get("Location"))
}

func TestLegacyHandlerKeepsPathAndQuery(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"/admin":                       "/console/",
		"/admin/":                      "/console/",
		"/admin/machines/42":           "/console/machines/42",
		"/admin/login?invite=deadbeef": "/console/login?invite=deadbeef",
		"/admin/machines?q=alice&x=1":  "/console/machines?q=alice&x=1",
	}

	for target, want := range cases {
		rec := httptest.NewRecorder()
		LegacyHandler(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody))

		assert.Equal(t, http.StatusMovedPermanently, rec.Code, target)
		assert.Equal(t, want, rec.Header().Get("Location"), target)
	}
}
