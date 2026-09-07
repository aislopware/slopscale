package cli

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/juanfont/headscale/hscontrol/util"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(networksCmd)

	networksCmd.AddCommand(listNetworksCmd)
	networksCmd.AddCommand(showNetworkCmd)
	networksCmd.AddCommand(createNetworkCmd)
	networksCmd.AddCommand(updateNetworkCmd)
	networksCmd.AddCommand(enableNetworkCmd)
	networksCmd.AddCommand(disableNetworkCmd)
	networksCmd.AddCommand(deleteNetworkCmd)

	showNetworkCmd.Flags().Uint64P("identifier", "i", 0, "Network identifier (ID)")
	mustMarkRequired(showNetworkCmd, "identifier")

	createNetworkCmd.Flags().StringP("name", "n", "", "Network name")
	createNetworkCmd.Flags().StringP("description", "d", "", "Network description")
	createNetworkCmd.Flags().StringSliceP("prefix", "p", []string{}, "Network prefixes (CIDR)")
	createNetworkCmd.Flags().StringSliceP("router", "r", []string{}, "Router node identifiers")
	createNetworkCmd.Flags().StringSliceP("group", "g", []string{}, "Group identifiers")
	createNetworkCmd.Flags().Bool("disabled", false, "Create network in disabled state")
	mustMarkRequired(createNetworkCmd, "name", "group")

	updateNetworkCmd.Flags().Uint64P("identifier", "i", 0, "Network identifier (ID)")
	updateNetworkCmd.Flags().StringP("name", "n", "", "Network name")
	updateNetworkCmd.Flags().StringP("description", "d", "", "Network description")
	updateNetworkCmd.Flags().StringSliceP("prefix", "p", []string{}, "Network prefixes (CIDR)")
	updateNetworkCmd.Flags().StringSliceP("router", "r", []string{}, "Router node identifiers")
	updateNetworkCmd.Flags().StringSliceP("group", "g", []string{}, "Group identifiers")
	updateNetworkCmd.Flags().Bool("disabled", false, "Disable the network")
	mustMarkRequired(updateNetworkCmd, "identifier")

	enableNetworkCmd.Flags().Uint64P("identifier", "i", 0, "Network identifier (ID)")
	mustMarkRequired(enableNetworkCmd, "identifier")

	disableNetworkCmd.Flags().Uint64P("identifier", "i", 0, "Network identifier (ID)")
	mustMarkRequired(disableNetworkCmd, "identifier")

	deleteNetworkCmd.Flags().Uint64P("identifier", "i", 0, "Network identifier (ID)")
	mustMarkRequired(deleteNetworkCmd, "identifier")
}

var networksCmd = &cobra.Command{
	Use:     "networks",
	Short:   "Manage networks: prefixes routed through nodes and handed to groups",
	Aliases: []string{"network", "net"},
}

var listNetworksCmd = &cobra.Command{
	Use:     cmdList,
	Short:   "List networks",
	Aliases: []string{"ls"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.ListNetworksWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("listing networks: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			networks := resp.JSON200.Networks

			return printListOutput(cmd, networks, func() error {
				rows := networksToRows(networks)

				return renderTable(
					[]string{"ID", "Name", "Enabled", "Prefixes", "Routers", "Groups", "Exit node"},
					rows,
				)
			})
		},
	),
}

var showNetworkCmd = &cobra.Command{
	Use:     cmdShow,
	Short:   "Show a network",
	Aliases: []string{"get"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			netID := strconv.FormatUint(identifier, util.Base10)

			resp, err := client.GetNetworkWithResponse(ctx, netID)
			if err != nil {
				return fmt.Errorf("getting network: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printNetwork(cmd, &resp.JSON200.Network)
		},
	),
}

var createNetworkCmd = &cobra.Command{
	Use:   cmdCreate,
	Short: "Create a network",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			name, _ := cmd.Flags().GetString("name")
			prefixes, _ := cmd.Flags().GetStringSlice("prefix")
			groups, _ := cmd.Flags().GetStringSlice("group")
			routers, _ := cmd.Flags().GetStringSlice("router")
			disabled, _ := cmd.Flags().GetBool("disabled")
			enabled := !disabled

			body := clientv1.CreateNetworkJSONRequestBody{
				Name:          name,
				Enabled:       &enabled,
				Prefixes:      &prefixes,
				GroupIds:      &groups,
				RouterNodeIds: &routers,
			}

			if cmd.Flags().Changed("description") {
				desc, _ := cmd.Flags().GetString("description")
				body.Description = &desc
			}

			resp, err := client.CreateNetworkWithResponse(ctx, body)
			if err != nil {
				return fmt.Errorf("creating network: %w", err)
			}

			if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusCreated {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printNetwork(cmd, &resp.JSON200.Network)
		},
	),
}

var updateNetworkCmd = &cobra.Command{
	Use:   "update",
	Short: "Update a network",
	Long: `Replaces the network configuration. The update command fetches the current
network, overrides only the fields specified by flags, and sends the merged
configuration.`,
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			netID := strconv.FormatUint(identifier, util.Base10)

			getResp, err := client.GetNetworkWithResponse(ctx, netID)
			if err != nil {
				return fmt.Errorf("getting network: %w", err)
			}

			if getResp.StatusCode() != http.StatusOK {
				return apiError(getResp.StatusCode(), getResp.ApplicationproblemJSONDefault)
			}

			body := buildUpdateNetworkBody(cmd, &getResp.JSON200.Network)

			resp, err := client.UpdateNetworkWithResponse(ctx, netID, body)
			if err != nil {
				return fmt.Errorf("updating network: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printNetwork(cmd, &resp.JSON200.Network)
		},
	),
}

var enableNetworkCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable a network",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			return setNetworkEnabled(ctx, client, cmd, true, "enable")
		},
	),
}

var disableNetworkCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable a network",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			return setNetworkEnabled(ctx, client, cmd, false, "disable")
		},
	),
}

var deleteNetworkCmd = &cobra.Command{
	Use:     cmdDelete,
	Short:   "Delete a network",
	Aliases: []string{aliasDel},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			netID := strconv.FormatUint(identifier, util.Base10)

			getResp, err := client.GetNetworkWithResponse(ctx, netID)
			if err != nil {
				return fmt.Errorf("getting network: %w", err)
			}

			if getResp.StatusCode() != http.StatusOK {
				return apiError(getResp.StatusCode(), getResp.ApplicationproblemJSONDefault)
			}

			prompt := fmt.Sprintf("Do you want to remove the network %s?", getResp.JSON200.Network.Name)
			if !confirmAction(cmd, prompt) {
				return printOutput(cmd, map[string]string{colResult: "Network not deleted"}, "Network not deleted")
			}

			deleteResp, err := client.DeleteNetworkWithResponse(ctx, netID)
			if err != nil {
				return fmt.Errorf("deleting network: %w", err)
			}

			if deleteResp.StatusCode() != http.StatusOK && deleteResp.StatusCode() != http.StatusNoContent {
				return apiError(deleteResp.StatusCode(), deleteResp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, map[string]string{colResult: "Network deleted"}, "Network deleted")
		},
	),
}

func setNetworkEnabled(
	ctx context.Context,
	client *clientv1.ClientWithResponses,
	cmd *cobra.Command,
	enabled bool,
	action string,
) error {
	identifier, _ := cmd.Flags().GetUint64("identifier")
	netID := strconv.FormatUint(identifier, util.Base10)

	resp, err := client.SetNetworkEnabledWithResponse(ctx, netID, clientv1.SetNetworkEnabledJSONRequestBody{
		Enabled: enabled,
	})
	if err != nil {
		return fmt.Errorf("%sing network: %w", action, err)
	}

	if resp.StatusCode() != http.StatusOK {
		return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
	}

	msg := fmt.Sprintf("Network %s %sd", resp.JSON200.Network.Name, action)

	return printOutput(cmd, map[string]string{colResult: msg}, msg)
}

func buildUpdateNetworkBody(
	cmd *cobra.Command,
	current *clientv1.Network,
) clientv1.UpdateNetworkJSONRequestBody {
	desc := current.Description
	enabled := current.Enabled
	prefixes := current.Prefixes
	groups := current.GroupIds
	routers := current.RouterNodeIds

	body := clientv1.UpdateNetworkJSONRequestBody{
		Name:          current.Name,
		Description:   &desc,
		Enabled:       &enabled,
		Prefixes:      &prefixes,
		GroupIds:      &groups,
		RouterNodeIds: &routers,
	}

	if cmd.Flags().Changed("name") {
		name, _ := cmd.Flags().GetString("name")
		body.Name = name
	}

	if cmd.Flags().Changed("description") {
		d, _ := cmd.Flags().GetString("description")
		body.Description = &d
	}

	if cmd.Flags().Changed("disabled") {
		disabled, _ := cmd.Flags().GetBool("disabled")
		e := !disabled
		body.Enabled = &e
	}

	if cmd.Flags().Changed("prefix") {
		pfx, _ := cmd.Flags().GetStringSlice("prefix")
		body.Prefixes = &pfx
	}

	if cmd.Flags().Changed("group") {
		grp, _ := cmd.Flags().GetStringSlice("group")
		body.GroupIds = &grp
	}

	if cmd.Flags().Changed("router") {
		rtr, _ := cmd.Flags().GetStringSlice("router")
		body.RouterNodeIds = &rtr
	}

	return body
}

func networksToRows(networks []clientv1.Network) [][]string {
	rows := make([][]string, 0, len(networks))

	for _, n := range networks {
		routerNames := make([]string, 0, len(n.Routers))

		for _, r := range n.Routers {
			name := r.Name
			if !r.Online {
				name += " (offline)"
			}

			routerNames = append(routerNames, name)
		}

		exitNode := ""
		if n.ExitNode {
			exitNode = "yes"
		}

		rows = append(rows, []string{
			n.Id,
			n.Name,
			onOff(n.Enabled),
			strings.Join(n.Prefixes, ", "),
			strings.Join(routerNames, ", "),
			strings.Join(n.GroupIds, ", "),
			exitNode,
		})
	}

	return rows
}

func printNetwork(cmd *cobra.Command, network *clientv1.Network) error {
	return printListOutput(cmd, network, func() error {
		return printNetworkHuman(network)
	})
}

func printNetworkHuman(n *clientv1.Network) error {
	fmt.Printf("Name: %s\n", n.Name)

	if n.Description != "" {
		fmt.Printf("Description: %s\n", n.Description)
	}

	fmt.Printf("Enabled: %s\n", onOff(n.Enabled))

	if len(n.Prefixes) > 0 {
		fmt.Printf("Prefixes: %s\n", strings.Join(n.Prefixes, ", "))
	} else {
		fmt.Println("Prefixes: (none)")
	}

	if len(n.GroupIds) > 0 {
		fmt.Printf("Groups: %s\n", strings.Join(n.GroupIds, ", "))
	} else {
		fmt.Println("Groups: (none)")
	}

	if len(n.Routers) == 0 {
		fmt.Println("no routers")

		return nil
	}

	rows := make([][]string, 0, len(n.Routers))
	for _, r := range n.Routers {
		online := "offline"
		if r.Online {
			online = "online"
		}

		var missing string
		if len(r.MissingPrefixes) > 0 {
			missing = strings.Join(r.MissingPrefixes, ", ")
		}

		rows = append(rows, []string{
			r.NodeId,
			r.Name,
			online,
			strings.Join(r.PrimaryPrefixes, ", "),
			missing,
		})
	}

	return renderTable([]string{"ID", "Name", "Online", "Serving", "Missing"}, rows)
}
