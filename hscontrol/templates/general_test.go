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
}
