// Package web serves the admin console: a client-rendered React app built
// into dist/ by `make web` and embedded into the slopscale binary.
//
// The console is a static bundle that talks to /api/v1 with the API key the
// operator pastes at sign-in, so the server only has to deliver files. A
// binary built without running `make web` still serves a page under /admin/
// that says so instead of a 404.
package web

import (
	"bytes"
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// dist holds the Vite output. `all:` keeps dot-files so the committed
// .gitkeep makes the pattern match even before the console is built.
//
//go:embed all:dist
var dist embed.FS

// Prefix is the path the console is mounted at, with a trailing slash. Vite's
// `base` in web/vite.config.ts must match it.
const Prefix = "/admin/"

// indexFile is the SPA entry; every unknown path under Prefix serves it so
// the client router can take over after a reload or a pasted link.
const indexFile = "index.html"

// assetsDir holds Vite's content-hashed output; it may be cached forever.
const assetsDir = "assets/"

const cacheForever = "public, max-age=31536000, immutable"

// contentSecurityPolicy locks the console down to its own origin. Inline
// styles are needed for Base UI's positioning and CodeMirror's injected
// stylesheets; scripts stay strictly self-hosted, and wasm-unsafe-eval
// lets the SSH terminal instantiate Tailscale's in-browser client, which
// is served from this origin too. Images may come from any https origin
// so a user's profile picture can be previewed. Connections stay on this
// origin except for wss:, which the in-browser client needs to reach the
// tailnet's relays; which relays is the DERP map's call, so the policy
// admits any secure websocket rather than a list that would go stale.
const contentSecurityPolicy = "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'; " +
	"style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self'; " +
	"connect-src 'self' wss:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

// tsconnectDir holds the in-browser client; its hashed files may be
// cached forever like Vite's assets, and the wasm is stored gzipped.
const tsconnectDir = "tsconnect/"

// Built reports whether the embedded bundle contains a console.
func Built() bool {
	_, err := fs.Stat(dist, path.Join("dist", indexFile))

	return err == nil
}

// Handler serves the console. Mount it at both Prefix without the trailing
// slash (to redirect) and Prefix followed by a wildcard.
func Handler() http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// The embed directive guarantees the directory exists.
		panic(err)
	}

	files := http.FileServerFS(sub)
	built := Built()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)

			return
		}

		w.Header().Set("Content-Security-Policy", contentSecurityPolicy)

		rel, ok := strings.CutPrefix(r.URL.Path, Prefix)
		if !ok {
			// "/admin" without the slash: relative asset URLs need it.
			http.Redirect(w, r, Prefix, http.StatusMovedPermanently)

			return
		}

		if !built {
			serveUnbuilt(w)

			return
		}

		if rel == "" || rel == indexFile {
			serveIndex(w, r, sub)

			return
		}

		_, statErr := fs.Stat(sub, rel)
		if errors.Is(statErr, fs.ErrNotExist) && strings.HasSuffix(rel, ".wasm") {
			// make web keeps only the gzipped client.
			_, statErr = fs.Stat(sub, rel+".gz")
		}

		if errors.Is(statErr, fs.ErrNotExist) {
			serveIndex(w, r, sub)

			return
		}

		if statErr != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

			return
		}

		if strings.HasPrefix(rel, assetsDir) || isHashedTsconnect(rel) {
			w.Header().Set("Cache-Control", cacheForever)
		}

		if strings.HasSuffix(rel, ".wasm") {
			serveGzipped(w, r, sub, rel)

			return
		}

		r2 := r.Clone(r.Context())
		r2.URL.Path = "/" + rel
		files.ServeHTTP(w, r2)
	})
}

// isHashedTsconnect reports whether the path is a hashed file of the
// in-browser client (main-<hash>.wasm); its manifest and loader are not
// hashed and stay revalidated.
func isHashedTsconnect(rel string) bool {
	name, ok := strings.CutPrefix(rel, tsconnectDir)

	return ok && strings.HasPrefix(name, "main-") && strings.Contains(name, ".wasm")
}

// serveGzipped serves the client's wasm, which dist holds gzipped. Every
// browser accepts gzip, so the compressed bytes go out as they are with
// the encoding declared, and the browser's streaming instantiation gets
// the wasm content type it insists on. The raw file, when present (a
// dist that make web did not prune), is served as is.
func serveGzipped(w http.ResponseWriter, r *http.Request, sub fs.FS, rel string) {
	w.Header().Set("Content-Type", "application/wasm")

	packed, err := fs.ReadFile(sub, rel+".gz")
	if err != nil {
		raw, rawErr := fs.ReadFile(sub, rel)
		if rawErr != nil {
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)

			return
		}

		http.ServeContent(w, r, rel, time.Time{}, bytes.NewReader(raw))

		return
	}

	w.Header().Set("Content-Encoding", "gzip")
	w.Header().Set("Vary", "Accept-Encoding")
	http.ServeContent(w, r, rel, time.Time{}, bytes.NewReader(packed))
}

// serveIndex writes index.html uncached so a new release is picked up on the
// next load while its hashed assets stay cached.
func serveIndex(w http.ResponseWriter, r *http.Request, sub fs.FS) {
	index, err := fs.ReadFile(sub, indexFile)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

		return
	}

	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, indexFile, time.Time{}, strings.NewReader(string(index)))
}

// The page served in place of the console when the binary was built without it. It is its own
// file so the HTML stays readable and out of Go's line length.
//
//go:embed unbuilt.html
var unbuiltPage string

func serveUnbuilt(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(unbuiltPage))
}
