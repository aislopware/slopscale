package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/juanfont/headscale/hscontrol/util"
	"github.com/spf13/cobra"
)

var (
	errInvalidProvider = errors.New(
		"invalid provider; must be one of: slack, mattermost, googlechat, discord, teams, telegram, ntfy, email",
	)
	errDeliveryFailed = errors.New("delivery failed")
)

const (
	secretNoteFormat = "Secret (shown once, store it now): %s"
	defaultProvider  = "json"
	providerUsage    = "Provider: slack, mattermost, googlechat, discord, teams, telegram, ntfy, email, " +
		"or json for the signed array"
	neverDelivered = "never"
)

func init() {
	rootCmd.AddCommand(webhooksCmd)

	webhooksCmd.AddCommand(listWebhooksCmd)
	webhooksCmd.AddCommand(showWebhookCmd)
	webhooksCmd.AddCommand(listWebhookEventTypesCmd)
	webhooksCmd.AddCommand(createWebhookCmd)
	webhooksCmd.AddCommand(updateWebhookCmd)
	webhooksCmd.AddCommand(rotateWebhookCmd)
	webhooksCmd.AddCommand(testWebhookCmd)
	webhooksCmd.AddCommand(deleteWebhookCmd)
	webhooksCmd.AddCommand(listWebhookDeliveriesCmd)

	showWebhookCmd.Flags().Uint64P("identifier", "i", 0, "Webhook identifier (ID)")
	mustMarkRequired(showWebhookCmd, "identifier")

	createWebhookCmd.Flags().StringP("url", "u", "", "Webhook endpoint URL (mailto:a@x,b@y for email)")
	createWebhookCmd.Flags().StringP("description", "d", "", "Webhook description")
	createWebhookCmd.Flags().
		StringP("provider", "p", "", providerUsage)
	createWebhookCmd.Flags().StringSliceP("event", "e", []string{}, "Subscribed event types")
	mustMarkRequired(createWebhookCmd, "url", "event")

	updateWebhookCmd.Flags().Uint64P("identifier", "i", 0, "Webhook identifier (ID)")
	updateWebhookCmd.Flags().StringP("url", "u", "", "Webhook endpoint URL")
	updateWebhookCmd.Flags().StringP("description", "d", "", "Webhook description")
	updateWebhookCmd.Flags().
		StringP("provider", "p", "", providerUsage)
	updateWebhookCmd.Flags().StringSliceP("event", "e", []string{}, "Subscribed event types")
	mustMarkRequired(updateWebhookCmd, "identifier")

	rotateWebhookCmd.Flags().Uint64P("identifier", "i", 0, "Webhook identifier (ID)")
	mustMarkRequired(rotateWebhookCmd, "identifier")

	testWebhookCmd.Flags().Uint64P("identifier", "i", 0, "Webhook identifier (ID)")
	mustMarkRequired(testWebhookCmd, "identifier")

	deleteWebhookCmd.Flags().Uint64P("identifier", "i", 0, "Webhook identifier (ID)")
	mustMarkRequired(deleteWebhookCmd, "identifier")

	listWebhookDeliveriesCmd.Flags().Uint64P("identifier", "i", 0, "Webhook identifier (ID)")
	mustMarkRequired(listWebhookDeliveriesCmd, "identifier")
}

var webhooksCmd = &cobra.Command{
	Use:     "webhooks",
	Short:   "Manage webhooks: endpoints that receive signed event notifications",
	Aliases: []string{"webhook", "hooks"},
}

var listWebhooksCmd = &cobra.Command{
	Use:     cmdList,
	Short:   "List webhooks",
	Aliases: []string{"ls"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.ListWebhooksWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("listing webhooks: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			webhooks := resp.JSON200.Webhooks

			return printListOutput(cmd, webhooks, func() error {
				rows := webhooksToRows(webhooks)

				return renderTable(
					[]string{"ID", "URL", "Provider", "Subscriptions", "Last delivery", "Description"},
					rows,
				)
			})
		},
	),
}

var showWebhookCmd = &cobra.Command{
	Use:     cmdShow,
	Short:   "Show a webhook",
	Aliases: []string{"get"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			_, current, err := fetchWebhook(ctx, client, cmd)
			if err != nil {
				return err
			}

			return printWebhook(cmd, current)
		},
	),
}

var listWebhookEventTypesCmd = &cobra.Command{
	Use:   "event-types",
	Short: "List subscribable webhook event types",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.ListWebhookEventTypesWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("listing webhook event types: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			eventTypes := resp.JSON200.Types

			return printListOutput(cmd, eventTypes, func() error {
				for _, t := range eventTypes {
					fmt.Println(t)
				}

				return nil
			})
		},
	),
}

var listWebhookDeliveriesCmd = &cobra.Command{
	Use:     "deliveries",
	Short:   "List the newest deliveries of a webhook",
	Aliases: []string{"history"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			hookID := strconv.FormatUint(identifier, util.Base10)

			resp, err := client.ListWebhookDeliveriesWithResponse(ctx, hookID)
			if err != nil {
				return fmt.Errorf("listing webhook deliveries: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			deliveries := resp.JSON200.Deliveries

			return printListOutput(cmd, deliveries, func() error {
				if len(deliveries) == 0 {
					fmt.Println("No deliveries yet")

					return nil
				}

				return renderTable(
					[]string{"Time", "Event", "Result", "Status", "Attempts", "Duration"},
					deliveriesToRows(deliveries),
				)
			})
		},
	),
}

var createWebhookCmd = &cobra.Command{
	Use:   cmdCreate,
	Short: "Create a webhook",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			body, err := buildWebhookBody(cmd, nil)
			if err != nil {
				return err
			}

			resp, err := client.CreateWebhookWithResponse(ctx, body)
			if err != nil {
				return fmt.Errorf("creating webhook: %w", err)
			}

			if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusCreated {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printWebhook(cmd, &resp.JSON200.Webhook)
		},
	),
}

var updateWebhookCmd = &cobra.Command{
	Use:   cmdUpdate,
	Short: "Update a webhook",
	Long: `Replaces the webhook configuration. The update command fetches the current
webhook, overrides only the fields specified by flags, and sends the merged
configuration.`,
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			if cmd.Flags().Changed("provider") {
				_, err := parseProviderFlag(cmd)
				if err != nil {
					return err
				}
			}

			hookID, current, err := fetchWebhook(ctx, client, cmd)
			if err != nil {
				return err
			}

			body, err := buildWebhookBody(cmd, current)
			if err != nil {
				return err
			}

			resp, err := client.UpdateWebhookWithResponse(ctx, hookID, body)
			if err != nil {
				return fmt.Errorf("updating webhook: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printWebhook(cmd, &resp.JSON200.Webhook)
		},
	),
}

var rotateWebhookCmd = &cobra.Command{
	Use:   "rotate",
	Short: "Rotate a webhook signing secret",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			hookID := strconv.FormatUint(identifier, util.Base10)

			resp, err := client.RotateWebhookSecretWithResponse(ctx, hookID)
			if err != nil {
				return fmt.Errorf("rotating webhook secret: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			secret := ""
			if resp.JSON200.Webhook.Secret != nil {
				secret = *resp.JSON200.Webhook.Secret
			}

			msg := secretMessage(secret)

			return printOutput(cmd, resp.JSON200.Webhook, msg)
		},
	),
}

var testWebhookCmd = &cobra.Command{
	Use:   "test",
	Short: "Send a test event to a webhook",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			hookID := strconv.FormatUint(identifier, util.Base10)

			resp, err := client.TestWebhookWithResponse(ctx, hookID)
			if err != nil {
				return fmt.Errorf("testing webhook: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			if !resp.JSON200.Delivered {
				return fmt.Errorf("%w: %s", errDeliveryFailed, resp.JSON200.Status)
			}

			msg := fmt.Sprintf("Delivered (status %s)", resp.JSON200.Status)

			return printOutput(cmd, resp.JSON200, msg)
		},
	),
}

var deleteWebhookCmd = &cobra.Command{
	Use:     cmdDelete,
	Short:   "Delete a webhook",
	Aliases: []string{aliasDel},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			hookID, current, err := fetchWebhook(ctx, client, cmd)
			if err != nil {
				return err
			}

			prompt := fmt.Sprintf("Do you want to remove the webhook %s?", current.Url)
			if !confirmAction(cmd, prompt) {
				return printOutput(cmd, map[string]string{colResult: "Webhook not deleted"}, "Webhook not deleted")
			}

			deleteResp, err := client.DeleteWebhookWithResponse(ctx, hookID)
			if err != nil {
				return fmt.Errorf("deleting webhook: %w", err)
			}

			if deleteResp.StatusCode() != http.StatusOK && deleteResp.StatusCode() != http.StatusNoContent {
				return apiError(deleteResp.StatusCode(), deleteResp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, map[string]string{colResult: "Webhook deleted"}, "Webhook deleted")
		},
	),
}

// fetchWebhook reads the --identifier flag and loads that webhook, so show,
// update and delete share one GET.
func fetchWebhook(
	ctx context.Context,
	client *clientv1.ClientWithResponses,
	cmd *cobra.Command,
) (string, *clientv1.Webhook, error) {
	identifier, _ := cmd.Flags().GetUint64("identifier")
	hookID := strconv.FormatUint(identifier, util.Base10)

	resp, err := client.GetWebhookWithResponse(ctx, hookID)
	if err != nil {
		return "", nil, fmt.Errorf("getting webhook: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return "", nil, apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
	}

	return hookID, &resp.JSON200.Webhook, nil
}

func secretMessage(secret string) string {
	return fmt.Sprintf(secretNoteFormat, secret)
}

func parseProviderFlag(cmd *cobra.Command) (*clientv1.WebhookRequestBodyProviderType, error) {
	provider, _ := cmd.Flags().GetString("provider")
	provider = strings.TrimSpace(strings.ToLower(provider))

	if provider == "" || provider == defaultProvider {
		empty := clientv1.WebhookRequestBodyProviderTypeEmpty

		return &empty, nil
	}

	p := clientv1.WebhookRequestBodyProviderType(provider)
	if !p.Valid() {
		return nil, errInvalidProvider
	}

	return &p, nil
}

func buildWebhookBody(
	cmd *cobra.Command,
	existing *clientv1.Webhook,
) (clientv1.WebhookRequestBody, error) {
	var body clientv1.WebhookRequestBody

	if existing != nil {
		desc := existing.Description
		subs := existing.Subscriptions
		body.Url = existing.Url
		body.Description = &desc
		body.Subscriptions = &subs

		if existing.ProviderType != "" {
			p := clientv1.WebhookRequestBodyProviderType(existing.ProviderType)
			body.ProviderType = &p
		}
	}

	if cmd.Flags().Changed("url") {
		url, _ := cmd.Flags().GetString("url")
		body.Url = url
	}

	if cmd.Flags().Changed("description") {
		desc, _ := cmd.Flags().GetString("description")
		body.Description = &desc
	}

	if cmd.Flags().Changed("provider") {
		p, err := parseProviderFlag(cmd)
		if err != nil {
			return body, err
		}

		body.ProviderType = p
	}

	if cmd.Flags().Changed("event") {
		events, _ := cmd.Flags().GetStringSlice("event")
		body.Subscriptions = &events
	}

	return body, nil
}

func formatLastDelivery(status string, at *time.Time) string {
	if at == nil {
		return neverDelivered
	}

	timeStr := at.Format("2006-01-02 15:04")
	if status == "" {
		return timeStr
	}

	return fmt.Sprintf("%s (%s)", status, timeStr)
}

func deliveriesToRows(deliveries []clientv1.WebhookDelivery) [][]string {
	rows := make([][]string, 0, len(deliveries))

	for _, d := range deliveries {
		result := "failed"
		if d.Ok {
			result = "delivered"
		}

		rows = append(rows, []string{
			d.At.Format("2006-01-02 15:04:05"),
			d.EventType,
			result,
			d.Status,
			strconv.FormatInt(d.Attempts, util.Base10),
			(time.Duration(d.DurationMs) * time.Millisecond).String(),
		})
	}

	return rows
}

func webhooksToRows(webhooks []clientv1.Webhook) [][]string {
	rows := make([][]string, 0, len(webhooks))

	for _, w := range webhooks {
		provider := w.ProviderType
		if provider == "" {
			provider = defaultProvider
		}

		rows = append(rows, []string{
			w.Id,
			w.Url,
			provider,
			strings.Join(w.Subscriptions, ", "),
			formatLastDelivery(w.LastDeliveryStatus, w.LastDeliveryAt),
			w.Description,
		})
	}

	return rows
}

func printWebhook(cmd *cobra.Command, w *clientv1.Webhook) error {
	return printListOutput(cmd, w, func() error {
		printWebhookHuman(w)

		if w.Secret != nil && *w.Secret != "" {
			fmt.Println(secretMessage(*w.Secret))
		}

		return nil
	})
}

func printWebhookHuman(w *clientv1.Webhook) {
	fmt.Printf("URL: %s\n", w.Url)

	if w.Description != "" {
		fmt.Printf("Description: %s\n", w.Description)
	}

	provider := w.ProviderType
	if provider == "" {
		provider = defaultProvider
	}

	fmt.Printf("Provider: %s\n", provider)
	fmt.Printf("Created by: %s\n", w.CreatedByUserId)
	fmt.Printf("Created at: %s\n", w.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Last delivery: %s\n", formatLastDelivery(w.LastDeliveryStatus, w.LastDeliveryAt))

	fmt.Println("Subscriptions:")

	for _, s := range w.Subscriptions {
		fmt.Println(s)
	}
}
