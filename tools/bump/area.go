package main

import (
	"context"
	"log"
)

type state string

const (
	stateApplied state = "applied"
	statePartial state = "partial"
	stateEmpty   state = "unchanged"
	stateDropped state = "dropped"
	stateSkipped state = "skipped"
)

// change is what an area did, in a shape the report can render directly.
type change struct {
	Summary string
	Detail  []string
	// Drops are sub-units the area rewound on its own, such as a single
	// dependency that failed to build.
	Drops []dropped
	Empty bool
}

// area is one independently revertible unit of work. Areas are the granularity
// at which a run succeeds or fails: each becomes its own commit, so a broken
// one can be dropped without disturbing the others.
type area struct {
	Name  string
	Needs []string
	// Message renders the commit subject as a Conventional Commit.
	Message func(change) string
	Apply   func(ctx context.Context, r *repo) (change, error)
	// Gate is a structural check, or the narrowest build that proves the area
	// on its own. The whole tree is judged by the final gate.
	Gate func(ctx context.Context, r *repo) error
}

type result struct {
	Area   string
	State  state
	Change change
	Reason string
	Log    string
	Commit string
}

// allAreas is the running order. Regeneration comes last because it consumes
// what everything before it settled: the toolchain from the lock, the module
// versions, the console's packages and the generator pin from the Makefile.
func allAreas() []area {
	areas := []area{
		{
			Name:    "flake",
			Apply:   applyFlake,
			Gate:    gateFlake,
			Message: func(c change) string { return "build(nix): update flake inputs " + c.Summary },
		},
	}

	areas = append(areas, toolAreas()...)
	areas = append(areas,
		area{
			Name: "gomod",
			// No Needs on flake. Position in this slice is what puts it after
			// the lock bump; making it a dependency meant an unadoptable
			// nixpkgs also threw away the day's dependency upgrades, which
			// have nothing to do with it.
			Apply:   applyGoMod,
			Gate:    gateGoMod,
			Message: func(c change) string { return "deps: " + c.Summary },
		},
		area{
			Name:    "docker-go",
			Apply:   applyDockerGo,
			Gate:    gateDockerGo,
			Message: func(c change) string { return "build(docker): bump " + c.Summary },
		},
	)
	areas = append(areas, imageAreas()...)
	areas = append(areas, packageAreas()...)
	areas = append(areas, actionAreas()...)

	areas = append(areas, area{
		Name:    "generate",
		Needs:   []string{"gomod", "tools:oapi-codegen"},
		Apply:   applyGenerate,
		Gate:    gateGenerate,
		Message: func(change) string { return "chore(gen): regenerate generated files" },
	})

	// Last, so it also covers whatever the generators emitted, and so the
	// pre-commit hooks judge the finished tree rather than an intermediate one.
	return append(areas, area{
		Name:    "fmt",
		Apply:   applyFormat,
		Message: func(change) string { return "style: satisfy the formatters and pre-commit hooks" },
	})
}

// shippedAreas change what an operator runs: the binary, the console embedded
// in it, or the image it is published in. Their commits are deps, a changelog
// line; the rest are build.
var shippedAreas = map[string]bool{
	"gomod":            true,
	"packages:web":     true,
	"image:distroless": true,
}

// runAreas applies each area in order, committing the ones that hold and
// rewinding the ones that do not. A dropped area leaves no residue because the
// rewind is a hard reset to a commit that is known good.
func runAreas(ctx context.Context, r *repo, areas []area, want func(string) bool) ([]result, error) {
	states := make(map[string]state, len(areas))
	results := make([]result, 0, len(areas))

	record := func(res result) {
		states[res.Area] = res.State
		results = append(results, res)
	}

	for _, a := range areas {
		if !want(a.Name) {
			record(result{Area: a.Name, State: stateSkipped, Reason: "not selected"})

			continue
		}

		if blocker, ok := blockedBy(a, states); ok {
			record(result{Area: a.Name, State: stateSkipped, Reason: "depends on dropped " + blocker})

			continue
		}

		res, err := runArea(ctx, r, a)
		if err != nil {
			return results, err
		}

		record(res)
	}

	return results, nil
}

// blockedBy reports the first dependency that did not survive.
func blockedBy(a area, states map[string]state) (string, bool) {
	for _, need := range a.Needs {
		if states[need] == stateDropped {
			return need, true
		}
	}

	return "", false
}

func runArea(ctx context.Context, r *repo, a area) (result, error) {
	snapshot, err := headSHA(ctx, r)
	if err != nil {
		return result{}, err
	}

	log.Printf("area %s: applying", a.Name)

	rewind := func(res result) (result, error) {
		resetErr := resetTo(ctx, r, snapshot)
		if resetErr != nil {
			return result{}, resetErr
		}

		return res, nil
	}

	ch, err := a.Apply(ctx, r)
	if err != nil {
		return rewind(result{
			Area: a.Name, State: stateDropped,
			Reason: reasonOf(err), Log: logOf(err),
		})
	}

	if ch.Empty {
		return rewind(result{Area: a.Name, State: stateEmpty, Change: ch})
	}

	if a.Gate != nil {
		gateErr := a.Gate(ctx, r)
		if gateErr != nil {
			return rewind(result{
				Area: a.Name, State: stateDropped, Change: ch,
				Reason: reasonOf(gateErr), Log: logOf(gateErr),
			})
		}
	}

	err = commitAll(ctx, r, a.Message(ch))
	if err != nil {
		return result{}, err
	}

	sha, err := headSHA(ctx, r)
	if err != nil {
		return result{}, err
	}

	st := stateApplied
	if len(ch.Drops) > 0 {
		st = statePartial
	}

	return result{Area: a.Name, State: st, Change: ch, Commit: sha}, nil
}
