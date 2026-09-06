package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(settingsCmd)
	settingsCmd.AddCommand(getSettingsCmd)
	settingsCmd.AddCommand(setSettingsCmd)
	setSettingsCmd.Flags().Bool("devices-approval", false,
		"Require an administrator to approve new nodes (unless registered with a preauthorized key)")
	setSettingsCmd.Flags().Bool("users-approval", false,
		"Require an administrator to approve users created by OIDC login")
}

var errNoSettingGiven = errors.New("give at least one of --devices-approval or --users-approval")

var settingsCmd = &cobra.Command{
	Use:   "settings",
	Short: "Manage the tailnet-wide settings",
}

var getSettingsCmd = &cobra.Command{
	Use:     "get",
	Short:   "Show the tailnet-wide settings",
	Aliases: []string{"show", cmdList},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.GetSettingsWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("getting settings: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printSettings(cmd, resp.JSON200)
		},
	),
}

var setSettingsCmd = &cobra.Command{
	Use:   "set",
	Short: "Change tailnet-wide settings",
	Long: `Changes the given settings; the others keep their value. Switching device or
users approval off approves every node or user that was waiting.`,
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			var body clientv1.UpdateSettingsJSONRequestBody

			if cmd.Flags().Changed("devices-approval") {
				on, _ := cmd.Flags().GetBool("devices-approval")
				body.DevicesApprovalOn = &on
			}

			if cmd.Flags().Changed("users-approval") {
				on, _ := cmd.Flags().GetBool("users-approval")
				body.UsersApprovalOn = &on
			}

			if body.DevicesApprovalOn == nil && body.UsersApprovalOn == nil {
				return errNoSettingGiven
			}

			resp, err := client.UpdateSettingsWithResponse(ctx, body)
			if err != nil {
				return fmt.Errorf("updating settings: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printSettings(cmd, resp.JSON200)
		},
	),
}

func printSettings(cmd *cobra.Command, settings *clientv1.Settings) error {
	return printListOutput(cmd, settings, func() error {
		return renderTable(
			[]string{"Setting", "Value"},
			[][]string{
				{"Device approval", onOff(settings.DevicesApprovalOn)},
				{"Users approval", onOff(settings.UsersApprovalOn)},
			},
		)
	})
}

func onOff(on bool) string {
	if on {
		return "on"
	}

	return "off"
}
