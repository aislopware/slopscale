package templates

import (
	"testing"

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
