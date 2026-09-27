package main

import (
	"context"
	"strings"
)

// porcelainStatus is the width of the "XY " status that starts every line of
// `git status --porcelain`.
const porcelainStatus = 3

// changedFiles lists paths that differ from HEAD, tracked or not.
func changedFiles(ctx context.Context, r *repo) ([]string, error) {
	out, err := r.run(ctx, "git", "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return nil, err
	}

	var files []string

	for line := range strings.Lines(out) {
		line = strings.TrimRight(line, "\n")
		if len(line) <= porcelainStatus {
			continue
		}

		path := strings.TrimSpace(line[porcelainStatus:])
		// Renames read as "old -> new"; the destination is what matters.
		if _, dst, ok := strings.Cut(path, " -> "); ok {
			path = dst
		}

		files = append(files, strings.Trim(path, `"`))
	}

	return files, nil
}

func headSHA(ctx context.Context, r *repo) (string, error) {
	out, err := r.run(ctx, "git", "rev-parse", "HEAD")

	return strings.TrimSpace(out), err
}

// resetTo rewinds the worktree to sha, leaving nothing behind. Every dropped
// area goes through here, which is why a drop cannot leave a partial edit in
// the branch.
func resetTo(ctx context.Context, r *repo, sha string) error {
	_, err := r.run(ctx, "git", "reset", "--hard", sha)
	if err != nil {
		return err
	}

	_, err = r.run(ctx, "git", "clean", "-fd")

	return err
}

func commitAll(ctx context.Context, r *repo, message string) error {
	_, err := r.run(ctx, "git", "add", "--all")
	if err != nil {
		return err
	}

	// Each area is gated before it is committed; the hooks would re-run the
	// same checks far more slowly.
	_, err = r.run(ctx, "git", "commit", "--no-verify", "--message", message)

	return err
}

// startBranch rebuilds the working branch from the base every run. The branch
// therefore never accumulates history and never conflicts.
func startBranch(ctx context.Context, r *repo, remote, base, branch string) error {
	_, err := r.run(ctx, "git", "fetch", remote, base)
	if err != nil {
		return err
	}

	_, err = r.run(ctx, "git", "checkout", "-B", branch, remote+"/"+base)

	return err
}
