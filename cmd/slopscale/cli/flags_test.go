package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

// Every command must be able to merge the root's persistent flags into its
// own: a local shorthand that reuses -c (config) or -o (output) makes cobra
// panic the moment the command is looked up, so the command cannot run at
// all. The other tests build their flags on fresh commands and never see it.
func TestEveryCommandMergesPersistentFlags(t *testing.T) {
	t.Parallel()

	var walk func(cmd *cobra.Command)

	walk = func(cmd *cobra.Command) {
		require.NotPanics(t, func() { cmd.LocalFlags() }, "command %q", cmd.CommandPath())

		for _, sub := range cmd.Commands() {
			walk(sub)
		}
	}

	walk(rootCmd)
}
