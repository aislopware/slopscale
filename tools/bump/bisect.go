package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
)

// dropped records an atom that was rewound, and why, so the pull request body
// can say what did not move instead of leaving it silently stale.
type dropped struct {
	Name   string
	Reason string
	Log    string
}

// atom is a set of changes that must be applied together. Splitting a
// lockstep pair across two atoms would let the bisect keep one half of it.
type atom struct {
	Name string
	// Apply makes the change and describes it, or returns "" when there was
	// nothing to move.
	Apply func(ctx context.Context, r *repo) (string, error)
}

// batch is what the bisect needs to know about one family of atoms: the files
// an atom may write, which a rewind restores, and how a set of atoms is
// applied and judged.
type batch struct {
	Files []string
	// Try applies every atom, settles the tree and runs the atom gate,
	// returning the summaries of the atoms that moved something.
	Try func(ctx context.Context, r *repo, atoms []atom) ([]string, error)
}

// fileState holds a batch's files in memory, so the bisect can rewind to an
// intermediate point that was never committed.
type fileState map[string][]byte

func saveFiles(r *repo, files []string) (fileState, error) {
	s := make(fileState, len(files))

	for _, f := range files {
		b, err := os.ReadFile(r.path(f))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", f, err)
		}

		s[f] = b
	}

	return s, nil
}

func (s fileState) restore(r *repo) error {
	for f, b := range s {
		err := r.writeFile(f, string(b))
		if err != nil {
			return err
		}
	}

	return nil
}

// equal reports whether two snapshots of the same files hold the same bytes.
func (s fileState) equal(other fileState) bool {
	if len(s) != len(other) {
		return false
	}

	for f, b := range s {
		if !bytes.Equal(other[f], b) {
			return false
		}
	}

	return true
}

// applyAtoms keeps every atom it can. The optimistic path applies the whole set
// at once; only when that fails does it split, so a healthy day costs one gate
// run and a bad day costs log2(n) rather than n.
//
// The split assumes a failure is attributable to one side. A genuine
// interaction between two atoms shows up as both being dropped, which is the
// safe direction to be wrong in.
func applyAtoms(ctx context.Context, r *repo, b batch, atoms []atom) ([]string, []dropped, error) {
	if len(atoms) == 0 {
		return nil, nil, nil
	}

	base, err := saveFiles(r, b.Files)
	if err != nil {
		return nil, nil, err
	}

	summaries, applyErr := b.Try(ctx, r, atoms)
	if applyErr == nil {
		return summaries, nil, nil
	}

	err = base.restore(r)
	if err != nil {
		return nil, nil, err
	}

	if len(atoms) == 1 {
		return nil, []dropped{{
			Name:   atoms[0].Name,
			Reason: reasonOf(applyErr),
			Log:    logOf(applyErr),
		}}, nil
	}

	mid := len(atoms) / 2

	keptFirst, dropFirst, err := applyAtoms(ctx, r, b, atoms[:mid])
	if err != nil {
		return nil, nil, err
	}

	keptSecond, dropSecond, err := applyAtoms(ctx, r, b, atoms[mid:])
	if err != nil {
		return nil, nil, err
	}

	return append(keptFirst, keptSecond...), append(dropFirst, dropSecond...), nil
}

// applyEach runs every atom's Apply in order, keeping the summaries of the
// ones that moved something.
func applyEach(ctx context.Context, r *repo, atoms []atom) ([]string, error) {
	summaries := make([]string, 0, len(atoms))

	for _, a := range atoms {
		summary, err := a.Apply(ctx, r)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", a.Name, err)
		}

		// An atom that found nothing to do reports no summary; listing it
		// would fill the report with packages that did not move.
		if summary != "" {
			summaries = append(summaries, summary)
		}
	}

	return summaries, nil
}

// reasonOf renders a one-line cause for the report.
func reasonOf(err error) string {
	if ce, ok := errors.AsType[*cmdError](err); ok {
		return "`" + strings.Join(ce.Argv, " ") + "` failed"
	}

	first, _, _ := strings.Cut(err.Error(), "\n")

	return first
}

// logOf returns the captured output of a failing command, if there was one.
func logOf(err error) string {
	if ce, ok := errors.AsType[*cmdError](err); ok {
		return ce.Tail
	}

	return ""
}
