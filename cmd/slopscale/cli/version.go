package cli

import (
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(versionCmd)
	versionCmd.Flags().
		StringP("output", "o", "", "Output format. Empty for human-readable, 'json', 'json-line' or 'yaml'")
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version",
	Long:  "The version of slopscale.",
	RunE: func(cmd *cobra.Command, _ []string) error {
		info := types.GetVersionInfo()

		return printOutput(cmd, info, info.String())
	},
}
