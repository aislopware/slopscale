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
	rootCmd.AddCommand(appsCmd)

	appsCmd.AddCommand(listAppsCmd)
	appsCmd.AddCommand(showAppCmd)
	appsCmd.AddCommand(createAppCmd)
	appsCmd.AddCommand(updateAppCmd)
	appsCmd.AddCommand(deleteAppCmd)

	showAppCmd.Flags().Uint64P("id", "i", 0, "App identifier (ID)")
	mustMarkRequired(showAppCmd, "id")

	createAppCmd.Flags().StringP("name", "n", "", "App name")
	createAppCmd.Flags().StringSliceP("domain", "d", []string{}, "Domain the connectors route (repeatable)")
	createAppCmd.Flags().StringSlice("connector", []string{"*"}, "Connector node tags (repeatable, default *)")
	createAppCmd.Flags().StringSliceP("route", "r", []string{}, "CIDRs the connectors advertise (repeatable)")
	createAppCmd.Flags().String("description", "", "App description")
	mustMarkRequired(createAppCmd, "name")

	updateAppCmd.Flags().Uint64P("id", "i", 0, "App identifier (ID)")
	updateAppCmd.Flags().StringP("name", "n", "", "App name")
	updateAppCmd.Flags().StringSliceP("domain", "d", []string{}, "Domain the connectors route (repeatable)")
	updateAppCmd.Flags().StringSlice("connector", []string{}, "Connector node tags (repeatable)")
	updateAppCmd.Flags().StringSliceP("route", "r", []string{}, "CIDRs the connectors advertise (repeatable)")
	updateAppCmd.Flags().String("description", "", "App description")
	mustMarkRequired(updateAppCmd, "id")

	deleteAppCmd.Flags().Uint64P("id", "i", 0, "App identifier (ID)")
	mustMarkRequired(deleteAppCmd, "id")
}

var appsCmd = &cobra.Command{
	Use:     "apps",
	Short:   "Manage app connectors, exposing services without running a client on the host",
	Aliases: []string{"app"},
}

var listAppsCmd = &cobra.Command{
	Use:     cmdList,
	Short:   "List apps",
	Aliases: []string{"ls"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.ListAppsWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("listing apps: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			apps := resp.JSON200.Apps

			return printListOutput(cmd, apps, func() error {
				return renderTable(
					[]string{"ID", "Name", "Domains", "Connectors", "Nodes", "Routes"},
					appsToRows(apps),
				)
			})
		},
	),
}

var showAppCmd = &cobra.Command{
	Use:     cmdShow,
	Short:   "Show an app and its connector nodes",
	Aliases: []string{"get"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			_, app, err := fetchApp(ctx, client, cmd)
			if err != nil {
				return err
			}

			return printApp(cmd, app)
		},
	),
}

var createAppCmd = &cobra.Command{
	Use:   cmdCreate,
	Short: "Create an app definition",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			name, _ := cmd.Flags().GetString("name")
			body := clientv1.CreateAppJSONRequestBody{Name: name}

			if cmd.Flags().Changed("description") {
				desc, _ := cmd.Flags().GetString("description")
				body.Description = &desc
			}

			if cmd.Flags().Changed("domain") {
				domains, _ := cmd.Flags().GetStringSlice("domain")
				domains = compactStrings(domains)
				body.Domains = &domains
			}

			connectors, _ := cmd.Flags().GetStringSlice("connector")

			connectors = compactStrings(connectors)
			if len(connectors) > 0 {
				body.Connectors = &connectors
			}

			if cmd.Flags().Changed("route") {
				routes, _ := cmd.Flags().GetStringSlice("route")
				routes = compactStrings(routes)
				body.Routes = &routes
			}

			resp, err := client.CreateAppWithResponse(ctx, body)
			if err != nil {
				return fmt.Errorf("creating app: %w", err)
			}

			if resp.StatusCode() != http.StatusCreated {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printApp(cmd, &resp.JSON201.App)
		},
	),
}

var updateAppCmd = &cobra.Command{
	Use:   cmdUpdate,
	Short: "Update an app definition",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			appID, current, err := fetchApp(ctx, client, cmd)
			if err != nil {
				return err
			}

			body := buildUpdateAppBody(cmd, current)

			resp, err := client.UpdateAppWithResponse(ctx, appID, body)
			if err != nil {
				return fmt.Errorf("updating app: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printApp(cmd, &resp.JSON200.App)
		},
	),
}

//nolint:dupl // standard CRUD delete command structure
var deleteAppCmd = &cobra.Command{
	Use:     cmdDelete,
	Short:   "Delete an app",
	Aliases: []string{aliasDel},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			appID, current, err := fetchApp(ctx, client, cmd)
			if err != nil {
				return err
			}

			prompt := fmt.Sprintf("Do you want to remove the app %s?", current.Name)
			if !confirmAction(cmd, prompt) {
				return printOutput(cmd, map[string]string{colResult: "App not deleted"}, "App not deleted")
			}

			resp, err := client.DeleteAppWithResponse(ctx, appID)
			if err != nil {
				return fmt.Errorf("deleting app: %w", err)
			}

			if resp.StatusCode() != http.StatusNoContent {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, map[string]string{colResult: "App deleted"}, "App deleted")
		},
	),
}

func fetchApp(
	ctx context.Context,
	client *clientv1.ClientWithResponses,
	cmd *cobra.Command,
) (string, *clientv1.App, error) {
	id, _ := cmd.Flags().GetUint64("id")
	appID := strconv.FormatUint(id, util.Base10)

	resp, err := client.GetAppWithResponse(ctx, appID)
	if err != nil {
		return "", nil, fmt.Errorf("getting app: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return "", nil, apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
	}

	return appID, &resp.JSON200.App, nil
}

func buildUpdateAppBody(cmd *cobra.Command, current *clientv1.App) clientv1.UpdateAppJSONRequestBody {
	body := clientv1.UpdateAppJSONRequestBody{
		Name:        current.Name,
		Description: &current.Description,
		Domains:     &current.Domains,
		Connectors:  &current.Connectors,
		Routes:      &current.Routes,
	}

	if cmd.Flags().Changed("name") {
		name, _ := cmd.Flags().GetString("name")
		body.Name = name
	}

	if cmd.Flags().Changed("description") {
		desc, _ := cmd.Flags().GetString("description")
		body.Description = &desc
	}

	if cmd.Flags().Changed("domain") {
		domains, _ := cmd.Flags().GetStringSlice("domain")
		domains = compactStrings(domains)
		body.Domains = &domains
	}

	if cmd.Flags().Changed("connector") {
		connectors, _ := cmd.Flags().GetStringSlice("connector")
		connectors = compactStrings(connectors)
		body.Connectors = &connectors
	}

	if cmd.Flags().Changed("route") {
		routes, _ := cmd.Flags().GetStringSlice("route")
		routes = compactStrings(routes)
		body.Routes = &routes
	}

	return body
}

func appsToRows(apps []clientv1.App) [][]string {
	rows := make([][]string, 0, len(apps))

	for i := range apps {
		a := &apps[i]
		onlineCount := 0

		for _, n := range a.Nodes {
			if n.Online {
				onlineCount++
			}
		}

		rows = append(rows, []string{
			a.Id,
			a.Name,
			strings.Join(a.Domains, ", "),
			strings.Join(a.Connectors, ", "),
			fmt.Sprintf("%d (%d online)", len(a.Nodes), onlineCount),
			strings.Join(a.Routes, ", "),
		})
	}

	return rows
}

func printApp(cmd *cobra.Command, a *clientv1.App) error {
	return printListOutput(cmd, a, func() error {
		return printAppHuman(a)
	})
}

func printAppHuman(a *clientv1.App) error {
	fmt.Printf("ID: %s\n", a.Id)
	fmt.Printf("Name: %s\n", a.Name)

	if a.Description != "" {
		fmt.Printf("Description: %s\n", a.Description)
	}

	if len(a.Domains) > 0 {
		fmt.Printf("Domains: %s\n", strings.Join(a.Domains, ", "))
	} else {
		fmt.Println("Domains: (none)")
	}

	if len(a.Connectors) > 0 {
		fmt.Printf("Connectors: %s\n", strings.Join(a.Connectors, ", "))
	} else {
		fmt.Println("Connectors: (none)")
	}

	if len(a.Routes) > 0 {
		fmt.Printf("Routes: %s\n", strings.Join(a.Routes, ", "))
	} else {
		fmt.Println("Routes: (none)")
	}

	if len(a.Nodes) == 0 {
		fmt.Println("no connector nodes")

		return nil
	}

	rows := make([][]string, 0, len(a.Nodes))

	for _, n := range a.Nodes {
		online := "offline"
		if n.Online {
			online = "online"
		}

		rows = append(rows, []string{
			n.NodeId,
			n.Name,
			online,
			strconv.FormatInt(n.LearnedRoutes, util.Base10),
			strconv.FormatInt(n.Pending, util.Base10),
		})
	}

	fmt.Println()

	return renderTable([]string{"ID", "Name", "Online", "Learned routes", "Pending routes"}, rows)
}
