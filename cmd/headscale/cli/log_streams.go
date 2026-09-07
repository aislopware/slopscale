package cli

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/juanfont/headscale/hscontrol/util"
	"github.com/spf13/cobra"
)

const destinationUsage = "Destination: http, splunk, elastic, datadog, axiom or loki"

func init() {
	rootCmd.AddCommand(logStreamsCmd)

	logStreamsCmd.AddCommand(listLogStreamsCmd)

	addLogStreamFlags(createLogStreamCmd)
	mustMarkRequired(createLogStreamCmd, "name", "destination", "url")
	logStreamsCmd.AddCommand(createLogStreamCmd)

	updateLogStreamCmd.Flags().Uint64P("identifier", "i", 0, "Log stream identifier (ID)")
	mustMarkRequired(updateLogStreamCmd, "identifier")
	addLogStreamFlags(updateLogStreamCmd)
	logStreamsCmd.AddCommand(updateLogStreamCmd)

	for _, c := range []*cobra.Command{deleteLogStreamCmd, testLogStreamCmd} {
		c.Flags().Uint64P("identifier", "i", 0, "Log stream identifier (ID)")
		mustMarkRequired(c, "identifier")
		logStreamsCmd.AddCommand(c)
	}
}

func addLogStreamFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("name", "n", "", "Stream name")
	cmd.Flags().StringP("destination", "d", "", destinationUsage)
	cmd.Flags().StringP("url", "u", "", "Sink URL")
	cmd.Flags().StringP("token", "t", "", "Sink credential; on update, empty keeps the stored one")
	cmd.Flags().Bool("disabled", false, "Keep the stream but ship nothing")
}

var logStreamsCmd = &cobra.Command{
	Use:     "log-streams",
	Short:   "Manage log streams, the sinks the audit log is shipped to",
	Aliases: []string{"log-stream", "logstreams"},
}

var listLogStreamsCmd = &cobra.Command{
	Use:     cmdList,
	Short:   "List log streams",
	Aliases: []string{"ls"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.ListLogStreamsWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("listing log streams: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			streams := resp.JSON200.LogStreams

			return printListOutput(cmd, streams, func() error {
				rows := make([][]string, 0, len(streams))
				for _, l := range streams {
					rows = append(rows, []string{
						l.Id, l.Name, l.Destination, l.Url, enabledText(l.Enabled),
						formatLastDelivery(l.LastDeliveryStatus, l.LastDeliveryAt),
						strconv.FormatInt(l.Delivered, util.Base10), strconv.FormatInt(l.Dropped, util.Base10),
					})
				}

				return renderTable(
					[]string{"ID", "Name", "Destination", "URL", "Enabled", "Last delivery", "Delivered", "Dropped"},
					rows,
				)
			})
		},
	),
}

func enabledText(enabled bool) string {
	if enabled {
		return "yes"
	}

	return "no"
}

func logStreamBodyFromFlags(cmd *cobra.Command) clientv1.LogStreamRequestBody {
	name, _ := cmd.Flags().GetString("name")
	destination, _ := cmd.Flags().GetString("destination")
	url, _ := cmd.Flags().GetString("url")
	token, _ := cmd.Flags().GetString("token")
	disabled, _ := cmd.Flags().GetBool("disabled")

	enabled := !disabled

	return clientv1.LogStreamRequestBody{
		Name:        name,
		Destination: clientv1.LogStreamRequestBodyDestination(destination),
		Url:         url,
		Token:       &token,
		Enabled:     &enabled,
	}
}

var createLogStreamCmd = &cobra.Command{
	Use:   cmdCreate,
	Short: "Create a log stream",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.CreateLogStreamWithResponse(ctx, logStreamBodyFromFlags(cmd))
			if err != nil {
				return fmt.Errorf("creating log stream: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.LogStream, "Log stream created")
		},
	),
}

var updateLogStreamCmd = &cobra.Command{
	Use:   cmdUpdate,
	Short: "Replace a log stream; flags not given keep their stored value",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			id := strconv.FormatUint(identifier, util.Base10)

			current, err := client.GetLogStreamWithResponse(ctx, id)
			if err != nil {
				return fmt.Errorf("getting log stream: %w", err)
			}

			if current.StatusCode() != http.StatusOK {
				return apiError(current.StatusCode(), current.ApplicationproblemJSONDefault)
			}

			body := logStreamBodyFromFlags(cmd)
			existing := current.JSON200.LogStream

			if !cmd.Flags().Changed("name") {
				body.Name = existing.Name
			}

			if !cmd.Flags().Changed("destination") {
				body.Destination = clientv1.LogStreamRequestBodyDestination(existing.Destination)
			}

			if !cmd.Flags().Changed("url") {
				body.Url = existing.Url
			}

			if !cmd.Flags().Changed("disabled") {
				body.Enabled = &existing.Enabled
			}

			resp, err := client.UpdateLogStreamWithResponse(ctx, id, body)
			if err != nil {
				return fmt.Errorf("updating log stream: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.LogStream, "Log stream updated")
		},
	),
}

var deleteLogStreamCmd = &cobra.Command{
	Use:     cmdDelete,
	Short:   "Delete a log stream",
	Aliases: []string{aliasDel},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")

			resp, err := client.DeleteLogStreamWithResponse(ctx, strconv.FormatUint(identifier, util.Base10))
			if err != nil {
				return fmt.Errorf("deleting log stream: %w", err)
			}

			if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusNoContent {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, map[string]string{colResult: "Log stream deleted"}, "Log stream deleted")
		},
	),
}

var testLogStreamCmd = &cobra.Command{
	Use:   "test",
	Short: "Ship a test entry to the sink now",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")

			resp, err := client.TestLogStreamWithResponse(ctx, strconv.FormatUint(identifier, util.Base10))
			if err != nil {
				return fmt.Errorf("testing log stream: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			if !resp.JSON200.Delivered {
				return fmt.Errorf("%w: %s", errDeliveryFailed, resp.JSON200.Status)
			}

			return printOutput(cmd, resp.JSON200, "Delivered (status "+resp.JSON200.Status+")")
		},
	),
}
