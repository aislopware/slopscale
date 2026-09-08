package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/juanfont/headscale/hscontrol/util"
	"github.com/juanfont/headscale/hscontrol/util/zlog/zf"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

// CLI user errors.
var (
	errFlagRequired       = errors.New("--name or --identifier flag is required")
	errMultipleUsersMatch = errors.New("multiple users match query, specify an ID")
)

func usernameAndIDFlag(cmd *cobra.Command) {
	cmd.Flags().Int64P("identifier", "i", -1, "User identifier (ID)")
	cmd.Flags().StringP("name", "n", "", "Username")
}

// usernameAndIDFromFlag returns the username and ID from the flags of the command.
func usernameAndIDFromFlag(cmd *cobra.Command) (uint64, string, error) {
	username, _ := cmd.Flags().GetString("name")

	identifier, _ := cmd.Flags().GetInt64("identifier")
	if username == "" && identifier < 0 {
		return 0, "", errFlagRequired
	}

	// Normalise unset/negative identifiers to 0 so the uint64
	// conversion does not produce a bogus large value.
	identifier = max(identifier, 0)

	return uint64(identifier), username, nil
}

// resolveSingleUser resolves exactly one user from the --name/--id flags,
// returning the identifier of the matched user and the user itself.
func resolveSingleUser(
	ctx context.Context,
	client *clientv1.ClientWithResponses,
	cmd *cobra.Command,
) (string, *clientv1.User, error) {
	id, username, err := usernameAndIDFromFlag(cmd)
	if err != nil {
		return "", nil, err
	}

	params := &clientv1.ListUsersParams{}
	if username != "" {
		params.Name = &username
	}

	if id != 0 {
		idStr := strconv.FormatUint(id, util.Base10)
		params.Id = &idStr
	}

	resp, err := client.ListUsersWithResponse(ctx, params)
	if err != nil {
		return "", nil, fmt.Errorf("listing users: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return "", nil, apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
	}

	users := resp.JSON200.Users
	if len(users) != 1 {
		return "", nil, errMultipleUsersMatch
	}

	return users[0].Id, &users[0], nil
}

func init() {
	rootCmd.AddCommand(userCmd)
	userCmd.AddCommand(createUserCmd)
	createUserCmd.Flags().StringP("display-name", "d", "", "Display name")
	createUserCmd.Flags().StringP("email", "e", "", "Email")
	createUserCmd.Flags().StringP("picture-url", "p", "", "Profile picture URL")
	userCmd.AddCommand(listUsersCmd)
	usernameAndIDFlag(listUsersCmd)
	listUsersCmd.Flags().StringP("email", "e", "", "Email")
	userCmd.AddCommand(destroyUserCmd)
	usernameAndIDFlag(destroyUserCmd)
	userCmd.AddCommand(renameUserCmd)
	usernameAndIDFlag(renameUserCmd)
	renameUserCmd.Flags().StringP("new-name", "r", "", "New username")
	mustMarkRequired(renameUserCmd, "new-name")
	userCmd.AddCommand(setUserCmd)
	usernameAndIDFlag(setUserCmd)
	setUserCmd.Flags().StringP("display-name", "d", "", "Display name; an empty value clears it")
	setUserCmd.Flags().StringP("email", "e", "", "Email; an empty value clears it")
	setUserCmd.Flags().StringP("picture-url", "p", "", "Profile picture URL; an empty value clears it")
	userCmd.AddCommand(setUserRoleCmd)
	usernameAndIDFlag(setUserRoleCmd)
	userCmd.AddCommand(signOutUserCmd)
	usernameAndIDFlag(signOutUserCmd)
	userCmd.AddCommand(approveUserCmd)
	usernameAndIDFlag(approveUserCmd)
	approveUserCmd.Flags().Bool("revoke", false, "Withdraw the approval instead of granting it")
	setUserRoleCmd.Flags().StringP("role", "r", "", "Role: owner, admin, network-admin, it-admin, auditor or member")
	mustMarkRequired(setUserRoleCmd, "role")
}

var userCmd = &cobra.Command{
	Use:     "users",
	Short:   "Manage users",
	Aliases: []string{"user"},
}

var createUserCmd = &cobra.Command{
	Use:     "create NAME",
	Short:   "Create a user",
	Aliases: []string{"c", cmdNew},
	Args: func(_ *cobra.Command, args []string) error {
		if len(args) < 1 {
			return errMissingParameter
		}

		return nil
	},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, args []string) error {
			userName := args[0]

			request := clientv1.CreateUserJSONRequestBody{Name: &userName}

			if displayName, _ := cmd.Flags().GetString("display-name"); displayName != "" {
				request.DisplayName = &displayName
			}

			if email, _ := cmd.Flags().GetString("email"); email != "" {
				request.Email = &email
			}

			if pictureURL, _ := cmd.Flags().GetString("picture-url"); pictureURL != "" {
				_, err := url.Parse(pictureURL)
				if err != nil {
					return fmt.Errorf("invalid picture URL: %w", err)
				}

				request.PictureUrl = &pictureURL
			}

			log.Trace().Interface(zf.Request, request).Msg("sending CreateUser request")

			resp, err := client.CreateUserWithResponse(ctx, request)
			if err != nil {
				return fmt.Errorf("creating user: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.User, "User created")
		},
	),
}

var destroyUserCmd = &cobra.Command{
	Use:     "destroy --identifier ID or --name NAME",
	Short:   "Delete a user",
	Aliases: []string{cmdDelete},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			_, user, err := resolveSingleUser(ctx, client, cmd)
			if err != nil {
				return err
			}

			if !confirmAction(cmd, fmt.Sprintf(
				"Do you want to remove the user %q (%s) and any associated preauthkeys?",
				user.Name, user.Id,
			)) {
				return printOutput(cmd, map[string]string{colResult: "User not destroyed"}, "User not destroyed")
			}

			resp, err := client.DeleteUserWithResponse(ctx, user.Id)
			if err != nil {
				return fmt.Errorf("destroying user: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200, "User destroyed")
		},
	),
}

var listUsersCmd = &cobra.Command{
	Use:     cmdList,
	Short:   "List all the users",
	Aliases: []string{"ls", cmdShow},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			params := &clientv1.ListUsersParams{}

			id, _ := cmd.Flags().GetInt64("identifier")
			username, _ := cmd.Flags().GetString("name")
			email, _ := cmd.Flags().GetString("email")

			// filter by one param at most
			switch {
			case id > 0:
				idStr := strconv.FormatInt(id, util.Base10)
				params.Id = &idStr
			case username != "":
				params.Name = &username
			case email != "":
				params.Email = &email
			}

			resp, err := client.ListUsersWithResponse(ctx, params)
			if err != nil {
				return fmt.Errorf("listing users: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			users := resp.JSON200.Users

			return printListOutput(cmd, users, func() error {
				rows := make([][]string, 0, len(users))
				for _, user := range users {
					rows = append(
						rows,
						[]string{
							user.Id,
							user.DisplayName,
							user.Name,
							user.Email,
							user.Role,
							approvedLabel(user.Approved),
							user.CreatedAt.Format(HeadscaleDateTimeFormat),
						},
					)
				}

				return renderTable([]string{"ID", "Name", "Username", "Email", "Role", "Approved", colCreated}, rows)
			})
		},
	),
}

var renameUserCmd = &cobra.Command{
	Use:     "rename",
	Short:   "Rename a user",
	Aliases: []string{"mv"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			userID, _, err := resolveSingleUser(ctx, client, cmd)
			if err != nil {
				return err
			}

			newName, _ := cmd.Flags().GetString("new-name")

			resp, err := client.RenameUserWithResponse(ctx, userID, newName)
			if err != nil {
				return fmt.Errorf("renaming user: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.User, "User renamed")
		},
	),
}

var setUserCmd = &cobra.Command{
	Use:   "set --identifier ID or --name NAME",
	Short: "Set a user's display name, email or profile picture",
	Long: `
Changes the profile the clients show for a user. A flag left out keeps its
value and a flag set to an empty string clears it. A user who logs in through
OIDC gets the values from the provider again at the next login.`,
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			userID, _, err := resolveSingleUser(ctx, client, cmd)
			if err != nil {
				return err
			}

			var request clientv1.UpdateUserJSONRequestBody

			changed := false

			if cmd.Flags().Changed("display-name") {
				displayName, _ := cmd.Flags().GetString("display-name")
				request.DisplayName = &displayName
				changed = true
			}

			if cmd.Flags().Changed("email") {
				email, _ := cmd.Flags().GetString("email")
				request.Email = &email
				changed = true
			}

			if cmd.Flags().Changed("picture-url") {
				pictureURL, _ := cmd.Flags().GetString("picture-url")

				_, parseErr := url.Parse(pictureURL)
				if parseErr != nil {
					return fmt.Errorf("invalid picture URL: %w", parseErr)
				}

				request.PictureUrl = &pictureURL
				changed = true
			}

			if !changed {
				return fmt.Errorf(
					"%w: give at least one of --display-name, --email or --picture-url",
					errMissingParameter,
				)
			}

			resp, err := client.UpdateUserWithResponse(ctx, userID, request)
			if err != nil {
				return fmt.Errorf("updating user: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.User, "User updated")
		},
	),
}

var signOutUserCmd = &cobra.Command{
	Use:   "sign-out --identifier ID or --name NAME",
	Short: "Sign a user out of every browser",
	Long: `Ends every console session of the user, so each browser is asked to sign in
again on its next request. This is what to reach for when a laptop goes
missing; nothing else about the account changes.`,
	Aliases: []string{"signout", "logout"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			userID, _, err := resolveSingleUser(ctx, client, cmd)
			if err != nil {
				return err
			}

			resp, err := client.EndUserSessionsWithResponse(ctx, userID)
			if err != nil {
				return fmt.Errorf("ending the user's sessions: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200, endedSessionsMessage(resp.JSON200.Ended))
		},
	),
}

// endedSessionsMessage reports how many sessions a sign-out ended.
func endedSessionsMessage(ended int64) string {
	if ended == 1 {
		return "Ended 1 session"
	}

	return fmt.Sprintf("Ended %d sessions", ended)
}

var approveUserCmd = &cobra.Command{
	Use:   "approve --identifier ID or --name NAME",
	Short: "Approve a user that is waiting for users approval",
	Long: `
Admits a user created by OIDC login while users approval was on. Until then
the user cannot register nodes. Use --revoke to withdraw the approval again,
which also withdraws every node the user owns.`,
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			userID, _, err := resolveSingleUser(ctx, client, cmd)
			if err != nil {
				return err
			}

			revoke, _ := cmd.Flags().GetBool("revoke")
			approved := !revoke

			resp, err := client.ApproveUserWithResponse(ctx, userID, clientv1.ApproveUserJSONRequestBody{
				Approved: &approved,
			})
			if err != nil {
				return fmt.Errorf("approving user: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			msg := "User approved"
			if revoke {
				msg = "User approval revoked"
			}

			return printOutput(cmd, resp.JSON200.User, msg)
		},
	),
}

var setUserRoleCmd = &cobra.Command{
	Use:   "set-role --identifier ID or --name NAME --role ROLE",
	Short: "Set a user's role",
	Long: `Sets the role that bounds what the user may do through the admin API and
console. Assigning owner transfers ownership, and the previous owner becomes an
admin. The owner's role changes only by such a transfer.`,
	Aliases: []string{"role"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			userID, _, err := resolveSingleUser(ctx, client, cmd)
			if err != nil {
				return err
			}

			role, _ := cmd.Flags().GetString("role")

			resp, err := client.SetUserRoleWithResponse(ctx, userID, clientv1.SetUserRoleJSONRequestBody{Role: role})
			if err != nil {
				return fmt.Errorf("setting user role: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.User, "User role set to "+resp.JSON200.User.Role)
		},
	),
}

// approvedLabel renders a user's approval for the table: pending users are
// the ones an administrator has to act on.
func approvedLabel(approved bool) string {
	if approved {
		return "yes"
	}

	return "pending"
}
