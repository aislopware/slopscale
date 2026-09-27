package main

import (
	"fmt"
	"strings"
)

// renderReport writes what the run moved and dropped. It is the starting point
// of the review, not its result: every moved item still has its changes read
// for what slopscale should drop, change or adopt before a pull request opens.
func renderReport(results []result, gate string) string {
	var sb strings.Builder

	sb.WriteString("Version bump, applied locally.\n\n")
	sb.WriteString("| Area | Result | Change |\n|---|---|---|\n")

	for _, res := range results {
		summary := res.Change.Summary
		if summary == "" {
			summary = res.Reason
		}

		fmt.Fprintf(&sb, "| `%s` | %s | %s |\n", res.Area, res.State, cell(summary))
	}

	writeDetails(&sb, results)
	writeDropped(&sb, results)

	sb.WriteString("\n")
	sb.WriteString(gateNote(gate))
	sb.WriteString("\nBefore opening a pull request, read each moved item's release notes between the two " +
		"versions and act on what slopscale should drop, change or adopt.\n")

	return sb.String()
}

func writeDetails(sb *strings.Builder, results []result) {
	var opened bool

	for _, res := range results {
		if len(res.Change.Detail) == 0 {
			continue
		}

		if !opened {
			sb.WriteString("\n<details><summary>What moved</summary>\n\n")

			opened = true
		}

		fmt.Fprintf(sb, "**%s**\n\n", res.Area)

		for _, line := range res.Change.Detail {
			fmt.Fprintf(sb, "- %s\n", line)
		}

		sb.WriteString("\n")
	}

	if opened {
		sb.WriteString("</details>\n")
	}
}

func writeDropped(sb *strings.Builder, results []result) {
	var opened bool

	open := func() {
		if !opened {
			sb.WriteString("\n### Dropped\n\n")

			opened = true
		}
	}

	for _, res := range results {
		if res.State == stateDropped {
			open()
			fmt.Fprintf(sb, "- **`%s`** — %s\n", res.Area, res.Reason)
			writeLog(sb, res.Log)
		}

		for _, d := range res.Change.Drops {
			open()
			fmt.Fprintf(sb, "- **`%s`** (in `%s`) — %s\n", d.Name, res.Area, d.Reason)
			writeLog(sb, d.Log)
		}
	}
}

func writeLog(sb *strings.Builder, log string) {
	if log == "" {
		return
	}

	sb.WriteString("\n  ```\n")

	for line := range strings.Lines(log) {
		fmt.Fprintf(sb, "  %s", line)
	}

	sb.WriteString("\n  ```\n")
}

// gateNote says what the run already checked, so the reader knows what is
// left to the pull request's CI.
func gateNote(gate string) string {
	switch gate {
	case gateFull:
		return "The nix checks, the console and docs checks and the tailscale builder images passed; " +
			"servertest, end-to-end and integration are left to the pull request's CI.\n"
	case gateQuick:
		return "Only `nix build .#checks.<system>.build` ran; everything else is left to the pull request's CI.\n"
	default:
		return "No gate ran.\n"
	}
}

// cell keeps a markdown table cell from breaking the table.
func cell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")

	const maxCell = 160

	runes := []rune(s)
	if len(runes) > maxCell {
		s = string(runes[:maxCell]) + "…"
	}

	if s == "" {
		return "—"
	}

	return s
}
