// Package clientversion tells clients whether a newer Tailscale client
// exists, the way the hosted control plane does through
// [tailcfg.MapResponse.ClientVersion]: the server reads the latest
// stable release from pkgs.tailscale.com and each client compares its
// own version against it and shows the "update available" health
// warning when it is behind.
package clientversion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"tailscale.com/tailcfg"
	"tailscale.com/util/cmpver"
)

// DefaultURL lists the stable track's packages; its Version field is the
// latest stable release.
const DefaultURL = "https://pkgs.tailscale.com/stable/?mode=json"

// maxBody bounds the package listing; it is a few kilobytes.
const maxBody = 1 << 20

// ErrNoVersion is returned when the listing names no version.
var ErrNoVersion = errors.New("package listing names no version")

// ErrUnexpectedStatus is returned for a listing the server refused.
var ErrUnexpectedStatus = errors.New("unexpected status fetching the package listing")

// Latest fetches the latest stable release version, "1.86.2", from url.
func Latest(ctx context.Context, client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("building the package listing request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetching the package listing: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: %s", ErrUnexpectedStatus, resp.Status)
	}

	var listing struct {
		Version string `json:"Version"`
	}

	err = json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(&listing)
	if err != nil {
		return "", fmt.Errorf("decoding the package listing: %w", err)
	}

	version := strings.TrimSpace(listing.Version)
	if version == "" {
		return "", ErrNoVersion
	}

	return version, nil
}

// Short strips the build suffix from a client version: "1.86.2-t1a2b-g3c4d"
// becomes "1.86.2".
func Short(version string) string {
	before, _, _ := strings.Cut(version, "-")

	return before
}

// Outdated reports whether a client running version is behind latest.
// Either being unknown means no, as does an unstable client ahead of the
// stable release.
func Outdated(version, latest string) bool {
	running := Short(version)
	if running == "" || latest == "" {
		return false
	}

	return cmpver.Less(running, latest)
}

// For is what a client running version is told: nil while the latest
// release is unknown, otherwise whether it runs the latest and, when
// not, which version to get.
func For(version, latest string) *tailcfg.ClientVersion {
	if latest == "" || Short(version) == "" {
		return nil
	}

	if Outdated(version, latest) {
		return &tailcfg.ClientVersion{LatestVersion: latest}
	}

	return &tailcfg.ClientVersion{RunningLatest: true}
}
