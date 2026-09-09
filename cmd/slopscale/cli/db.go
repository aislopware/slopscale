package cli

import (
	"fmt"
	"os"
	"strconv"

	"github.com/aislopware/slopscale/hscontrol/db"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/spf13/cobra"
)

const colPath = "Path"

func init() {
	rootCmd.AddCommand(dbCmd)
	dbCmd.AddCommand(dbBackupCmd)
	dbBackupCmd.Flags().
		String("out", "", "Where the backup is written. The default is <database>.backup-<timestamp>")
	dbCmd.AddCommand(dbVerifyCmd)
	dbCmd.AddCommand(dbRestoreCmd)
	dbRestoreCmd.Flags().Bool("yes", false, "Restore without asking for confirmation")
}

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Back up, check and restore the database",
	Long: `Works on the SQLite database the configuration file points at, without
talking to the server: a backup can be taken while slopscale runs, a
restore needs it stopped. PostgreSQL is backed up with pg_dump.`,
}

var dbBackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Write a copy of the database",
	Long: `Copies the database through SQLite itself, so the result is a single
consistent file even when the server is writing to it. Copying the
database file with cp is not safe while slopscale runs.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := types.LoadServerConfig()
		if err != nil {
			return fmt.Errorf("loading configuration: %w", err)
		}

		dest, _ := cmd.Flags().GetString("out")
		if dest == "" {
			dest = db.DefaultBackupPath(cfg)
		}

		err = db.Backup(cfg, dest)
		if err != nil {
			return fmt.Errorf("backing up the database: %w", err)
		}

		info, err := os.Stat(dest)
		if err != nil {
			return fmt.Errorf("reading %s: %w", dest, err)
		}

		size := strconv.FormatInt(info.Size(), util.Base10)

		return printListOutput(cmd, map[string]string{colPath: dest, "Bytes": size}, func() error {
			return renderTable([]string{colPath, "Bytes"}, [][]string{{dest, size}})
		})
	},
}

var dbVerifyCmd = &cobra.Command{
	Use:   "verify PATH",
	Short: "Check that a backup is a usable database",
	Long: `Runs SQLite's integrity check on the file, confirms it carries the
slopscale migration history and compares the version that last wrote it
with this binary's.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		err := db.Verify(args[0])
		if err != nil {
			return fmt.Errorf("verifying %s: %w", args[0], err)
		}

		return printListOutput(cmd, map[string]string{colPath: args[0], colResult: "Valid"}, func() error {
			return renderTable([]string{colPath, colResult}, [][]string{{args[0], "Valid"}})
		})
	},
}

var dbRestoreCmd = &cobra.Command{
	Use:   "restore PATH",
	Short: "Replace the database with a backup",
	Long: `Verifies the backup, moves the current database aside as
<database>.pre-restore-<timestamp> and puts the backup in its place. Stop
slopscale first: it keeps the database open and would carry on with the
one it started with.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := types.LoadServerConfig()
		if err != nil {
			return fmt.Errorf("loading configuration: %w", err)
		}

		src := args[0]

		yes, _ := cmd.Flags().GetBool("yes")
		if !yes && !confirmAction(cmd, fmt.Sprintf(
			"Do you want to replace the database at %s with %s? Slopscale must be stopped.",
			cfg.Database.Sqlite.Path, src,
		)) {
			return printOutput(cmd, map[string]string{colResult: "Database not restored"}, "Database not restored")
		}

		err = db.Restore(cfg, src)
		if err != nil {
			return fmt.Errorf("restoring the database: %w", err)
		}

		return printListOutput(
			cmd,
			map[string]string{colPath: cfg.Database.Sqlite.Path, colResult: "Restored"},
			func() error {
				return renderTable(
					[]string{colPath, colResult},
					[][]string{{cfg.Database.Sqlite.Path, "Restored from " + src}},
				)
			},
		)
	},
}
