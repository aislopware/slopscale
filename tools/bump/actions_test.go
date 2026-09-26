package main

import (
	"strings"
	"testing"
)

func TestActionPinMatch(t *testing.T) {
	tests := []struct {
		name             string
		line             string
		owner, repo, sub string
		sha, ref         string
		match            bool
	}{
		{
			name:  "sha pinned with version comment",
			line:  "      - uses: actions/checkout@8e8c483db84b4bee98b60c0593521ed34d9990e8 # v6.0.1\n",
			owner: "actions", repo: "checkout",
			sha:   "8e8c483db84b4bee98b60c0593521ed34d9990e8",
			ref:   "v6.0.1",
			match: true,
		},
		{
			name:  "branch comment",
			line:  "      - uses: NixOS/nix-installer-action@6b8548fe06acfb0155a50ab5d561accb215764cc # main\n",
			owner: "NixOS", repo: "nix-installer-action",
			sha:   "6b8548fe06acfb0155a50ab5d561accb215764cc",
			ref:   "main",
			match: true,
		},
		{
			// No SHA to replace and no comment to correct; leave it alone
			// rather than silently changing how it is pinned.
			name: "unpinned branch reference",
			line: "        uses: alexellis/setup-sshd-actor@master\n",
		},
		{
			name: "local reusable workflow",
			line: "    uses: ./.github/workflows/integration-test-template.yml\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := actionPin.FindStringSubmatch(test.line)
			if (m != nil) != test.match {
				t.Fatalf("match = %v, want %v", m != nil, test.match)
			}

			if m == nil {
				return
			}

			for i, want := range map[int]string{2: test.owner, 3: test.repo, 4: test.sub, 5: test.sha, 7: test.ref} {
				if m[i] != want {
					t.Errorf("group %d = %q, want %q", i, m[i], want)
				}
			}
		})
	}
}

func TestActionPinRewrite(t *testing.T) {
	const line = "      - uses: actions/checkout@8e8c483db84b4bee98b60c0593521ed34d9990e8 # v6.0.1\n"

	m := actionPin.FindStringSubmatch(line)
	got := m[1] + m[2] + "/" + m[3] + m[4] + "@" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + m[6] + "v7.0.0"

	const want = "uses: actions/checkout@aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa # v7.0.0"
	if got != want {
		t.Errorf("rewrite = %q, want %q", got, want)
	}
}

// hestia downloads the release its version input names, "latest" by
// default, so the input is pinned next to the SHA and has to move with it.
func TestActionPinMovesVersionInput(t *testing.T) {
	const pinned = "    - uses: Mic92/hestia@dfed9ced335d28978ba74e513939a10db1f71025 # v3.1.0\n" +
		"      with:\n" +
		"        version: v3.1.0\n" +
		"    - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1\n"

	target := actionTarget{SHA: strings.Repeat("a", 40), Ref: "v3.2.0"}
	resolved := map[string]actionTarget{
		"Mic92/hestia@v3.1.0":     target,
		"actions/checkout@v7.0.1": {SHA: "3d3c42e5aac5ba805825da76410c181273ba90b1", Ref: "v7.0.1"},
	}

	got, err := rewriteActions(t.Context(), pinned, resolved, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}

	want := "    - uses: Mic92/hestia@" + target.SHA + " # v3.2.0\n" +
		"      with:\n" +
		"        version: v3.2.0\n" +
		"    - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1\n"
	if got != want {
		t.Errorf("rewrite =\n%s\nwant\n%s", got, want)
	}
}

func TestIsVersionRef(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{in: "v6.0.1", want: true},
		{in: "v3.22", want: true},
		{in: "main"},
		{in: "master"},
		{in: "validate"},
		{in: "v"},
	}

	for _, test := range tests {
		t.Run(test.in, func(t *testing.T) {
			if got := isVersionRef(test.in); got != test.want {
				t.Errorf("isVersionRef(%q) = %v, want %v", test.in, got, test.want)
			}
		})
	}
}

// The composite setup action pins the Nix installer and every cache; missing
// it would leave half the pins in the repository to rot.
func TestRepoWorkflowFilesIncludeCompositeActions(t *testing.T) {
	files, err := workflowFiles(&repo{Root: "../.."})
	if err != nil {
		t.Fatalf("workflowFiles: %v", err)
	}

	want := map[string]bool{".github/actions/setup/action.yml": false, ".github/workflows/ci.yml": false}

	for _, f := range files {
		if _, ok := want[f]; ok {
			want[f] = true
		}
	}

	for f, found := range want {
		if !found {
			t.Errorf("workflowFiles() is missing %s: %v", f, files)
		}
	}
}
