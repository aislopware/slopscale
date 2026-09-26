package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/mod/semver"
)

// flake.nix overrides nixpkgs' bun, because web/bun.lock is written by a newer
// bun than nixpkgs ships and an older one cannot parse it. The override is a
// version and one release asset hash per system, so the lock bump never moves
// it and nothing else will.
var (
	bunVersionRef = regexp.MustCompile(`(version = ")(\d+\.\d+\.\d+)(";\s*\n\s*__intentionallyOverridingVersion)`)
	bunAssetRef   = regexp.MustCompile(`bun-v\$\{finalAttrs\.version\}/(bun-[\w-]+)\.zip";\s*\n\s*hash = "([^"]+)"`)
)

const bunDownload = "https://github.com/oven-sh/bun/releases/download/bun-v%s/%s.zip"

var (
	errNoBunPin     = errors.New("no bun override found in flake.nix")
	errBunMismatch  = errors.New("the devShell does not run the pinned bun")
	errNoPrefetched = errors.New("nix store prefetch-file reported no hash")
)

// pinnedBun is the bun version flake.nix overrides nixpkgs with.
func pinnedBun(flake string) (string, error) {
	m := bunVersionRef.FindStringSubmatch(flake)
	if m == nil {
		return "", errNoBunPin
	}

	return m[2], nil
}

// bunAssets lists the release assets flake.nix downloads, one per system.
func bunAssets(flake string) []string {
	matches := bunAssetRef.FindAllStringSubmatch(flake, -1)
	assets := make([]string, 0, len(matches))

	for _, m := range matches {
		assets = append(assets, m[1])
	}

	return assets
}

// setBunVersion rewrites the override's version.
func setBunVersion(flake, version string) string {
	return bunVersionRef.ReplaceAllString(flake, "${1}"+version+"${3}")
}

// setBunHash rewrites the hash of one release asset.
func setBunHash(flake, asset, hash string) string {
	re := regexp.MustCompile(`(bun-v\$\{finalAttrs\.version\}/` + regexp.QuoteMeta(asset) +
		`\.zip";\s*\n\s*hash = ")[^"]+(")`)

	return re.ReplaceAllString(flake, "${1}"+hash+"${2}")
}

// latestBun is the newest bun release, without the "bun-v" tag prefix.
func latestBun(ctx context.Context) (string, error) {
	tag, err := latestRelease(ctx, "oven-sh", "bun")
	if err != nil {
		return "", err
	}

	return strings.TrimPrefix(tag, "bun-v"), nil
}

// prefetchHash downloads url into the store and returns its SRI hash, which is
// what fetchurl checks the download against.
func prefetchHash(ctx context.Context, r *repo, url string) (string, error) {
	out, err := r.run(ctx, "nix", "store", "prefetch-file", "--json", url)
	if err != nil {
		return "", err
	}

	var res struct {
		Hash string `json:"hash"`
	}

	err = json.Unmarshal([]byte(out), &res)
	if err != nil {
		return "", fmt.Errorf("decoding prefetch of %s: %w", url, err)
	}

	if res.Hash == "" {
		return "", fmt.Errorf("%w: %s", errNoPrefetched, url)
	}

	return res.Hash, nil
}

// applyBun moves the override to the newest bun release, hashing every asset
// before writing anything, then lets the new bun rewrite the lockfiles it
// owns. Without a package change bun install keeps every resolved version, so
// the only thing that can move there is the lockfile format.
func applyBun(ctx context.Context, r *repo) (change, error) {
	flake, err := r.readFile("flake.nix")
	if err != nil {
		return change{}, err
	}

	have, err := pinnedBun(flake)
	if err != nil {
		return change{}, err
	}

	want, err := latestBun(ctx)
	if err != nil {
		return change{}, err
	}

	if !semver.IsValid("v"+want) || semver.Compare("v"+want, "v"+have) <= 0 {
		return change{Empty: true}, nil
	}

	for _, asset := range bunAssets(flake) {
		hash, hashErr := prefetchHash(ctx, r, fmt.Sprintf(bunDownload, want, asset))
		if hashErr != nil {
			return change{}, hashErr
		}

		flake = setBunHash(flake, asset, hash)
	}

	err = r.writeFile("flake.nix", setBunVersion(flake, want))
	if err != nil {
		return change{}, err
	}

	for _, set := range packageSets {
		_, err = r.nixRunIn(ctx, set.Dir, "bun", "install")
		if err != nil {
			return change{}, err
		}
	}

	return change{
		Summary: fmt.Sprintf("bun %s to %s", have, want),
		Detail:  []string{fmt.Sprintf("flake.nix bun %s -> %s", have, want)},
	}, nil
}

// gateBun proves the devShell runs the version just pinned, which is what a
// hash that does not match its asset would break, and that each lockfile
// installs frozen under it.
func gateBun(ctx context.Context, r *repo) error {
	flake, err := r.readFile("flake.nix")
	if err != nil {
		return err
	}

	want, err := pinnedBun(flake)
	if err != nil {
		return err
	}

	out, err := r.nixRun(ctx, "bun", "--version")
	if err != nil {
		return err
	}

	if got := strings.TrimSpace(out); got != want {
		return fmt.Errorf("%w: flake.nix pins %s, bun --version says %s", errBunMismatch, want, got)
	}

	for _, set := range packageSets {
		_, err = r.nixRunIn(ctx, set.Dir, "bun", "install", "--frozen-lockfile")
		if err != nil {
			return err
		}
	}

	return nil
}
