package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"

	clientv1 "github.com/aislopware/slopscale/gen/client/v1"
	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/spf13/cobra"
)

// diagnosticFileMode is what a downloaded diagnostic is written with; the
// dumps carry a machine's prefs and goroutines, so only the operator reads
// them.
const diagnosticFileMode = 0o600

func init() {
	updateNodeClientCmd.Flags().Uint64P("identifier", "i", 0, "Node identifier (ID)")
	mustMarkRequired(updateNodeClientCmd, "identifier")
	updateNodeClientCmd.Flags().Bool("force", false, "Update even while the node is serving SSH sessions")
	nodeCmd.AddCommand(updateNodeClientCmd)

	nodeHealthCmd.Flags().Uint64P("identifier", "i", 0, "Node identifier (ID)")
	mustMarkRequired(nodeHealthCmd, "identifier")
	nodeCmd.AddCommand(nodeHealthCmd)

	nodeDiagnosticsCmd.Flags().Uint64P("identifier", "i", 0, "Node identifier (ID)")
	mustMarkRequired(nodeDiagnosticsCmd, "identifier")
	nodeDiagnosticsCmd.Flags().
		StringP("kind", "k", "", "Which dump to fetch: prefs, netmap, metrics, goroutines, sockstats or tka-log")
	mustMarkRequired(nodeDiagnosticsCmd, "kind")
	nodeDiagnosticsCmd.Flags().String("out", "", "Write the dump to this file instead of stdout")
	nodeCmd.AddCommand(nodeDiagnosticsCmd)

	nodePrefsGetCmd.Flags().Uint64P("identifier", "i", 0, "Node identifier (ID)")
	mustMarkRequired(nodePrefsGetCmd, "identifier")
	nodePrefsCmd.AddCommand(nodePrefsGetCmd)

	nodePrefsSetCmd.Flags().Uint64P("identifier", "i", 0, "Node identifier (ID)")
	mustMarkRequired(nodePrefsSetCmd, "identifier")
	nodePrefsSetCmd.Flags().StringSlice("advertise-routes", nil, "Routes the node offers to the tailnet")
	nodePrefsSetCmd.Flags().Bool("advertise-exit-node", false, "Offer to be an exit node")
	nodePrefsSetCmd.Flags().Bool("accept-routes", false, "Accept the routes other nodes advertise")
	nodePrefsSetCmd.Flags().Bool("accept-dns", false, "Use the tailnet's DNS configuration")
	nodePrefsSetCmd.Flags().String("exit-node", "", "Exit node to use, by stable ID or address; empty stops using one")
	nodePrefsSetCmd.Flags().
		Bool("exit-node-allow-lan-access", false, "Reach the local network while using an exit node")
	nodePrefsSetCmd.Flags().Bool("ssh", false, "Run Tailscale SSH")
	nodePrefsSetCmd.Flags().Bool("shields-up", false, "Block incoming connections")
	nodePrefsSetCmd.Flags().String("hostname", "", "The name the node reports")
	nodePrefsSetCmd.Flags().Bool("auto-update-check", false, "Check for client updates in the background")
	nodePrefsSetCmd.Flags().Bool("auto-update-apply", false, "Apply client updates in the background")
	nodePrefsSetCmd.Flags().Bool("advertise-connector", false, "Offer to be an app connector")
	nodePrefsSetCmd.Flags().Bool("posture-checking", false, "Report device posture")
	nodePrefsCmd.AddCommand(nodePrefsSetCmd)

	nodeCmd.AddCommand(nodePrefsCmd)
}

var updateNodeClientCmd = &cobra.Command{
	Use:   cmdUpdate,
	Short: "Update the node's Tailscale client",
	Long: `Asks the node to update its own Tailscale installation now. The client
refuses unless its owner allowed it (tailscale set --auto-update, or
TS_ALLOW_REMOTE_UPDATE), and while it is serving SSH sessions unless --force
is given. Without --identifier's node connected the command reports a
conflict.`,
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			force, _ := cmd.Flags().GetBool("force")

			resp, err := client.StartNodeClientUpdateWithResponse(ctx,
				strconv.FormatUint(identifier, util.Base10),
				clientv1.StartNodeClientUpdateJSONRequestBody{Force: &force})
			if err != nil {
				return fmt.Errorf("updating the node's client: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			message := "The node started updating"
			if !resp.JSON200.Started {
				message = "The node did not start updating"
			}

			return printOutput(cmd, resp.JSON200, message)
		},
	),
}

var nodeHealthCmd = &cobra.Command{
	Use:   "health",
	Short: "Show what the node's client reports about its own health",
	Long: `Asks the node for the warnings it would show its own user, which is where a
node that is connected but not working says why.`,
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")

			resp, err := client.GetNodeClientHealthWithResponse(ctx,
				strconv.FormatUint(identifier, util.Base10))
			if err != nil {
				return fmt.Errorf("getting the node's health: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			warnings := resp.JSON200.Warnings

			return printListOutput(cmd, warnings, func() error {
				if len(warnings) == 0 {
					fmt.Println("The client reports no warnings")

					return nil
				}

				rows := make([][]string, 0, len(warnings))
				for _, w := range warnings {
					rows = append(rows, []string{w.Code, w.Severity, w.Title, w.Text})
				}

				return renderTable([]string{"Code", "Severity", "Title", "Text"}, rows)
			})
		},
	),
}

var nodeDiagnosticsCmd = &cobra.Command{
	Use:   "diagnostics",
	Short: "Download a diagnostic dump from the node",
	Long: `Asks the node for one of the dumps its client hands over for support and
writes it out as the client wrote it. A client built without its debug
endpoints refuses:

  slopscale nodes diagnostics --identifier 3 --kind netmap --out netmap.json`,
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			kind, _ := cmd.Flags().GetString("kind")

			// The dump is a file download rather than a JSON body, so the
			// generated typed wrapper cannot parse it; the raw response is
			// read here and a failure's problem detail decoded by hand.
			resp, err := client.GetNodeDiagnostic(ctx,
				strconv.FormatUint(identifier, util.Base10),
				clientv1.GetNodeDiagnosticParamsKind(kind))
			if err != nil {
				return fmt.Errorf("getting the node diagnostic: %w", err)
			}
			defer func() { _ = resp.Body.Close() }()

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				return fmt.Errorf("reading the node diagnostic: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				var problem clientv1.ErrorModel

				if json.Unmarshal(body, &problem) != nil {
					return apiError(resp.StatusCode, nil)
				}

				return apiError(resp.StatusCode, &problem)
			}

			return writeDiagnostic(cmd, body)
		},
	),
}

// writeDiagnostic writes the dump where --out points, or to stdout when it
// is unset or "-".
func writeDiagnostic(cmd *cobra.Command, body []byte) error {
	out, _ := cmd.Flags().GetString("out")

	if out == "" || out == "-" {
		_, err := cmd.OutOrStdout().Write(body)
		if err != nil {
			return fmt.Errorf("writing the node diagnostic: %w", err)
		}

		return nil
	}

	err := os.WriteFile(out, body, diagnosticFileMode)
	if err != nil {
		return fmt.Errorf("writing the node diagnostic: %w", err)
	}

	fmt.Printf("Diagnostic written to %s\n", out)

	return nil
}

var nodePrefsCmd = &cobra.Command{
	Use:     "prefs",
	Short:   "Read and change the preferences a node's client holds",
	Aliases: []string{"preferences"},
}

var nodePrefsGetCmd = &cobra.Command{
	Use:   "get",
	Short: "Show the preferences the node's client holds",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")

			resp, err := client.GetNodePreferencesWithResponse(ctx,
				strconv.FormatUint(identifier, util.Base10))
			if err != nil {
				return fmt.Errorf("getting the node's preferences: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printListOutput(cmd, resp.JSON200, func() error {
				return renderTable([]string{"Preference", "Value"}, preferenceRows(*resp.JSON200))
			})
		},
	),
}

var nodePrefsSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Change the preferences the node's client holds",
	Long: `Changes only the preferences named on the command line and answers with what
the client ended up with. The machine has to have handed its configuration
over to the tailnet admin by running "tailscale set --remote-config" on it;
without that the node refuses:

  slopscale nodes prefs set --identifier 3 --accept-routes=true --shields-up=false`,
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")

			body := preferencesFromFlags(cmd)

			resp, err := client.UpdateNodePreferencesWithResponse(ctx,
				strconv.FormatUint(identifier, util.Base10), body)
			if err != nil {
				return fmt.Errorf("changing the node's preferences: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printListOutput(cmd, resp.JSON200, func() error {
				return renderTable([]string{"Preference", "Value"}, preferenceRows(*resp.JSON200))
			})
		},
	),
}

// preferencesFromFlags sends only what the command line named, so a flag
// left out keeps whatever the machine's owner set.
func preferencesFromFlags(cmd *cobra.Command) clientv1.UpdateNodePreferencesJSONRequestBody {
	body := clientv1.UpdateNodePreferencesJSONRequestBody{}

	if cmd.Flags().Changed("advertise-routes") {
		routes, _ := cmd.Flags().GetStringSlice("advertise-routes")
		body.AdvertiseRoutes = &routes
	}

	if cmd.Flags().Changed("exit-node") {
		exitNode, _ := cmd.Flags().GetString("exit-node")
		body.ExitNode = &exitNode
	}

	if cmd.Flags().Changed("hostname") {
		hostname, _ := cmd.Flags().GetString("hostname")
		body.Hostname = &hostname
	}

	for name, field := range map[string]**bool{
		"advertise-exit-node":        &body.AdvertiseExitNode,
		"accept-routes":              &body.AcceptRoutes,
		"accept-dns":                 &body.AcceptDns,
		"exit-node-allow-lan-access": &body.ExitNodeAllowLanAccess,
		"ssh":                        &body.RunSsh,
		"shields-up":                 &body.ShieldsUp,
		"auto-update-check":          &body.AutoUpdateCheck,
		"auto-update-apply":          &body.AutoUpdateApply,
		"advertise-connector":        &body.AdvertiseConnector,
		"posture-checking":           &body.PostureChecking,
	} {
		if cmd.Flags().Changed(name) {
			value, _ := cmd.Flags().GetBool(name)
			*field = &value
		}
	}

	return body
}

// preferenceRows renders the curated preferences as the flags that set
// them, so a reader can copy a line back into `nodes prefs set`.
func preferenceRows(prefs clientv1.NodePreferences) [][]string {
	return [][]string{
		{"advertise-routes", strings.Join(prefs.AdvertiseRoutes, ", ")},
		{"advertise-exit-node", strconv.FormatBool(prefs.AdvertiseExitNode)},
		{"accept-routes", strconv.FormatBool(prefs.AcceptRoutes)},
		{"accept-dns", strconv.FormatBool(prefs.AcceptDns)},
		{"exit-node", prefs.ExitNode},
		{"exit-node-allow-lan-access", strconv.FormatBool(prefs.ExitNodeAllowLanAccess)},
		{"ssh", strconv.FormatBool(prefs.RunSsh)},
		{"shields-up", strconv.FormatBool(prefs.ShieldsUp)},
		{"hostname", prefs.Hostname},
		{"auto-update-check", strconv.FormatBool(prefs.AutoUpdateCheck)},
		{"auto-update-apply", strconv.FormatBool(prefs.AutoUpdateApply)},
		{"advertise-connector", strconv.FormatBool(prefs.AdvertiseConnector)},
		{"posture-checking", strconv.FormatBool(prefs.PostureChecking)},
	}
}
