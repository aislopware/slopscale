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
	rootCmd.AddCommand(servicesCmd)

	servicesCmd.AddCommand(listServicesCmd)
	servicesCmd.AddCommand(showServiceCmd)
	servicesCmd.AddCommand(createServiceCmd)
	servicesCmd.AddCommand(updateServiceCmd)
	servicesCmd.AddCommand(deleteServiceCmd)
	servicesCmd.AddCommand(approveServiceCmd)

	showServiceCmd.Flags().StringP("name", "n", "", "Service name, svc:web or web")
	mustMarkRequired(showServiceCmd, "name")

	createServiceCmd.Flags().StringP("name", "n", "", "Service name, svc:web or web; a DNS label")
	createServiceCmd.Flags().String("display-name", "", "The label clients show")
	createServiceCmd.Flags().StringP("comment", "c", "", "A note for operators")
	createServiceCmd.Flags().
		StringSliceP("port", "p", []string{}, "Protocol and ports clients are told about, such as tcp:443 or udp:53-60")
	mustMarkRequired(createServiceCmd, "name")

	updateServiceCmd.Flags().StringP("name", "n", "", "Service name, svc:web or web")
	updateServiceCmd.Flags().String("display-name", "", "The label clients show")
	updateServiceCmd.Flags().StringP("comment", "c", "", "A note for operators")
	updateServiceCmd.Flags().StringSliceP("port", "p", []string{}, "Protocol and ports; pass an empty value to clear")
	mustMarkRequired(updateServiceCmd, "name")

	deleteServiceCmd.Flags().StringP("name", "n", "", "Service name, svc:web or web")
	mustMarkRequired(deleteServiceCmd, "name")

	approveServiceCmd.Flags().Uint64P("identifier", "i", 0, "Node identifier (ID)")
	approveServiceCmd.Flags().StringSliceP("service", "s", []string{}, "Services the node may host")
	mustMarkRequired(approveServiceCmd, "identifier")
}

var servicesCmd = &cobra.Command{
	Use:     "services",
	Short:   "Manage Tailscale Services, names with their own addresses served by tagged nodes",
	Aliases: []string{"service", "svc"},
}

var listServicesCmd = &cobra.Command{
	Use:     cmdList,
	Short:   "List services",
	Aliases: []string{"ls"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.ListServicesWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("listing services: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			services := resp.JSON200.Services

			return printListOutput(cmd, services, func() error {
				return renderTable(
					[]string{"Name", "Display name", "DNS name", "Addresses", "Ports", "Hosts", "Serving"},
					servicesToRows(services),
				)
			})
		},
	),
}

var showServiceCmd = &cobra.Command{
	Use:     cmdShow,
	Short:   "Show a service and its hosts",
	Aliases: []string{"get"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			name, _ := cmd.Flags().GetString("name")

			resp, err := client.GetServiceWithResponse(ctx, name)
			if err != nil {
				return fmt.Errorf("getting service: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printService(cmd, &resp.JSON200.Service)
		},
	),
}

var createServiceCmd = &cobra.Command{
	Use:   cmdCreate,
	Short: "Create a service; the server gives it an address of each family",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			name, _ := cmd.Flags().GetString("name")
			body := clientv1.CreateServiceJSONRequestBody{Name: name}

			if cmd.Flags().Changed("display-name") {
				displayName, _ := cmd.Flags().GetString("display-name")
				body.DisplayName = &displayName
			}

			if cmd.Flags().Changed("comment") {
				comment, _ := cmd.Flags().GetString("comment")
				body.Comment = &comment
			}

			if cmd.Flags().Changed("port") {
				ports, _ := cmd.Flags().GetStringSlice("port")
				body.Ports = &ports
			}

			resp, err := client.CreateServiceWithResponse(ctx, body)
			if err != nil {
				return fmt.Errorf("creating service: %w", err)
			}

			if resp.StatusCode() != http.StatusCreated {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printService(cmd, &resp.JSON201.Service)
		},
	),
}

var updateServiceCmd = &cobra.Command{
	Use:   cmdUpdate,
	Short: "Update a service's display name, comment or ports",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			name, _ := cmd.Flags().GetString("name")

			var body clientv1.UpdateServiceJSONRequestBody

			if cmd.Flags().Changed("display-name") {
				displayName, _ := cmd.Flags().GetString("display-name")
				body.DisplayName = &displayName
			}

			if cmd.Flags().Changed("comment") {
				comment, _ := cmd.Flags().GetString("comment")
				body.Comment = &comment
			}

			if cmd.Flags().Changed("port") {
				ports, _ := cmd.Flags().GetStringSlice("port")
				ports = compactStrings(ports)
				body.Ports = &ports
			}

			resp, err := client.UpdateServiceWithResponse(ctx, name, body)
			if err != nil {
				return fmt.Errorf("updating service: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printService(cmd, &resp.JSON200.Service)
		},
	),
}

var deleteServiceCmd = &cobra.Command{
	Use:     cmdDelete,
	Short:   "Delete a service and free its addresses",
	Aliases: []string{aliasDel},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			name, _ := cmd.Flags().GetString("name")

			getResp, err := client.GetServiceWithResponse(ctx, name)
			if err != nil {
				return fmt.Errorf("getting service: %w", err)
			}

			if getResp.StatusCode() != http.StatusOK {
				return apiError(getResp.StatusCode(), getResp.ApplicationproblemJSONDefault)
			}

			prompt := fmt.Sprintf("Do you want to remove the service %s?", getResp.JSON200.Service.Name)
			if !confirmAction(cmd, prompt) {
				return printOutput(cmd, map[string]string{colResult: "Service not deleted"}, "Service not deleted")
			}

			deleteResp, err := client.DeleteServiceWithResponse(ctx, name)
			if err != nil {
				return fmt.Errorf("deleting service: %w", err)
			}

			if deleteResp.StatusCode() != http.StatusNoContent {
				return apiError(deleteResp.StatusCode(), deleteResp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, map[string]string{colResult: "Service deleted"}, "Service deleted")
		},
	),
}

var approveServiceCmd = &cobra.Command{
	Use:   "approve",
	Short: "Set the services a tagged node may host",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			services, _ := cmd.Flags().GetStringSlice("service")
			services = compactStrings(services)

			resp, err := client.SetApprovedServicesWithResponse(
				ctx,
				strconv.FormatUint(identifier, util.Base10),
				clientv1.SetApprovedServicesJSONRequestBody{Services: services},
			)
			if err != nil {
				return fmt.Errorf("approving services: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.Node, "Node updated")
		},
	),
}

// compactStrings drops empty entries, so `--port ""` clears a list.
func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))

	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}

	return out
}

func serviceStatus(s *clientv1.Service) string {
	for _, h := range s.Hosts {
		if h.Primary {
			return h.Name
		}
	}

	for _, h := range s.Hosts {
		if h.Announced && !h.Approved {
			return "waiting for approval"
		}
	}

	return "no host"
}

func servicesToRows(services []clientv1.Service) [][]string {
	rows := make([][]string, 0, len(services))

	for i := range services {
		s := &services[i]
		rows = append(rows, []string{
			s.Name,
			s.DisplayName,
			s.DnsName,
			strings.Join(s.Addresses, ", "),
			strings.Join(s.Ports, ", "),
			strconv.Itoa(len(s.Hosts)),
			serviceStatus(s),
		})
	}

	return rows
}

func printService(cmd *cobra.Command, s *clientv1.Service) error {
	return printListOutput(cmd, s, func() error {
		return printServiceHuman(s)
	})
}

func printServiceHuman(s *clientv1.Service) error {
	fmt.Printf("Name: %s\n", s.Name)

	if s.DisplayName != "" {
		fmt.Printf("Display name: %s\n", s.DisplayName)
	}

	if s.Comment != "" {
		fmt.Printf("Comment: %s\n", s.Comment)
	}

	if s.DnsName != "" {
		fmt.Printf("DNS name: %s\n", s.DnsName)
	}

	fmt.Printf("Addresses: %s\n", strings.Join(s.Addresses, ", "))

	if len(s.Ports) > 0 {
		fmt.Printf("Ports: %s\n", strings.Join(s.Ports, ", "))
	} else {
		fmt.Println("Ports: (whatever the hosts serve)")
	}

	fmt.Printf("Serving: %s\n", serviceStatus(s))

	if len(s.Hosts) == 0 {
		fmt.Println("no hosts")

		return nil
	}

	rows := make([][]string, 0, len(s.Hosts))
	for _, h := range s.Hosts {
		rows = append(rows, []string{
			h.NodeId, h.Name, yesNo(h.Announced), yesNo(h.Active), strings.Join(h.Ports, ", "),
			yesNo(h.Approved), yesNo(h.Primary),
		})
	}

	fmt.Println()

	return renderTable([]string{"ID", "Node", "Announced", "Active", "Ports", "Approved", "Serving"}, rows)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}

	return "no"
}
