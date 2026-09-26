package templates

import (
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
)

// TestPageSocialCard pins the tags a chat or a feed reads to draw a preview
// of a shared link: the image and the URL are absolute, built from the
// server's address without a doubled slash, and a page rendered before the
// address is known still says what it is, with the compact card.
func TestPageSocialCard(t *testing.T) {
	SetServerURL("https://hs.example.test/")
	t.Cleanup(func() { SetServerURL("") })

	out := page("Sign in - Slopscale").Render()

	assert.Contains(t, out, `<title>Sign in - Slopscale</title>`)
	assert.Contains(t, out, `content="Sign in - Slopscale" property="og:title"`)
	assert.Contains(t, out, `content="https://hs.example.test/opengraph.png" property="og:image"`)
	assert.Contains(t, out, `content="summary_large_image" name="twitter:card"`)

	SetServerURL("")

	bare := page("Sign in - Slopscale").Render()

	assert.NotContains(t, bare, "og:image")
	assert.Contains(t, bare, `content="summary" name="twitter:card"`)
}

// TestPageBranding proves an operator's name and logo reach the pages, and
// that the baked social card is dropped for them: it is drawn with the
// product's own mark, which is the wrong picture for a rebranded server.
func TestPageBranding(t *testing.T) {
	SetServerURL("https://vpn.example.test")
	t.Cleanup(func() { SetServerURL("") })

	out := page(pageTitle("Sign in")).Render()

	assert.Contains(t, out, `<title>Sign in - Slopscale</title>`)
	assert.Contains(t, out, `class="slopscale-logo"`)

	SetBranding(types.Branding{Title: "Example VPN"})
	t.Cleanup(func() { SetBranding(types.Branding{Title: types.DefaultBrandTitle}) })

	named := page(pageTitle("Sign in")).Render()

	assert.Contains(t, named, `<title>Sign in - Example VPN</title>`)
	assert.Contains(t, named, `content="Example VPN" property="og:site_name"`)
	// No logo was configured, so the built-in mark still stands in for one.
	assert.Contains(t, named, `class="slopscale-logo"`)
	assert.Contains(t, named, `content="https://vpn.example.test/opengraph.png" property="og:image"`)
}

// TestPageBrandingLogo proves a configured logo is referenced rather than
// inlined, and that it takes the social card with it.
func TestPageBrandingLogo(t *testing.T) {
	SetServerURL("https://vpn.example.test")
	t.Cleanup(func() { SetServerURL("") })

	SetBranding(types.NewBranding("Example VPN", "image/png", []byte("png bytes")))
	t.Cleanup(func() { SetBranding(types.Branding{Title: types.DefaultBrandTitle}) })

	out := page(pageTitle("Sign in")).Render()

	assert.Contains(t, out, `class="brand-logo"`)
	assert.Contains(t, out, `src="`+types.BrandingLogoPath+`?v=`)
	assert.NotContains(t, out, `class="slopscale-logo"`)
	assert.NotContains(t, out, "og:image")
	assert.Contains(t, out, `content="summary" name="twitter:card"`)
	// One logo answers for both themes, so there is nothing to choose
	// between and the image stands on its own.
	assert.NotContains(t, out, "srcset")
}

// TestPageBrandingDarkLogo proves a page with two logos offers both and lets
// the browser pick, since these pages carry no theme control of their own.
func TestPageBrandingDarkLogo(t *testing.T) {
	brand := types.NewBranding("Example VPN", "image/png", []byte("png bytes")).
		WithDarkLogo("image/png", []byte("dark png bytes"))

	SetBranding(brand)
	t.Cleanup(func() { SetBranding(types.Branding{Title: types.DefaultBrandTitle}) })

	out := page(pageTitle("Sign in")).Render()

	assert.Contains(t, out, `<picture class="brand-picture">`)
	assert.Contains(t, out, `media="(prefers-color-scheme: dark)"`)
	assert.Contains(t, out, `srcset="`+types.BrandingDarkLogoPath+`?v=`)
	// The light logo stays the <img>, so a browser without <picture>
	// support still draws something.
	assert.Contains(t, out, `class="brand-logo"`)
	assert.Contains(t, out, `src="`+types.BrandingLogoPath+`?v=`)
}

// TestPageDescription proves the line a chat or a feed shows under a link
// is the operator's where they wrote one.
func TestPageDescription(t *testing.T) {
	out := page(pageTitle("Sign in")).Render()

	assert.Contains(t, out, `content="`+types.DefaultBrandDescription+`" name="description"`)

	SetBranding(types.Branding{Title: "Example VPN", Description: "The Example Inc private network"})
	t.Cleanup(func() { SetBranding(types.Branding{Title: types.DefaultBrandTitle}) })

	named := page(pageTitle("Sign in")).Render()

	assert.Contains(t, named, `content="The Example Inc private network" name="description"`)
	assert.Contains(t, named, `content="The Example Inc private network" property="og:description"`)
	assert.Contains(t, named, `content="The Example Inc private network" name="twitter:description"`)
	assert.NotContains(t, named, types.DefaultBrandDescription)
}

// TestPageBrandingSocialImage proves an operator who supplies their own card
// gets the picture back that a custom logo takes away, at the size read from
// their file rather than the product card's.
func TestPageBrandingSocialImage(t *testing.T) {
	SetServerURL("https://vpn.example.test")
	t.Cleanup(func() { SetServerURL("") })

	brand := types.NewBranding("Example VPN", "image/png", []byte("png bytes")).
		WithSocialImage("image/jpeg", []byte("jpeg bytes"), 1600, 900)

	SetBranding(brand)
	t.Cleanup(func() { SetBranding(types.Branding{Title: types.DefaultBrandTitle}) })

	out := page(pageTitle("Sign in")).Render()

	assert.Contains(t, out, `content="https://vpn.example.test`+types.BrandingSocialPath+`?v=`)
	assert.Contains(t, out, `content="image/jpeg" property="og:image:type"`)
	assert.Contains(t, out, `content="1600" property="og:image:width"`)
	assert.Contains(t, out, `content="900" property="og:image:height"`)
	assert.Contains(t, out, `content="Example VPN" property="og:image:alt"`)
	assert.Contains(t, out, `content="summary_large_image" name="twitter:card"`)
	// The product's own card is never named for a rebranded server.
	assert.NotContains(t, out, OpenGraphPath)
}
