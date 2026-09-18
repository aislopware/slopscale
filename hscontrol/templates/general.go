package templates

import (
	"strings"
	"sync/atomic"

	"github.com/aislopware/slopscale/hscontrol/assets"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/chasefleming/elem-go"
	"github.com/chasefleming/elem-go/attrs"
	"github.com/chasefleming/elem-go/styles"
)

// mdTypesetBody creates a body element with md-typeset styling
// that matches the official Slopscale documentation design.
// Uses CSS classes with styles defined in [assets.CSS].
func mdTypesetBody(children ...elem.Node) *elem.Element {
	return elem.Body(
		attrs.Props{
			attrs.Style: styles.Props{
				styles.MinHeight:       "100vh",
				styles.Display:         "flex",
				styles.FlexDirection:   "column",
				styles.AlignItems:      "center",
				styles.BackgroundColor: "var(--hs-bg)",
				styles.Padding:         "3rem 1.5rem",
			}.ToInline(),
			"translate": "no",
		},
		elem.Main(attrs.Props{
			attrs.Class: "md-typeset",
			attrs.Style: styles.Props{
				styles.MaxWidth: "min(800px, 90vw)",
				styles.Width:    "100%",
			}.ToInline(),
		}, children...),
	)
}

// Styled Element Wrappers
// These functions wrap elem-go elements using CSS classes.
// Styling is handled by the CSS in [assets.CSS].

// H1 creates a H1 element styled by .md-typeset h1
func H1(children ...elem.Node) *elem.Element {
	return elem.H1(nil, children...)
}

// H2 creates a H2 element styled by .md-typeset h2
func H2(children ...elem.Node) *elem.Element {
	return elem.H2(nil, children...)
}

// H3 creates a H3 element styled by .md-typeset h3
func H3(children ...elem.Node) *elem.Element {
	return elem.H3(nil, children...)
}

// P creates a paragraph element styled by .md-typeset p
func P(children ...elem.Node) *elem.Element {
	return elem.P(nil, children...)
}

// Ol creates an ordered list element styled by .md-typeset ol
func Ol(children ...elem.Node) *elem.Element {
	return elem.Ol(nil, children...)
}

// Ul creates an unordered list element styled by .md-typeset ul
func Ul(children ...elem.Node) *elem.Element {
	return elem.Ul(nil, children...)
}

// A creates a link element styled by .md-typeset a
func A(href string, children ...elem.Node) *elem.Element {
	return elem.A(attrs.Props{attrs.Href: href}, children...)
}

// Code creates an inline code element styled by .md-typeset code
func Code(children ...elem.Node) *elem.Element {
	return elem.Code(nil, children...)
}

// codeBlockText creates a preformatted code block styled by
// .md-typeset pre > code.
func codeBlockText(code string) *elem.Element {
	return elem.Pre(nil, elem.Code(nil, elem.Text(code)))
}

// pageSettings is what every page render needs and no handler carries: the
// server's own address, for the absolute URLs a social card needs, and the
// operator's brand. Pages are rendered from several packages and from
// handlers that hold no configuration, so it is package state, replaced as
// a whole under an atomic pointer: a server starting in one test renders
// pages in another.
//
//nolint:gochecknoglobals // set at startup, read by every page render
var pageSettings atomic.Pointer[pageState]

// pageState is one consistent view of those settings.
type pageState struct {
	// serverURL has no trailing slash.
	serverURL string
	branding  types.Branding
}

// settings returns the current view, the product's own before a server set
// anything, so a page rendered in a test still says what it is.
func settings() pageState {
	current := pageSettings.Load()
	if current == nil {
		return pageState{branding: types.Branding{Title: types.DefaultBrandTitle}}
	}

	return *current
}

// SetBranding records the operator's product name and logo for the pages.
// A branding without a title keeps the product's own, so a page always has
// a name in its title bar.
func SetBranding(b types.Branding) {
	if b.Title == "" {
		b.Title = types.DefaultBrandTitle
	}

	next := settings()
	next.branding = b
	pageSettings.Store(&next)
}

// brandTitle is what the operator calls this server.
func brandTitle() string {
	return settings().branding.Title
}

// pageTitle is a document title: what the page is, then whose server it is.
func pageTitle(what string) string {
	return what + " - " + brandTitle()
}

// brandLogo returns the mark every page opens with: the operator's image
// when the config file names one, the built-in SVG otherwise. The image is
// referenced rather than inlined so a browser caches it once for every
// page; its URL carries a hash, so replacing it is picked up.
func brandLogo() elem.Node {
	brand := settings().branding

	url := brand.LogoURL()
	if url == "" {
		return elem.Raw(assets.SVG)
	}

	return elem.Img(attrs.Props{
		attrs.Src:   url,
		attrs.Alt:   brand.Title,
		attrs.Class: "brand-logo",
	})
}

// pageFooter creates a consistent footer for all pages.
func pageFooter() *elem.Element {
	return elem.Footer(
		attrs.Props{
			attrs.Style: styles.Props{
				styles.MarginTop:  space3XL,
				styles.TextAlign:  "center",
				styles.FontSize:   fontSizeSmall,
				styles.Color:      "var(--md-default-fg-color--light)",
				styles.LineHeight: lineHeightBase,
			}.ToInline(),
		},
		elem.Text("Powered by "),
		elem.A(attrs.Props{
			attrs.Href:   "https://github.com/aislopware/slopscale",
			attrs.Rel:    "noreferrer noopener",
			attrs.Target: "_blank",
		}, elem.Text("Slopscale")),
	)
}

// OpenGraphPath is where the server serves the 1200x630 card the pages name
// as their og:image.
const OpenGraphPath = "/opengraph.png"

// socialDescription is what a chat or a feed shows under a link to one of
// the server's pages.
const socialDescription = "Self-hosted Tailscale control server with a built-in admin console"

// SetServerURL records the server's public address for the pages' social
// cards. A card needs absolute URLs, and a crawler that unfurls a link has
// no other way to learn them. The empty string leaves the cards without an
// image or a URL.
func SetServerURL(url string) {
	next := settings()
	next.serverURL = strings.TrimSuffix(url, "/")
	pageSettings.Store(&next)
}

// socialMeta returns the Open Graph and Twitter card tags for a page, which
// Facebook, Slack, Telegram, Discord and the like read to draw a preview of
// a shared link.
func socialMeta(title string) []elem.Node {
	property := func(name, content string) elem.Node {
		return elem.Meta(attrs.Props{"property": name, attrs.Content: content})
	}
	named := func(name, content string) elem.Node {
		return elem.Meta(attrs.Props{attrs.Name: name, attrs.Content: content})
	}

	current := settings()

	tags := []elem.Node{
		named("description", socialDescription),
		property("og:type", "website"),
		property("og:site_name", current.branding.Title),
		property("og:title", title),
		property("og:description", socialDescription),
		named("twitter:title", title),
		named("twitter:description", socialDescription),
	}

	// The card is baked with the product's own mark and name, so an
	// operator who put their own brand on the server gets no image
	// rather than somebody else's.
	if current.serverURL == "" || current.branding.Custom() {
		return append(tags, named("twitter:card", "summary"))
	}

	image := current.serverURL + OpenGraphPath

	return append(tags,
		property("og:image", image),
		property("og:image:type", "image/png"),
		property("og:image:width", "1200"),
		property("og:image:height", "630"),
		property("og:image:alt", "The slopscale mark and name"),
		named("twitter:card", "summary_large_image"),
		named("twitter:image", image),
	)
}

// page renders a standard page: the given title in the document head, and a
// body that begins with the brand's logo, contains the supplied content
// nodes in order, and ends with the shared footer.
func page(title string, content ...elem.Node) *elem.Element {
	body := make([]elem.Node, 0, len(content)+2)
	body = append(body, brandLogo())
	body = append(body, content...)
	body = append(body, pageFooter())

	head := append([]elem.Node{elem.Title(nil, elem.Text(title))}, socialMeta(title)...)

	return HtmlStructure(head, mdTypesetBody(body...))
}

// HtmlStructure creates a complete HTML document structure with proper meta tags
// and semantic HTML5 structure. The head nodes and the body element are passed as
// parameters to allow for customization of each page.
// Styling is provided via a CSS stylesheet (Material for MkDocs design system) with
// minimal inline styles for layout and positioning.
func HtmlStructure(head []elem.Node, body *elem.Element) *elem.Element {
	children := []elem.Node{
		elem.Meta(attrs.Props{
			attrs.Charset: "UTF-8",
		}),
		elem.Meta(attrs.Props{
			attrs.HTTPequiv: "X-UA-Compatible",
			attrs.Content:   "IE=edge",
		}),
		elem.Meta(attrs.Props{
			attrs.Name:    "viewport",
			attrs.Content: "width=device-width, initial-scale=1.0",
		}),
		elem.Link(attrs.Props{
			attrs.Rel:  "icon",
			attrs.Href: "/favicon.ico",
		}),
		// Google Fonts for Roboto and Roboto Mono
		elem.Link(attrs.Props{
			attrs.Rel:     "preconnect",
			attrs.Href:    "https://fonts.gstatic.com",
			"crossorigin": "",
		}),
		elem.Link(attrs.Props{
			attrs.Rel:  "stylesheet",
			attrs.Href: "https://fonts.googleapis.com/css2?family=Roboto:wght@300;400;500;700&family=Roboto+Mono:wght@400;700&display=swap",
		}),
		// Material for MkDocs CSS styles
		elem.Style(attrs.Props{attrs.Type: "text/css"}, elem.Raw(assets.CSS)),
	}
	children = append(children, head...)

	return elem.Html(
		attrs.Props{attrs.Lang: "en"},
		elem.Head(nil, children...),
		body,
	)
}
