package apiv2

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/aislopware/slopscale/hscontrol/api/principal"
	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/state"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
)

func init() {
	registrations = append(registrations, registerPostureIntegrations)
}

// PostureIntegration is Tailscale's posture integration shape, what the
// Terraform tailscale_posture_integration resource reads and writes. The
// secret is write-only, as it is in slopscale's own API.
type PostureIntegration struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	CloudID  string `json:"cloudId,omitempty"`
	ClientID string `json:"clientId,omitempty"`
	TenantID string `json:"tenantId,omitempty"`
}

// CreatePostureIntegrationRequest is the POST body.
type CreatePostureIntegrationRequest struct {
	Provider     string `json:"provider"`
	CloudID      string `json:"cloudId,omitempty"`
	ClientID     string `json:"clientId,omitempty"`
	TenantID     string `json:"tenantId,omitempty"`
	ClientSecret string `json:"clientSecret,omitempty"`
}

// UpdatePostureIntegrationRequest is the PATCH body; a field left out
// keeps its stored value, and so does an omitted clientSecret.
type UpdatePostureIntegrationRequest struct {
	CloudID      *string `json:"cloudId,omitempty"`
	ClientID     *string `json:"clientId,omitempty"`
	TenantID     *string `json:"tenantId,omitempty"`
	ClientSecret *string `json:"clientSecret,omitempty"`
}

type (
	postureIntegrationOutput struct {
		Body PostureIntegration
	}
	listPostureIntegrationsOutput struct {
		Body struct {
			Integrations []PostureIntegration `json:"integrations" nullable:"false"`
		}
	}
	createPostureIntegrationInput struct {
		Tailnet string `path:"tailnet"`
		Body    CreatePostureIntegrationRequest
	}
	postureIntegrationIDInput struct {
		ID string `path:"id"`
	}
	updatePostureIntegrationInput struct {
		ID   string `path:"id"`
		Body UpdatePostureIntegrationRequest
	}
)

// tailscaleProviders maps Tailscale's provider names onto slopscale's. The
// two vocabularies agree except for Jamf Pro, which Tailscale spells
// jamfpro; Tailscale's fleet and huntress have no slopscale equivalent and
// are refused by name.
var tailscaleProviders = map[string]types.PostureProvider{
	"falcon":      types.PostureProviderFalcon,
	"sentinelone": types.PostureProviderSentinelOne,
	"intune":      types.PostureProviderIntune,
	"jamfpro":     types.PostureProviderJamf,
	"jamf":        types.PostureProviderJamf,
	"kandji":      types.PostureProviderKandji,
	"kolide":      types.PostureProviderKolide,
}

// falconClouds are the CrowdStrike clouds Tailscale's cloudId names, with
// the API origin each one answers on. A cloudId that is already an http(s)
// URL is used as-is, for a provider whose cloudId is a server address.
var falconClouds = map[string]string{
	"us-1":     "https://api.crowdstrike.com",
	"us-2":     "https://api.us-2.crowdstrike.com",
	"eu-1":     "https://api.eu-1.crowdstrike.com",
	"us-gov-1": "https://api.laggar.gcw.crowdstrike.com",
}

// v2IntegrationName is the name slopscale stores for an integration made
// through this API. Slopscale names integrations and Tailscale's shape has
// no field for one, so the provider names it, prefixed to keep it distinct
// from anything an operator created in the console.
func v2IntegrationName(provider types.PostureProvider) string {
	return "tailscale-api:" + string(provider)
}

func registerPostureIntegrations(api huma.API, b Backend) {
	registerPostureIntegrationReads(api, b)
	registerPostureIntegrationWrites(api, b)
}

func registerPostureIntegrationReads(api huma.API, b Backend) {
	postureTags := []string{"Device posture", tagTailscaleCompat}

	huma.Register(api, principal.RequireScope(huma.Operation{
		OperationID: "listPostureIntegrations",
		Method:      http.MethodGet,
		Path:        "/api/v2/tailnet/{tailnet}/posture/integrations",
		Summary:     "List posture integrations",
		Tags:        postureTags,
		Security:    security,
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.DevicesPostureAttributesRead), func(
		_ context.Context, in *tailnetInput,
	) (*listPostureIntegrationsOutput, error) {
		err := requireDefaultTailnet(in.Tailnet)
		if err != nil {
			return nil, err
		}

		out := &listPostureIntegrationsOutput{}
		out.Body.Integrations = []PostureIntegration{}

		for _, i := range b.State.PostureIntegrations() {
			out.Body.Integrations = append(out.Body.Integrations, postureIntegrationTo(i))
		}

		return out, nil
	})

	huma.Register(api, principal.RequireScope(huma.Operation{
		OperationID: "getPostureIntegration",
		Method:      http.MethodGet,
		Path:        "/api/v2/posture/integrations/{id}",
		Summary:     "Get a posture integration",
		Tags:        postureTags,
		Security:    security,
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.DevicesPostureAttributesRead), func(
		_ context.Context, in *postureIntegrationIDInput,
	) (*postureIntegrationOutput, error) {
		i, err := lookupPostureIntegration(b, in.ID)
		if err != nil {
			return nil, err
		}

		return &postureIntegrationOutput{Body: postureIntegrationTo(i)}, nil
	})
}

func registerPostureIntegrationWrites(api huma.API, b Backend) {
	postureTags := []string{"Device posture", tagTailscaleCompat}

	huma.Register(api, audit.Declare(principal.RequireScope(huma.Operation{
		OperationID: "createPostureIntegration",
		Method:      http.MethodPost,
		Path:        "/api/v2/tailnet/{tailnet}/posture/integrations",
		Summary:     "Create a posture integration",
		Description: "cloudId is the provider's endpoint: an https:// origin, or one of the " +
			"CrowdStrike cloud names (us-1, us-2, eu-1, us-gov-1) for falcon.",
		Tags:     postureTags,
		Security: security,
		Errors: []int{
			http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
			http.StatusNotFound, http.StatusInternalServerError,
		},
	}, scope.DevicesPostureAttributes), "posture_integration.create", "posture_integration", ""), func(
		ctx context.Context, in *createPostureIntegrationInput,
	) (*postureIntegrationOutput, error) {
		return handleCreatePostureIntegration(ctx, b, in)
	})

	huma.Register(api, audit.Declare(principal.RequireScope(huma.Operation{
		OperationID:   "updatePostureIntegration",
		Method:        http.MethodPatch,
		Path:          "/api/v2/posture/integrations/{id}",
		Summary:       "Update a posture integration",
		Description:   "A field left out keeps its stored value, clientSecret included.",
		Tags:          postureTags,
		Security:      security,
		DefaultStatus: http.StatusOK,
		Errors: []int{
			http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
			http.StatusNotFound, http.StatusInternalServerError,
		},
	}, scope.DevicesPostureAttributes), "posture_integration.update", "posture_integration", "id"), func(
		ctx context.Context, in *updatePostureIntegrationInput,
	) (*postureIntegrationOutput, error) {
		return handleUpdatePostureIntegration(ctx, b, in)
	})

	huma.Register(api, audit.Declare(principal.RequireScope(huma.Operation{
		OperationID:   "deletePostureIntegration",
		Method:        http.MethodDelete,
		Path:          "/api/v2/posture/integrations/{id}",
		Summary:       "Delete a posture integration",
		Tags:          postureTags,
		Security:      security,
		DefaultStatus: http.StatusOK,
		Errors: []int{
			http.StatusUnauthorized, http.StatusForbidden,
			http.StatusNotFound, http.StatusInternalServerError,
		},
	}, scope.DevicesPostureAttributes), "posture_integration.delete", "posture_integration", "id"), func(
		ctx context.Context, in *postureIntegrationIDInput,
	) (*emptyOutput, error) {
		i, err := lookupPostureIntegration(b, in.ID)
		if err != nil {
			return nil, err
		}

		audit.Target(ctx, "posture_integration", i.ID.String(), i.Name)

		c, err := b.State.DeletePostureIntegration(i.ID)
		if err != nil {
			return nil, mapPostureIntegrationError("deleting posture integration", err)
		}

		b.Change(c)

		return &emptyOutput{}, nil
	})
}

func handleCreatePostureIntegration(
	ctx context.Context, b Backend, in *createPostureIntegrationInput,
) (*postureIntegrationOutput, error) {
	err := requireDefaultTailnet(in.Tailnet)
	if err != nil {
		return nil, err
	}

	provider, err := slopscaleProvider(in.Body.Provider)
	if err != nil {
		return nil, err
	}

	baseURL, err := providerBaseURL(provider, in.Body.CloudID)
	if err != nil {
		return nil, err
	}

	i := types.PostureIntegration{
		Provider: provider,
		Name:     v2IntegrationName(provider),
		Enabled:  true,
		Config: types.PostureIntegrationConfig{
			BaseURL:  baseURL,
			ClientID: in.Body.ClientID,
			TenantID: in.Body.TenantID,
		},
	}
	setProviderSecret(&i, in.Body.ClientSecret)

	audit.Detail(ctx, "provider", string(provider))

	created, c, err := b.State.CreatePostureIntegration(ctx, i)
	if err != nil {
		return nil, mapPostureIntegrationError("creating posture integration", err)
	}

	audit.Target(ctx, "posture_integration", created.ID.String(), created.Name)
	b.Change(c)

	return &postureIntegrationOutput{Body: postureIntegrationTo(created)}, nil
}

func handleUpdatePostureIntegration(
	ctx context.Context, b Backend, in *updatePostureIntegrationInput,
) (*postureIntegrationOutput, error) {
	i, err := lookupPostureIntegration(b, in.ID)
	if err != nil {
		return nil, err
	}

	if in.Body.CloudID != nil {
		i.Config.BaseURL, err = providerBaseURL(i.Provider, *in.Body.CloudID)
		if err != nil {
			return nil, err
		}
	}

	if in.Body.ClientID != nil {
		i.Config.ClientID = *in.Body.ClientID
	}

	if in.Body.TenantID != nil {
		i.Config.TenantID = *in.Body.TenantID
	}

	if in.Body.ClientSecret != nil {
		// An empty secret means "keep the stored one" to State.Update, so
		// clear both fields first and let the provider choose which to fill.
		i.Config.ClientSecret = ""
		i.Config.APIToken = ""
		setProviderSecret(&i, *in.Body.ClientSecret)
	}

	audit.Target(ctx, "posture_integration", i.ID.String(), i.Name)

	updated, c, err := b.State.UpdatePostureIntegration(ctx, i)
	if err != nil {
		return nil, mapPostureIntegrationError("updating posture integration", err)
	}

	b.Change(c)

	return &postureIntegrationOutput{Body: postureIntegrationTo(updated)}, nil
}

// lookupPostureIntegration resolves the id path segment, mapping a
// malformed or unknown id to 404 so the SDK's IsNotFound behaves.
func lookupPostureIntegration(b Backend, rawID string) (types.PostureIntegration, error) {
	id, err := parseID(rawID, "posture integration")
	if err != nil {
		return types.PostureIntegration{}, err
	}

	i, err := b.State.GetPostureIntegration(types.PostureIntegrationID(id))
	if err != nil {
		return types.PostureIntegration{}, mapPostureIntegrationError("looking up posture integration", err)
	}

	return i, nil
}

// slopscaleProvider translates Tailscale's provider name, naming the
// supported ones when it cannot.
func slopscaleProvider(name string) (types.PostureProvider, error) {
	provider, ok := tailscaleProviders[strings.ToLower(name)]
	if !ok {
		return "", huma.Error400BadRequest(
			"unsupported posture provider " + name + "; slopscale integrates with " +
				"falcon, sentinelone, intune, jamfpro, kandji and kolide",
		)
	}

	return provider, nil
}

// providerBaseURL turns Tailscale's cloudId into the API origin slopscale
// asks the provider on: an https:// origin as given, or a CrowdStrike
// cloud name for falcon. An empty cloudId leaves the provider's default.
func providerBaseURL(provider types.PostureProvider, cloudID string) (string, error) {
	cloudID = strings.TrimSpace(cloudID)
	if cloudID == "" {
		return "", nil
	}

	u, err := url.Parse(cloudID)
	if err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" {
		return cloudID, nil
	}

	if provider == types.PostureProviderFalcon {
		origin, ok := falconClouds[strings.ToLower(cloudID)]
		if ok {
			return origin, nil
		}
	}

	return "", huma.Error400BadRequest(
		"cloudId must be an https:// origin" +
			", or one of us-1, us-2, eu-1 and us-gov-1 for falcon",
	)
}

// setProviderSecret puts Tailscale's single clientSecret where the
// provider expects it: an OAuth secret for falcon, intune and Jamf Pro, an
// API token for the rest.
func setProviderSecret(i *types.PostureIntegration, secret string) {
	switch i.Provider {
	case types.PostureProviderFalcon, types.PostureProviderIntune, types.PostureProviderJamf:
		i.Config.ClientSecret = secret
	case types.PostureProviderSentinelOne, types.PostureProviderKandji, types.PostureProviderKolide:
		i.Config.APIToken = secret
	}
}

// postureIntegrationTo renders a stored integration in Tailscale's shape.
// The secret is never returned.
func postureIntegrationTo(i types.PostureIntegration) PostureIntegration {
	provider := string(i.Provider)
	if i.Provider == types.PostureProviderJamf {
		provider = "jamfpro"
	}

	return PostureIntegration{
		ID:       i.ID.String(),
		Provider: provider,
		CloudID:  i.Config.BaseURL,
		ClientID: i.Config.ClientID,
		TenantID: i.Config.TenantID,
	}
}

func mapPostureIntegrationError(msg string, err error) error {
	switch {
	case errors.Is(err, types.ErrPostureIntegrationNotFound):
		return huma.Error404NotFound("posture integration not found")
	case errors.Is(err, types.ErrPostureIntegrationProvider),
		errors.Is(err, types.ErrPostureIntegrationName),
		errors.Is(err, types.ErrPostureIntegrationNameTaken),
		errors.Is(err, types.ErrPostureIntegrationConfig),
		errors.Is(err, state.ErrPostureProviderEnabled):
		return huma.Error400BadRequest(msg, err)
	}

	return mapError(msg, err)
}
