package cli

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	clientv1 "github.com/aislopware/slopscale/gen/client/v1"
	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(accessRequestsCmd)

	listAccessRequestsCmd.Flags().
		String("status", "", "Only requests in this status: pending, approved, denied, cancelled")
	listAccessRequestsCmd.Flags().Bool("mine", false, "Only the caller's own requests")
	accessRequestsCmd.AddCommand(listAccessRequestsCmd)

	createAccessRequestCmd.Flags().StringP("group", "g", "", "Group identifier (ID) to join")
	mustMarkRequired(createAccessRequestCmd, "group")
	createAccessRequestCmd.Flags().String("node", "", "Machine identifier (ID); omitted means every machine you own")
	createAccessRequestCmd.Flags().DurationP("duration", "d", time.Hour, "How long the access should last")
	createAccessRequestCmd.Flags().String("reason", "", "Why the access is needed")
	accessRequestsCmd.AddCommand(createAccessRequestCmd)

	for _, c := range []*cobra.Command{approveAccessRequestCmd, denyAccessRequestCmd} {
		c.Flags().Uint64P("identifier", "i", 0, "Access request identifier (ID)")
		mustMarkRequired(c, "identifier")
		c.Flags().String("note", "", "A note for the requester")
	}

	approveAccessRequestCmd.Flags().DurationP("duration", "d", 0, "Grant this long instead of what was asked for")
	accessRequestsCmd.AddCommand(approveAccessRequestCmd)
	accessRequestsCmd.AddCommand(denyAccessRequestCmd)

	cancelAccessRequestCmd.Flags().Uint64P("identifier", "i", 0, "Access request identifier (ID)")
	mustMarkRequired(cancelAccessRequestCmd, "identifier")
	accessRequestsCmd.AddCommand(cancelAccessRequestCmd)
}

var accessRequestsCmd = &cobra.Command{
	Use:     "access-requests",
	Short:   "Ask for temporary access to a group, and decide on the asks",
	Aliases: []string{"access-request", "requests"},
}

var listAccessRequestsCmd = &cobra.Command{
	Use:     cmdList,
	Short:   "List access requests",
	Aliases: []string{"ls"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			params := &clientv1.ListAccessRequestsParams{}

			if status, _ := cmd.Flags().GetString("status"); status != "" {
				params.Status = &status
			}

			if mine, _ := cmd.Flags().GetBool("mine"); mine {
				params.Mine = &mine
			}

			resp, err := client.ListAccessRequestsWithResponse(ctx, params)
			if err != nil {
				return fmt.Errorf("listing access requests: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			requests := resp.JSON200.Requests

			return printListOutput(cmd, requests, func() error {
				rows := make([][]string, 0, len(requests))
				for _, r := range requests {
					rows = append(rows, []string{
						r.Id, r.UserId, deref(r.NodeId), r.GroupId,
						(time.Duration(r.DurationSeconds) * time.Second).String(),
						r.Status, r.DecidedBy, optionalTime(r.ExpiresAt), r.Reason,
					})
				}

				return renderTable(
					[]string{"ID", "User", "Machine", "Group", "Duration", "Status", "Decided by", "Expires", "Reason"},
					rows,
				)
			})
		},
	),
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}

func optionalTime(t *time.Time) string {
	if t == nil {
		return ""
	}

	return t.Format(SlopscaleDateTimeFormat)
}

var createAccessRequestCmd = &cobra.Command{
	Use:     cmdCreate,
	Short:   "Ask to join a group for a while",
	Aliases: []string{"request"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			group, _ := cmd.Flags().GetString("group")
			node, _ := cmd.Flags().GetString("node")
			duration, _ := cmd.Flags().GetDuration("duration")
			reason, _ := cmd.Flags().GetString("reason")

			body := clientv1.CreateAccessRequestJSONRequestBody{
				GroupId:         group,
				DurationSeconds: int64(duration / time.Second),
				Reason:          &reason,
			}

			if node != "" {
				body.NodeId = &node
			}

			resp, err := client.CreateAccessRequestWithResponse(ctx, body)
			if err != nil {
				return fmt.Errorf("filing access request: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.Request, "Access request filed")
		},
	),
}

var approveAccessRequestCmd = &cobra.Command{
	Use:   "approve",
	Short: "Approve an access request",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			note, _ := cmd.Flags().GetString("note")
			duration, _ := cmd.Flags().GetDuration("duration")

			body := clientv1.ApproveAccessRequestJSONRequestBody{Note: &note}

			if duration > 0 {
				seconds := int64(duration / time.Second)
				body.DurationSeconds = &seconds
			}

			resp, err := client.ApproveAccessRequestWithResponse(ctx, strconv.FormatUint(identifier, util.Base10), body)
			if err != nil {
				return fmt.Errorf("approving access request: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.Request, "Access request approved")
		},
	),
}

var denyAccessRequestCmd = &cobra.Command{
	Use:   "deny",
	Short: "Deny an access request",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			note, _ := cmd.Flags().GetString("note")

			resp, err := client.DenyAccessRequestWithResponse(
				ctx,
				strconv.FormatUint(identifier, util.Base10),
				clientv1.DenyAccessRequestJSONRequestBody{Note: &note},
			)
			if err != nil {
				return fmt.Errorf("denying access request: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.Request, "Access request denied")
		},
	),
}

var cancelAccessRequestCmd = &cobra.Command{
	Use:     "cancel",
	Short:   "Withdraw a pending request, or delete a decided one",
	Aliases: []string{cmdDelete, aliasDel},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")

			resp, err := client.CancelAccessRequestWithResponse(ctx, strconv.FormatUint(identifier, util.Base10))
			if err != nil {
				return fmt.Errorf("cancelling access request: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(
				cmd,
				map[string]string{colResult: "Access request cancelled"},
				"Access request cancelled",
			)
		},
	),
}
