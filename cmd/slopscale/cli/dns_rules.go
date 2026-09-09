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
	dnsCmd.AddCommand(dnsRulesCmd)

	dnsRulesCmd.AddCommand(listDNSRulesCmd)

	addDNSRuleFlags(createDNSRuleCmd)
	mustMarkRequired(createDNSRuleCmd, "name", "domain", "nameserver", "group")
	dnsRulesCmd.AddCommand(createDNSRuleCmd)

	updateDNSRuleCmd.Flags().Uint64P("identifier", "i", 0, "Rule identifier (ID)")
	mustMarkRequired(updateDNSRuleCmd, "identifier")
	addDNSRuleFlags(updateDNSRuleCmd)
	dnsRulesCmd.AddCommand(updateDNSRuleCmd)

	deleteDNSRuleCmd.Flags().Uint64P("identifier", "i", 0, "Rule identifier (ID)")
	mustMarkRequired(deleteDNSRuleCmd, "identifier")
	dnsRulesCmd.AddCommand(deleteDNSRuleCmd)
}

func addDNSRuleFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("name", "n", "", "Rule name")
	cmd.Flags().String("description", "", "Rule description")
	cmd.Flags().StringSliceP("domain", "d", []string{}, "Domain the nameservers answer for (repeatable)")
	cmd.Flags().StringSlice("nameserver", []string{}, "Nameserver: IP, IP:port or a provider's DoH URL (repeatable)")
	cmd.Flags().StringSliceP("group", "g", []string{}, "Group identifiers whose machines receive the rule")
	cmd.Flags().Bool("disabled", false, "Keep the rule but hand it to nobody")
}

var dnsRulesCmd = &cobra.Command{
	Use:     "rules",
	Short:   "Manage the split DNS domains and nameservers only some groups receive",
	Aliases: []string{"rule"},
}

var listDNSRulesCmd = &cobra.Command{
	Use:     cmdList,
	Short:   "List group DNS rules",
	Aliases: []string{"ls"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.ListDNSRulesWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("listing dns rules: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			rules := resp.JSON200.Rules

			return printListOutput(cmd, rules, func() error {
				rows := make([][]string, 0, len(rules))
				for _, r := range rules {
					rows = append(rows, []string{
						r.Id,
						r.Name,
						onOff(r.Enabled),
						strings.Join(
							r.Domains,
							", ",
						),
						strings.Join(r.Nameservers, ", "),
						strings.Join(r.GroupIds, ", "),
					})
				}

				return renderTable([]string{"ID", "Name", colEnabled, "Domains", "Nameservers", "Groups"}, rows)
			})
		},
	),
}

func dnsRuleBodyFromFlags(cmd *cobra.Command) clientv1.DNSRuleRequestBody {
	name, _ := cmd.Flags().GetString("name")
	desc, _ := cmd.Flags().GetString("description")
	domains, _ := cmd.Flags().GetStringSlice("domain")
	nameservers, _ := cmd.Flags().GetStringSlice("nameserver")
	groups, _ := cmd.Flags().GetStringSlice("group")
	disabled, _ := cmd.Flags().GetBool("disabled")
	enabled := !disabled

	return clientv1.DNSRuleRequestBody{
		Name:        name,
		Description: &desc,
		Enabled:     &enabled,
		Domains:     &domains,
		Nameservers: &nameservers,
		GroupIds:    &groups,
	}
}

var createDNSRuleCmd = &cobra.Command{
	Use:   cmdCreate,
	Short: "Create a group DNS rule",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.CreateDNSRuleWithResponse(ctx, dnsRuleBodyFromFlags(cmd))
			if err != nil {
				return fmt.Errorf("creating dns rule: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.Rule, "DNS rule created")
		},
	),
}

var updateDNSRuleCmd = &cobra.Command{
	Use:   cmdUpdate,
	Short: "Replace a group DNS rule; flags not given keep their value",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			id := strconv.FormatUint(identifier, util.Base10)

			current, err := client.GetDNSRuleWithResponse(ctx, id)
			if err != nil {
				return fmt.Errorf("getting dns rule: %w", err)
			}

			if current.StatusCode() != http.StatusOK {
				return apiError(current.StatusCode(), current.ApplicationproblemJSONDefault)
			}

			body := dnsRuleUpdateBody(cmd, &current.JSON200.Rule)

			resp, err := client.UpdateDNSRuleWithResponse(ctx, id, body)
			if err != nil {
				return fmt.Errorf("updating dns rule: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.Rule, "DNS rule updated")
		},
	),
}

// dnsRuleUpdateBody starts from the stored rule and replaces only the
// flags the operator gave.
func dnsRuleUpdateBody(cmd *cobra.Command, current *clientv1.DNSRule) clientv1.DNSRuleRequestBody {
	given := dnsRuleBodyFromFlags(cmd)
	body := clientv1.DNSRuleRequestBody{
		Name:        current.Name,
		Description: &current.Description,
		Enabled:     &current.Enabled,
		Domains:     &current.Domains,
		Nameservers: &current.Nameservers,
		GroupIds:    &current.GroupIds,
	}

	if cmd.Flags().Changed("name") {
		body.Name = given.Name
	}

	if cmd.Flags().Changed("description") {
		body.Description = given.Description
	}

	if cmd.Flags().Changed("disabled") {
		body.Enabled = given.Enabled
	}

	if cmd.Flags().Changed("domain") {
		body.Domains = given.Domains
	}

	if cmd.Flags().Changed("nameserver") {
		body.Nameservers = given.Nameservers
	}

	if cmd.Flags().Changed("group") {
		body.GroupIds = given.GroupIds
	}

	return body
}

var deleteDNSRuleCmd = &cobra.Command{
	Use:     cmdDelete,
	Short:   "Delete a group DNS rule",
	Aliases: []string{aliasDel},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")

			resp, err := client.DeleteDNSRuleWithResponse(ctx, strconv.FormatUint(identifier, util.Base10))
			if err != nil {
				return fmt.Errorf("deleting dns rule: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, map[string]string{colResult: "DNS rule deleted"}, "DNS rule deleted")
		},
	),
}
