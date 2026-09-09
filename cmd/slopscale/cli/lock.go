package cli

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	clientv1 "github.com/aislopware/slopscale/gen/client/v1"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(lockCmd)
	lockCmd.AddCommand(statusLockCmd)
	lockCmd.AddCommand(disableLockCmd)
}

var lockCmd = &cobra.Command{
	Use:   "lock",
	Short: "Show or switch off tailnet lock",
	Long: `Tailnet lock is switched on from a node with "tailscale lock init". This
command only reads its state and switches it off with the support secret.`,
}

var statusLockCmd = &cobra.Command{
	Use:     "status",
	Short:   "Show tailnet lock status",
	Aliases: []string{"show", "get"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.GetTailnetLockWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("getting tailnet lock: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printTailnetLock(cmd, resp.JSON200)
		},
	),
}

var disableLockCmd = &cobra.Command{
	Use:   "disable",
	Short: "Switch off tailnet lock",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.DisableTailnetLockWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("disabling tailnet lock: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printTailnetLock(cmd, resp.JSON200)
		},
	),
}

func printTailnetLock(cmd *cobra.Command, lock *clientv1.TailnetLock) error {
	return printListOutput(cmd, lock, func() error {
		return printTailnetLockHuman(lock)
	})
}

func printTailnetLockHuman(lock *clientv1.TailnetLock) error {
	fmt.Printf("Tailnet lock: %s\n", onOff(lock.Enabled))

	if lock.Enabled && lock.Head != "" {
		fmt.Printf("Head: %s\n", lock.Head)
	}

	if len(lock.Keys) > 0 {
		fmt.Println("Trusted keys:")

		for _, k := range lock.Keys {
			fmt.Printf("  %s (votes %d)\n", k.Public, k.Votes)
		}
	}

	fmt.Printf("Signed nodes: %d\n", len(lock.SignedNodeIds))

	unsigned := "none"
	if len(lock.UnsignedNodeIds) > 0 {
		unsigned = strings.Join(lock.UnsignedNodeIds, ", ")
	}

	fmt.Printf("Unsigned nodes: %s\n", unsigned)

	supportDisablement := "not recorded"
	if lock.SupportDisablementAvailable {
		supportDisablement = "available"
	}

	fmt.Printf("Support disablement: %s\n", supportDisablement)

	if lock.EnabledAt != nil && !lock.EnabledAt.IsZero() {
		fmt.Printf("Enabled at: %s\n", lock.EnabledAt.Format(SlopscaleDateTimeFormat))
	}

	if lock.DisabledAt != nil && !lock.DisabledAt.IsZero() {
		fmt.Printf("Disabled at: %s\n", lock.DisabledAt.Format(SlopscaleDateTimeFormat))
	}

	return nil
}
