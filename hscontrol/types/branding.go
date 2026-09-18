package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // a social card may be a JPEG; registered for image.DecodeConfig
	_ "image/png"  // and a PNG
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

// DefaultBrandDescription is the one line a chat or a feed shows under a
// link to this server, unless branding.description replaces it.
const DefaultBrandDescription = "Self-hosted Tailscale control server with a built-in admin console"

// Where the server serves the configured images. All three answer with the
// next best image rather than 404 -- the dark path falls back to the light
// logo, the light path to the built-in mark and the social path to the
// built-in card -- so a proxy or a fail2ban rule that watches for 404s
// never sees one because an operator left a setting out.
const (
	BrandingLogoPath     = "/branding/logo"
	BrandingDarkLogoPath = "/branding/logo-dark"
	BrandingSocialPath   = "/branding/social"
)

// maxLogoBytes caps the logo file. It is served on the sign-in page and on
// every rendered page, and it is held in memory for the life of the
// process, so a photograph dropped in by mistake is refused at startup
// rather than sent to every visitor.
const maxLogoBytes = 1 << 20

// maxSocialBytes caps the social card. It is larger than the logo's limit
// because the card is a full 1200x630 picture rather than a mark, and it is
// only ever fetched by a crawler unfurling a link.
const maxSocialBytes = 4 << 20

// socialTypes are the formats a social card may be in. Every unfurler
// handles PNG and JPEG; SVG and WebP are refused often enough that
// accepting them would mostly produce links that unfurl without a picture.
var socialTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
}

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

func newLogoFile(contentType string, raw []byte) logoFile {
	if len(raw) == 0 {
		return logoFile{}
	}

	sum := sha256.Sum256(raw)

	return logoFile{
		bytes:       raw,
		contentType: contentType,
		version:     hex.EncodeToString(sum[:])[:8],
	}
}

func (l logoFile) set() bool { return len(l.bytes) > 0 }

// SocialCard describes the picture a chat or a feed draws for a link to
// this server. The size is read from the file rather than assumed, because
// an unfurler that is told the wrong one crops the picture to fit it.
type SocialCard struct {
	// URL is where the card is served, relative to the server's address.
	URL         string
	ContentType string
	Width       int
	Height      int
	// Alt is what a screen reader says in place of the picture.
	Alt string
}

// Branding is the name and the mark an operator puts on what their users
// see. Both come from the config file: a logo is a file, like every other
// path the server reads, and the sign-in page needs them before the caller
// has any credential to read a setting with.
type Branding struct {
	// Title replaces "Slopscale" in the console, in the pages the server
	// renders and in the mail it sends.
	Title string
	// Description is the line under a link to this server in a chat or a
	// feed, and the page description a search engine reads.
	Description string
	// LogoPath is an image file served in place of the built-in mark.
	// Empty keeps the mark.
	LogoPath string
	// DarkLogoPath is the image to draw against a dark background. Empty
	// means the one logo serves both, which is right for a mark that
	// already carries its own background.
	DarkLogoPath string
	// SocialImagePath is the picture a shared link unfurls with, in place
	// of the built-in card. 1200x630 is what every unfurler expects.
	SocialImagePath string

	// The files, read once at startup. A render must not touch the disk,
	// and a file that goes away afterwards must not take the logo with it.
	light  logoFile
	dark   logoFile
	social logoFile
	// The social card's pixels, decoded from its header at startup.
	socialWidth  int
	socialHeight int
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

// WithSocialImage returns the branding with the picture a shared link
// unfurls with, at the size decoded from the image itself.
func (b Branding) WithSocialImage(contentType string, card []byte, width, height int) Branding {
	b.social = newLogoFile(contentType, card)
	b.socialWidth, b.socialHeight = width, height

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

// SocialImage returns the configured card and its Content-Type. The second
// return is false while none is configured, which is how the handler knows
// to serve the built-in one.
func (b Branding) SocialImage() ([]byte, string, bool) {
	if !b.social.set() {
		return nil, "", false
	}

	return b.social.bytes, b.social.contentType, true
}

// Card is the operator's own social card, with the address the pages name
// it by. The second return is false while none is configured.
func (b Branding) Card() (SocialCard, bool) {
	if !b.social.set() {
		return SocialCard{}, false
	}

	return SocialCard{
		URL:         BrandingSocialPath + "?v=" + b.social.version,
		ContentType: b.social.contentType,
		Width:       b.socialWidth,
		Height:      b.socialHeight,
		Alt:         b.Title,
	}, true
}

// Errors returned while reading the branding section.
var (
	errBrandingTitleEmpty       = errors.New("branding.title must not be empty")
	errBrandingDescriptionEmpty = errors.New("branding.description must not be empty")
	errLogoFormat               = errors.New(
		"must be an .svg, .png, .jpg or .webp file",
	)
	errSocialFormat = errors.New(
		"must be a .png or .jpg file: an unfurler draws nothing else",
	)
	errSocialDecode = errors.New("is not a readable image")
	errLogoEmpty    = errors.New("is an empty file")
	errLogoSize     = errors.New("is too large")
	errDarkNoLight  = errors.New(
		"branding.logo_dark_path needs branding.logo_path: it is the variant of that logo, not a logo of its own",
	)
)

// imageSpec is what one configured image setting accepts: the formats, by
// file extension, and the size beyond which the file is refused.
type imageSpec struct {
	types     map[string]string
	max       int
	formatErr error
}

var (
	logoSpec   = imageSpec{types: logoTypes, max: maxLogoBytes, formatErr: errLogoFormat}
	socialSpec = imageSpec{types: socialTypes, max: maxSocialBytes, formatErr: errSocialFormat}
)

// readImage reads one configured image, reporting the key it came from so a
// refusal names the setting the operator has to fix.
func readImage(key, path string, spec imageSpec) (logoFile, string, error) {
	path = util.AbsolutePathFromConfigPath(path)

	contentType, ok := spec.types[strings.ToLower(filepath.Ext(path))]
	if !ok {
		return logoFile{}, path, fmt.Errorf("%s %w, not %q", key, spec.formatErr, path)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return logoFile{}, path, fmt.Errorf("reading %s: %w", key, err)
	}

	switch {
	case len(raw) == 0:
		return logoFile{}, path, fmt.Errorf("%s %w: %s", key, errLogoEmpty, path)
	case len(raw) > spec.max:
		return logoFile{}, path, fmt.Errorf(
			"%s %w: %d bytes, the limit is %d", key, errLogoSize, len(raw), spec.max,
		)
	}

	return newLogoFile(contentType, raw), path, nil
}

// readLogo reads a configured logo.
func readLogo(key, path string) (logoFile, string, error) {
	return readImage(key, path, logoSpec)
}

// socialImage reads the configured social card and the size an unfurler has
// to be told. The size comes from the file's own header rather than from
// the operator, and decoding it also catches a picture whose extension lies
// about what it is.
func socialImage(key, path string) (logoFile, string, int, int, error) {
	file, abs, err := readImage(key, path, socialSpec)
	if err != nil {
		return logoFile{}, abs, 0, 0, err
	}

	cfg, format, err := image.DecodeConfig(bytes.NewReader(file.bytes))
	if err != nil {
		return logoFile{}, abs, 0, 0, fmt.Errorf("%s %w: %s: %w", key, errSocialDecode, abs, err)
	}

	// The decoder read the format out of the bytes, so a JPEG named .png
	// is served as one rather than as whatever the extension claimed.
	file.contentType = "image/" + format

	return file, abs, cfg.Width, cfg.Height, nil
}

func brandingConfig() (Branding, error) {
	b := Branding{
		Title:           strings.TrimSpace(conf.GetString("branding.title")),
		Description:     strings.TrimSpace(conf.GetString("branding.description")),
		LogoPath:        conf.GetString("branding.logo_path"),
		DarkLogoPath:    conf.GetString("branding.logo_dark_path"),
		SocialImagePath: conf.GetString("branding.social_image_path"),
	}

	switch {
	case b.Title == "":
		return Branding{}, errBrandingTitleEmpty
	case b.Description == "":
		return Branding{}, errBrandingDescriptionEmpty
	}

	b, err := b.readLogos()
	if err != nil {
		return Branding{}, err
	}

	if b.SocialImagePath == "" {
		return b, nil
	}

	social, path, width, height, err := socialImage("branding.social_image_path", b.SocialImagePath)
	if err != nil {
		return Branding{}, err
	}

	b.social, b.SocialImagePath = social, path
	b.socialWidth, b.socialHeight = width, height

	return b, nil
}

// readLogos fills in the light and dark marks named by the config file.
func (b Branding) readLogos() (Branding, error) {
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
