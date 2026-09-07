// Package web serves the admin console: a client-rendered React app built
// into dist/ by `make web` and embedded into the headscale binary.
//
// The console is a static bundle that talks to /api/v1 with the API key the
// operator pastes at sign-in, so the server only has to deliver files. A
// binary built without running `make web` still serves a page under /admin/
// that says so instead of a 404.
package web

import (
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
// stylesheets; scripts stay strictly self-hosted.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; " +
	"base-uri 'self'; form-action 'self'"

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
		if errors.Is(statErr, fs.ErrNotExist) {
			serveIndex(w, r, sub)

			return
		}

		if statErr != nil {
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

			return
		}

		if strings.HasPrefix(rel, assetsDir) {
			w.Header().Set("Cache-Control", cacheForever)
		}

		r2 := r.Clone(r.Context())
		r2.URL.Path = "/" + rel
		files.ServeHTTP(w, r2)
	})
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

const unbuiltPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>headscale admin console</title>
<style>
body{font-family:system-ui,sans-serif;max-width:40rem;margin:4rem auto;padding:0 1.5rem;line-height:1.5;color:#1f2430}
code{background:#eef0f4;padding:.1rem .35rem;border-radius:.25rem}
</style>
</head>
<body>
<h1>Admin console not built</h1>
<p>This headscale binary was compiled without the web console. Build it with
<code>make web</code> before <code>make build</code>, or use a release binary, and it will be served here.</p>
<p>The API is still available under <code>/api/v1</code>.</p>
</body>
</html>
`

func serveUnbuilt(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte(unbuiltPage))
}
