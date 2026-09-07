package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/spf13/cobra"
)

const defaultAuditLimit = 50

func init() {
	rootCmd.AddCommand(auditCmd)
	auditCmd.AddCommand(listAuditCmd)
	listAuditCmd.Flags().Int64P("limit", "l", defaultAuditLimit, "Newest events to show (at most 500)")
	listAuditCmd.Flags().StringP("user", "u", "", "Only events by this user ID")
	listAuditCmd.Flags().StringP("action", "a", "",
		"Only this action, or every action under a prefix ending with a dot (node.)")
	listAuditCmd.Flags().String("target-kind", "", "Only events about this kind of object (node, user, ...)")
	listAuditCmd.Flags().String("target-id", "", "Only events about this object ID (with --target-kind)")
	listAuditCmd.Flags().String("since", "", "Only events at or after this time (RFC 3339 or a duration such as 24h)")
	listAuditCmd.Flags().String("before", "", "Page: only events with an ID below this one")
}

var auditCmd = &cobra.Command{
	Use:   "audit",
	Short: "Read the audit log",
}

var listAuditCmd = &cobra.Command{
	Use:     cmdList,
	Short:   "List audit events, newest first",
	Aliases: []string{"ls", "show"},
	Long: `Lists what was done through the API and the console: who did it, to what,
and how the request ended. The log is paged by ID; pass the last event's ID
as --before to fetch the next page.`,
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			params, err := auditParams(cmd)
			if err != nil {
				return err
			}

			resp, err := client.ListAuditEventsWithResponse(ctx, params)
			if err != nil {
				return fmt.Errorf("listing audit events: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printListOutput(cmd, resp.JSON200, func() error {
				return renderTable(
					[]string{"Time", "Actor", "Action", "Target", "Result", "Detail"},
					auditRows(resp.JSON200.Events),
				)
			})
		},
	),
}

// auditParams turns the flags into query parameters, resolving a relative
// --since against the current time.
func auditParams(cmd *cobra.Command) (*clientv1.ListAuditEventsParams, error) {
	params := &clientv1.ListAuditEventsParams{}

	limit, _ := cmd.Flags().GetInt64("limit")
	params.Limit = &limit

	for flag, dst := range map[string]**string{
		"user":        &params.ActorUserId,
		"action":      &params.Action,
		"target-kind": &params.TargetKind,
		"target-id":   &params.TargetId,
		"before":      &params.Before,
	} {
		value, _ := cmd.Flags().GetString(flag)
		if value != "" {
			*dst = &value
		}
	}

	since, _ := cmd.Flags().GetString("since")
	if since != "" {
		at, err := parseSince(since)
		if err != nil {
			return nil, err
		}

		params.Since = &at
	}

	return params, nil
}

// parseSince accepts an RFC 3339 time or a duration back from now.
func parseSince(value string) (time.Time, error) {
	at, err := time.Parse(time.RFC3339, value)
	if err == nil {
		return at, nil
	}

	back, err := time.ParseDuration(value)
	if err != nil {
		return time.Time{}, fmt.Errorf("--since %q is neither an RFC 3339 time nor a duration: %w", value, err)
	}

	return time.Now().Add(-back), nil
}

func auditRows(events []clientv1.AuditEvent) [][]string {
	rows := make([][]string, 0, len(events))

	for _, e := range events {
		rows = append(rows, []string{
			e.CreatedAt.Format(HeadscaleDateTimeFormat),
			auditActor(e),
			e.Action,
			auditTarget(e),
			strconv.FormatInt(e.Outcome, 10),
			auditDetail(e.Detail),
		})
	}

	return rows
}

func auditActor(e clientv1.AuditEvent) string {
	if e.ActorName == "" {
		return e.ActorKind
	}

	return e.ActorName + " (" + e.ActorKind + ")"
}

func auditTarget(e clientv1.AuditEvent) string {
	switch {
	case e.TargetKind == "":
		return ""
	case e.TargetName != "":
		return e.TargetKind + " " + e.TargetName
	case e.TargetId != "":
		return e.TargetKind + " " + e.TargetId
	default:
		return e.TargetKind
	}
}

func auditDetail(detail map[string]any) string {
	if len(detail) == 0 {
		return ""
	}

	raw, err := json.Marshal(detail)
	if err != nil {
		return fmt.Sprint(detail)
	}

	return string(raw)
}
