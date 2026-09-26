package main

import (
	"context"
	"fmt"
	"strings"
)

// oapiModule is the module the Makefile runs oapi-codegen from.
const oapiModule = "github.com/oapi-codegen/oapi-codegen/v2"

// applyOapiCodegen moves the generator pin in the Makefile. The generate area
// runs `make client`, so the clients are regenerated with whatever this lands
// on.
func applyOapiCodegen(ctx context.Context, r *repo) (change, error) {
	have, err := oapiVersion(r)
	if err != nil {
		return change{}, err
	}

	want, err := latestVersion(ctx, oapiModule, have)
	if err != nil {
		return change{}, err
	}

	if want == have {
		return change{Empty: true}, nil
	}

	content, err := r.readFile("Makefile")
	if err != nil {
		return change{}, err
	}

	// Both invocations carry the pin; leaving one behind would generate the
	// two clients with different tools.
	updated := strings.ReplaceAll(content,
		"oapi-codegen/v2/cmd/oapi-codegen@"+have,
		"oapi-codegen/v2/cmd/oapi-codegen@"+want)

	err = r.writeFile("Makefile", updated)
	if err != nil {
		return change{}, err
	}

	return change{
		Summary: fmt.Sprintf("oapi-codegen %s to %s", have, want),
		Detail:  []string{fmt.Sprintf("Makefile oapi-codegen %s -> %s", have, want)},
	}, nil
}

// gateOapiCodegen asserts no invocation kept the old pin.
func gateOapiCodegen(_ context.Context, r *repo) error {
	content, err := r.readFile("Makefile")
	if err != nil {
		return err
	}

	version, err := oapiVersion(r)
	if err != nil {
		return err
	}

	want := strings.Count(content, "oapi-codegen/v2/cmd/oapi-codegen@")

	got := strings.Count(content, "oapi-codegen/v2/cmd/oapi-codegen@"+version)
	if got != want {
		return fmt.Errorf("%w: %d of %d oapi-codegen invocations use %s",
			errNoMatchingTag, got, want, version)
	}

	return nil
}

// applyPrekHooks moves the revisions of the remote hook repositories. Every
// local hook runs a devShell tool, so the lock bump already moves those.
func applyPrekHooks(ctx context.Context, r *repo) (change, error) {
	_, err := r.nixRun(ctx, "prek", "autoupdate")
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

	diff, err := r.run(ctx, "git", "diff", "--unified=0", "--", ".pre-commit-config.yaml")
	if err != nil {
		return change{}, err
	}

	var revs []string

	for line := range strings.Lines(diff) {
		if rev, ok := strings.CutPrefix(line, "+    rev: "); ok {
			revs = append(revs, strings.TrimSpace(rev))
		}
	}

	return change{
		Summary: "prek hook revisions to " + strings.Join(revs, ", "),
		Detail:  revs,
	}, nil
}

func toolAreas() []area {
	return []area{
		{
			Name:    "tools:oapi-codegen",
			Apply:   applyOapiCodegen,
			Gate:    gateOapiCodegen,
			Message: func(c change) string { return "build: bump " + c.Summary },
		},
		{
			Name:    "tools:prek",
			Apply:   applyPrekHooks,
			Message: func(c change) string { return "build(prek): bump " + c.Summary },
		},
	}
}
