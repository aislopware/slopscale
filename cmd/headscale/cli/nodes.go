package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/juanfont/headscale/hscontrol/util"
	"github.com/pterm/pterm"
	"github.com/samber/lo"
	"github.com/spf13/cobra"
	"tailscale.com/types/key"
)

func init() {
	rootCmd.AddCommand(nodeCmd)
	listNodesCmd.Flags().StringP("user", "u", "", "Filter by user")
	nodeCmd.AddCommand(listNodesCmd)

	listNodeRoutesCmd.Flags().Uint64P("identifier", "i", 0, "Node identifier (ID)")
	nodeCmd.AddCommand(listNodeRoutesCmd)

	registerNodeCmd.Flags().StringP("user", "u", "", "User")
	registerNodeCmd.Flags().StringP("key", "k", "", "Key")
	mustMarkRequired(registerNodeCmd, "user", "key")
	nodeCmd.AddCommand(registerNodeCmd)

	expireNodeCmd.Flags().Uint64P("identifier", "i", 0, "Node identifier (ID)")
	expireNodeCmd.Flags().
		StringP("expiry", "e", "",
			"Set expire to (RFC3339 format, e.g. 2025-08-27T10:00:00Z), or leave empty to expire immediately.")
	expireNodeCmd.Flags().BoolP("disable", "d", false, "Disable key expiry (node will never expire)")
	mustMarkRequired(expireNodeCmd, "identifier")
	nodeCmd.AddCommand(expireNodeCmd)

	renameNodeCmd.Flags().Uint64P("identifier", "i", 0, "Node identifier (ID)")
	mustMarkRequired(renameNodeCmd, "identifier")
	nodeCmd.AddCommand(renameNodeCmd)

	deleteNodeCmd.Flags().Uint64P("identifier", "i", 0, "Node identifier (ID)")
	mustMarkRequired(deleteNodeCmd, "identifier")
	nodeCmd.AddCommand(deleteNodeCmd)

	tagCmd.Flags().Uint64P("identifier", "i", 0, "Node identifier (ID)")
	mustMarkRequired(tagCmd, "identifier")
	tagCmd.Flags().StringSliceP("tags", "t", []string{}, "List of tags to add to the node")
	nodeCmd.AddCommand(tagCmd)

	approveRoutesCmd.Flags().Uint64P("identifier", "i", 0, "Node identifier (ID)")
	mustMarkRequired(approveRoutesCmd, "identifier")
	approveRoutesCmd.Flags().
		StringSliceP("routes", "r", []string{},
			`List of routes that will be approved (comma-separated, e.g. "10.0.0.0/8,192.168.0.0/24" `+
				`or empty string to remove all approved routes)`)
	nodeCmd.AddCommand(approveRoutesCmd)

	approveNodeCmd.Flags().Uint64P("identifier", "i", 0, "Node identifier (ID)")
	mustMarkRequired(approveNodeCmd, "identifier")
	approveNodeCmd.Flags().Bool("revoke", false, "Withdraw the approval instead of granting it")
	nodeCmd.AddCommand(approveNodeCmd)

	suspendNodeCmd.Flags().Uint64P("identifier", "i", 0, "Node identifier (ID)")
	mustMarkRequired(suspendNodeCmd, "identifier")
	suspendNodeCmd.Flags().Bool("revoke", false, "Lift the suspension instead of imposing it")
	nodeCmd.AddCommand(suspendNodeCmd)

	shareNodeCmd.Flags().Uint64P("identifier", "i", 0, "Node identifier (ID)")
	mustMarkRequired(shareNodeCmd, "identifier")
	shareNodeCmd.Flags().StringP("user", "u", "", "ID of the user to share the node with")
	mustMarkRequired(shareNodeCmd, "user")
	nodeCmd.AddCommand(shareNodeCmd)

	unshareNodeCmd.Flags().Uint64P("identifier", "i", 0, "Node identifier (ID)")
	mustMarkRequired(unshareNodeCmd, "identifier")
	unshareNodeCmd.Flags().StringP("user", "u", "", "ID of the user to stop sharing the node with")
	mustMarkRequired(unshareNodeCmd, "user")
	nodeCmd.AddCommand(unshareNodeCmd)

	globalExitNodeCmd.Flags().Uint64P("identifier", "i", 0, "Node identifier (ID)")
	mustMarkRequired(globalExitNodeCmd, "identifier")
	globalExitNodeCmd.Flags().Bool("revoke", false, "Clear the mark instead of setting it")
	nodeCmd.AddCommand(globalExitNodeCmd)

	nodeCmd.AddCommand(backfillNodeIPsCmd)
}

var nodeCmd = &cobra.Command{
	Use:     "nodes",
	Short:   "Manage the nodes of Headscale",
	Aliases: []string{"node"},
}

var registerNodeCmd = &cobra.Command{
	Use:        "register",
	Short:      "Registers a node to your network",
	Deprecated: "use 'headscale auth register --auth-id <id> --user <user>' instead",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			user, _ := cmd.Flags().GetString("user")
			registrationID, _ := cmd.Flags().GetString("key")

			params := &clientv1.RegisterNodeParams{
				User: &user,
				Key:  &registrationID,
			}

			resp, err := client.RegisterNodeWithResponse(ctx, params)
			if err != nil {
				return fmt.Errorf("registering node: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			node := resp.JSON200.Node

			return printOutput(
				cmd,
				node,
				fmt.Sprintf("Node %s registered", node.GivenName),
			)
		},
	),
}

var listNodesCmd = &cobra.Command{
	Use:     cmdList,
	Short:   "List nodes",
	Aliases: []string{"ls", cmdShow},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			user, _ := cmd.Flags().GetString("user")

			params := &clientv1.ListNodesParams{}
			if user != "" {
				params.User = &user
			}

			resp, err := client.ListNodesWithResponse(ctx, params)
			if err != nil {
				return fmt.Errorf("listing nodes: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			nodes := resp.JSON200.Nodes

			return printListOutput(cmd, nodes, func() error {
				tableData, err := nodesToPtables(nodes)
				if err != nil {
					return fmt.Errorf("converting to table: %w", err)
				}

				return pterm.DefaultTable.WithHasHeader().WithData(tableData).Render()
			})
		},
	),
}

var listNodeRoutesCmd = &cobra.Command{
	Use:     "list-routes",
	Short:   "List routes available on nodes",
	Aliases: []string{"lsr", "routes"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")

			resp, err := client.ListNodesWithResponse(ctx, &clientv1.ListNodesParams{})
			if err != nil {
				return fmt.Errorf("listing nodes: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			nodes := resp.JSON200.Nodes

			if identifier != 0 {
				idStr := strconv.FormatUint(identifier, util.Base10)
				for _, node := range nodes {
					if node.Id == idStr {
						nodes = []clientv1.Node{node}

						break
					}
				}
			}

			nodes = lo.Filter(nodes, func(n clientv1.Node, _ int) bool {
				return len(n.SubnetRoutes) > 0 || len(n.ApprovedRoutes) > 0 || len(n.AvailableRoutes) > 0
			})

			return printListOutput(cmd, nodes, func() error {
				return pterm.DefaultTable.WithHasHeader().WithData(nodeRoutesToPtables(nodes)).Render()
			})
		},
	),
}

var approveNodeCmd = &cobra.Command{
	Use:   "approve",
	Short: "Approve a node that is waiting for device approval",
	Long: `Admits a node that registered while device approval was on. A node waiting
for approval stays registered but has no peers and is not visible to any.

Use --revoke to withdraw the approval again.`,
	RunE: toggleNodeRunE(
		"approving node",
		"Node approved",
		"Node approval revoked",
		func(ctx context.Context, client *clientv1.ClientWithResponses, id string, on bool) (
			*nodeToggleResponse, error,
		) {
			resp, err := client.ApproveNodeWithResponse(ctx, id, clientv1.ApproveNodeJSONRequestBody{Approved: &on})
			if err != nil {
				return nil, err
			}

			return &nodeToggleResponse{resp.StatusCode(), resp.ApplicationproblemJSONDefault, resp.JSON200}, nil
		},
	),
}

var suspendNodeCmd = &cobra.Command{
	Use:   "suspend",
	Short: "Suspend a node, or lift the suspension with --revoke",
	Long: `Suspends a node. It stays registered and keeps its addresses, but it has no
peers, no peer sees it and its client is told it is not authorized. Unlike
expiring the key, lifting the suspension needs no login on the device.

Use --revoke to lift the suspension again.`,
	RunE: toggleNodeRunE(
		"suspending node",
		"Node suspended",
		"Node suspension lifted",
		func(ctx context.Context, client *clientv1.ClientWithResponses, id string, on bool) (
			*nodeToggleResponse, error,
		) {
			resp, err := client.SuspendNodeWithResponse(ctx, id, clientv1.SuspendNodeJSONRequestBody{Suspended: &on})
			if err != nil {
				return nil, err
			}

			return &nodeToggleResponse{resp.StatusCode(), resp.ApplicationproblemJSONDefault, resp.JSON200}, nil
		},
	),
}

var globalExitNodeCmd = &cobra.Command{
	Use:   "global-exit-node",
	Short: "Mark a node as the exit node every client is told to prefer",
	Long: `Marks an exit node every client is told to prefer. Marking approves the node's
exit routes; the node then carries suggest-exit-node and every node
auto-exit-node, so clients that use an exit node automatically
(tailscale set --exit-node=auto:any) pick it.

Use --revoke to clear the mark again.`,
	RunE: toggleNodeRunE(
		"setting global exit node",
		"Node marked as global exit node",
		"Global exit node mark cleared",
		func(ctx context.Context, client *clientv1.ClientWithResponses, id string, on bool) (
			*nodeToggleResponse, error,
		) {
			resp, err := client.SetGlobalExitNodeWithResponse(
				ctx, id, clientv1.SetGlobalExitNodeJSONRequestBody{Enabled: &on},
			)
			if err != nil {
				return nil, err
			}

			return &nodeToggleResponse{resp.StatusCode(), resp.ApplicationproblemJSONDefault, resp.JSON200}, nil
		},
	),
}

// nodeToggleCall invokes a node on/off endpoint for the node id.
type nodeToggleCall func(context.Context, *clientv1.ClientWithResponses, string, bool) (*nodeToggleResponse, error)

// nodeToggleResponse is what a node on/off endpoint answers.
type nodeToggleResponse struct {
	status  int
	problem *clientv1.ErrorModel
	node    *clientv1.NodeOutputBody
}

// toggleNodeRunE runs a node on/off command: --identifier names the node
// and --revoke turns the setting off instead of on.
func toggleNodeRunE(
	what, onMsg, offMsg string,
	call nodeToggleCall,
) func(*cobra.Command, []string) error {
	return clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			revoke, _ := cmd.Flags().GetBool("revoke")

			resp, err := call(ctx, client, strconv.FormatUint(identifier, util.Base10), !revoke)
			if err != nil {
				return fmt.Errorf("%s: %w", what, err)
			}

			if resp.status != http.StatusOK {
				return apiError(resp.status, resp.problem)
			}

			msg := onMsg
			if revoke {
				msg = offMsg
			}

			return printOutput(cmd, resp.node.Node, msg)
		},
	)
}

var shareNodeCmd = &cobra.Command{
	Use:   "share",
	Short: "Share a node with a user",
	Long: `Gives the user's personal devices access to the node wherever the policy
names autogroup:shared, and marks the node as shared in their netmaps. The
node gets no access back. A member may share the nodes they own.`,
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")

			user, err := shareUserFlag(cmd)
			if err != nil {
				return err
			}

			resp, err := client.ShareNodeWithResponse(
				ctx,
				strconv.FormatUint(identifier, util.Base10),
				clientv1.ShareNodeJSONRequestBody{UserId: user},
			)
			if err != nil {
				return fmt.Errorf("sharing node: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.Node, "Node shared")
		},
	),
}

// shareUserFlag reads --user as a user id; the node commands take names
// elsewhere, but sharing addresses the user by id like the API does.
func shareUserFlag(cmd *cobra.Command) (string, error) {
	user, _ := cmd.Flags().GetString("user")

	_, err := strconv.ParseUint(user, util.Base10, 64)
	if err != nil {
		return "", fmt.Errorf("--user must be a user id: %w", err)
	}

	return user, nil
}

var unshareNodeCmd = &cobra.Command{
	Use:   "unshare",
	Short: "Stop sharing a node with a user",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")

			user, err := shareUserFlag(cmd)
			if err != nil {
				return err
			}

			resp, err := client.UnshareNodeWithResponse(
				ctx,
				strconv.FormatUint(identifier, util.Base10),
				user,
			)
			if err != nil {
				return fmt.Errorf("unsharing node: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.Node, "Node share removed")
		},
	),
}

var expireNodeCmd = &cobra.Command{
	Use:   cmdExpire,
	Short: "Expire (log out) a node in your network",
	Long: `Expiring a node will keep the node in the database and force it to reauthenticate.

Use --disable to disable key expiry (node will never expire).`,
	Aliases: []string{"logout", aliasExp, "e"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			disableExpiry, _ := cmd.Flags().GetBool("disable")
			nodeID := strconv.FormatUint(identifier, util.Base10)

			// Handle disable expiry - node will never expire.
			if disableExpiry {
				disable := true

				resp, err := client.ExpireNodeWithResponse(ctx, nodeID, clientv1.ExpireNodeJSONRequestBody{
					DisableExpiry: &disable,
				})
				if err != nil {
					return fmt.Errorf("disabling node expiry: %w", err)
				}

				if resp.StatusCode() != http.StatusOK {
					return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
				}

				return printOutput(cmd, resp.JSON200.Node, "Node expiry disabled")
			}

			expiry, _ := cmd.Flags().GetString("expiry")

			now := time.Now()

			expiryTime := now

			if expiry != "" {
				var err error

				expiryTime, err = time.Parse(time.RFC3339, expiry)
				if err != nil {
					return fmt.Errorf("parsing expiry time: %w", err)
				}
			}

			resp, err := client.ExpireNodeWithResponse(ctx, nodeID, clientv1.ExpireNodeJSONRequestBody{
				Expiry: &expiryTime,
			})
			if err != nil {
				return fmt.Errorf("expiring node: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			node := resp.JSON200.Node

			if now.Equal(expiryTime) || now.After(expiryTime) {
				return printOutput(cmd, node, "Node expired")
			}

			return printOutput(cmd, node, "Node expiration updated")
		},
	),
}

var renameNodeCmd = &cobra.Command{
	Use:   "rename NEW_NAME",
	Short: "Renames a node in your network",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, args []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")

			newName := ""
			if len(args) > 0 {
				newName = args[0]
			}

			resp, err := client.RenameNodeWithResponse(ctx, strconv.FormatUint(identifier, util.Base10), newName)
			if err != nil {
				return fmt.Errorf("renaming node: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.Node, "Node renamed")
		},
	),
}

var deleteNodeCmd = &cobra.Command{
	Use:     cmdDelete,
	Short:   "Delete a node",
	Aliases: []string{aliasDel},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			nodeID := strconv.FormatUint(identifier, util.Base10)

			getResponse, err := client.GetNodeWithResponse(ctx, nodeID)
			if err != nil {
				return fmt.Errorf("getting node: %w", err)
			}

			if getResponse.StatusCode() != http.StatusOK {
				return apiError(getResponse.StatusCode(), getResponse.ApplicationproblemJSONDefault)
			}

			if !confirmAction(cmd, fmt.Sprintf(
				"Do you want to remove the node %s?",
				getResponse.JSON200.Node.Name,
			)) {
				return printOutput(cmd, map[string]string{colResult: "Node not deleted"}, "Node not deleted")
			}

			deleteResponse, err := client.DeleteNodeWithResponse(ctx, nodeID)
			if err != nil {
				return fmt.Errorf("deleting node: %w", err)
			}

			if deleteResponse.StatusCode() != http.StatusOK {
				return apiError(deleteResponse.StatusCode(), deleteResponse.ApplicationproblemJSONDefault)
			}

			return printOutput(
				cmd,
				map[string]string{colResult: "Node deleted"},
				"Node deleted",
			)
		},
	),
}

var backfillNodeIPsCmd = &cobra.Command{
	Use:   "backfillips",
	Short: "Backfill IPs missing from nodes",
	Long: `
Backfill IPs can be used to add/remove IPs from nodes
based on the current configuration of Headscale.

If there are nodes that does not have IPv4 or IPv6
even if prefixes for both are configured in the config,
this command can be used to assign IPs of the sort to
all nodes that are missing.

If you remove IPv4 or IPv6 prefixes from the config,
it can be run to remove the IPs that should no longer
be assigned to nodes.`,
	RunE: func(cmd *cobra.Command, _ []string) error {
		if !confirmAction(cmd, "Are you sure that you want to assign/remove IPs to/from nodes?") {
			return nil
		}

		return withClient(func(ctx context.Context, client *clientv1.ClientWithResponses) error {
			confirmed := true

			resp, err := client.BackfillNodeIPsWithResponse(ctx, &clientv1.BackfillNodeIPsParams{
				Confirmed: &confirmed,
			})
			if err != nil {
				return fmt.Errorf("backfilling IPs: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200, "Node IPs backfilled successfully")
		})
	},
}

func nodesToPtables(nodes []clientv1.Node) (pterm.TableData, error) {
	tableHeader := []string{
		"ID",
		"Hostname",
		"Name",
		"MachineKey",
		"NodeKey",
		"User",
		"Tags",
		"IP addresses",
		"Ephemeral",
		"Last seen",
		colExpiration,
		"Connected",
		"Approved",
		"Expired",
	}
	tableData := make(pterm.TableData, 1, 1+len(nodes))
	tableData[0] = tableHeader

	for _, node := range nodes {
		// An absent pre-auth key decodes into a zero NodePreAuthKey, so guard
		// on Id before reading its flags.
		ephemeral := node.PreAuthKey.Id != "" && node.PreAuthKey.Ephemeral

		var lastSeenTime string
		if node.LastSeen != nil {
			lastSeenTime = node.LastSeen.Format(HeadscaleDateTimeFormat)
		}

		expiryTime := "N/A"
		if node.Expiry != nil {
			expiryTime = node.Expiry.Format(HeadscaleDateTimeFormat)
		}

		var machineKey key.MachinePublic

		err := machineKey.UnmarshalText([]byte(node.MachineKey))
		if err != nil {
			machineKey = key.MachinePublic{}
		}

		var nodeKey key.NodePublic

		err = nodeKey.UnmarshalText([]byte(node.NodeKey))
		if err != nil {
			return nil, fmt.Errorf("parsing node key %q: %w", node.NodeKey, err)
		}

		online := pterm.LightRed("offline")
		if node.Online {
			online = pterm.LightGreen("online")
		}

		expired := pterm.LightGreen("no")
		if node.Expiry != nil && node.Expiry.Before(time.Now()) {
			expired = pterm.LightRed("yes")
		}

		approved := pterm.LightRed("pending")
		if node.Approved {
			approved = pterm.LightGreen("yes")
		}

		if node.Suspended {
			approved = pterm.LightRed("suspended")
		}

		tags := strings.Join(node.Tags, "\n")

		var ipBuilder strings.Builder

		for _, addr := range node.IpAddresses {
			ip, err := netip.ParseAddr(addr)
			if err == nil {
				if ipBuilder.Len() > 0 {
					ipBuilder.WriteString("\n")
				}

				ipBuilder.WriteString(ip.String())
			}
		}

		ipAddresses := ipBuilder.String()

		nodeData := []string{
			node.Id,
			node.Name,
			node.GivenName,
			machineKey.ShortString(),
			nodeKey.ShortString(),
			node.User.Name,
			tags,
			ipAddresses,
			strconv.FormatBool(ephemeral),
			lastSeenTime,
			expiryTime,
			online,
			approved,
			expired,
		}
		tableData = append(
			tableData,
			nodeData,
		)
	}

	return tableData, nil
}

func nodeRoutesToPtables(
	nodes []clientv1.Node,
) pterm.TableData {
	tableHeader := []string{
		"ID",
		"Hostname",
		"Approved",
		"Available",
		"Serving (Primary)",
	}
	tableData := make(pterm.TableData, 1, 1+len(nodes))
	tableData[0] = tableHeader

	for _, node := range nodes {
		nodeData := []string{
			node.Id,
			node.GivenName,
			strings.Join(node.ApprovedRoutes, "\n"),
			strings.Join(node.AvailableRoutes, "\n"),
			strings.Join(node.SubnetRoutes, "\n"),
		}
		tableData = append(
			tableData,
			nodeData,
		)
	}

	return tableData
}

var tagCmd = &cobra.Command{
	Use:     "tag",
	Short:   "Manage the tags of a node",
	Aliases: []string{"tags", "t"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			tagsToSet, _ := cmd.Flags().GetStringSlice("tags")

			resp, err := client.SetTagsWithResponse(
				ctx,
				strconv.FormatUint(identifier, util.Base10),
				clientv1.SetTagsJSONRequestBody{
					Tags: &tagsToSet,
				},
			)
			if err != nil {
				return fmt.Errorf("setting tags: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.Node, "Node updated")
		},
	),
}

var approveRoutesCmd = &cobra.Command{
	Use:   "approve-routes",
	Short: "Manage the approved routes of a node",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			routes, _ := cmd.Flags().GetStringSlice("routes")

			resp, err := client.SetApprovedRoutesWithResponse(
				ctx,
				strconv.FormatUint(identifier, util.Base10),
				clientv1.SetApprovedRoutesJSONRequestBody{
					Routes: &routes,
				},
			)
			if err != nil {
				return fmt.Errorf("setting approved routes: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.Node, "Node updated")
		},
	),
}
