package types

import (
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
	assert.False(t, b.Custom())
	assert.Empty(t, b.LogoURL())
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

	logo, contentType, ok := b.Logo()
	assert.True(t, ok)
	assert.Equal(t, "<svg/>", string(logo))
	assert.Equal(t, "image/svg+xml", contentType)

	first := b.LogoURL()
	assert.True(t, strings.HasPrefix(first, BrandingLogoPath+"?v="))

	require.NoError(t, os.WriteFile(path, []byte("<svg id='2'/>"), 0o600))

	conf.Reset()
	require.NoError(t, LoadConfig("testdata/minimal.yaml", true))

	replaced, err := brandingConfig()
	require.NoError(t, err)
	assert.NotEqual(t, first, replaced.LogoURL())
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
