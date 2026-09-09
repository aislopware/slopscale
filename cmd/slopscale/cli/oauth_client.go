package cli

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	clientv1 "github.com/aislopware/slopscale/gen/client/v1"
	clientv2 "github.com/aislopware/slopscale/gen/client/v2"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/spf13/cobra"
)

// oauthTailnet is the single Slopscale tailnet the v2 API addresses as "-".
const oauthTailnet = "-"

func init() {
	rootCmd.AddCommand(oauthClientsCmd)

	oauthClientsCmd.AddCommand(listOAuthClientsCmd)

	createOAuthClientCmd.Flags().
		StringArrayP("scope", "s", nil,
			"Scope the client's tokens are granted (repeatable): auth_keys, oauth_keys, devices:core, "+
				"devices:routes, policy_file, feature_settings (each with a :read variant), or all/all:read")
	createOAuthClientCmd.Flags().
		StringArrayP("tag", "t", nil,
			"Tag the client's tokens may assign to devices (repeatable), e.g. tag:k8s-operator")
	createOAuthClientCmd.Flags().StringP("description", "d", "", "Human-readable description")
	createOAuthClientCmd.Flags().
		Bool("federated", false,
			"Create a federated identity: a client with no secret, which mints tokens by presenting "+
				"a JWT its own OIDC issuer signed")
	federatedFlags(createOAuthClientCmd)
	oauthClientsCmd.AddCommand(createOAuthClientCmd)

	updateOAuthClientCmd.Flags().StringP("id", "i", "", "OAuth client id")
	updateOAuthClientCmd.Flags().StringArrayP("scope", "s", nil, "Replaces the scopes (repeatable)")
	updateOAuthClientCmd.Flags().StringArrayP("tag", "t", nil, "Replaces the tags (repeatable)")
	updateOAuthClientCmd.Flags().StringP("description", "d", "", "Human-readable description")
	updateOAuthClientCmd.Flags().Bool("clear-tags", false, "Remove every tag")
	updateOAuthClientCmd.Flags().Bool("clear-claims", false, "Remove every custom claim rule")
	federatedFlags(updateOAuthClientCmd)
	oauthClientsCmd.AddCommand(updateOAuthClientCmd)

	deleteOAuthClientCmd.Flags().StringP("id", "i", "", "OAuth client id")
	oauthClientsCmd.AddCommand(deleteOAuthClientCmd)
}

// federatedFlags are the trust conditions of a federated identity, shared by
// create and update.
func federatedFlags(cmd *cobra.Command) {
	cmd.Flags().String("issuer", "", "Federated: the https URL of the OIDC issuer that signs the JWT")
	cmd.Flags().String("audience", "", "Federated: the audience the JWT must carry")
	cmd.Flags().String("subject", "", "Federated: the subject the JWT must equal")
	cmd.Flags().
		StringArray("claim", nil,
			"Federated: a further claim the JWT must carry as name=value (repeatable)")
}

var oauthClientsCmd = &cobra.Command{
	Use:     "oauth-clients",
	Short:   "Manage OAuth clients",
	Aliases: []string{"oauth-client", "oauthclients", "oauthclient", "oauth"},
}

var createOAuthClientCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an OAuth client or a federated identity",
	Long: `Create a general-purpose OAuth client. It authenticates with the OAuth 2.0
client-credentials grant and mints short-lived, scope-limited access tokens.
The wire format is compatible with Tailscale tooling (the Terraform provider,
the Kubernetes operator, tscli, ...), so those can drive Slopscale unchanged.

The client secret is shown ONCE on creation and cannot be retrieved again; if
you lose it, delete the client and create a new one.

With --federated the result is a federated identity instead: it holds no
secret and mints the same tokens by presenting a JWT its own OIDC issuer
signed, so a CI job or a cloud workload needs nothing stored. Name the issuer,
the audience and the subject its tokens carry, and any further claim with
--claim name=value.

Scopes gate what the client's tokens may do; tags are the device tags those
tokens may assign and are required when the scopes include devices:core or
auth_keys.`,
	Aliases: []string{"c", cmdNew},
	RunE: func(cmd *cobra.Command, _ []string) error {
		body, err := oauthCreateRequest(cmd)
		if err != nil {
			return err
		}

		ctx, client, cancel, err := newV2Client()
		if err != nil {
			return err
		}
		defer cancel()

		resp, err := client.CreateKeyWithResponse(ctx, oauthTailnet, body)
		if err != nil {
			return fmt.Errorf("creating oauth client: %w", err)
		}

		err = v2Error(resp.HTTPResponse.StatusCode, resp.Body)
		if err != nil {
			return err
		}

		key := resp.JSON200
		if key.KeyType == types.OAuthKeyTypeFederated {
			return printOutput(cmd, key, "Federated identity "+key.Id+" created.")
		}

		return printOutput(cmd, key,
			fmt.Sprintf("OAuth client %s created.\nSecret (shown once, store it now): %s", key.Id, ptrStr(key.Key)))
	},
}

// oauthCreateRequest builds the create body from the flags, refusing a trust
// condition without --federated: on the wire those fields belong to a
// federated identity and a client would silently drop them.
func oauthCreateRequest(cmd *cobra.Command) (clientv2.CreateKeyRequest, error) {
	scopes, _ := cmd.Flags().GetStringArray("scope")
	tags, _ := cmd.Flags().GetStringArray("tag")
	description, _ := cmd.Flags().GetString("description")
	federated, _ := cmd.Flags().GetBool("federated")
	issuer, _ := cmd.Flags().GetString("issuer")
	audience, _ := cmd.Flags().GetString("audience")
	subject, _ := cmd.Flags().GetString("subject")

	claims, err := oauthClaims(cmd)
	if err != nil {
		return clientv2.CreateKeyRequest{}, err
	}

	if len(scopes) == 0 {
		return clientv2.CreateKeyRequest{}, fmt.Errorf(
			"at least one --scope is required: %w", errMissingParameter)
	}

	keyType := types.OAuthKeyTypeClient
	body := clientv2.CreateKeyRequest{
		KeyType:     &keyType,
		Scopes:      &scopes,
		Tags:        &tags,
		Description: &description,
	}

	if !federated {
		if issuer != "" || audience != "" || subject != "" || len(claims) > 0 {
			return clientv2.CreateKeyRequest{}, fmt.Errorf(
				"--issuer, --audience, --subject and --claim need --federated: %w", errMissingParameter)
		}

		return body, nil
	}

	if issuer == "" || audience == "" || subject == "" {
		return clientv2.CreateKeyRequest{}, fmt.Errorf(
			"--federated needs --issuer, --audience and --subject: %w", errMissingParameter)
	}

	keyType = types.OAuthKeyTypeFederated
	body.Issuer, body.Audience, body.Subject = &issuer, &audience, &subject

	if len(claims) > 0 {
		body.CustomClaimRules = &claims
	}

	return body, nil
}

// oauthClaims parses the repeatable --claim name=value into the claim rules a
// presented JWT must satisfy.
func oauthClaims(cmd *cobra.Command) (map[string]string, error) {
	pairs, _ := cmd.Flags().GetStringArray("claim")

	claims := make(map[string]string, len(pairs))

	for _, pair := range pairs {
		name, value, found := strings.Cut(pair, "=")
		if !found || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("--claim %q must be name=value: %w", pair, errMissingParameter)
		}

		claims[name] = value
	}

	return claims, nil
}

var listOAuthClientsCmd = &cobra.Command{
	Use:     cmdList,
	Short:   "List OAuth clients",
	Aliases: []string{"ls", cmdShow},
	RunE: func(cmd *cobra.Command, _ []string) error {
		ctx, client, cancel, err := newV2Client()
		if err != nil {
			return err
		}
		defer cancel()

		resp, err := client.ListKeysWithResponse(ctx, oauthTailnet, nil)
		if err != nil {
			return fmt.Errorf("listing oauth clients: %w", err)
		}

		err = v2Error(resp.HTTPResponse.StatusCode, resp.Body)
		if err != nil {
			return err
		}

		// The keys endpoint is multiplexed; keep the OAuth principals, both
		// the clients holding a secret and the federated identities.
		clients := make([]clientv2.Key, 0, len(resp.JSON200.Keys))

		for _, k := range resp.JSON200.Keys {
			if k.KeyType == types.OAuthKeyTypeClient || k.KeyType == types.OAuthKeyTypeFederated {
				clients = append(clients, k)
			}
		}

		return printListOutput(cmd, clients, func() error {
			rows := make([][]string, 0, len(clients))
			for _, c := range clients {
				rows = append(rows, []string{
					c.Id,
					c.KeyType,
					strings.Join(ptrStrs(c.Scopes), ","),
					strings.Join(ptrStrs(c.Tags), ","),
					ptrStr(c.Subject),
					ptrStr(c.Description),
					c.Created.Format(SlopscaleDateTimeFormat),
				})
			}

			return renderTable(
				[]string{"ID", "Kind", "Scopes", "Tags", "Subject", "Description", colCreated}, rows)
		})
	},
}

var updateOAuthClientCmd = &cobra.Command{
	Use:   cmdUpdate,
	Short: "Update an OAuth client or a federated identity",
	Long: `Change an OAuth client or a federated identity in place. Only the flags you
set are sent, and the rest keep the value they have; a client keeps its secret,
so it goes on working across an update.

Use --clear-tags or --clear-claims to remove every tag or claim rule; the
trust conditions (--issuer, --audience, --subject, --claim) belong to a
federated identity and are refused for a client.`,
	Aliases: []string{"set", "edit"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			id, _ := cmd.Flags().GetString("id")
			if id == "" {
				return fmt.Errorf("--id is required: %w", errMissingParameter)
			}

			body, err := oauthUpdateRequest(cmd)
			if err != nil {
				return err
			}

			resp, err := client.UpdateOAuthClientWithResponse(ctx, id, body)
			if err != nil {
				return fmt.Errorf("updating oauth client: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			updated := resp.JSON200.OauthClient

			return printOutput(cmd, updated, "OAuth client "+updated.ClientId+" updated")
		},
	),
}

// oauthUpdateRequest builds the patch body from the flags that were set, so
// an unmentioned field keeps its value. Emptying a list needs its own flag,
// because a repeatable flag cannot express "no values".
func oauthUpdateRequest(cmd *cobra.Command) (clientv1.UpdateOAuthClientJSONRequestBody, error) {
	var body clientv1.UpdateOAuthClientJSONRequestBody

	if cmd.Flags().Changed("description") {
		description, _ := cmd.Flags().GetString("description")
		body.Description = &description
	}

	if cmd.Flags().Changed("scope") {
		scopes, _ := cmd.Flags().GetStringArray("scope")
		body.Scopes = &scopes
	}

	if cmd.Flags().Changed("tag") {
		tags, _ := cmd.Flags().GetStringArray("tag")
		body.Tags = &tags
	}

	if empty, _ := cmd.Flags().GetBool("clear-tags"); empty {
		body.Tags = &[]string{}
	}

	for _, f := range []struct {
		name  string
		field **string
	}{
		{"issuer", &body.Issuer},
		{"audience", &body.Audience},
		{"subject", &body.Subject},
	} {
		if cmd.Flags().Changed(f.name) {
			value, _ := cmd.Flags().GetString(f.name)
			*f.field = &value
		}
	}

	if cmd.Flags().Changed("claim") {
		claims, err := oauthClaims(cmd)
		if err != nil {
			return body, err
		}

		body.CustomClaimRules = &claims
	}

	if empty, _ := cmd.Flags().GetBool("clear-claims"); empty {
		body.CustomClaimRules = &map[string]string{}
	}

	if body == (clientv1.UpdateOAuthClientJSONRequestBody{}) {
		return body, fmt.Errorf("nothing to update: %w", errMissingParameter)
	}

	return body, nil
}

var deleteOAuthClientCmd = &cobra.Command{
	Use:     cmdDelete,
	Short:   "Delete an OAuth client",
	Aliases: []string{"remove", aliasDel},
	RunE: func(cmd *cobra.Command, _ []string) error {
		id, _ := cmd.Flags().GetString("id")
		if id == "" {
			return fmt.Errorf("--id is required: %w", errMissingParameter)
		}

		ctx, client, cancel, err := newV2Client()
		if err != nil {
			return err
		}
		defer cancel()

		resp, err := client.DeleteKeyWithResponse(ctx, oauthTailnet, id)
		if err != nil {
			return fmt.Errorf("deleting oauth client: %w", err)
		}

		err = v2Error(resp.HTTPResponse.StatusCode, resp.Body)
		if err != nil {
			return err
		}

		return printOutput(cmd, map[string]string{"id": id}, "OAuth client "+id+" deleted")
	},
}

// newV2Client builds a generated v2 API client, selecting the transport the same
// way the v1 client does: over the local unix socket it is unauthenticated
// (local trust); a remote address injects the configured API key as a bearer
// token.
func newV2Client() (context.Context, *clientv2.ClientWithResponses, context.CancelFunc, error) {
	cfg, err := types.LoadCLIConfig()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("loading configuration: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.CLI.Timeout)

	if cfg.CLI.Address == "" {
		socketPath := cfg.UnixSocket

		httpClient := &http.Client{Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return dialSlopscaleSocket(ctx, socketPath)
			},
		}}

		client, clientErr := clientv2.NewClientWithResponses("http://local", clientv2.WithHTTPClient(httpClient))
		if clientErr != nil {
			cancel()

			return nil, nil, nil, clientErr
		}

		return ctx, client, cancel, nil
	}

	if cfg.CLI.APIKey == "" {
		cancel()

		return nil, nil, nil, errAPIKeyNotSet
	}

	transport := &http.Transport{}
	if cfg.CLI.Insecure {
		//nolint:gosec // intentionally honouring the insecure flag
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	apiKey := cfg.CLI.APIKey

	client, err := clientv2.NewClientWithResponses(
		clientBaseURL(cfg.CLI.Address),
		clientv2.WithHTTPClient(&http.Client{Transport: transport}),
		clientv2.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+apiKey)

			return nil
		}),
	)
	if err != nil {
		cancel()

		return nil, nil, nil, err
	}

	return ctx, client, cancel, nil
}

// v2Error turns a non-2xx v2 response into an error. The v2 API emits the
// Tailscale error body ({"message":...}) rather than RFC 7807, so it reads the
// "message" field instead of the generated problem+json types.
func v2Error(status int, body []byte) error {
	if status >= http.StatusOK && status < http.StatusMultipleChoices {
		return nil
	}

	var e struct {
		Message string `json:"message"`
	}

	if json.Unmarshal(body, &e) == nil && e.Message != "" {
		//nolint:err113 // surfacing the server's message
		return fmt.Errorf("api error (%d): %s", status, e.Message)
	}

	//nolint:err113 // surfacing the server's body
	return fmt.Errorf("api error (%d): %s", status, strings.TrimSpace(string(body)))
}

func ptrStr(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}

func ptrStrs(s *[]string) []string {
	if s == nil {
		return nil
	}

	return *s
}
