package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aislopware/slopscale/hscontrol/capver"
	"github.com/creachadair/command"
)

var (
	errUnknownSet    = errors.New("unknown --set value (want must|all)")
	errUnknownFormat = errors.New("unknown --format value (want space|newline|json)")
)

// VersionSet specifies which subset of Tailscale versions to list.
type VersionSet string

const (
	VersionSetMust VersionSet = "must"
	VersionSetAll  VersionSet = "all"
)

// OutputFormat specifies how listed versions are formatted.
type OutputFormat string

const (
	OutputFormatSpace   OutputFormat = "space"
	OutputFormatNewline OutputFormat = "newline"
	OutputFormatJSON    OutputFormat = "json"
)

// ListVersionsConfig holds flags for the list-versions subcommand.
type ListVersionsConfig struct {
	Set     VersionSet   `flag:"set,default=must,Version set: must|all"`
	Exclude string       `flag:"exclude,Comma-separated versions to exclude (e.g. head,unstable)"`
	Format  OutputFormat `flag:"format,default=space,Output format: space|newline|json"`
}

var listVersionsConfig ListVersionsConfig

// Validate verifies that Set and Format are recognized values.
func (c ListVersionsConfig) Validate() error {
	switch c.Set {
	case VersionSetMust, VersionSetAll:
	default:
		return fmt.Errorf("%w: %q", errUnknownSet, c.Set)
	}

	switch c.Format {
	case OutputFormatSpace, OutputFormatNewline, OutputFormatJSON:
	default:
		return fmt.Errorf("%w: %q", errUnknownFormat, c.Format)
	}

	return nil
}

// listVersions prints the Tailscale versions used by integration tests
// in a format CI can shell out to. Mirrors integration/scenario.go
// AllVersions and MustTestVersions: "head" and "unstable" are bare
// tags, releases get a "v" prefix so each entry can be appended to
// "ghcr.io/tailscale/tailscale:" directly.
func listVersions(_ *command.Env) error {
	err := listVersionsConfig.Validate()
	if err != nil {
		return err
	}

	release := capver.TailscaleLatestMajorMinor(capver.SupportedMajorMinorVersions, true)
	all := append([]string{"head", "unstable"}, release...)
	must := append(append([]string{}, all[0:4]...), all[len(all)-2:]...)

	var versions []string

	switch listVersionsConfig.Set {
	case VersionSetMust:
		versions = must
	case VersionSetAll:
		versions = all
	}

	excluded := make(map[string]bool)

	if listVersionsConfig.Exclude != "" {
		for v := range strings.SplitSeq(listVersionsConfig.Exclude, ",") {
			excluded[strings.TrimSpace(v)] = true
		}
	}

	out := make([]string, 0, len(versions))

	for _, v := range versions {
		if excluded[v] {
			continue
		}

		if v != "head" && v != "unstable" {
			v = "v" + v
		}

		out = append(out, v)
	}

	switch listVersionsConfig.Format {
	case OutputFormatSpace:
		fmt.Println(strings.Join(out, " "))
	case OutputFormatNewline:
		for _, v := range out {
			fmt.Println(v)
		}
	case OutputFormatJSON:
		b, err := json.Marshal(out)
		if err != nil {
			return fmt.Errorf("marshalling versions to JSON: %w", err)
		}

		fmt.Println(string(b))
	}

	return nil
}
