package main

import (
	"os"
	"strings"
	"testing"
)

func mustRead(t *testing.T, path string) []byte {
	t.Helper()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}

	return b
}

// The report is where the review starts, so a dropped area must say why and
// the report must end by asking for the review rather than implying it is done.
func TestReportNamesDropsAndAsksForReview(t *testing.T) {
	results := []result{
		{Area: "flake", State: stateApplied, Commit: "abc"},
		{Area: "gomod", State: stateDropped, Reason: "`go build ./...` failed"},
	}

	got := renderReport(results, gateFull)

	for _, want := range []string{"| `gomod` | dropped |", "`go build ./...` failed", "drop, change or adopt"} {
		if !strings.Contains(got, want) {
			t.Errorf("report lacks %q:\n%s", want, got)
		}
	}
}

// A summary containing a pipe would otherwise split the markdown table.
func TestCellEscapesTableBreakers(t *testing.T) {
	got := cell("a | b\nc")
	if strings.ContainsAny(got, "|\n") && !strings.Contains(got, `\|`) {
		t.Errorf("cell = %q, still breaks the table", got)
	}

	if cell("") != "—" {
		t.Errorf("cell(\"\") = %q, want an em dash", cell(""))
	}
}

func TestSelector(t *testing.T) {
	tests := []struct {
		name string
		only string
		skip string
		area string
		want bool
	}{
		{name: "default runs everything", area: "flake", want: true},
		{name: "explicit selection", only: "flake,gomod", area: "flake", want: true},
		{name: "not selected", only: "flake", area: "gomod"},
		{name: "skip wins over selection", only: "flake", skip: "flake", area: "flake"},
		{name: "whitespace tolerated", only: " flake , gomod ", area: "gomod", want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := selector(test.only, test.skip)(test.area); got != test.want {
				t.Errorf("selector(%q, %q)(%q) = %v, want %v", test.only, test.skip, test.area, got, test.want)
			}
		})
	}
}

// Detail lines are rendered as-is so a compare link stays a link. Wrapping
// them in backticks, the way the table cells are, would turn every entry in
// "What moved" back into text nobody can click.
func TestDetailKeepsCompareLinks(t *testing.T) {
	link := "github.com/spf13/cobra v1.8.0 -> " +
		"[v1.9.0](https://github.com/spf13/cobra/compare/v1.8.0...v1.9.0)"

	var sb strings.Builder

	writeDetails(&sb, []result{{
		Area:   "gomod",
		Change: change{Detail: []string{link}},
	}})

	if got := sb.String(); !strings.Contains(got, "- "+link+"\n") {
		t.Errorf("writeDetails() did not render the link unchanged:\n%s", got)
	}
}
