// Package assets provides embedded static assets for Slopscale.
// All static files (favicon, CSS, SVG) are embedded here for
// centralized asset management.
package assets

import (
	_ "embed"
)

// Favicon is the embedded favicon.png file served at /favicon.ico
//
//go:embed favicon.png
var Favicon []byte

// OpenGraph is the 1200x630 card served at /opengraph.png and named by the
// og:image of every page the server renders, so a link to the server
// unfurls with the mark and the name in a chat or a feed.
//
//go:embed opengraph.png
var OpenGraph []byte

// CSS is the embedded style.css stylesheet used in HTML templates.
// Contains Material for MkDocs design system styles.
//
//go:embed style.css
var CSS string

// SVG is the embedded slopscale.svg logo used in HTML templates.
//
//go:embed slopscale.svg
var SVG string
