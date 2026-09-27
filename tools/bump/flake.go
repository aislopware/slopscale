package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// lockNode is the slice of a flake.lock entry that identifies what an input is
// pinned to.
type lockNode struct {
	Locked struct {
		Rev          string `json:"rev"`
		LastModified int64  `json:"lastModified"`
	} `json:"locked"`
}

type flakeLock struct {
	Nodes map[string]lockNode `json:"nodes"`
}

func readLock(r *repo) (flakeLock, error) {
	var lock flakeLock

	b, err := r.readFile("flake.lock")
	if err != nil {
		return lock, err
	}

	err = json.Unmarshal([]byte(b), &lock)
	if err != nil {
		return lock, fmt.Errorf("parsing flake.lock: %w", err)
	}

	return lock, nil
}

// lockDiff reports the inputs whose revision moved: one detailed line each,
// plus the bare names for the commit subject.
func lockDiff(before, after flakeLock) ([]string, []string) {
	var detail, names []string

	for name, post := range after.Nodes {
		pre, ok := before.Nodes[name]
		if !ok || post.Locked.Rev == "" || pre.Locked.Rev == post.Locked.Rev {
			continue
		}

		detail = append(detail, fmt.Sprintf("%s %s -> %s", name, short(pre.Locked.Rev), short(post.Locked.Rev)))
		names = append(names, name)
	}

	slices.Sort(detail)
	slices.Sort(names)

	return detail, names
}

func short(rev string) string {
	const shortLen = 7

	if len(rev) > shortLen {
		return rev[:shortLen]
	}

	return rev
}

// applyFlake refreshes every flake input. Because flake.nix asks for
// buildGoLatestModule and go_latest, this is also how Go and every devShell
// tool but bun get a new version.
func applyFlake(ctx context.Context, r *repo) (change, error) {
	before, err := readLock(r)
	if err != nil {
		return change{}, err
	}

	_, err = r.run(ctx, "nix", "flake", "update")
	if err != nil {
		return change{}, err
	}

	after, err := readLock(r)
	if err != nil {
		return change{}, err
	}

	moved, names := lockDiff(before, after)
	if len(moved) == 0 {
		return change{Empty: true}, nil
	}

	// The lock decides which gofumpt, golines and nixfmt the tree is
	// judged by, so moving it can leave files no bump touched failing the
	// formatting check. Reformatting belongs in this commit rather than a later
	// one: the gate below judges this tree, and the cause is this change.
	err = runFormatters(ctx, r)
	if err != nil {
		return change{}, err
	}

	return change{
		Summary: shortList(names),
		Detail:  append(moved, toolVersions(ctx, r)...),
	}, nil
}

// shortList keeps a commit subject readable when many inputs move at once.
func shortList(names []string) string {
	const maxNamed = 3

	if len(names) <= maxNamed {
		return strings.Join(names, ", ")
	}

	return fmt.Sprintf("%s and %d more", strings.Join(names[:maxNamed], ", "), len(names)-maxNamed)
}

// gateFlake proves the flake still evaluates before anything expensive runs.
func gateFlake(ctx context.Context, r *repo) error {
	_, err := r.run(ctx, "nix", "eval", "--raw", fmt.Sprintf(".#packages.%s.slopscale.name", r.System))
	if err != nil {
		return err
	}

	// The lock bump is judged against the same checks the final gate runs,
	// because it is the one change that can invalidate all of them at once: a
	// new nixpkgs moves every formatter and linter the checks are built from.
	// It is also the first commit, which is the worst case for a rewind that
	// drops the newest commit first. Paying for one round of checks here is
	// what stops the final gate paying for one round per area stacked above it.
	err = runFlakeChecks(ctx, r)
	if err != nil {
		return err
	}

	// bun comes from nixpkgs, and a bun too old for a lockfile cannot parse it.
	for _, set := range packageSets {
		_, err = r.nixRunIn(ctx, set.Dir, "bun", "install", "--frozen-lockfile")
		if err != nil {
			return err
		}
	}

	return nil
}

// goVersion is the Go the devShell provides, without the "go" prefix.
//
// GOTOOLCHAIN=local is not optional here. Left to itself the go command
// switches to whatever go.mod asks for and reports that instead, so a go.mod
// that has outrun nixpkgs looks like a nixpkgs that has caught up, and the one
// check that would have noticed compares a value against itself.
func goVersion(ctx context.Context, r *repo) (string, error) {
	out, err := r.nixRun(ctx, "env", "GOTOOLCHAIN=local", "go", "env", "GOVERSION")
	if err != nil {
		return "", err
	}

	return strings.TrimPrefix(strings.TrimSpace(out), "go"), nil
}

// toolVersions reports the versions that most often decide whether a lock bump
// turns the pull request red. A tool that cannot report is left out rather
// than failing the area over a report line.
func toolVersions(ctx context.Context, r *repo) []string {
	var versions []string

	goVer, err := goVersion(ctx, r)
	if err == nil {
		versions = append(versions, "go "+goVer)
	}

	out, err := r.nixRun(ctx, "golangci-lint", "version")
	if err == nil {
		first, _, _ := strings.Cut(out, "\n")
		versions = append(versions, strings.TrimSpace(first))
	}

	return versions
}
