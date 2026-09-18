package types

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/aislopware/slopscale/hscontrol/conf"
	"github.com/aislopware/slopscale/hscontrol/util"
)

// DefaultBrandTitle is the product's own name: what the console, the pages
// the server renders and the mail it sends say unless branding.title
// renames them.
const DefaultBrandTitle = "Slopscale"

// BrandingLogoPath is where the server serves the configured logo. It
// answers with the built-in mark while no logo is configured, so the path
// never 404s; a proxy or a fail2ban rule that watches for those must not
// see one because an operator removed a setting.
const BrandingLogoPath = "/branding/logo"

// maxLogoBytes caps the logo file. It is served on the sign-in page and on
// every rendered page, and it is held in memory for the life of the
// process, so a photograph dropped in by mistake is refused at startup
// rather than sent to every visitor.
const maxLogoBytes = 1 << 20

// logoTypes are the image formats a logo may be in, by file extension. A
// browser has to draw it in an <img>, and SVG there cannot run script, so
// the list is about what renders rather than about trust.
var logoTypes = map[string]string{
	".svg":  "image/svg+xml",
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
}

// Branding is the name and the mark an operator puts on what their users
// see. Both come from the config file: a logo is a file, like every other
// path the server reads, and the sign-in page needs them before the caller
// has any credential to read a setting with.
type Branding struct {
	// Title replaces "Slopscale" in the console, in the pages the server
	// renders and in the mail it sends.
	Title string
	// LogoPath is an image file served in place of the built-in mark.
	// Empty keeps the mark.
	LogoPath string

	// logo is the file, read once at startup. A render must not touch the
	// disk, and a file that goes away afterwards must not take the logo
	// with it.
	logo []byte
	// logoType is the Content-Type the bytes are served with.
	logoType string
	// logoVersion is a hash of the bytes, the query the logo URL carries
	// so a replaced logo is fetched again despite an immutable cache.
	logoVersion string
}

// NewBranding returns the branding for a title and a logo, hashing the
// bytes for the address the pages cache the logo under. An empty logo
// leaves the built-in mark in place.
func NewBranding(title, contentType string, logo []byte) Branding {
	b := Branding{Title: title}
	if len(logo) == 0 {
		return b
	}

	sum := sha256.Sum256(logo)
	b.logo = logo
	b.logoType = contentType
	b.logoVersion = hex.EncodeToString(sum[:])[:8]

	return b
}

// Custom reports whether the operator replaced the built-in mark.
func (b Branding) Custom() bool {
	return len(b.logo) > 0
}

// Logo returns the configured image and its Content-Type. The second
// return is false while no logo is configured.
func (b Branding) Logo() ([]byte, string, bool) {
	if !b.Custom() {
		return nil, "", false
	}

	return b.logo, b.logoType, true
}

// LogoURL is where a page points at the logo, with the version that lets
// it be cached forever. It is empty while no logo is configured, which is
// how the console knows to draw its own mark instead.
func (b Branding) LogoURL() string {
	if !b.Custom() {
		return ""
	}

	return BrandingLogoPath + "?v=" + b.logoVersion
}

// Errors returned while reading the branding section.
var (
	errBrandingTitleEmpty = errors.New("branding.title must not be empty")
	errLogoFormat         = errors.New(
		"branding.logo_path must be an .svg, .png, .jpg or .webp file",
	)
	errLogoEmpty = errors.New("branding.logo_path is an empty file")
	errLogoSize  = errors.New("branding.logo_path is too large")
)

func brandingConfig() (Branding, error) {
	b := Branding{
		Title:    strings.TrimSpace(conf.GetString("branding.title")),
		LogoPath: conf.GetString("branding.logo_path"),
	}

	if b.Title == "" {
		return Branding{}, errBrandingTitleEmpty
	}

	if b.LogoPath == "" {
		return b, nil
	}

	b.LogoPath = util.AbsolutePathFromConfigPath(b.LogoPath)

	contentType, ok := logoTypes[strings.ToLower(filepath.Ext(b.LogoPath))]
	if !ok {
		return Branding{}, fmt.Errorf("%w, not %q", errLogoFormat, b.LogoPath)
	}

	logo, err := os.ReadFile(b.LogoPath)
	if err != nil {
		return Branding{}, fmt.Errorf("reading branding.logo_path: %w", err)
	}

	switch {
	case len(logo) == 0:
		return Branding{}, fmt.Errorf("%w: %s", errLogoEmpty, b.LogoPath)
	case len(logo) > maxLogoBytes:
		return Branding{}, fmt.Errorf(
			"%w: %d bytes, the limit is %d", errLogoSize, len(logo), maxLogoBytes,
		)
	}

	branded := NewBranding(b.Title, contentType, logo)
	branded.LogoPath = b.LogoPath

	return branded, nil
}
