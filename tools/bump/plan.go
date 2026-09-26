package main

import (
	"context"
	"fmt"
)

// cmdPlan resolves every upstream the bump reads and prints the gap against
// what is committed. It writes nothing, so it is the safe way to ask "what
// would the next run do" before trusting the automation.
func cmdPlan(ctx context.Context) error {
	r, err := openRepo(ctx)
	if err != nil {
		return err
	}

	for _, line := range planLines(ctx, r) {
		fmt.Println(line)
	}

	return nil
}

func planLines(ctx context.Context, r *repo) []string {
	lines := []string{"flake.lock: `nix flake update` moves nixpkgs, flake-utils and flake-checks"}

	lines = append(lines, planBun(ctx, r))
	lines = append(lines, planBuilders(ctx, r)...)
	lines = append(lines, planLockstep(ctx, r)...)
	lines = append(lines, planImages(ctx, r)...)
	lines = append(lines, planPackages(ctx, r)...)

	version, err := oapiVersion(r)
	if err != nil {
		return append(lines, "oapi-codegen: "+err.Error())
	}

	latest, err := latestVersion(ctx, oapiModule, version)
	if err != nil {
		return append(lines, "oapi-codegen: "+err.Error())
	}

	return append(lines, gap("Makefile oapi-codegen", version, latest))
}

func planBun(ctx context.Context, r *repo) string {
	content, err := r.readFile("flake.nix")
	if err != nil {
		return "bun: " + err.Error()
	}

	have, err := pinnedBun(content)
	if err != nil {
		return "bun: " + err.Error()
	}

	want, err := latestBun(ctx)
	if err != nil {
		return "bun: " + err.Error()
	}

	return gap("flake.nix bun", have, want)
}

func planImages(ctx context.Context, r *repo) []string {
	defs := imageBumps()
	lines := make([]string, 0, len(defs))

	for _, def := range defs {
		have, err := currentImageRef(r, def)
		if err != nil {
			lines = append(lines, def.Name+": "+err.Error())

			continue
		}

		want, err := def.Resolve(ctx, have)
		if err != nil {
			lines = append(lines, def.Name+": "+err.Error())

			continue
		}

		lines = append(lines, gap(def.Name, have, want))
	}

	return lines
}

// planPackages lists only the packages that would move: the console alone has
// fifty, and a line for each current one buries the few that matter.
func planPackages(ctx context.Context, r *repo) []string {
	var lines []string

	for _, set := range packageSets {
		deps, err := readDeps(r, set)
		if err != nil {
			lines = append(lines, set.Name+": "+err.Error())

			continue
		}

		current := 0

		for _, d := range deps {
			want, err := resolveDep(ctx, set, d)
			if err != nil {
				lines = append(lines, fmt.Sprintf("%s %s: %s", set.Name, d.Name, err))

				continue
			}

			if want == d.Version {
				current++

				continue
			}

			lines = append(lines, gap(fmt.Sprintf("%s %s", d.File, d.Name), d.Version, want))
		}

		lines = append(lines, fmt.Sprintf("%s: %d of %d packages current", set.Name, current, len(deps)))
	}

	return lines
}

func planBuilders(ctx context.Context, r *repo) []string {
	tsGo, err := tailscaleGo(ctx)
	if err != nil {
		return []string{"tailscale go directive: " + err.Error()}
	}

	nixGo, err := goVersion(ctx, r)
	if err != nil {
		return []string{"devShell Go: " + err.Error()}
	}

	ourMod, err := r.readFile("go.mod")
	if err != nil {
		return []string{err.Error()}
	}

	ourGo, err := goDirective(ourMod)
	if err != nil {
		return []string{err.Error()}
	}

	lines := []string{fmt.Sprintf("go: go.mod %s, devShell %s, tailscale main %s", ourGo, nixGo, tsGo)}

	for _, set := range []struct {
		pins []goPin
		want string
	}{
		{tailscaleBuilders, tsGo},
		{localBuilders, higher(nixGo, ourGo)},
	} {
		for _, pin := range set.pins {
			content, err := r.readFile(pin.File)
			if err != nil {
				lines = append(lines, err.Error())

				continue
			}

			have, ok := currentGolangTag(content)
			if !ok {
				lines = append(lines, pin.File+": no golang builder image found")

				continue
			}

			lines = append(lines, gap(pin.File+" golang", have, set.want))
		}
	}

	return lines
}

// planLockstep reports tailscale.com against the tip of main, and each partner
// against the version that tip pins.
func planLockstep(ctx context.Context, r *repo) []string {
	have, err := moduleVersion(ctx, r, modTailscale)
	if err != nil {
		return []string{modTailscale + ": " + err.Error()}
	}

	want, err := mainVersion(ctx, modTailscale)
	if err != nil {
		return []string{modTailscale + ": " + err.Error()}
	}

	lines := []string{gap(modTailscale+"@main", have, want)}

	for _, pair := range lockstepPairs {
		partner, err := partnerVersion(ctx, pair.owner, want, pair.dep)
		if err != nil {
			lines = append(lines, pair.dep+": "+err.Error())

			continue
		}

		haveDep, err := moduleVersion(ctx, r, pair.dep)
		if err != nil {
			lines = append(lines, pair.dep+": "+err.Error())

			continue
		}

		lines = append(lines, gap(pair.dep+" (pinned by "+pair.owner+")", haveDep, partner))
	}

	return lines
}

// gap renders one pin as either current or lagging.
func gap(what, have, want string) string {
	if have == want {
		return fmt.Sprintf("%s: %s (current)", what, have)
	}

	return fmt.Sprintf("%s: %s -> %s", what, have, want)
}
