package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"

	clientv1 "github.com/aislopware/slopscale/gen/client/v1"
	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/spf13/cobra"
)

const (
	recordingFileMode = 0o600
	recordingPageSize = 50
)

func init() {
	rootCmd.AddCommand(sshRecordingsCmd)

	listSSHRecordingsCmd.Flags().Uint64("before", 0, "Page: recordings with an ID below this one")
	listSSHRecordingsCmd.Flags().Int64("limit", recordingPageSize, "Page size, at most 500")
	sshRecordingsCmd.AddCommand(listSSHRecordingsCmd)

	for _, c := range []*cobra.Command{downloadSSHRecordingCmd, deleteSSHRecordingCmd} {
		c.Flags().Uint64P("identifier", "i", 0, "Recording identifier (ID)")
		mustMarkRequired(c, "identifier")
		sshRecordingsCmd.AddCommand(c)
	}

	downloadSSHRecordingCmd.Flags().StringP("output", "o", "",
		"Write the asciinema file here; default ssh-recording-<id>.cast, - for stdout")
}

var sshRecordingsCmd = &cobra.Command{
	Use:     "ssh-recordings",
	Short:   "Manage recorded SSH sessions",
	Aliases: []string{"ssh-recording", "recordings"},
}

var listSSHRecordingsCmd = &cobra.Command{
	Use:     cmdList,
	Short:   "List recorded SSH sessions, newest first",
	Aliases: []string{"ls"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			params := &clientv1.ListSSHRecordingsParams{}

			if before, _ := cmd.Flags().GetUint64("before"); before > 0 {
				s := strconv.FormatUint(before, util.Base10)
				params.Before = &s
			}

			if limit, _ := cmd.Flags().GetInt64("limit"); limit > 0 {
				params.Limit = &limit
			}

			resp, err := client.ListSSHRecordingsWithResponse(ctx, params)
			if err != nil {
				return fmt.Errorf("listing SSH recordings: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			recordings := resp.JSON200.Recordings

			return printListOutput(cmd, resp.JSON200, func() error {
				rows := make([][]string, 0, len(recordings))
				for _, r := range recordings {
					rows = append(rows, []string{
						r.Id,
						r.StartedAt.Format(SlopscaleDateTimeFormat),
						recordingSource(r),
						r.SshUser + "@" + r.DstNode,
						r.Command,
						strconv.FormatInt(r.Size, util.Base10),
						recordingState(r),
					})
				}

				err := renderTable(
					[]string{"ID", "Started", "From", "Session", "Command", "Bytes", "State"}, rows,
				)
				if err != nil {
					return err
				}

				if resp.JSON200.NextBefore != "" {
					cmd.Printf("More: --before %s\n", resp.JSON200.NextBefore)
				}

				return nil
			})
		},
	),
}

func recordingSource(r clientv1.SSHRecording) string {
	if r.SrcUser != "" {
		return r.SrcUser + " on " + r.SrcNode
	}

	return r.SrcNode
}

func recordingState(r clientv1.SSHRecording) string {
	switch {
	case r.EndedAt == nil:
		return "recording"
	case r.Complete:
		return "complete"
	default:
		return "interrupted"
	}
}

var downloadSSHRecordingCmd = &cobra.Command{
	Use:   "download",
	Short: "Download a recorded session as an asciinema file",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			id := strconv.FormatUint(identifier, util.Base10)

			resp, err := client.DownloadSSHRecordingWithResponse(ctx, id)
			if err != nil {
				return fmt.Errorf("downloading SSH recording: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			output, _ := cmd.Flags().GetString("output")
			if output == "-" {
				_, err = cmd.OutOrStdout().Write(resp.Body)
				if err != nil {
					return fmt.Errorf("writing recording: %w", err)
				}

				return nil
			}

			if output == "" {
				output = "ssh-recording-" + id + ".cast"
			}

			err = os.WriteFile(output, resp.Body, recordingFileMode)
			if err != nil {
				return fmt.Errorf("writing recording: %w", err)
			}

			return printOutput(cmd, map[string]string{colResult: "written " + output}, "Recording written to "+output)
		},
	),
}

var deleteSSHRecordingCmd = &cobra.Command{
	Use:     cmdDelete,
	Short:   "Delete a recorded session and its file",
	Aliases: []string{aliasDel},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")

			resp, err := client.DeleteSSHRecordingWithResponse(ctx, strconv.FormatUint(identifier, util.Base10))
			if err != nil {
				return fmt.Errorf("deleting SSH recording: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, map[string]string{colResult: "Recording deleted"}, "Recording deleted")
		},
	),
}
