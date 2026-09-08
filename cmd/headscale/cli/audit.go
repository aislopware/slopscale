package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/spf13/cobra"
)

const (
	defaultAuditLimit = 50

	// auditExportFileMode is the permission of a written export; it holds the
	// same records as the log itself, so it stays readable by its owner only.
	auditExportFileMode = 0o600
)

var errInvalidAuditFormat = errors.New("--format must be csv or json")

func init() {
	rootCmd.AddCommand(auditCmd)

	auditCmd.AddCommand(listAuditCmd)
	auditFilterFlags(listAuditCmd)
	listAuditCmd.Flags().Int64P("limit", "l", defaultAuditLimit, "Newest events to show (at most 500)")

	auditCmd.AddCommand(exportAuditCmd)
	auditFilterFlags(exportAuditCmd)
	exportAuditCmd.Flags().String("until", "", "Only events before this time (RFC 3339 or a duration such as 1h)")
	exportAuditCmd.Flags().String("format", "csv", "File format: csv or json")
	exportAuditCmd.Flags().StringP("output", "o", "",
		"Write the file here; stdout when unset, - for stdout explicitly")
}

// auditFilterFlags registers the filters the list and the export share, so
// the same flag names select the same events in both.
func auditFilterFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("user", "u", "", "Only events by this user ID")
	cmd.Flags().StringP("action", "a", "",
		"Only this action, or every action under a prefix ending with a dot (node.)")
	cmd.Flags().String("target-kind", "", "Only events about this kind of object (node, user, ...)")
	cmd.Flags().String("target-id", "", "Only events about this object ID (with --target-kind)")
	cmd.Flags().String("since", "", "Only events at or after this time (RFC 3339 or a duration such as 24h)")
	cmd.Flags().String("before", "", "Page: only events with an ID below this one")
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

var exportAuditCmd = &cobra.Command{
	Use:   "export",
	Short: "Export audit events as a file",
	Long: `Writes the events matching the filters as one file, oldest first, so a
spreadsheet reads top to bottom in the order things happened. The format is CSV
by default and JSON with --format json. Without --output the file goes to
stdout, ready to be piped:

  headscale audit export --since 2026-09-01T00:00:00Z --until 2026-10-01T00:00:00Z -o september.csv`,
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			params, err := exportAuditParams(cmd)
			if err != nil {
				return err
			}

			// The export is a file download, not a JSON body, so the generated
			// typed wrapper cannot parse it; the raw response is read here and
			// a failure's problem detail decoded by hand.
			resp, err := client.ExportAuditEvents(ctx, params)
			if err != nil {
				return fmt.Errorf("exporting audit events: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return fmt.Errorf("reading the audit export: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				return auditExportError(resp.StatusCode, body)
			}

			return writeAuditExportFile(cmd, body)
		},
	),
}

// writeAuditExportFile writes the export where --output points, or to stdout
// when it is unset or "-".
func writeAuditExportFile(cmd *cobra.Command, body []byte) error {
	output, _ := cmd.Flags().GetString("output")

	if output == "" || output == "-" {
		_, err := cmd.OutOrStdout().Write(body)
		if err != nil {
			return fmt.Errorf("writing the audit export: %w", err)
		}

		return nil
	}

	err := os.WriteFile(output, body, auditExportFileMode)
	if err != nil {
		return fmt.Errorf("writing the audit export: %w", err)
	}

	fmt.Printf("Audit log written to %s\n", output)

	return nil
}

// auditExportError turns a failing export into an error, decoding the problem
// detail the server sends in place of the file.
func auditExportError(statusCode int, body []byte) error {
	var problem clientv1.ErrorModel

	err := json.Unmarshal(body, &problem)
	if err != nil {
		return apiError(statusCode, nil)
	}

	return apiError(statusCode, &problem)
}

// exportAuditParams turns the flags into query parameters, resolving relative
// --since and --until against the current time.
func exportAuditParams(cmd *cobra.Command) (*clientv1.ExportAuditEventsParams, error) {
	params := &clientv1.ExportAuditEventsParams{}

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

	for flag, dst := range map[string]**time.Time{
		"since": &params.Since,
		"until": &params.Until,
	} {
		value, _ := cmd.Flags().GetString(flag)
		if value == "" {
			continue
		}

		at, err := parseAuditTime(flag, value)
		if err != nil {
			return nil, err
		}

		*dst = &at
	}

	format, _ := cmd.Flags().GetString("format")
	if format != "" {
		f := clientv1.ExportAuditEventsParamsFormat(format)
		if !f.Valid() {
			return nil, fmt.Errorf("%w, not %q", errInvalidAuditFormat, format)
		}

		params.Format = &f
	}

	return params, nil
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
		at, err := parseAuditTime("since", since)
		if err != nil {
			return nil, err
		}

		params.Since = &at
	}

	return params, nil
}

// parseAuditTime accepts an RFC 3339 time or a duration back from now, naming
// the flag it came from in the error.
func parseAuditTime(flag, value string) (time.Time, error) {
	at, err := time.Parse(time.RFC3339, value)
	if err == nil {
		return at, nil
	}

	back, err := time.ParseDuration(value)
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"--%s %q is neither an RFC 3339 time nor a duration: %w", flag, value, err,
		)
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
