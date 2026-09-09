package cli

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	clientv1 "github.com/aislopware/slopscale/gen/client/v1"
	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(posturesCmd)

	posturesCmd.AddCommand(listPosturesCmd)

	addPostureFlags(createPostureCmd)
	mustMarkRequired(createPostureCmd, "name")
	posturesCmd.AddCommand(createPostureCmd)

	updatePostureCmd.Flags().Uint64P("identifier", "i", 0, "Posture identifier (ID)")
	mustMarkRequired(updatePostureCmd, "identifier")
	addPostureFlags(updatePostureCmd)
	mustMarkRequired(updatePostureCmd, "name")
	posturesCmd.AddCommand(updatePostureCmd)

	deletePostureCmd.Flags().Uint64P("identifier", "i", 0, "Posture identifier (ID)")
	mustMarkRequired(deletePostureCmd, "identifier")
	posturesCmd.AddCommand(deletePostureCmd)

	checkPostureCmd.Flags().StringSliceP("expr", "e", []string{}, "Expression to check (repeatable)")
	mustMarkRequired(checkPostureCmd, "expr")
	posturesCmd.AddCommand(checkPostureCmd)
}

func addPostureFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("name", "n", "", "Posture name")
	cmd.Flags().String("description", "", "Posture description")
	cmd.Flags().StringSliceP("expr", "e", []string{},
		"Expression such as \"node:os == 'macos'\" (repeatable; every one must hold)")
	cmd.Flags().StringSlice("days", []string{}, "Schedule days: mon,tue,wed,thu,fri,sat,sun")
	cmd.Flags().String("start", "", "Schedule start, HH:MM")
	cmd.Flags().String("end", "", "Schedule end, HH:MM; before start wraps past midnight")
	cmd.Flags().String("timezone", "", "Schedule time zone, an IANA name; empty means UTC")
}

var posturesCmd = &cobra.Command{
	Use:     "postures",
	Short:   "Manage postures, the conditions access rules require of a source machine",
	Aliases: []string{"posture"},
}

var listPosturesCmd = &cobra.Command{
	Use:     cmdList,
	Short:   "List postures",
	Aliases: []string{"ls"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.ListPosturesWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("listing postures: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			postures := resp.JSON200.Postures

			return printListOutput(cmd, postures, func() error {
				rows := make([][]string, 0, len(postures))
				for _, p := range postures {
					rows = append(rows, []string{
						p.Id, p.Name, strings.Join(p.Expressions, "\n"), scheduleText(p.Schedule),
					})
				}

				return renderTable([]string{"ID", "Name", "Expressions", "Schedule"}, rows)
			})
		},
	),
}

func scheduleText(s *clientv1.PostureSchedule) string {
	if s == nil {
		return ""
	}

	text := strings.Join(s.Days, ",") + " " + s.Start + "-" + s.End
	if s.Timezone != nil && *s.Timezone != "" {
		text += " " + *s.Timezone
	}

	return text
}

func postureBodyFromFlags(cmd *cobra.Command) clientv1.PostureRequestBody {
	name, _ := cmd.Flags().GetString("name")
	desc, _ := cmd.Flags().GetString("description")
	exprs, _ := cmd.Flags().GetStringSlice("expr")

	body := clientv1.PostureRequestBody{Name: name, Description: &desc, Expressions: &exprs}

	days, _ := cmd.Flags().GetStringSlice("days")
	start, _ := cmd.Flags().GetString("start")
	end, _ := cmd.Flags().GetString("end")
	timezone, _ := cmd.Flags().GetString("timezone")

	if len(days) > 0 || start != "" || end != "" {
		body.Schedule = &clientv1.PostureSchedule{Days: days, Start: start, End: end, Timezone: &timezone}
	}

	return body
}

var createPostureCmd = &cobra.Command{
	Use:   cmdCreate,
	Short: "Create a posture",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.CreatePostureWithResponse(ctx, postureBodyFromFlags(cmd))
			if err != nil {
				return fmt.Errorf("creating posture: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.Posture, "Posture created")
		},
	),
}

var updatePostureCmd = &cobra.Command{
	Use:   cmdUpdate,
	Short: "Replace a posture",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")

			resp, err := client.UpdatePostureWithResponse(
				ctx, strconv.FormatUint(identifier, util.Base10), postureBodyFromFlags(cmd),
			)
			if err != nil {
				return fmt.Errorf("updating posture: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.Posture, "Posture updated")
		},
	),
}

var deletePostureCmd = &cobra.Command{
	Use:     cmdDelete,
	Short:   "Delete a posture no rule names",
	Aliases: []string{aliasDel},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")

			resp, err := client.DeletePostureWithResponse(ctx, strconv.FormatUint(identifier, util.Base10))
			if err != nil {
				return fmt.Errorf("deleting posture: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, map[string]string{colResult: "Posture deleted"}, "Posture deleted")
		},
	),
}

var checkPostureCmd = &cobra.Command{
	Use:   "check",
	Short: "Parse posture expressions without storing them",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			exprs, _ := cmd.Flags().GetStringSlice("expr")

			resp, err := client.CheckPostureExpressionsWithResponse(
				ctx, clientv1.CheckPostureExpressionsJSONRequestBody{Expressions: &exprs},
			)
			if err != nil {
				return fmt.Errorf("checking posture expressions: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			rows := make([][]string, 0, len(exprs))
			for i, e := range exprs {
				result := "ok"
				if i < len(resp.JSON200.Errors) && resp.JSON200.Errors[i] != "" {
					result = resp.JSON200.Errors[i]
				}

				rows = append(rows, []string{e, result})
			}

			return printListOutput(cmd, resp.JSON200.Errors, func() error {
				return renderTable([]string{"Expression", "Result"}, rows)
			})
		},
	),
}
