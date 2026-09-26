// Command bump moves slopscale's pinned versions on a local branch and reports
// what moved. It never publishes: a bump is finished by reading each moved
// item's changes and acting on what slopscale should drop, change or adopt,
// and only then does a pull request open.
//
// The pins are interlocked. flake.nix asks nixpkgs for the newest Go, so a lock
// update moves the compiler and every devShell tool at once, except bun, which
// flake.nix pins by hand because web/bun.lock needs a newer one than nixpkgs
// ships. Two Dockerfiles compile a tailscale tree cloned from an unpinned
// branch, so their builder image has to keep up with upstream's go directive.
// go.mod follows tailscale.com's main branch and must hold gvisor and
// wireguard-windows where that commit pins them. flakehashes.json has to follow
// go.sum. The capability-version table is scraped from tailscale's published
// tags, so it goes stale without anyone touching the repository.
//
// Each of those is an area: applied, gated, and committed on its own, so one
// failure costs one commit rather than the whole run.
//
//	bump plan     resolve every source of truth and print what would change
//	bump run      apply and gate on a local branch, one commit per area, and report
//	bump verify   assert the pins are mutually consistent
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/creachadair/command"
	"github.com/creachadair/flax"
)

type runFlags struct {
	DryRun bool   `flag:"dry-run,default=false,Apply and report without the final gate"`
	Areas  string `flag:"areas,Comma-separated areas to run (default: all)"`
	Skip   string `flag:"skip,Comma-separated areas to skip"`
	Branch string `flag:"branch,default=chore/version-bump,Local branch the areas are committed on"`
	Base   string `flag:"base,default=main,Base branch"`
	Remote string `flag:"remote,default=origin,Git remote the base is fetched from"`
	Gate   string `flag:"gate,default=full,Final gate level: none, quick or full"`
}

var runCfg runFlags

var errDirtyTree = errors.New("the working tree has uncommitted changes")

func main() {
	log.SetFlags(0)
	log.SetPrefix("bump: ")

	root := command.C{
		Name: "bump",
		Help: "Keep slopscale's pinned versions current",
		Commands: []*command.C{
			{
				Name: "plan",
				Help: "Resolve every source of truth and print what would change",
				Run:  func(env *command.Env) error { return cmdPlan(env.Context()) },
			},
			{
				Name:     "run",
				Help:     "Apply and gate on a local branch, one commit per area, and report",
				SetFlags: command.Flags(flax.MustBind, &runCfg),
				Run:      func(env *command.Env) error { return cmdRun(env.Context()) },
			},
			{
				Name: "verify",
				Help: "Assert the pins are mutually consistent",
				Run:  func(env *command.Env) error { return cmdVerify(env.Context()) },
			},
			command.HelpCommand(nil),
		},
	}

	command.RunOrFail(root.NewEnv(nil), os.Args[1:])
}

// selector turns the --areas and --skip flags into a predicate.
func selector(only, skip string) func(string) bool {
	set := func(s string) map[string]bool {
		m := map[string]bool{}

		for part := range strings.SplitSeq(s, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				m[part] = true
			}
		}

		return m
	}

	wanted, skipped := set(only), set(skip)

	return func(name string) bool {
		if skipped[name] {
			return false
		}

		return len(wanted) == 0 || wanted[name]
	}
}

func cmdRun(ctx context.Context) error {
	gate := runCfg.Gate
	if runCfg.DryRun {
		gate = gateNone
	}

	r, err := openRepo(ctx)
	if err != nil {
		return err
	}

	// The run rebuilds the branch with hard resets and cleans, so on a
	// developer's checkout it would take uncommitted work with it.
	dirty, err := changedFiles(ctx, r)
	if err != nil {
		return err
	}

	if len(dirty) > 0 {
		return fmt.Errorf("%w: %s", errDirtyTree, strings.Join(dirty, ", "))
	}

	err = startBranch(ctx, r, runCfg.Remote, runCfg.Base, runCfg.Branch)
	if err != nil {
		return err
	}

	results, err := runAreas(ctx, r, allAreas(), selector(runCfg.Areas, runCfg.Skip))
	if err != nil {
		return err
	}

	results, err = enforceFinalGate(ctx, r, results, gate)
	if err != nil {
		return err
	}

	if !anyApplied(results) {
		log.Print("nothing moved")

		return nil
	}

	fmt.Print(renderReport(results, gate))

	return nil
}

func anyApplied(results []result) bool {
	for _, res := range results {
		if res.Commit != "" {
			return true
		}
	}

	return false
}
