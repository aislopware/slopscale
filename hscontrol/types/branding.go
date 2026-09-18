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

// Where the server serves the configured logos. Both answer with the next
// best image rather than 404 -- the dark path falls back to the light logo
// and the light path to the built-in mark -- so a proxy or a fail2ban rule
// that watches for 404s never sees one because an operator left a setting
// out.
const (
	BrandingLogoPath     = "/branding/logo"
	BrandingDarkLogoPath = "/branding/logo-dark"
)

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

// logoFile is an image read from disk at startup, with the hash that lets
// a page cache it forever and still pick up a replacement.
type logoFile struct {
	bytes       []byte
	contentType string
	version     string
}

func newLogoFile(contentType string, image []byte) logoFile {
	if len(image) == 0 {
		return logoFile{}
	}

	sum := sha256.Sum256(image)

	return logoFile{
		bytes:       image,
		contentType: contentType,
		version:     hex.EncodeToString(sum[:])[:8],
	}
}

func (l logoFile) set() bool { return len(l.bytes) > 0 }

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
	// DarkLogoPath is the image to draw against a dark background. Empty
	// means the one logo serves both, which is right for a mark that
	// already carries its own background.
	DarkLogoPath string

	// The files, read once at startup. A render must not touch the disk,
	// and a file that goes away afterwards must not take the logo with it.
	light logoFile
	dark  logoFile
}

// NewBranding returns the branding for a title and a logo, hashing the
// bytes for the address the pages cache the logo under. An empty logo
// leaves the built-in mark in place.
func NewBranding(title, contentType string, logo []byte) Branding {
	return Branding{Title: title, light: newLogoFile(contentType, logo)}
}

// WithDarkLogo returns the branding with a second logo for dark
// backgrounds. It is ignored while no light logo is configured, since
// there is nothing for it to be the variant of.
func (b Branding) WithDarkLogo(contentType string, logo []byte) Branding {
	if !b.light.set() {
		return b
	}

	b.dark = newLogoFile(contentType, logo)

	return b
}

// Custom reports whether the operator replaced the built-in mark.
func (b Branding) Custom() bool {
	return b.light.set()
}

// Logo returns the configured image and its Content-Type. Asking for the
// dark one falls back to the light one, which is the whole logo for an
// operator who supplied a single mark. The second return is false while no
// logo is configured.
func (b Branding) Logo(dark bool) ([]byte, string, bool) {
	file := b.light
	if dark && b.dark.set() {
		file = b.dark
	}

	if !file.set() {
		return nil, "", false
	}

	return file.bytes, file.contentType, true
}

// LogoURL is where a page points at the logo, with the version that lets
// it be cached forever. It is empty while no logo is configured, which is
// how the console knows to draw its own mark instead.
func (b Branding) LogoURL() string {
	if !b.light.set() {
		return ""
	}

	return BrandingLogoPath + "?v=" + b.light.version
}

// DarkLogoURL is the address to draw against a dark background. It is the
// light logo's own URL when no dark variant is configured, so a caller
// picks by theme without asking whether there are one or two images.
func (b Branding) DarkLogoURL() string {
	if !b.dark.set() {
		return b.LogoURL()
	}

	return BrandingDarkLogoPath + "?v=" + b.dark.version
}

// Errors returned while reading the branding section.
var (
	errBrandingTitleEmpty = errors.New("branding.title must not be empty")
	errLogoFormat         = errors.New(
		"must be an .svg, .png, .jpg or .webp file",
	)
	errLogoEmpty   = errors.New("is an empty file")
	errLogoSize    = errors.New("is too large")
	errDarkNoLight = errors.New(
		"branding.logo_dark_path needs branding.logo_path: it is the variant of that logo, not a logo of its own",
	)
)

// readLogo reads one configured image, reporting the key it came from so a
// refusal names the setting the operator has to fix.
func readLogo(key, path string) (logoFile, string, error) {
	path = util.AbsolutePathFromConfigPath(path)

	contentType, ok := logoTypes[strings.ToLower(filepath.Ext(path))]
	if !ok {
		return logoFile{}, path, fmt.Errorf("%s %w, not %q", key, errLogoFormat, path)
	}

	image, err := os.ReadFile(path)
	if err != nil {
		return logoFile{}, path, fmt.Errorf("reading %s: %w", key, err)
	}

	switch {
	case len(image) == 0:
		return logoFile{}, path, fmt.Errorf("%s %w: %s", key, errLogoEmpty, path)
	case len(image) > maxLogoBytes:
		return logoFile{}, path, fmt.Errorf(
			"%s %w: %d bytes, the limit is %d", key, errLogoSize, len(image), maxLogoBytes,
		)
	}

	return newLogoFile(contentType, image), path, nil
}

func brandingConfig() (Branding, error) {
	b := Branding{
		Title:        strings.TrimSpace(conf.GetString("branding.title")),
		LogoPath:     conf.GetString("branding.logo_path"),
		DarkLogoPath: conf.GetString("branding.logo_dark_path"),
	}

	if b.Title == "" {
		return Branding{}, errBrandingTitleEmpty
	}

	if b.LogoPath == "" {
		if b.DarkLogoPath != "" {
			return Branding{}, errDarkNoLight
		}

		return b, nil
	}

	light, path, err := readLogo("branding.logo_path", b.LogoPath)
	if err != nil {
		return Branding{}, err
	}

	b.light, b.LogoPath = light, path

	if b.DarkLogoPath == "" {
		return b, nil
	}

	dark, darkPath, err := readLogo("branding.logo_dark_path", b.DarkLogoPath)
	if err != nil {
		return Branding{}, err
	}

	b.dark, b.DarkLogoPath = dark, darkPath

	return b, nil
}
