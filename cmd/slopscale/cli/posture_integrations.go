package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	clientv1 "github.com/aislopware/slopscale/gen/client/v1"
	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/spf13/cobra"
)

var (
	errNameAndProviderRequired = errors.New("name and provider are required when --id is not specified")
	errPostureCheckFailed      = errors.New("posture integration check failed")
)

func init() {
	rootCmd.AddCommand(postureIntegrationsCmd)

	postureIntegrationsCmd.AddCommand(listPostureProvidersCmd)
	postureIntegrationsCmd.AddCommand(listPostureIntegrationsCmd)
	postureIntegrationsCmd.AddCommand(showPostureIntegrationCmd)
	postureIntegrationsCmd.AddCommand(createPostureIntegrationCmd)
	postureIntegrationsCmd.AddCommand(updatePostureIntegrationCmd)
	postureIntegrationsCmd.AddCommand(deletePostureIntegrationCmd)
	postureIntegrationsCmd.AddCommand(syncPostureIntegrationCmd)
	postureIntegrationsCmd.AddCommand(checkPostureIntegrationCmd)

	showPostureIntegrationCmd.Flags().Uint64P("id", "i", 0, "Posture integration identifier (ID)")
	mustMarkRequired(showPostureIntegrationCmd, "id")

	createPostureIntegrationCmd.Flags().
		String("provider", "", "Provider: falcon, sentinelone, intune, jamf, kandji or kolide")
	createPostureIntegrationCmd.Flags().StringP("name", "n", "", "Integration name")
	createPostureIntegrationCmd.Flags().String("base-url", "", "API origin URL")
	createPostureIntegrationCmd.Flags().String("client-id", "", "OAuth client ID")
	createPostureIntegrationCmd.Flags().String("client-secret", "", "OAuth client secret")
	createPostureIntegrationCmd.Flags().String("api-token", "", "API token")
	createPostureIntegrationCmd.Flags().String("tenant-id", "", "Entra tenant ID")
	createPostureIntegrationCmd.Flags().Bool("disabled", false, "Create integration in disabled state")
	mustMarkRequired(createPostureIntegrationCmd, "provider", "name")

	updatePostureIntegrationCmd.Flags().Uint64P("id", "i", 0, "Posture integration identifier (ID)")
	updatePostureIntegrationCmd.Flags().
		String("provider", "", "Provider: falcon, sentinelone, intune, jamf, kandji or kolide")
	updatePostureIntegrationCmd.Flags().StringP("name", "n", "", "Integration name")
	updatePostureIntegrationCmd.Flags().String("base-url", "", "API origin URL")
	updatePostureIntegrationCmd.Flags().String("client-id", "", "OAuth client ID")
	updatePostureIntegrationCmd.Flags().
		String("client-secret", "", "OAuth client secret (empty keeps stored secret)")
	updatePostureIntegrationCmd.Flags().String("api-token", "", "API token (empty keeps stored secret)")
	updatePostureIntegrationCmd.Flags().String("tenant-id", "", "Entra tenant ID")
	updatePostureIntegrationCmd.Flags().Bool("disabled", false, "Disable the integration")
	mustMarkRequired(updatePostureIntegrationCmd, "id")

	deletePostureIntegrationCmd.Flags().Uint64P("id", "i", 0, "Posture integration identifier (ID)")
	mustMarkRequired(deletePostureIntegrationCmd, "id")

	syncPostureIntegrationCmd.Flags().Uint64P("id", "i", 0, "Posture integration identifier (ID)")
	mustMarkRequired(syncPostureIntegrationCmd, "id")

	checkPostureIntegrationCmd.Flags().Uint64P("id", "i", 0, "Existing integration ID to reuse stored secret")
	checkPostureIntegrationCmd.Flags().
		String("provider", "", "Provider: falcon, sentinelone, intune, jamf, kandji or kolide")
	checkPostureIntegrationCmd.Flags().StringP("name", "n", "", "Integration name")
	checkPostureIntegrationCmd.Flags().String("base-url", "", "API origin URL")
	checkPostureIntegrationCmd.Flags().String("client-id", "", "OAuth client ID")
	checkPostureIntegrationCmd.Flags().String("client-secret", "", "OAuth client secret")
	checkPostureIntegrationCmd.Flags().String("api-token", "", "API token")
	checkPostureIntegrationCmd.Flags().String("tenant-id", "", "Entra tenant ID")
	checkPostureIntegrationCmd.Flags().Bool("disabled", false, "Integration disabled")
}

var postureIntegrationsCmd = &cobra.Command{
	Use:     "posture-integrations",
	Short:   "Manage integrations with endpoint security and device management providers",
	Aliases: []string{"posture-integration"},
}

var listPostureProvidersCmd = &cobra.Command{
	Use:   "providers",
	Short: "List supported posture providers and their attributes",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.ListPostureProvidersWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("listing posture providers: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			providers := resp.JSON200.Providers

			return printListOutput(cmd, providers, func() error {
				rows := make([][]string, 0, len(providers))

				for _, p := range providers {
					attrs := make([]string, len(p.Attributes))
					for i, a := range p.Attributes {
						attrs[i] = a.Name
					}

					rows = append(rows, []string{
						p.Provider,
						p.Label,
						p.Prefix,
						strings.Join(p.Fields, ", "),
						strings.Join(attrs, ", "),
					})
				}

				return renderTable(
					[]string{"Provider", "Label", "Prefix", "Fields", "Attributes"},
					rows,
				)
			})
		},
	),
}

var listPostureIntegrationsCmd = &cobra.Command{
	Use:     cmdList,
	Short:   "List posture integrations",
	Aliases: []string{"ls"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.ListPostureIntegrationsWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("listing posture integrations: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			integrations := resp.JSON200.Integrations

			return printListOutput(cmd, integrations, func() error {
				rows := make([][]string, 0, len(integrations))

				for _, pi := range integrations {
					lastSync := "never"
					if pi.LastSyncAt != nil {
						lastSync = pi.LastSyncAt.UTC().Format("2006-01-02 15:04")
					}

					status := "ok"
					if pi.LastError != "" {
						status = pi.LastError
					}

					rows = append(rows, []string{
						pi.Id,
						pi.Name,
						string(pi.Provider),
						yesNo(pi.Enabled),
						lastSync,
						status,
						strconv.FormatInt(pi.LastMatched, util.Base10),
					})
				}

				return renderTable(
					[]string{"ID", "Name", "Provider", colEnabled, "Last sync", "Status", "Matched"},
					rows,
				)
			})
		},
	),
}

var showPostureIntegrationCmd = &cobra.Command{
	Use:     cmdShow,
	Short:   "Show a posture integration",
	Aliases: []string{"get"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			_, pi, err := fetchPostureIntegration(ctx, client, cmd)
			if err != nil {
				return err
			}

			return printPostureIntegration(cmd, pi)
		},
	),
}

var createPostureIntegrationCmd = &cobra.Command{
	Use:   cmdCreate,
	Short: "Create a posture integration",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			provider, _ := cmd.Flags().GetString("provider")
			name, _ := cmd.Flags().GetString("name")
			disabled, _ := cmd.Flags().GetBool("disabled")

			enabled := !disabled
			body := clientv1.CreatePostureIntegrationJSONRequestBody{
				Name:     name,
				Provider: clientv1.PostureIntegrationRequestBodyProvider(provider),
				Enabled:  &enabled,
			}

			if cmd.Flags().Changed("base-url") {
				baseURL, _ := cmd.Flags().GetString("base-url")
				body.BaseUrl = &baseURL
			}

			if cmd.Flags().Changed("client-id") {
				clientID, _ := cmd.Flags().GetString("client-id")
				body.ClientId = &clientID
			}

			if cmd.Flags().Changed("client-secret") {
				secret, _ := cmd.Flags().GetString("client-secret")
				body.ClientSecret = &secret
			}

			if cmd.Flags().Changed("api-token") {
				token, _ := cmd.Flags().GetString("api-token")
				body.ApiToken = &token
			}

			if cmd.Flags().Changed("tenant-id") {
				tenantID, _ := cmd.Flags().GetString("tenant-id")
				body.TenantId = &tenantID
			}

			resp, err := client.CreatePostureIntegrationWithResponse(ctx, body)
			if err != nil {
				return fmt.Errorf("creating posture integration: %w", err)
			}

			if resp.StatusCode() != http.StatusCreated {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printPostureIntegration(cmd, &resp.JSON201.Integration)
		},
	),
}

var updatePostureIntegrationCmd = &cobra.Command{
	Use:   cmdUpdate,
	Short: "Update a posture integration",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			id, current, err := fetchPostureIntegration(ctx, client, cmd)
			if err != nil {
				return err
			}

			body := buildUpdatePostureIntegrationBody(cmd, current)

			resp, err := client.UpdatePostureIntegrationWithResponse(ctx, id, body)
			if err != nil {
				return fmt.Errorf("updating posture integration: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printPostureIntegration(cmd, &resp.JSON200.Integration)
		},
	),
}

func fetchPostureIntegration(
	ctx context.Context,
	client *clientv1.ClientWithResponses,
	cmd *cobra.Command,
) (string, *clientv1.PostureIntegration, error) {
	id, _ := cmd.Flags().GetUint64("id")
	integrationID := strconv.FormatUint(id, util.Base10)

	resp, err := client.GetPostureIntegrationWithResponse(ctx, integrationID)
	if err != nil {
		return "", nil, fmt.Errorf("getting posture integration: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return "", nil, apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
	}

	return integrationID, &resp.JSON200.Integration, nil
}

func buildUpdatePostureIntegrationBody(
	cmd *cobra.Command,
	current *clientv1.PostureIntegration,
) clientv1.UpdatePostureIntegrationJSONRequestBody {
	enabled := current.Enabled

	if cmd.Flags().Changed("disabled") {
		disabled, _ := cmd.Flags().GetBool("disabled")
		enabled = !disabled
	}

	body := clientv1.UpdatePostureIntegrationJSONRequestBody{
		Name:     current.Name,
		Provider: clientv1.PostureIntegrationRequestBodyProvider(current.Provider),
		Enabled:  &enabled,
		BaseUrl:  current.Config.BaseUrl,
		ClientId: current.Config.ClientId,
		TenantId: current.Config.TenantId,
	}

	if cmd.Flags().Changed("name") {
		name, _ := cmd.Flags().GetString("name")
		body.Name = name
	}

	if cmd.Flags().Changed("provider") {
		provider, _ := cmd.Flags().GetString("provider")
		body.Provider = clientv1.PostureIntegrationRequestBodyProvider(provider)
	}

	if cmd.Flags().Changed("base-url") {
		baseURL, _ := cmd.Flags().GetString("base-url")
		body.BaseUrl = &baseURL
	}

	if cmd.Flags().Changed("client-id") {
		clientID, _ := cmd.Flags().GetString("client-id")
		body.ClientId = &clientID
	}

	if cmd.Flags().Changed("tenant-id") {
		tenantID, _ := cmd.Flags().GetString("tenant-id")
		body.TenantId = &tenantID
	}

	if cmd.Flags().Changed("client-secret") {
		secret, _ := cmd.Flags().GetString("client-secret")
		body.ClientSecret = &secret
	}

	if cmd.Flags().Changed("api-token") {
		token, _ := cmd.Flags().GetString("api-token")
		body.ApiToken = &token
	}

	return body
}

//nolint:dupl // standard CRUD delete command structure
var deletePostureIntegrationCmd = &cobra.Command{
	Use:     cmdDelete,
	Short:   "Delete a posture integration",
	Aliases: []string{aliasDel},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			id, current, err := fetchPostureIntegration(ctx, client, cmd)
			if err != nil {
				return err
			}

			prompt := fmt.Sprintf(
				"Do you want to remove the posture integration %s?",
				current.Name,
			)
			if !confirmAction(cmd, prompt) {
				return printOutput(
					cmd,
					map[string]string{colResult: "Posture integration not deleted"},
					"Posture integration not deleted",
				)
			}

			resp, err := client.DeletePostureIntegrationWithResponse(ctx, id)
			if err != nil {
				return fmt.Errorf("deleting posture integration: %w", err)
			}

			if resp.StatusCode() != http.StatusNoContent {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(
				cmd,
				map[string]string{colResult: "Posture integration deleted"},
				"Posture integration deleted",
			)
		},
	),
}

var syncPostureIntegrationCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync a posture integration now",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			id, _ := cmd.Flags().GetUint64("id")
			integrationID := strconv.FormatUint(id, util.Base10)

			resp, err := client.SyncPostureIntegrationWithResponse(ctx, integrationID)
			if err != nil {
				return fmt.Errorf("syncing posture integration: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printPostureIntegration(cmd, &resp.JSON200.Integration)
		},
	),
}

var checkPostureIntegrationCmd = &cobra.Command{
	Use:   "check",
	Short: "Check posture integration credentials",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			body, err := buildCheckPostureIntegrationBody(ctx, client, cmd)
			if err != nil {
				return err
			}

			resp, err := client.CheckPostureIntegrationWithResponse(ctx, body)
			if err != nil {
				return fmt.Errorf("checking posture integration: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			if !resp.JSON200.Ok {
				return errPostureCheckFailed
			}

			return printOutput(cmd, resp.JSON200, "ok")
		},
	),
}

func buildCheckPostureIntegrationBody(
	ctx context.Context,
	client *clientv1.ClientWithResponses,
	cmd *cobra.Command,
) (clientv1.CheckPostureIntegrationJSONRequestBody, error) {
	var body clientv1.CheckPostureIntegrationJSONRequestBody

	if cmd.Flags().Changed("id") {
		id, _ := cmd.Flags().GetUint64("id")
		idStr := strconv.FormatUint(id, util.Base10)

		getResp, err := client.GetPostureIntegrationWithResponse(ctx, idStr)
		if err != nil {
			return body, fmt.Errorf("getting posture integration: %w", err)
		}

		if getResp.StatusCode() != http.StatusOK {
			return body, apiError(getResp.StatusCode(), getResp.ApplicationproblemJSONDefault)
		}

		existing := &getResp.JSON200.Integration
		body.Id = &idStr
		body.Name = existing.Name
		body.Provider = clientv1.PostureIntegrationCheckBodyProvider(existing.Provider)
		body.BaseUrl = existing.Config.BaseUrl
		body.ClientId = existing.Config.ClientId
		body.TenantId = existing.Config.TenantId
	}

	if cmd.Flags().Changed("name") {
		name, _ := cmd.Flags().GetString("name")
		body.Name = name
	}

	if cmd.Flags().Changed("provider") {
		provider, _ := cmd.Flags().GetString("provider")
		body.Provider = clientv1.PostureIntegrationCheckBodyProvider(provider)
	}

	if body.Name == "" || body.Provider == "" {
		return body, errNameAndProviderRequired
	}

	if cmd.Flags().Changed("base-url") {
		baseURL, _ := cmd.Flags().GetString("base-url")
		body.BaseUrl = &baseURL
	}

	if cmd.Flags().Changed("client-id") {
		clientID, _ := cmd.Flags().GetString("client-id")
		body.ClientId = &clientID
	}

	if cmd.Flags().Changed("client-secret") {
		secret, _ := cmd.Flags().GetString("client-secret")
		body.ClientSecret = &secret
	}

	if cmd.Flags().Changed("api-token") {
		token, _ := cmd.Flags().GetString("api-token")
		body.ApiToken = &token
	}

	if cmd.Flags().Changed("tenant-id") {
		tenantID, _ := cmd.Flags().GetString("tenant-id")
		body.TenantId = &tenantID
	}

	if cmd.Flags().Changed("disabled") {
		disabled, _ := cmd.Flags().GetBool("disabled")
		enabled := !disabled
		body.Enabled = &enabled
	}

	return body, nil
}

func printPostureIntegration(cmd *cobra.Command, pi *clientv1.PostureIntegration) error {
	return printListOutput(cmd, pi, func() error {
		return printPostureIntegrationHuman(pi)
	})
}

func printPostureIntegrationHuman(pi *clientv1.PostureIntegration) error {
	fmt.Printf("ID: %s\n", pi.Id)
	fmt.Printf("Name: %s\n", pi.Name)
	fmt.Printf("Provider: %s\n", pi.Provider)

	if pi.Prefix != "" {
		fmt.Printf("Prefix: %s\n", pi.Prefix)
	}

	fmt.Printf("Enabled: %s\n", yesNo(pi.Enabled))

	if pi.Config.BaseUrl != nil && *pi.Config.BaseUrl != "" {
		fmt.Printf("Base URL: %s\n", *pi.Config.BaseUrl)
	}

	if pi.Config.ClientId != nil && *pi.Config.ClientId != "" {
		fmt.Printf("Client ID: %s\n", *pi.Config.ClientId)
	}

	if pi.Config.TenantId != nil && *pi.Config.TenantId != "" {
		fmt.Printf("Tenant ID: %s\n", *pi.Config.TenantId)
	}

	fmt.Printf("Has secret: %s\n", yesNo(pi.HasSecret))

	if pi.LastSyncAt != nil {
		fmt.Printf("Last sync: %s\n", pi.LastSyncAt.UTC().Format("2006-01-02 15:04"))
	} else {
		fmt.Println("Last sync: never")
	}

	status := "ok"
	if pi.LastError != "" {
		status = pi.LastError
	}

	fmt.Printf("Status: %s\n", status)
	fmt.Printf("Matched machines: %d\n", pi.LastMatched)

	return nil
}
