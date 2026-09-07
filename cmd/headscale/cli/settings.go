package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

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
	setSettingsCmd.Flags().Bool("posture-identity", false,
		"Ask clients for their hardware serial numbers, for node:serialNumber posture checks")
	setSettingsCmd.Flags().Int64("key-expiry-days", 0,
		"Cap node key expiry at this many days after a login; 0 leaves the config file and the client in charge")
	setSettingsCmd.Flags().StringSlice("ssh-recorders", nil,
		"Default SSH session recorders, tags or addresses, for rules that name none; an empty string clears them")
	setSettingsCmd.Flags().Bool("ssh-recording-enforce", false,
		"Reject SSH sessions that cannot reach a default recorder")
}

var errNoSettingGiven = errors.New(
	"give at least one of --devices-approval, --users-approval, --posture-identity, --key-expiry-days, " +
		"--ssh-recorders or --ssh-recording-enforce",
)

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

			if cmd.Flags().Changed("posture-identity") {
				on, _ := cmd.Flags().GetBool("posture-identity")
				body.PostureIdentityOn = &on
			}

			if cmd.Flags().Changed("key-expiry-days") {
				days, _ := cmd.Flags().GetInt64("key-expiry-days")
				body.KeyExpiryDays = &days
			}

			if cmd.Flags().Changed("ssh-recorders") {
				recorders, _ := cmd.Flags().GetStringSlice("ssh-recorders")
				recorders = slices.DeleteFunc(recorders, func(r string) bool { return r == "" })
				body.SshRecorders = &recorders
			}

			if cmd.Flags().Changed("ssh-recording-enforce") {
				on, _ := cmd.Flags().GetBool("ssh-recording-enforce")
				body.SshRecordingEnforce = &on
			}

			if body.DevicesApprovalOn == nil && body.UsersApprovalOn == nil && body.KeyExpiryDays == nil &&
				body.PostureIdentityOn == nil && body.SshRecorders == nil && body.SshRecordingEnforce == nil {
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
				{"Posture identity", onOff(settings.PostureIdentityOn)},
				{"Key expiry", keyExpiryLabel(settings)},
				{"SSH recorders", sshRecordersLabel(settings)},
				{"SSH recording enforced", onOff(settings.SshRecordingEnforce)},
			},
		)
	})
}

// keyExpiryLabel names the cap, or the config file's default when there
// is none.
func keyExpiryLabel(settings *clientv1.Settings) string {
	switch {
	case settings.KeyExpiryDays > 0:
		return fmt.Sprintf("%d days", settings.KeyExpiryDays)
	case settings.DefaultKeyExpiryDays > 0:
		return fmt.Sprintf("client's choice, %d days by default (config file)", settings.DefaultKeyExpiryDays)
	default:
		return "client's choice, never by default (config file)"
	}
}

// sshRecordersLabel lists the default recorders, and the embedded one
// when the server runs it.
func sshRecordersLabel(settings *clientv1.Settings) string {
	recorders := slices.Clone(settings.SshRecorders)
	if settings.EmbeddedRecorder {
		recorders = append(recorders, "embedded recorder")
	}

	if len(recorders) == 0 {
		return "none"
	}

	return strings.Join(recorders, ", ")
}

func onOff(on bool) string {
	if on {
		return "on"
	}

	return "off"
}
