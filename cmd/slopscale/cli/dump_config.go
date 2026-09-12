package cli

import (
	"fmt"

	"github.com/aislopware/slopscale/hscontrol/conf"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(dumpConfigCmd)
}

var dumpConfigCmd = &cobra.Command{
	Use:    "dumpConfig",
	Short:  "Dump the current config to /etc/slopscale/config.dump.yaml (integration tests only)",
	Hidden: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		err := conf.WriteConfigAs("/etc/slopscale/config.dump.yaml")
		if err != nil {
			return fmt.Errorf("dumping config: %w", err)
		}

		return nil
	},
}
