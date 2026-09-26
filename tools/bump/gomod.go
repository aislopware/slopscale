package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

// Modules whose versions are not independent. tailscale.com follows its main
// branch, and the modules it pins by pseudo-version or for platform code this
// repository never compiles move with it or not at all.
const (
	modTailscale        = "tailscale.com"
	modTSClient         = "tailscale.com/client/tailscale/v2"
	modGvisor           = "gvisor.dev/gvisor"
	modWireguardWindows = "golang.zx2c4.com/wireguard/windows"
)

// goModFiles are what a dependency update writes, and what a rewind restores.
var goModFiles = []string{"go.mod", "go.sum"}

var errLockstepDrift = errors.New("lockstep pair drifted after tidy")

// parseGoMod reads and parses the repository's go.mod.
func parseGoMod(r *repo) (*modfile.File, error) {
	b, err := os.ReadFile(r.path("go.mod"))
	if err != nil {
		return nil, fmt.Errorf("reading go.mod: %w", err)
	}

	f, err := modfile.Parse("go.mod", b, nil)
	if err != nil {
		return nil, fmt.Errorf("parsing go.mod: %w", err)
	}

	return f, nil
}

// moduleVersion asks the go command what a module currently resolves to,
// which is the authority after MVS has had its say.
func moduleVersion(ctx context.Context, r *repo, path string) (string, error) {
	out, err := r.nixRun(ctx, "go", "list", "-m", "-f", "{{.Version}}", path)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(out), nil
}

// tailscaleAtom moves tailscale.com to the tip of main and drags gvisor to
// whatever that exact commit requires.
//
// `go get -u tailscale.com` is wrong here: the pin is a pseudo-version that
// sorts above the newest release tag, so -u either no-ops or downgrades.
func tailscaleAtom(ctx context.Context, r *repo) (string, error) {
	before, err := moduleVersion(ctx, r, modTailscale)
	if err != nil {
		return "", err
	}

	_, err = r.nixRun(ctx, "go", "get", modTailscale+"@main")
	if err != nil {
		return "", err
	}

	after, err := moduleVersion(ctx, r, modTailscale)
	if err != nil {
		return "", err
	}

	gvisor, err := partnerVersion(ctx, modTailscale, after, modGvisor)
	if err != nil {
		return "", err
	}

	_, err = r.nixRun(ctx, "go", "get", modGvisor+"@"+gvisor)
	if err != nil {
		return "", err
	}

	// A separate module with ordinary release tags, so -u is correct.
	_, err = r.nixRun(ctx, "go", "get", "-u", modTSClient)
	if err != nil {
		return "", err
	}

	if before == after {
		return "", nil
	}

	return fmt.Sprintf("tailscale.com %s -> %s (gvisor %s)", before, after, gvisor), nil
}

// directRequirements is every direct requirement not already owned by the
// tailscale atom.
func directRequirements(r *repo) ([]string, error) {
	f, err := parseGoMod(r)
	if err != nil {
		return nil, err
	}

	owned := map[string]bool{
		modTailscale:        true,
		modTSClient:         true,
		modGvisor:           true,
		modWireguardWindows: true,
	}

	var paths []string

	for _, req := range f.Require {
		if req.Indirect || owned[req.Mod.Path] {
			continue
		}

		paths = append(paths, req.Mod.Path)
	}

	return paths, nil
}

// moduleAtom upgrades one direct requirement. One atom per module costs nothing
// on a good day, because the bisect tries every atom together first and only
// splits when that fails. It is what stops a single dependency deprecating an
// API the tree still uses from taking every other upgrade down with it.
func moduleAtom(path string) func(context.Context, *repo) (string, error) {
	return func(ctx context.Context, r *repo) (string, error) {
		before, err := moduleVersion(ctx, r, path)
		if err != nil {
			return "", err
		}

		// Resolve the target here rather than letting `go get -u` choose it.
		// The go command takes the highest semver it is offered, which is how
		// a stray tag on a fork ends up committed.
		want, err := latestVersion(ctx, path, before)
		if err != nil {
			return "", err
		}

		if want == before {
			return "", nil
		}

		_, err = r.nixRun(ctx, "go", "get", "-u", path+"@"+want)
		if err != nil {
			return "", err
		}

		after, err := moduleVersion(ctx, r, path)
		if err != nil {
			return "", err
		}

		if before == after {
			return "", nil
		}

		return describeChange(path, before, after), nil
	}
}

func goModAtoms(r *repo) ([]atom, error) {
	atoms := []atom{{Name: "tailscale", Apply: tailscaleAtom}}

	paths, err := directRequirements(r)
	if err != nil {
		return nil, err
	}

	for _, path := range paths {
		atoms = append(atoms, atom{Name: path, Apply: moduleAtom(path)})
	}

	return atoms, nil
}

// lockstepPairs are the indirect dependencies whose version is dictated by
// another module rather than by minimal version selection. wireguard-windows
// only compiles into Windows builds, which this repository never makes, so a
// mismatch would not fail anything here; it is held anyway so the tailscale
// code in the module graph is always the combination tailscale tested.
var lockstepPairs = []struct{ owner, dep string }{
	{modTailscale, modGvisor},
	{modTailscale, modWireguardWindows},
}

// repin drags each lockstep dependency back to the version its owner requires.
// Upgrading unrelated modules routinely raises a shared indirect past its
// owner's pin, so without this the common case is a whole dependency batch
// failing the assertion below and being dropped wholesale.
func repin(ctx context.Context, r *repo) error {
	for _, p := range lockstepPairs {
		ownerVer, err := moduleVersion(ctx, r, p.owner)
		if err != nil {
			return err
		}

		want, err := partnerVersion(ctx, p.owner, ownerVer, p.dep)
		if err != nil {
			return err
		}

		have, err := moduleVersion(ctx, r, p.dep)
		if err != nil {
			return err
		}

		if have == want {
			continue
		}

		_, err = r.nixRun(ctx, "go", "get", p.dep+"@"+want)
		if err != nil {
			return err
		}
	}

	return nil
}

// checkLockstep re-reads the resolved versions and asserts the pairs still
// agree. MVS is allowed to raise an indirect above what its owner pins when a
// third module demands it; that is exactly the failure this catches.
func checkLockstep(ctx context.Context, r *repo) error {
	for _, p := range lockstepPairs {
		ownerVer, err := moduleVersion(ctx, r, p.owner)
		if err != nil {
			return err
		}

		want, err := partnerVersion(ctx, p.owner, ownerVer, p.dep)
		if err != nil {
			return err
		}

		got, err := moduleVersion(ctx, r, p.dep)
		if err != nil {
			return err
		}

		if got != want {
			return fmt.Errorf("%w: %s requires %s %s, go.mod resolved %s",
				errLockstepDrift, p.owner, p.dep, want, got)
		}
	}

	return nil
}

// lockstepNotes are the prose blocks in go.mod that explain why the pairs
// exist. `go mod tidy` re-sorts requires and can detach a comment from the line
// it documents, silently dropping the reasoning; assert attachment, not mere
// presence.
var lockstepNotes = []struct{ module, needle string }{
	{modGvisor, "gvisor must be updated in lockstep"},
}

var (
	errNoteDetached = errors.New("lockstep note no longer attached to its require")
	errToolBlockOne = errors.New("go.mod tool block disappeared")
)

func checkModComments(r *repo) error {
	f, err := parseGoMod(r)
	if err != nil {
		return err
	}

	for _, note := range lockstepNotes {
		if !noteAttached(f, note.module, note.needle) {
			return fmt.Errorf("%w: %s (%q)", errNoteDetached, note.module, note.needle)
		}
	}

	if len(f.Tool) == 0 {
		return errToolBlockOne
	}

	return nil
}

// noteAttached reports whether the require line for module carries a preceding
// comment containing needle.
func noteAttached(f *modfile.File, module, needle string) bool {
	for _, req := range f.Require {
		if req.Mod.Path != module || req.Syntax == nil {
			continue
		}

		var sb strings.Builder
		for _, c := range req.Syntax.Before {
			sb.WriteString(c.Token)
			sb.WriteString("\n")
		}

		if strings.Contains(sb.String(), needle) {
			return true
		}
	}

	return false
}

var errDowngrade = errors.New("upgrade moved a requirement backwards")

// checkDowngrades refuses a set that lowered any requirement. An upgrade run
// that moves a version backwards means selection resolved something nobody
// asked for, and unwinding that is a human's rollback commit, not an
// unattended bump. Diffing go.mod catches it wherever it came from; reading
// what go get printed only catches what go get did itself.
//
// The lockstep partners are exempt because repin lowers them on purpose, back
// to the version their owner requires.
func checkDowngrades(before, after *modfile.File) error {
	exempt := map[string]bool{modGvisor: true, modWireguardWindows: true}

	was := make(map[string]string, len(before.Require))
	for _, req := range before.Require {
		was[req.Mod.Path] = req.Mod.Version
	}

	var lowered []string

	for _, req := range after.Require {
		old, ok := was[req.Mod.Path]
		if !ok || exempt[req.Mod.Path] {
			continue
		}

		if semver.Compare(req.Mod.Version, old) < 0 {
			lowered = append(lowered, fmt.Sprintf("%s %s -> %s", req.Mod.Path, old, req.Mod.Version))
		}
	}

	if len(lowered) > 0 {
		return fmt.Errorf("%w: %s", errDowngrade, strings.Join(lowered, ", "))
	}

	return nil
}

var errToolchainAhead = errors.New("dependencies require a newer Go than the devShell provides")

// checkToolchain catches a dependency that dragged go.mod's go directive above
// the toolchain nixpkgs ships.
//
// The go command papers over this by downloading the newer toolchain, so
// `go build` succeeds and nothing looks wrong. The nix builders set
// GOTOOLCHAIN=local and fail outright, which is why this has to be an explicit
// check rather than something the build would surface on its own.
func checkToolchain(ctx context.Context, r *repo) error {
	goMod, err := r.readFile("go.mod")
	if err != nil {
		return err
	}

	want, err := goDirective(goMod)
	if err != nil {
		return err
	}

	have, err := goVersion(ctx, r)
	if err != nil {
		return err
	}

	if semver.Compare("v"+want, "v"+have) > 0 {
		return fmt.Errorf("%w: go.mod now requires go %s, the devShell provides %s",
			errToolchainAhead, want, have)
	}

	return nil
}

// settle runs the steps every dependency change needs before it can be judged:
// tidy, restore the lockstep pins that the upgrade may have disturbed, tidy
// again, then assert go.mod's hand-written rules survived.
func settle(ctx context.Context, r *repo) error {
	_, err := r.nixRun(ctx, "go", "mod", "tidy")
	if err != nil {
		return err
	}

	err = repin(ctx, r)
	if err != nil {
		return err
	}

	_, err = r.nixRun(ctx, "go", "mod", "tidy")
	if err != nil {
		return err
	}

	err = checkLockstep(ctx, r)
	if err != nil {
		return err
	}

	err = checkToolchain(ctx, r)
	if err != nil {
		return err
	}

	return checkModComments(r)
}

// atomGate is the signal that one dependency set is viable. It runs once per
// bisect step, so it stays well short of the full nix checks the final gate
// runs over the finished tree.
func atomGate(ctx context.Context, r *repo) error {
	_, err := r.nixRun(ctx, "go", "build", "./...")
	if err != nil {
		return err
	}

	_, err = r.nixRun(ctx, "go", "vet", "./...")
	if err != nil {
		return err
	}

	// Lint belongs here, not only in the final gate. A dependency that
	// deprecates an API the tree still uses compiles and vets cleanly and fails
	// staticcheck, so without this the whole area is dropped for one module's
	// sake instead of the bisect narrowing to that module.
	_, err = r.nixRun(ctx, "golangci-lint", "run", "--timeout", "10m")

	return err
}

// tryGoMod applies a set of module atoms, settles go.mod and asks the atom
// gate whether the result is viable.
func tryGoMod(ctx context.Context, r *repo, atoms []atom) ([]string, error) {
	before, err := parseGoMod(r)
	if err != nil {
		return nil, err
	}

	summaries, err := applyEach(ctx, r, atoms)
	if err != nil {
		return nil, err
	}

	err = settle(ctx, r)
	if err != nil {
		return nil, err
	}

	after, err := parseGoMod(r)
	if err != nil {
		return nil, err
	}

	err = checkDowngrades(before, after)
	if err != nil {
		return nil, err
	}

	err = atomGate(ctx, r)
	if err != nil {
		return nil, err
	}

	return summaries, nil
}

// applyGoMod upgrades dependencies, then refreshes the vendor hash that
// flake.nix reads. Skipping that refresh is the classic way to hand over a
// pull request that cannot nix build.
func applyGoMod(ctx context.Context, r *repo) (change, error) {
	atoms, err := goModAtoms(r)
	if err != nil {
		return change{}, err
	}

	kept, drops, err := applyAtoms(ctx, r, batch{Files: goModFiles, Try: tryGoMod}, atoms)
	if err != nil {
		return change{}, err
	}

	touched, err := changedFiles(ctx, r)
	if err != nil {
		return change{}, err
	}

	if len(touched) == 0 {
		return change{Empty: true, Drops: drops}, nil
	}

	_, err = r.nixRun(ctx, "go", "run", "./cmd/vendorhash", "update")
	if err != nil {
		return change{}, err
	}

	return change{
		Summary: "update Go modules",
		Detail:  kept,
		Drops:   drops,
	}, nil
}

var errTidyNotIdempotent = errors.New("go mod tidy is not idempotent")

// gateGoMod re-runs the settling steps and asserts they are a no-op. A tidy
// that still has work to do means the committed go.mod is not what the go
// command would produce, and the lint job's tidy check would say so later.
func gateGoMod(ctx context.Context, r *repo) error {
	before, err := saveFiles(r, goModFiles)
	if err != nil {
		return err
	}

	err = settle(ctx, r)
	if err != nil {
		return err
	}

	after, err := saveFiles(r, goModFiles)
	if err != nil {
		return err
	}

	if !before.equal(after) {
		return errTidyNotIdempotent
	}

	_, err = r.nixRun(ctx, "go", "run", "./cmd/vendorhash", "check")

	return err
}
