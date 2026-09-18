package types

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/conf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBrandingDefault proves a server with no branding section is the
// product, and that the console is told to draw its own mark.
func TestBrandingDefault(t *testing.T) {
	conf.Reset()
	require.NoError(t, LoadConfig("testdata/minimal.yaml", true))

	b, err := brandingConfig()
	require.NoError(t, err)

	assert.Equal(t, DefaultBrandTitle, b.Title)
	assert.Equal(t, DefaultBrandDescription, b.Description)
	assert.False(t, b.Custom())
	assert.Empty(t, b.LogoURL())

	_, ok := b.Card()
	assert.False(t, ok)
}

// TestBrandingFromEnv proves the title can be renamed without touching the
// config file, the way the deployment sets every other value.
func TestBrandingFromEnv(t *testing.T) {
	t.Setenv("SLOPSCALE_BRANDING_TITLE", "Example VPN")

	conf.Reset()
	require.NoError(t, LoadConfig("testdata/minimal.yaml", true))

	b, err := brandingConfig()
	require.NoError(t, err)

	assert.Equal(t, "Example VPN", b.Title)
}

// TestBrandingSocialImage proves an operator's own card is read, sized from
// the file itself, and served under a hashed address of its own.
func TestBrandingSocialImage(t *testing.T) {
	dir := t.TempDir()
	card := filepath.Join(dir, "card.png")
	require.NoError(t, os.WriteFile(card, testPNG(t, 1200, 630), 0o600))

	t.Setenv("SLOPSCALE_BRANDING_SOCIAL_IMAGE_PATH", card)

	conf.Reset()
	require.NoError(t, LoadConfig("testdata/minimal.yaml", true))

	b, err := brandingConfig()
	require.NoError(t, err)

	got, ok := b.Card()
	require.True(t, ok)
	assert.True(t, strings.HasPrefix(got.URL, BrandingSocialPath+"?v="))
	assert.Equal(t, "image/png", got.ContentType)
	assert.Equal(t, 1200, got.Width)
	assert.Equal(t, 630, got.Height)
	assert.Equal(t, DefaultBrandTitle, got.Alt)

	raw, contentType, ok := b.SocialImage()
	assert.True(t, ok)
	assert.NotEmpty(t, raw)
	assert.Equal(t, "image/png", contentType)
}

// testPNG returns a real PNG of the given size, so the header a card is
// measured by is the one a decoder wrote.
func testPNG(t *testing.T, width, height int) []byte {
	t.Helper()

	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, width, height))))

	return buf.Bytes()
}

// TestBrandingLogo proves a configured logo is read at startup and given a
// URL that changes with the file, so a replacement is fetched again rather
// than served from a cache that was told to keep it forever.
func TestBrandingLogo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logo.svg")
	require.NoError(t, os.WriteFile(path, []byte("<svg/>"), 0o600))

	t.Setenv("SLOPSCALE_BRANDING_LOGO_PATH", path)

	conf.Reset()
	require.NoError(t, LoadConfig("testdata/minimal.yaml", true))

	b, err := brandingConfig()
	require.NoError(t, err)

	logo, contentType, ok := b.Logo(false)
	assert.True(t, ok)
	assert.Equal(t, "<svg/>", string(logo))
	assert.Equal(t, "image/svg+xml", contentType)

	first := b.LogoURL()
	assert.True(t, strings.HasPrefix(first, BrandingLogoPath+"?v="))

	// One logo answers for both themes, so a caller picking by theme does
	// not have to ask whether a second one exists.
	assert.Equal(t, first, b.DarkLogoURL())

	darkLogo, _, ok := b.Logo(true)
	assert.True(t, ok)
	assert.Equal(t, "<svg/>", string(darkLogo))

	require.NoError(t, os.WriteFile(path, []byte("<svg id='2'/>"), 0o600))

	conf.Reset()
	require.NoError(t, LoadConfig("testdata/minimal.yaml", true))

	replaced, err := brandingConfig()
	require.NoError(t, err)
	assert.NotEqual(t, first, replaced.LogoURL())
}

// TestBrandingDarkLogo proves the dark variant is served from its own
// address, so the console can swap logos with its theme control and a
// browser cache keeps both.
func TestBrandingDarkLogo(t *testing.T) {
	dir := t.TempDir()
	light := filepath.Join(dir, "logo.svg")
	dark := filepath.Join(dir, "logo-dark.png")

	require.NoError(t, os.WriteFile(light, []byte("<svg/>"), 0o600))
	require.NoError(t, os.WriteFile(dark, []byte("dark bytes"), 0o600))

	t.Setenv("SLOPSCALE_BRANDING_LOGO_PATH", light)
	t.Setenv("SLOPSCALE_BRANDING_LOGO_DARK_PATH", dark)

	conf.Reset()
	require.NoError(t, LoadConfig("testdata/minimal.yaml", true))

	b, err := brandingConfig()
	require.NoError(t, err)

	raw, contentType, ok := b.Logo(true)
	assert.True(t, ok)
	assert.Equal(t, "dark bytes", string(raw))
	assert.Equal(t, "image/png", contentType)

	assert.True(t, strings.HasPrefix(b.DarkLogoURL(), BrandingDarkLogoPath+"?v="))
	assert.NotEqual(t, b.LogoURL(), b.DarkLogoURL())

	light2, _, ok := b.Logo(false)
	assert.True(t, ok)
	assert.Equal(t, "<svg/>", string(light2))
}

// TestBrandingRefused proves the server refuses a branding section it
// cannot serve at startup, where configtest catches it, rather than on the
// first page render.
func TestBrandingRefused(t *testing.T) {
	dir := t.TempDir()
	unsupported := filepath.Join(dir, "logo.bmp")
	require.NoError(t, os.WriteFile(unsupported, []byte("x"), 0o600))

	empty := filepath.Join(dir, "empty.png")
	require.NoError(t, os.WriteFile(empty, nil, 0o600))

	big := filepath.Join(dir, "big.png")
	require.NoError(t, os.WriteFile(big, make([]byte, maxLogoBytes+1), 0o600))

	good := filepath.Join(dir, "good.svg")
	require.NoError(t, os.WriteFile(good, []byte("<svg/>"), 0o600))

	socialFormat := filepath.Join(dir, "card.webp")
	require.NoError(t, os.WriteFile(socialFormat, []byte("x"), 0o600))

	notAnImage := filepath.Join(dir, "card.png")
	require.NoError(t, os.WriteFile(notAnImage, []byte("not a png"), 0o600))

	tests := []struct {
		name string
		env  map[string]string
		want error
	}{
		{
			name: "empty title",
			env:  map[string]string{"SLOPSCALE_BRANDING_TITLE": "  "},
			want: errBrandingTitleEmpty,
		},
		{
			name: "unsupported format",
			env:  map[string]string{"SLOPSCALE_BRANDING_LOGO_PATH": unsupported},
			want: errLogoFormat,
		},
		{
			name: "empty file",
			env:  map[string]string{"SLOPSCALE_BRANDING_LOGO_PATH": empty},
			want: errLogoEmpty,
		},
		{
			name: "too large",
			env:  map[string]string{"SLOPSCALE_BRANDING_LOGO_PATH": big},
			want: errLogoSize,
		},
		{
			name: "missing file",
			env:  map[string]string{"SLOPSCALE_BRANDING_LOGO_PATH": filepath.Join(dir, "gone.png")},
			want: os.ErrNotExist,
		},
		{
			name: "dark logo without a light one",
			env:  map[string]string{"SLOPSCALE_BRANDING_LOGO_DARK_PATH": unsupported},
			want: errDarkNoLight,
		},
		{
			name: "empty description",
			env:  map[string]string{"SLOPSCALE_BRANDING_DESCRIPTION": " "},
			want: errBrandingDescriptionEmpty,
		},
		{
			name: "social card in a format no unfurler draws",
			env:  map[string]string{"SLOPSCALE_BRANDING_SOCIAL_IMAGE_PATH": socialFormat},
			want: errSocialFormat,
		},
		{
			name: "social card that is not an image",
			env:  map[string]string{"SLOPSCALE_BRANDING_SOCIAL_IMAGE_PATH": notAnImage},
			want: errSocialDecode,
		},
		{
			name: "missing dark logo behind a good light one",
			env: map[string]string{
				"SLOPSCALE_BRANDING_LOGO_PATH":      good,
				"SLOPSCALE_BRANDING_LOGO_DARK_PATH": filepath.Join(dir, "gone.png"),
			},
			want: os.ErrNotExist,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			conf.Reset()
			require.NoError(t, LoadConfig("testdata/minimal.yaml", true))

			_, err := brandingConfig()
			require.ErrorIs(t, err, tt.want)
		})
	}
}
