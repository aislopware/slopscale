package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// unstableURL lists the packages on Tailscale's unstable track. It is a var
// only so the tests can point it at a fake server.
var unstableURL = "https://pkgs.tailscale.com/unstable/?mode=json"

// unstableCommitsPerPage is the page size for walking main from the start of a
// release cycle; GitHub's compare endpoint allows up to 250.
const unstableCommitsPerPage = 100

// versionParts is major.minor.patch.
const versionParts = 3

var (
	errNotUnstable    = errors.New("not an unstable tailscale version")
	errUnstableCommit = errors.New("cannot place the unstable release on main")
)

// latestUnstable is the version of Tailscale's newest unstable release, the one
// its tarballs and containers carry, such as "1.105.59". Some platforms (the
// Windows installers, Android) are published on their own cadence and carry
// other numbers; the integration tests' "unstable" client is the container.
func latestUnstable(ctx context.Context) (string, error) {
	body, err := fetch(ctx, unstableURL)
	if err != nil {
		return "", err
	}

	var doc struct {
		Version string `json:"Version"`
	}

	err = json.Unmarshal(body, &doc)
	if err != nil {
		return "", fmt.Errorf("decoding %s: %w", unstableURL, err)
	}

	if doc.Version == "" {
		return "", fmt.Errorf("%w: %s names no version", errNotUnstable, unstableURL)
	}

	return doc.Version, nil
}

// parseUnstable splits an unstable version into its cycle's opening tag and
// its patch number. Unstable releases have an odd minor version.
func parseUnstable(version string) (string, int, error) {
	parts := strings.Split(version, ".")
	if len(parts) != versionParts {
		return "", 0, fmt.Errorf("%w: %q", errNotUnstable, version)
	}

	nums := make([]int, len(parts))

	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return "", 0, fmt.Errorf("%w: %q", errNotUnstable, version)
		}

		nums[i] = n
	}

	if nums[1]%2 == 0 || nums[2] < 1 {
		return "", 0, fmt.Errorf("%w: %q", errNotUnstable, version)
	}

	return fmt.Sprintf("v%d.%d.0-pre", nums[0], nums[1]), nums[2], nil
}

// unstableCommit finds the commit an unstable release was built from.
//
// Tailscale's version/mkversion numbers an unstable build by counting the
// commits since VERSION.txt last changed, and VERSION.txt changes at the
// vX.Y.0-pre tag that opens each cycle, so 1.105.59 is the 59th commit on main
// after v1.105.0-pre. The count is checked back against GitHub, so a change to
// that scheme fails here instead of pinning the wrong commit.
func unstableCommit(ctx context.Context, version string) (string, error) {
	base, patch, err := parseUnstable(version)
	if err != nil {
		return "", err
	}

	var walk struct {
		Commits []struct {
			SHA string `json:"sha"`
		} `json:"commits"`
	}

	page := (patch-1)/unstableCommitsPerPage + 1

	err = githubJSON(ctx, fmt.Sprintf("/repos/tailscale/tailscale/compare/%s...main?per_page=%d&page=%d",
		base, unstableCommitsPerPage, page), &walk)
	if err != nil {
		return "", err
	}

	i := (patch - 1) % unstableCommitsPerPage
	if i >= len(walk.Commits) {
		return "", fmt.Errorf("%w: main has fewer than %d commits after %s", errUnstableCommit, patch, base)
	}

	sha := walk.Commits[i].SHA

	var check struct {
		AheadBy  int `json:"ahead_by"`
		BehindBy int `json:"behind_by"`
	}

	err = githubJSON(ctx, fmt.Sprintf("/repos/tailscale/tailscale/compare/%s...%s", base, sha), &check)
	if err != nil {
		return "", err
	}

	if check.AheadBy != patch || check.BehindBy != 0 {
		return "", fmt.Errorf("%w: %s is %d commits after %s (and %d behind), want %d",
			errUnstableCommit, sha, check.AheadBy, base, check.BehindBy, patch)
	}

	return sha, nil
}

// unstablePin is the pseudo-version of tailscale.com at the commit of
// Tailscale's newest unstable release, and that release's version.
func unstablePin(ctx context.Context) (string, string, error) {
	release, err := latestUnstable(ctx)
	if err != nil {
		return "", "", err
	}

	sha, err := unstableCommit(ctx, release)
	if err != nil {
		return "", "", err
	}

	info, err := fetchInfo(ctx, modTailscale, "@v/"+sha+".info")
	if err != nil {
		return "", "", err
	}

	return info.Version, release, nil
}
