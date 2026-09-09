package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func init() {
	rootCmd.AddCommand(dumpConfigCmd)
}

var dumpConfigCmd = &cobra.Command{
	Use:    "dumpConfig",
	Short:  "Dump the current config to /etc/slopscale/config.dump.yaml (integration tests only)",
	Hidden: true,
	RunE: func(_ *cobra.Command, _ []string) error {
		err := viper.WriteConfigAs("/etc/slopscale/config.dump.yaml")
		if err != nil {
			return fmt.Errorf("dumping config: %w", err)
		}

		return nil
	},
}
