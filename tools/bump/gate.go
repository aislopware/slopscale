package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
)

// Gate levels, from cheapest to most thorough.
const (
	gateNone  = "none"
	gateQuick = "quick"
	gateFull  = "full"
)

var errUnknownGate = errors.New("unknown gate level (want none|quick|full)")

// flakeChecks are the same derivations nix-checks.yml and the formatting job
// build on a pull request. flake.nix defines them for Linux only.
var flakeChecks = []string{"build", "gotest", "golangci-lint", "formatting"}

// makeGates are the checks CI runs outside the sandbox, because they need the
// console's toolchain from web/bun.lock or the docs site's from docs/bun.lock.
var makeGates = []string{"lint-markup", "lint-web", "docs"}

// dockerGates stand in for the integration matrix. They are the two images
// whose builder pin actually breaks, plus the wasm client, which is cheap and
// exercises go.mod against the pinned builder.
var dockerGates = []struct {
	File   string
	Target string
}{
	{File: "Dockerfile.tailscale-HEAD", Target: "build-env"},
	{File: "Dockerfile.derper"},
	{File: "Dockerfile.wasmclient"},
}

// runFlakeChecks builds every flake check this system defines. On macOS there
// are none to build, and the Linux runner is the one that answers.
func runFlakeChecks(ctx context.Context, r *repo) error {
	if !strings.HasSuffix(r.System, "-linux") {
		log.Printf("gate: no Go flake checks on %s; leaving them to CI", r.System)

		return nil
	}

	for _, check := range flakeChecks {
		err := nixCheck(ctx, r, check)
		if err != nil {
			return err
		}
	}

	return nil
}

// finalGate judges the accumulated tree. The integration matrix is
// deliberately left to the pull request's own CI rather than duplicated here.
func finalGate(ctx context.Context, r *repo, level string) error {
	switch level {
	case gateNone:
		return nil
	case gateQuick:
		return nixCheck(ctx, r, "build")
	case gateFull:
	default:
		return fmt.Errorf("%w: %s", errUnknownGate, level)
	}

	err := runFlakeChecks(ctx, r)
	if err != nil {
		return err
	}

	for _, target := range makeGates {
		log.Printf("gate: make %s", target)

		_, err = r.nixRun(ctx, "make", target)
		if err != nil {
			return err
		}
	}

	for _, d := range dockerGates {
		argv := []string{"docker", "build", "--file", d.File}
		if d.Target != "" {
			argv = append(argv, "--target", d.Target)
		}

		log.Printf("gate: %s", d.File)

		_, err = r.run(ctx, append(argv, ".")...)
		if err != nil {
			return err
		}
	}

	return nil
}

func nixCheck(ctx context.Context, r *repo, name string) error {
	log.Printf("gate: nix check %s", name)

	_, err := r.run(ctx, "nix", "build", "--fallback", "-L", "--no-link",
		fmt.Sprintf(".#checks.%s.%s", r.System, name))

	return err
}

// enforceFinalGate drops committed areas newest-first until the tree passes.
// Popping from the tip is safe because every area is exactly one commit, and it
// is the cheapest correct answer: the gate cannot say which area broke, only
// that the combination did.
func enforceFinalGate(ctx context.Context, r *repo, results []result, level string) ([]result, error) {
	for {
		gateErr := finalGate(ctx, r, level)
		if gateErr == nil {
			return results, nil
		}

		newest := -1

		for i, res := range results {
			if res.Commit != "" {
				newest = i
			}
		}

		if newest < 0 {
			return results, fmt.Errorf("gate fails on an unmodified tree: %w", gateErr)
		}

		log.Printf("gate failed, dropping area %s", results[newest].Area)

		_, err := r.run(ctx, "git", "reset", "--hard", "HEAD~1")
		if err != nil {
			return results, err
		}

		results[newest].State = stateDropped
		results[newest].Reason = reasonOf(gateErr)
		results[newest].Log = logOf(gateErr)
		results[newest].Commit = ""
	}
}
