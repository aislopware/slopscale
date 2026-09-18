// Package web serves the admin console: a client-rendered React app built
// into dist/ by `make web` and embedded into the slopscale binary.
//
// The console is a static bundle that talks to /api/v1 with the API key the
// operator pastes at sign-in, so the server only has to deliver files. A
// binary built without running `make web` still serves a page under /console/
// that says so instead of a 404.
package web

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"io/fs"
	"net/http"
	"path"
	"strconv"
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
const Prefix = "/console/"

// LegacyPrefix is where the console lived before 0.31. A link made then, in
// a webhook, an invitation or a bookmark, is sent on to Prefix with the rest
// of its path and its query kept.
const LegacyPrefix = "/admin/"

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
// The entry page carries an inline script that sets the colour mode before
// the first paint; scriptSources adds its hash so the policy admits it and
// nothing else inline.
const contentSecurityPolicy = "default-src 'self'; script-src 'self' 'wasm-unsafe-eval'%s; " +
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

// RootHandler sends the server's front door to the console: there is nothing
// else for a browser at "/", and an operator who types the bare address
// expects to land somewhere.
func RootHandler(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, Prefix, http.StatusFound)
}

// LegacyHandler sends a request for the console's old address to its new
// one. Mount it at LegacyPrefix without the trailing slash and LegacyPrefix
// followed by a wildcard, like Handler.
func LegacyHandler(w http.ResponseWriter, r *http.Request) {
	// "/admin" without the slash has nothing after the prefix.
	rel, ok := strings.CutPrefix(r.URL.Path, LegacyPrefix)
	if !ok {
		rel = ""
	}

	target := Prefix + rel

	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}

	// The target always starts with Prefix on this origin; the request only supplies what follows.
	//nolint:gosec // G710: not an open redirect, see above.
	http.Redirect(w, r, target, http.StatusMovedPermanently)
}

// serverURLPlaceholder is what index.html carries where the server's own
// address belongs: the social card a chat or a feed draws for a shared
// console link needs absolute URLs, and the bundle is built before the
// address is known.
const serverURLPlaceholder = "__SLOPSCALE_URL__"

// brandPlaceholder is what index.html carries where the product's name
// belongs, for the same reason: the bundle is built before the operator's
// configuration is read.
const brandPlaceholder = "__SLOPSCALE_BRAND__"

// descriptionPlaceholder is what index.html carries where the line under a
// shared link belongs.
const descriptionPlaceholder = "__SLOPSCALE_DESCRIPTION__"

// cardMarkers bracket the social card tags in index.html. The card between
// them is drawn with the product's own mark, so a console an operator
// rebranded serves their own card in its place, or none at all where they
// configured none, rather than unfurling the wrong picture.
var cardMarkers = [2]string{"<!--card-->", "<!--/card-->"}

// Brand is what the entry page calls the server.
type Brand struct {
	// Title is the product name, "Slopscale" unless the config file
	// renames it.
	Title string
	// Description is the line a chat or a feed shows under a link to the
	// console.
	Description string
	// Custom reports that the operator put their own logo on the server.
	Custom bool
	// Card is the operator's own social image. The zero value means they
	// configured none.
	Card SocialCard
}

// SocialCard is the picture a link to the console unfurls with. The size is
// the file's own, because an unfurler told the wrong one crops to fit it.
type SocialCard struct {
	// URL is the path the card is served at, relative to the server.
	URL         string
	ContentType string
	Width       int
	Height      int
	Alt         string
}

func (c SocialCard) set() bool { return c.URL != "" }

// Handler serves the console. Mount it at both Prefix without the trailing
// slash (to redirect) and Prefix followed by a wildcard. serverURL is the
// address the server is reached at, written into the entry page's social
// card tags, and brand is what the page calls it.
func Handler(serverURL string, brand Brand) http.Handler {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		// The embed directive guarantees the directory exists.
		panic(err)
	}

	files := http.FileServerFS(sub)
	built := Built()
	policy := fmt.Sprintf(contentSecurityPolicy, scriptSources(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)

			return
		}

		w.Header().Set("Content-Security-Policy", policy)

		rel, ok := strings.CutPrefix(r.URL.Path, Prefix)
		if !ok {
			// "/console" without the slash: relative asset URLs need it.
			http.Redirect(w, r, Prefix, http.StatusMovedPermanently)

			return
		}

		if !built {
			serveUnbuilt(w)

			return
		}

		if rel == "" || rel == indexFile {
			serveIndex(w, r, sub, serverURL, brand)

			return
		}

		_, statErr := fs.Stat(sub, rel)
		if errors.Is(statErr, fs.ErrNotExist) && strings.HasSuffix(rel, ".wasm") {
			// make web keeps only the gzipped client.
			_, statErr = fs.Stat(sub, rel+".gz")
		}

		if errors.Is(statErr, fs.ErrNotExist) {
			serveIndex(w, r, sub, serverURL, brand)

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

// scriptSources returns a CSP source expression, with a leading space, for
// every inline script in the entry page: the SHA-256 of the text between the
// tags, which is what a browser checks. Vite leaves a classic inline script
// untouched, so the hash of the embedded page is the hash of the served one;
// serveIndex's placeholder is outside the script. An unbuilt bundle has no
// page and adds nothing.
func scriptSources(sub fs.FS) string {
	index, err := fs.ReadFile(sub, indexFile)
	if err != nil {
		return ""
	}

	var sources strings.Builder

	for _, script := range inlineScripts(string(index)) {
		sum := sha256.Sum256([]byte(script))

		sources.WriteString(" 'sha256-")
		sources.WriteString(base64.StdEncoding.EncodeToString(sum[:]))
		sources.WriteString("'")
	}

	return sources.String()
}

// inlineScripts returns the body of every attribute-less <script> element in
// the page, in order. External scripts carry src and are covered by 'self'.
func inlineScripts(page string) []string {
	const open, closing = "<script>", "</script>"

	var scripts []string

	for {
		_, rest, found := strings.Cut(page, open)
		if !found {
			return scripts
		}

		body, after, found := strings.Cut(rest, closing)
		if !found {
			return scripts
		}

		scripts = append(scripts, body)
		page = after
	}
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
// next load while its hashed assets stay cached, with the server's address
// and the operator's brand in place of the placeholders it carries.
func serveIndex(w http.ResponseWriter, r *http.Request, sub fs.FS, serverURL string, brand Brand) {
	index, err := fs.ReadFile(sub, indexFile)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)

		return
	}

	base := strings.TrimSuffix(serverURL, "/")

	page := strings.ReplaceAll(string(index), serverURLPlaceholder, base)
	page = strings.ReplaceAll(page, brandPlaceholder, html.EscapeString(brand.Title))
	page = strings.ReplaceAll(page, descriptionPlaceholder, html.EscapeString(brand.Description))

	switch {
	case brand.Card.set():
		page = replaceCard(page, cardTags(base, brand))
	case brand.Custom:
		page = cutCard(page)
	}

	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, indexFile, time.Time{}, strings.NewReader(page))
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

// cutCard removes the social card tags, markers and all. A page without the
// markers is returned as it is, so a bundle built from a different entry
// page still serves.
func cutCard(page string) string {
	return replaceCard(page, "")
}

// replaceCard puts tags in place of the ones between the markers.
func replaceCard(page, tags string) string {
	before, rest, found := strings.Cut(page, cardMarkers[0])
	if !found {
		return page
	}

	_, after, found := strings.Cut(rest, cardMarkers[1])
	if !found {
		return page
	}

	return before + tags + strings.TrimLeft(after, " \t\n")
}

// cardTags draws the operator's own card, at the size and type read from
// their file rather than the product card's.
func cardTags(base string, brand Brand) string {
	card := brand.Card
	image := html.EscapeString(base + card.URL)
	title := html.EscapeString(brand.Title) + " admin console"

	var tags strings.Builder

	for _, tag := range [][2]string{
		{`property="og:image"`, image},
		{`property="og:image:type"`, html.EscapeString(card.ContentType)},
		{`property="og:image:width"`, strconv.Itoa(card.Width)},
		{`property="og:image:height"`, strconv.Itoa(card.Height)},
		{`property="og:image:alt"`, html.EscapeString(card.Alt)},
		{`name="twitter:card"`, "summary_large_image"},
		{`name="twitter:title"`, title},
		{`name="twitter:description"`, html.EscapeString(brand.Description)},
		{`name="twitter:image"`, image},
	} {
		fmt.Fprintf(&tags, "<meta %s content=\"%s\" />\n    ", tag[0], tag[1])
	}

	return tags.String()
}
