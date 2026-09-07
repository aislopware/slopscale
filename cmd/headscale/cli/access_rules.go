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
	rootCmd.AddCommand(accessRulesCmd)

	accessRulesCmd.AddCommand(listAccessRulesCmd)

	addAccessRuleFlags(createAccessRuleCmd)
	mustMarkRequired(createAccessRuleCmd, "name")
	mustMarkRequired(createAccessRuleCmd, "src")
	mustMarkRequired(createAccessRuleCmd, "dst")
	accessRulesCmd.AddCommand(createAccessRuleCmd)

	updateAccessRuleCmd.Flags().Uint64P("identifier", "i", 0, "Access rule identifier (ID)")
	mustMarkRequired(updateAccessRuleCmd, "identifier")
	addAccessRuleFlags(updateAccessRuleCmd)
	mustMarkRequired(updateAccessRuleCmd, "name")
	mustMarkRequired(updateAccessRuleCmd, "src")
	mustMarkRequired(updateAccessRuleCmd, "dst")
	accessRulesCmd.AddCommand(updateAccessRuleCmd)

	enableAccessRuleCmd.Flags().Uint64P("identifier", "i", 0, "Access rule identifier (ID)")
	mustMarkRequired(enableAccessRuleCmd, "identifier")
	accessRulesCmd.AddCommand(enableAccessRuleCmd)

	disableAccessRuleCmd.Flags().Uint64P("identifier", "i", 0, "Access rule identifier (ID)")
	mustMarkRequired(disableAccessRuleCmd, "identifier")
	accessRulesCmd.AddCommand(disableAccessRuleCmd)

	deleteAccessRuleCmd.Flags().Uint64P("identifier", "i", 0, "Access rule identifier (ID)")
	mustMarkRequired(deleteAccessRuleCmd, "identifier")
	accessRulesCmd.AddCommand(deleteAccessRuleCmd)
}

func addAccessRuleFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("name", "n", "", "Rule name")
	cmd.Flags().String("description", "", "Rule description")
	cmd.Flags().StringSliceP("src", "s", []string{}, "Source group identifier (repeatable)")
	cmd.Flags().StringSliceP("dst", "d", []string{}, "Destination group identifier (repeatable)")
	cmd.Flags().StringP("protocol", "p", "all", "Protocol: all, tcp, udp, icmp")
	cmd.Flags().String("ports", "", "Ports (e.g. \"22,80-90\")")
	cmd.Flags().Bool("bidirectional", false, "Allow bidirectional traffic")
	cmd.Flags().Bool("disabled", false, "Create rule in disabled state")
}

var accessRulesCmd = &cobra.Command{
	Use:     "access-rules",
	Short:   "Manage access rules",
	Aliases: []string{"access-rule", "rules"},
}

var listAccessRulesCmd = &cobra.Command{
	Use:     cmdList,
	Short:   "List access rules",
	Aliases: []string{"ls"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.ListAccessRulesWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("listing access rules: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			rules := resp.JSON200.Rules

			return printListOutput(cmd, rules, func() error {
				rows := make([][]string, 0, len(rules))
				for _, rule := range rules {
					direction := "one-way"
					if rule.Bidirectional {
						direction = "both"
					}

					enabled := "no"
					if rule.Enabled {
						enabled = "yes"
					}

					rows = append(
						rows,
						[]string{
							rule.Id,
							rule.Name,
							strings.Join(rule.SourceGroupIds, ","),
							strings.Join(rule.DestinationGroupIds, ","),
							rule.Protocol,
							rule.Ports,
							direction,
							enabled,
						},
					)
				}

				return renderTable(
					[]string{"ID", "Name", "Sources", "Destinations", "Protocol", "Ports", "Direction", "Enabled"},
					rows,
				)
			})
		},
	),
}

var createAccessRuleCmd = &cobra.Command{
	Use:   cmdCreate,
	Short: "Create an access rule",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			name, _ := cmd.Flags().GetString("name")
			srcs, _ := cmd.Flags().GetStringSlice("src")
			dsts, _ := cmd.Flags().GetStringSlice("dst")
			protocol, _ := cmd.Flags().GetString("protocol")
			ports, _ := cmd.Flags().GetString("ports")
			bidi, _ := cmd.Flags().GetBool("bidirectional")
			disabled, _ := cmd.Flags().GetBool("disabled")
			enabled := !disabled

			body := clientv1.CreateAccessRuleJSONRequestBody{
				Name:                name,
				Enabled:             &enabled,
				Protocol:            protocol,
				Ports:               &ports,
				Bidirectional:       &bidi,
				SourceGroupIds:      &srcs,
				DestinationGroupIds: &dsts,
			}

			if cmd.Flags().Changed("description") {
				desc, _ := cmd.Flags().GetString("description")
				body.Description = &desc
			}

			resp, err := client.CreateAccessRuleWithResponse(ctx, body)
			if err != nil {
				return fmt.Errorf("creating access rule: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.Rule, "Access rule created")
		},
	),
}

var updateAccessRuleCmd = &cobra.Command{
	Use:   "update",
	Short: "Update an access rule",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			ruleID := strconv.FormatUint(identifier, util.Base10)

			name, _ := cmd.Flags().GetString("name")
			desc, _ := cmd.Flags().GetString("description")
			srcs, _ := cmd.Flags().GetStringSlice("src")
			dsts, _ := cmd.Flags().GetStringSlice("dst")
			protocol, _ := cmd.Flags().GetString("protocol")
			ports, _ := cmd.Flags().GetString("ports")
			bidi, _ := cmd.Flags().GetBool("bidirectional")
			disabled, _ := cmd.Flags().GetBool("disabled")
			enabled := !disabled

			body := clientv1.UpdateAccessRuleJSONRequestBody{
				Name:                name,
				Description:         &desc,
				Enabled:             &enabled,
				Protocol:            protocol,
				Ports:               &ports,
				Bidirectional:       &bidi,
				SourceGroupIds:      &srcs,
				DestinationGroupIds: &dsts,
			}

			resp, err := client.UpdateAccessRuleWithResponse(ctx, ruleID, body)
			if err != nil {
				return fmt.Errorf("updating access rule: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, resp.JSON200.Rule, "Access rule updated")
		},
	),
}

var enableAccessRuleCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable an access rule",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			return setAccessRuleEnabled(ctx, client, cmd, true, "Access rule enabled")
		},
	),
}

var disableAccessRuleCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable an access rule",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			return setAccessRuleEnabled(ctx, client, cmd, false, "Access rule disabled")
		},
	),
}

var deleteAccessRuleCmd = &cobra.Command{
	Use:     cmdDelete,
	Short:   "Delete an access rule",
	Aliases: []string{aliasDel},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			ruleID := strconv.FormatUint(identifier, util.Base10)

			resp, err := client.DeleteAccessRuleWithResponse(ctx, ruleID)
			if err != nil {
				return fmt.Errorf("deleting access rule: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, map[string]string{colResult: "Access rule deleted"}, "Access rule deleted")
		},
	),
}

func setAccessRuleEnabled(
	ctx context.Context,
	client *clientv1.ClientWithResponses,
	cmd *cobra.Command,
	enabled bool,
	successMsg string,
) error {
	identifier, _ := cmd.Flags().GetUint64("identifier")
	ruleID := strconv.FormatUint(identifier, util.Base10)

	getResp, err := client.GetAccessRuleWithResponse(ctx, ruleID)
	if err != nil {
		return fmt.Errorf("getting access rule: %w", err)
	}

	if getResp.StatusCode() != http.StatusOK {
		return apiError(getResp.StatusCode(), getResp.ApplicationproblemJSONDefault)
	}

	rule := getResp.JSON200.Rule
	body := clientv1.UpdateAccessRuleJSONRequestBody{
		Name:                rule.Name,
		Description:         &rule.Description,
		Enabled:             &enabled,
		Protocol:            rule.Protocol,
		Ports:               &rule.Ports,
		Bidirectional:       &rule.Bidirectional,
		SourceGroupIds:      &rule.SourceGroupIds,
		DestinationGroupIds: &rule.DestinationGroupIds,
	}

	updateResp, err := client.UpdateAccessRuleWithResponse(ctx, ruleID, body)
	if err != nil {
		return fmt.Errorf("updating access rule: %w", err)
	}

	if updateResp.StatusCode() != http.StatusOK {
		return apiError(updateResp.StatusCode(), updateResp.ApplicationproblemJSONDefault)
	}

	return printOutput(cmd, updateResp.JSON200.Rule, successMsg)
}
