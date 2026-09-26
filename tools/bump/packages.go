package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"golang.org/x/mod/semver"
)

// npmRegistry is where bun resolves packages from. It is a var only so the
// tests can point it at a fake registry.
var npmRegistry = "https://registry.npmjs.org"

// npmCooldown is how old a release has to be before it is adopted. Hijacked
// npm releases are usually found and pulled within days; a pin that lags by
// that much costs nothing.
const npmCooldown = 3 * 24 * time.Hour

// packageSet is one bun workspace: a lockfile, the manifests it covers, and the
// make targets that prove a change to it.
type packageSet struct {
	Name      string
	Dir       string
	Manifests []string
	// Holds keep a package on its current major, keyed "manifest:package".
	Holds map[string]string
	// Gate runs after the lockfile is updated and the tree reformatted.
	Gate []string
	// Format rewrites what a new formatter or linter would change.
	Format []string
}

var packageSets = []packageSet{
	{
		Name:      "web",
		Dir:       "web",
		Manifests: []string{"web/package.json", "web/codegen/package.json"},
		Holds: map[string]string{
			"web/codegen/package.json:typescript": "openapi-typescript drives the TypeScript 5 compiler API, " +
				"which the Go-based TypeScript 7 does not expose",
		},
		Format: []string{"fmt-web"},
		Gate:   []string{"lint-web", "test-web", "web"},
	},
	{
		Name:      "docs",
		Dir:       "docs",
		Manifests: []string{"docs/package.json"},
		Gate:      []string{"docs"},
	},
}

// dep is one pinned package in one manifest.
type dep struct {
	File    string
	Name    string
	Prefix  string // "", "^" or "~"
	Version string
}

// npmSpec accepts the specs this tool owns: an exact version, or a caret or
// tilde range on one. Anything else (tags, workspaces, URLs, wider ranges) was
// written that way on purpose and is left alone.
var npmSpec = regexp.MustCompile(`^([~^]?)(\d+\.\d+\.\d+)$`)

var (
	errSpecNotUnique = errors.New("package spec is not unique in its manifest")
	errNoCandidate   = errors.New("no release is old enough to adopt")
)

// readDeps lists every package this tool can move in a set's manifests.
func readDeps(r *repo, set packageSet) ([]dep, error) {
	var deps []dep

	for _, file := range set.Manifests {
		content, err := r.readFile(file)
		if err != nil {
			return nil, err
		}

		var manifest struct {
			Dependencies    map[string]string `json:"dependencies"`
			DevDependencies map[string]string `json:"devDependencies"`
		}

		err = json.Unmarshal([]byte(content), &manifest)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", file, err)
		}

		for _, section := range []map[string]string{manifest.Dependencies, manifest.DevDependencies} {
			for name, spec := range section {
				m := npmSpec.FindStringSubmatch(spec)
				if m == nil {
					continue
				}

				deps = append(deps, dep{File: file, Name: name, Prefix: m[1], Version: m[2]})
			}
		}
	}

	slices.SortFunc(deps, func(a, b dep) int {
		return strings.Compare(a.File+":"+a.Name, b.File+":"+b.Name)
	})

	return deps, nil
}

// packument is the part of the registry's package document the resolver
// reads.
type packument struct {
	DistTags map[string]string    `json:"dist-tags"`
	Time     map[string]time.Time `json:"time"`
}

// packuments caches the registry documents for the length of a run. The bisect
// re-applies a family on every split, and the larger documents run to
// megabytes.
var packuments = map[string]packument{}

func fetchPackument(ctx context.Context, name string) (packument, error) {
	if doc, ok := packuments[name]; ok {
		return doc, nil
	}

	var doc packument

	body, err := fetch(ctx, npmRegistry+"/"+url.PathEscape(name))
	if err != nil {
		return doc, err
	}

	err = json.Unmarshal(body, &doc)
	if err != nil {
		return doc, fmt.Errorf("decoding the registry document of %s: %w", name, err)
	}

	packuments[name] = doc

	return doc, nil
}

// pickVersion is the newest release that is no newer than the latest tag,
// not a prerelease, old enough to trust, and on the current major when the
// package is held. It returns have when nothing qualifies as newer.
func pickVersion(doc packument, have string, held bool, now time.Time) (string, error) {
	latest := doc.DistTags["latest"]
	if !semver.IsValid("v" + latest) {
		return "", fmt.Errorf("%w: latest tag is %q", errNoCandidate, latest)
	}

	best := have

	for version, published := range doc.Time {
		v := "v" + version

		switch {
		case !semver.IsValid(v), semver.Prerelease(v) != "", semver.Build(v) != "":
			continue
		case semver.Compare(v, "v"+latest) > 0, semver.Compare(v, "v"+best) <= 0:
			continue
		case held && semver.Major(v) != semver.Major("v"+have):
			continue
		case now.Sub(published) < npmCooldown:
			continue
		}

		best = version
	}

	return best, nil
}

// resolveDep is the version dep should be pinned to.
func resolveDep(ctx context.Context, set packageSet, d dep) (string, error) {
	doc, err := fetchPackument(ctx, d.Name)
	if err != nil {
		return "", err
	}

	_, held := set.Holds[d.File+":"+d.Name]

	return pickVersion(doc, d.Version, held, time.Now())
}

// setSpec rewrites one package's spec in a manifest, keeping its range
// prefix. It refuses a manifest that lists the package twice, since the text
// alone cannot say which entry was meant.
func setSpec(manifest string, d dep, version string) (string, error) {
	old := fmt.Sprintf("%q: %q", d.Name, d.Prefix+d.Version)
	if strings.Count(manifest, old) != 1 {
		return "", fmt.Errorf("%w: %s in %s", errSpecNotUnique, d.Name, d.File)
	}

	return strings.Replace(manifest, old, fmt.Sprintf("%q: %q", d.Name, d.Prefix+version), 1), nil
}

// packageFamilies names packages that are released together and only work at
// matching versions, so the bisect never keeps one without the other. Scoped
// packages already group by scope; @types packages group with what they type.
var packageFamilies = map[string]string{
	"react-dom":                  "react",
	"vitest-browser-react":       "@vitest",
	"vitest":                     "@vitest",
	"playwright":                 "@playwright",
	"tailwindcss":                "@tailwindcss",
	"oxlint":                     "@oxlint",
	"oxlint-tsgolint":            "@oxlint",
	"codemirror":                 "@codemirror",
	"openapi-fetch":              "openapi-ts",
	"openapi-react-query":        "openapi-ts",
	"openapi-typescript":         "openapi-ts",
	"openapi-typescript-helpers": "openapi-ts",
}

// familyOf is the group a package moves with.
func familyOf(name string) string {
	if typed, ok := strings.CutPrefix(name, "@types/"); ok {
		name = typed
	}

	if fam, ok := packageFamilies[name]; ok {
		return fam
	}

	if scope, _, ok := strings.Cut(name, "/"); ok && strings.HasPrefix(scope, "@") {
		return scope
	}

	return name
}

// packageAtoms groups a set's packages by family, one atom per family.
func packageAtoms(set packageSet, deps []dep) []atom {
	var order []string

	byFamily := map[string][]dep{}

	for _, d := range deps {
		fam := familyOf(d.Name)
		if _, seen := byFamily[fam]; !seen {
			order = append(order, fam)
		}

		byFamily[fam] = append(byFamily[fam], d)
	}

	atoms := make([]atom, 0, len(order))

	for _, fam := range order {
		members := byFamily[fam]
		atoms = append(atoms, atom{Name: fam, Apply: familyAtom(set, members)})
	}

	return atoms
}

// familyAtom moves every member of one family to its resolved version.
func familyAtom(set packageSet, members []dep) func(context.Context, *repo) (string, error) {
	return func(ctx context.Context, r *repo) (string, error) {
		var moved []string

		for _, d := range members {
			want, err := resolveDep(ctx, set, d)
			if err != nil {
				return "", err
			}

			if want == d.Version {
				continue
			}

			content, err := r.readFile(d.File)
			if err != nil {
				return "", err
			}

			updated, err := setSpec(content, d, want)
			if err != nil {
				return "", err
			}

			err = r.writeFile(d.File, updated)
			if err != nil {
				return "", err
			}

			moved = append(moved, fmt.Sprintf("%s %s -> %s", d.Name, d.Version, want))
		}

		return strings.Join(moved, ", "), nil
	}
}

// tryPackages applies a set of family atoms, re-locks, reformats with
// whatever formatter the set now pins, and runs the set's gate.
func tryPackages(set packageSet) func(context.Context, *repo, []atom) ([]string, error) {
	return func(ctx context.Context, r *repo, atoms []atom) ([]string, error) {
		summaries, err := applyEach(ctx, r, atoms)
		if err != nil {
			return nil, err
		}

		if len(summaries) == 0 {
			return nil, nil
		}

		_, err = r.nixRunIn(ctx, set.Dir, "bun", "install")
		if err != nil {
			return nil, err
		}

		for _, target := range set.Format {
			_, err = r.nixRun(ctx, "make", target)
			if err != nil {
				return nil, err
			}
		}

		// A browser test needs the browsers of the Playwright just locked.
		if set.Name == "web" {
			_, err = r.nixRunIn(ctx, set.Dir, "bunx", "playwright", "install", "chromium")
			if err != nil {
				return nil, err
			}
		}

		for _, target := range set.Gate {
			_, err = r.nixRun(ctx, "make", target)
			if err != nil {
				return nil, err
			}
		}

		return summaries, nil
	}
}

// trackedFiles lists the tracked files under dir. A package set's rewind has
// to cover all of them, because the formatter a bump brings in can rewrite
// any of them.
func trackedFiles(ctx context.Context, r *repo, dir string) ([]string, error) {
	out, err := r.run(ctx, "git", "ls-files", "--", dir)
	if err != nil {
		return nil, err
	}

	return strings.Fields(out), nil
}

func applyPackages(set packageSet) func(context.Context, *repo) (change, error) {
	return func(ctx context.Context, r *repo) (change, error) {
		deps, err := readDeps(r, set)
		if err != nil {
			return change{}, err
		}

		files, err := trackedFiles(ctx, r, set.Dir)
		if err != nil {
			return change{}, err
		}

		kept, drops, err := applyAtoms(ctx, r, batch{Files: files, Try: tryPackages(set)}, packageAtoms(set, deps))
		if err != nil {
			return change{}, err
		}

		if len(kept) == 0 {
			return change{Empty: true, Drops: drops}, nil
		}

		return change{
			Summary: set.Name + " packages",
			Detail:  kept,
			Drops:   drops,
		}, nil
	}
}

func packageAreas() []area {
	areas := make([]area, 0, len(packageSets))

	for _, set := range packageSets {
		areas = append(areas, area{
			Name:  "packages:" + set.Name,
			Apply: applyPackages(set),
			Message: func(c change) string {
				if shippedAreas["packages:"+set.Name] {
					return "deps(" + set.Name + "): update " + c.Summary
				}

				return "build(" + set.Name + "): update " + c.Summary
			},
		})
	}

	return areas
}
