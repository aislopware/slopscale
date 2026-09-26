package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// generatedFiles and generatedDirs are everything the generators are allowed
// to touch. Anything outside them means a generator reached further than
// expected, and the area is dropped rather than committed.
var (
	generatedFiles = map[string]bool{
		"hscontrol/types/types_clone.go":          true,
		"hscontrol/types/types_view.go":           true,
		"hscontrol/capver/capver_generated.go":    true,
		"hscontrol/capver/capver_test_data.go":    true,
		"web/src/api/schema.gen.ts":               true,
		"web/src/lib/hujson/parser.gen.ts":        true,
		".github/workflows/test-integration.yaml": true,
	}
	generatedDirs = []string{"gen/"}
)

// oapiPin finds the oapi-codegen version the Makefile pins.
var oapiPin = regexp.MustCompile(`github\.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@(v[\w.\-]+)`)

var (
	errNoOapiPin  = errors.New("no oapi-codegen pin found in Makefile")
	errStrayWrite = errors.New("generator wrote outside the generated set")
)

func oapiVersion(r *repo) (string, error) {
	mk, err := r.readFile("Makefile")
	if err != nil {
		return "", err
	}

	m := oapiPin.FindStringSubmatch(mk)
	if m == nil {
		return "", errNoOapiPin
	}

	return m[1], nil
}

func isGenerated(path string) bool {
	if generatedFiles[path] {
		return true
	}

	for _, dir := range generatedDirs {
		if strings.HasPrefix(path, dir) {
			return true
		}
	}

	return false
}

// applyGenerate refreshes every checked-in generated file through the same
// targets a developer runs, so the generated check on the pull request judges
// exactly what was produced here. Most of the churn is not caused by this
// repository at all: the capability-version table is scraped from tailscale's
// published tags, so it goes stale on an untouched tree the moment upstream
// ships a release.
func applyGenerate(ctx context.Context, r *repo) (change, error) {
	_, err := r.nixRun(ctx, "make", "generate")
	if err != nil {
		return change{}, err
	}

	// go generate ./... skips dot-directories, so the integration matrix
	// generator has to be invoked from inside .github/workflows.
	_, err = r.nixRunIn(ctx, ".github/workflows", "go", "generate")
	if err != nil {
		return change{}, err
	}

	touched, err := changedFiles(ctx, r)
	if err != nil {
		return change{}, err
	}

	if len(touched) == 0 {
		return change{Empty: true}, nil
	}

	return change{
		Summary: strings.Join(touched, ", "),
		Detail:  touched,
	}, nil
}

// gateGenerate refuses a generator run that reached outside the known set.
func gateGenerate(ctx context.Context, r *repo) error {
	touched, err := changedFiles(ctx, r)
	if err != nil {
		return err
	}

	for _, f := range touched {
		if !isGenerated(f) {
			return fmt.Errorf("%w: %s", errStrayWrite, f)
		}
	}

	return nil
}

// applyFormat re-runs everything that decides whether the tree is acceptable to
// CI, and commits whatever it rewrites. Two reasons it cannot be skipped:
// nixpkgs and the console's lockfile pick which gofumpt, golines, oxfmt and
// nixpkgs-fmt the tree is formatted with, so a bump reformats files no area
// touched; and the bot rewrites workflow YAML and Dockerfiles, where a stray
// trailing space or an unformatted file fails a hook rather than the compiler.
func applyFormat(ctx context.Context, r *repo) (change, error) {
	err := runFormatters(ctx, r)
	if err != nil {
		return change{}, err
	}

	touched, err := changedFiles(ctx, r)
	if err != nil {
		return change{}, err
	}

	if len(touched) == 0 {
		return change{Empty: true}, nil
	}

	return change{
		Summary: fmt.Sprintf("%d file(s) reformatted", len(touched)),
		Detail:  touched,
	}, nil
}

// runFormatters brings the tree up to whatever the current toolchain considers
// formatted. The first hook pass is expected to fail, because a hook that
// rewrites a file reports failure; the second pass is the verdict.
func runFormatters(ctx context.Context, r *repo) error {
	_, err := r.nixRun(ctx, "make", "fmt")
	if err != nil {
		return err
	}

	_, _ = r.nixRun(ctx, "prek", "run", "--all-files")

	_, err = r.nixRun(ctx, "prek", "run", "--all-files")

	return err
}
