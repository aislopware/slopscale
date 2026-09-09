package cli

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	clientv1 "github.com/aislopware/slopscale/gen/client/v1"
	"github.com/spf13/cobra"
)

// defaultInviteExpiry is how long a link works for unless --expiry says
// otherwise; the server accepts 720h at most.
const defaultInviteExpiry = "168h"

func init() {
	rootCmd.AddCommand(invitesCmd)

	invitesCmd.AddCommand(listInvitesCmd)

	createInviteCmd.Flags().StringP("email", "e", "", "Address to invite")
	mustMarkRequired(createInviteCmd, "email")
	createInviteCmd.Flags().StringP("role", "r", "",
		"Role the invited user is created with: admin, network-admin, it-admin, auditor or member")
	createInviteCmd.Flags().StringSliceP("group", "g", []string{}, "Group ID the invited user joins (repeatable)")
	createInviteCmd.Flags().String("expiry", defaultInviteExpiry, "How long the link works for; 720h at most")
	invitesCmd.AddCommand(createInviteCmd)

	invitesCmd.AddCommand(deleteInviteCmd)

	resendInviteCmd.Flags().String("expiry", "",
		"How long the new link works for; the default of "+defaultInviteExpiry+" when unset, 720h at most")
	invitesCmd.AddCommand(resendInviteCmd)
}

var invitesCmd = &cobra.Command{
	Use:     "invites",
	Short:   "Manage invitations, the links that let someone sign in for the first time",
	Aliases: []string{"invite"},
}

var listInvitesCmd = &cobra.Command{
	Use:     cmdList,
	Short:   "List invitations",
	Aliases: []string{"ls", cmdShow},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.ListInvitesWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("listing invites: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			invites := resp.JSON200.Invites

			return printListOutput(cmd, invites, func() error {
				return renderTable(
					[]string{"ID", "Email", "Role", "Groups", "Expires", "Status", "Created by"},
					inviteRows(invites),
				)
			})
		},
	),
}

var createInviteCmd = &cobra.Command{
	Use:     cmdCreate,
	Short:   "Invite someone by email",
	Aliases: []string{"c", cmdNew},
	Long: `Creates an invitation and prints the link to send. The link is shown once: the
server keeps only a hash of the token. When notifications.smtp is configured the
link is also mailed to the address, and the output says whether that worked; a
mail that cannot be sent does not fail the invitation.`,
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			email, _ := cmd.Flags().GetString("email")
			body := clientv1.CreateInviteJSONRequestBody{Email: email}

			if role, _ := cmd.Flags().GetString("role"); role != "" {
				body.Role = &role
			}

			if groups, _ := cmd.Flags().GetStringSlice("group"); len(groups) > 0 {
				body.GroupIds = &groups
			}

			if expiry, _ := cmd.Flags().GetString("expiry"); expiry != "" {
				body.Expiry = &expiry
			}

			resp, err := client.CreateInviteWithResponse(ctx, body)
			if err != nil {
				return fmt.Errorf("creating invite: %w", err)
			}

			if resp.StatusCode() != http.StatusCreated {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON201, inviteMessage(resp.JSON201))
		},
	),
}

var resendInviteCmd = &cobra.Command{
	Use:   "resend ID",
	Short: "Resend an invitation with a fresh link",
	Long: `Replaces the invitation's token, so the link sent before stops working, and
prints and mails the new one. The email, role and groups stay as they were.`,
	Args: cobra.ExactArgs(1),
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, args []string) error {
			body := clientv1.ResendInviteJSONRequestBody{}

			if expiry, _ := cmd.Flags().GetString("expiry"); expiry != "" {
				body.Expiry = &expiry
			}

			resp, err := client.ResendInviteWithResponse(ctx, args[0], body)
			if err != nil {
				return fmt.Errorf("resending invite: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200, inviteMessage(resp.JSON200))
		},
	),
}

var deleteInviteCmd = &cobra.Command{
	Use:     cmdDelete + " ID",
	Short:   "Delete an invitation, so its link stops working",
	Aliases: []string{aliasDel, "revoke"},
	Args:    cobra.ExactArgs(1),
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, args []string) error {
			resp, err := client.DeleteInviteWithResponse(ctx, args[0])
			if err != nil {
				return fmt.Errorf("deleting invite: %w", err)
			}

			if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusNoContent {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, map[string]string{colResult: "Invitation deleted"}, "Invitation deleted")
		},
	),
}

// inviteMessage is the human-readable output of a create or a resend: the link
// on its own line, so it can be copied, and whether it was also mailed.
func inviteMessage(out *clientv1.InviteOutputBody) string {
	lines := []string{
		fmt.Sprintf("Invitation for %s expires %s",
			out.Invite.Email, out.Invite.ExpiresAt.Format(SlopscaleDateTimeFormat)),
		out.Url,
	}

	switch {
	case out.EmailSent:
		lines = append(lines, "Mailed to "+out.Invite.Email)
	case out.EmailError != nil && *out.EmailError != "":
		lines = append(lines, "Not mailed: "+*out.EmailError)
	default:
		lines = append(lines, "Not mailed; send the link yourself")
	}

	return strings.Join(lines, "\n")
}

func inviteRows(invites []clientv1.Invite) [][]string {
	rows := make([][]string, 0, len(invites))

	for _, i := range invites {
		createdBy := "-"
		if i.CreatedBy != nil && *i.CreatedBy != "" {
			createdBy = *i.CreatedBy
		}

		rows = append(rows, []string{
			i.Id,
			i.Email,
			i.Role,
			orDash(strings.Join(i.GroupIds, ", ")),
			ColourTime(i.ExpiresAt),
			inviteStatus(i),
			createdBy,
		})
	}

	return rows
}

// inviteStatus is what an administrator acts on: a pending invitation can be
// resent, an expired one is gone, and an accepted one became a user.
func inviteStatus(invite clientv1.Invite) string {
	switch {
	case invite.Accepted:
		return "accepted"
	case invite.Expired:
		return "expired"
	default:
		return "pending"
	}
}
