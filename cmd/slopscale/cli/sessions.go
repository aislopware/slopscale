package cli

import (
	"context"
	"fmt"
	"net/http"

	clientv1 "github.com/aislopware/slopscale/gen/client/v1"
	"github.com/spf13/cobra"
)

// userAgentWidth is how much of a browser's user agent the table shows; the
// full string is long enough to wrap every row.
const userAgentWidth = 40

func init() {
	rootCmd.AddCommand(sessionsCmd)

	listSessionsCmd.Flags().StringP("user", "u", "", "Only sessions of this user ID")
	sessionsCmd.AddCommand(listSessionsCmd)

	sessionsCmd.AddCommand(endSessionCmd)
}

var sessionsCmd = &cobra.Command{
	Use:     "sessions",
	Short:   "Manage console sessions, the sign-ins a browser holds",
	Aliases: []string{"session"},
}

var listSessionsCmd = &cobra.Command{
	Use:     cmdList,
	Short:   "List console sessions",
	Aliases: []string{"ls", cmdShow},
	Long: `Lists the sign-ins that can still be used, with the user each belongs to and
where it came from. An administrator sees every session; anyone else sees only
their own.`,
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.ListSessionsWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("listing sessions: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			user, _ := cmd.Flags().GetString("user")
			sessions := filterSessionsByUser(resp.JSON200.Sessions, user)

			return printListOutput(cmd, sessions, func() error {
				return renderTable(
					[]string{"ID", "User", "Opened", "Last seen", "Address", "Browser", "Current"},
					sessionRows(sessions),
				)
			})
		},
	),
}

var endSessionCmd = &cobra.Command{
	Use:     "end ID",
	Short:   "End a console session",
	Aliases: []string{cmdDelete, aliasDel, "revoke"},
	Long: `Ends one sign-in, so the browser holding its cookie is asked to sign in again
on its next request. Ending your own session signs you out of the console.`,
	Args: cobra.ExactArgs(1),
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, args []string) error {
			resp, err := client.EndSessionByIDWithResponse(ctx, args[0])
			if err != nil {
				return fmt.Errorf("ending session: %w", err)
			}

			if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusNoContent {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, map[string]string{colResult: "Session ended"}, "Session ended")
		},
	),
}

// filterSessionsByUser keeps the sessions of one user id. The API returns
// every session the caller may see, so the flag narrows the list here.
func filterSessionsByUser(sessions []clientv1.ConsoleSession, userID string) []clientv1.ConsoleSession {
	if userID == "" {
		return sessions
	}

	kept := make([]clientv1.ConsoleSession, 0, len(sessions))

	for _, s := range sessions {
		if s.User.Id == userID {
			kept = append(kept, s)
		}
	}

	return kept
}

func sessionRows(sessions []clientv1.ConsoleSession) [][]string {
	rows := make([][]string, 0, len(sessions))

	for _, s := range sessions {
		rows = append(rows, []string{
			s.Id,
			s.User.Name,
			s.CreatedAt.Format(SlopscaleDateTimeFormat),
			s.LastSeenAt.Format(SlopscaleDateTimeFormat),
			orDash(s.RemoteAddr),
			orDash(shortUserAgent(s.UserAgent)),
			currentLabel(s.Current),
		})
	}

	return rows
}

// shortUserAgent trims a user agent to the table's column width.
func shortUserAgent(userAgent string) string {
	if len(userAgent) <= userAgentWidth {
		return userAgent
	}

	return userAgent[:userAgentWidth-3] + "..."
}

// currentLabel marks the session making this request, the one a sign-out ends.
func currentLabel(current bool) string {
	if current {
		return "yes"
	}

	return "-"
}

// orDash renders an empty value as a dash, which a session opened before the
// server recorded addresses and browsers has.
func orDash(value string) string {
	if value == "" {
		return "-"
	}

	return value
}
