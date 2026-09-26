package main

import (
	"testing"

	"golang.org/x/mod/modfile"
)

// The lockstep notes in go.mod are load-bearing prose: they are the only record
// of why the pairs exist. `go mod tidy` re-sorts requires and can leave a note
// stranded above the wrong line, which reads fine in a diff and is wrong.
func TestNoteAttached(t *testing.T) {
	const attached = `module example.com/x

go 1.27.0

require (
	// NOTE: gvisor must be updated in lockstep with
	// tailscale.com.
	gvisor.dev/gvisor v0.0.0-20260224225140-573d5e7127a8 // indirect
	pgregory.net/rapid v1.3.0
)
`

	// The note is still in the file, but now documents the wrong module.
	const detached = `module example.com/x

go 1.27.0

require (
	// NOTE: gvisor must be updated in lockstep with
	// tailscale.com.
	pgregory.net/rapid v1.3.0

	gvisor.dev/gvisor v0.0.0-20260224225140-573d5e7127a8 // indirect
)
`

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{name: "attached", content: attached, want: true},
		{name: "detached", content: detached, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, err := modfile.Parse("go.mod", []byte(test.content), nil)
			if err != nil {
				t.Fatalf("parsing fixture: %v", err)
			}

			if got := noteAttached(f, modGvisor, "gvisor must be updated in lockstep"); got != test.want {
				t.Errorf("noteAttached = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRequiredVersion(t *testing.T) {
	const goMod = `module tailscale.com

go 1.27.1

require (
	golang.zx2c4.com/wireguard/windows v0.5.3
	gvisor.dev/gvisor v0.0.0-20260224225140-573d5e7127a8
)
`

	f, err := modfile.Parse("go.mod", []byte(goMod), nil)
	if err != nil {
		t.Fatalf("parsing fixture: %v", err)
	}

	tests := []struct {
		name  string
		dep   string
		want  string
		found bool
	}{
		{name: "present", dep: modWireguardWindows, want: "v0.5.3", found: true},
		{name: "absent", dep: "modernc.org/sqlite", found: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := requiredVersion(f, test.dep)
			if ok != test.found {
				t.Fatalf("requiredVersion found = %v, want %v", ok, test.found)
			}

			if got != test.want {
				t.Errorf("requiredVersion = %q, want %q", got, test.want)
			}
		})
	}
}

// The repository's own go.mod is the case that actually matters: the notes must
// survive whatever the last tidy did to the require blocks.
func TestRepoLockstepNotesAttached(t *testing.T) {
	b, err := modfile.Parse("../../go.mod", mustRead(t, "../../go.mod"), nil)
	if err != nil {
		t.Fatalf("parsing go.mod: %v", err)
	}

	for _, note := range lockstepNotes {
		if !noteAttached(b, note.module, note.needle) {
			t.Errorf("go.mod: note %q is not attached to %s", note.needle, note.module)
		}
	}

	if len(b.Tool) == 0 {
		t.Error("go.mod: tool block is missing")
	}
}
