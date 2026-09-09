package apiv1

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/danielgtaylor/huma/v2"
)

func init() {
	registrations = append(registrations, registerPostureIntegrations)
}

const tagPostureIntegrations = "Posture integrations"

// PostureIntegration is a device management or endpoint security service
// asked about every machine by serial number; its answer becomes
// prefixed posture attributes. See docs/ref/device-trust.md.
type PostureIntegration struct {
	ID       string `format:"uint64"                                            json:"id"`
	Provider string `doc:"falcon, sentinelone, intune, jamf, kandji or kolide." enum:"falcon,sentinelone,intune,jamf,kandji,kolide" json:"provider"` //nolint:lll // struct tag
	Name     string `json:"name"`
	// Prefix is the attribute namespace the provider writes, with the
	// colon: falcon:, intune:.
	Prefix  string                   `json:"prefix"`
	Enabled bool                     `json:"enabled"`
	Config  PostureIntegrationConfig `json:"config"`
	// HasSecret reports whether a credential is stored; the secret itself
	// is never returned.
	HasSecret   bool       `json:"hasSecret"`
	LastSyncAt  *time.Time `doc:"When the provider was last asked; absent before the first sync." json:"lastSyncAt,omitempty"` //nolint:lll // struct tag
	LastError   string     `doc:"What the last sync failed with; empty after a good one."         json:"lastError"`
	LastMatched int        `doc:"How many machines the provider knew at the last sync."           json:"lastMatched"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// PostureIntegrationConfig is how the provider is reached, without the
// secret.
type PostureIntegrationConfig struct {
	BaseURL  string `doc:"The API origin: the Falcon cloud, the SentinelOne console, the Jamf Pro server or the Kandji tenant. Intune and Kolide have fixed endpoints." json:"baseUrl,omitempty"`  //nolint:lll // struct tag
	ClientID string `doc:"OAuth client id (Falcon, Intune, Jamf Pro)."                                                                                                  json:"clientId,omitempty"` //nolint:lll // struct tag
	TenantID string `doc:"Entra tenant id (Intune)."                                                                                                                    json:"tenantId,omitempty"` //nolint:lll // struct tag
}

// PostureIntegrationRequestBody creates, replaces or checks an
// integration. On an update an empty secret keeps the stored one.
type PostureIntegrationRequestBody struct {
	Provider     string `enum:"falcon,sentinelone,intune,jamf,kandji,kolide"   json:"provider"`
	Name         string `json:"name"                                           minLength:"1"`
	Enabled      *bool  `doc:"Defaults to true."                               json:"enabled,omitempty"`
	BaseURL      string `json:"baseUrl,omitempty"`
	ClientID     string `json:"clientId,omitempty"`
	ClientSecret string `doc:"OAuth client secret (Falcon, Intune, Jamf Pro)." json:"clientSecret,omitempty"`
	APIToken     string `doc:"API token (SentinelOne, Kandji, Kolide)."        json:"apiToken,omitempty"`
	TenantID     string `json:"tenantId,omitempty"`
}

// PostureIntegrationCheckBody is a definition to try against the
// provider; with an id, empty secrets come from the stored integration.
type PostureIntegrationCheckBody struct {
	PostureIntegrationRequestBody

	ID string `doc:"A stored integration whose secret to use when none is given." format:"uint64" json:"id,omitempty"`
}

// PostureProvider describes one supported service for the console: what
// it needs and which attributes it writes.
type PostureProvider struct {
	Provider string `json:"provider"`
	Label    string `json:"label"`
	Prefix   string `json:"prefix"`
	// Fields are the configuration fields the provider needs, in form
	// order: baseUrl, clientId, clientSecret, apiToken, tenantId.
	Fields []string `json:"fields" nullable:"false"`
	// BaseURLDefault is the fixed endpoint when the provider has one.
	BaseURLDefault string                     `json:"baseUrlDefault,omitempty"`
	Attributes     []PostureProviderAttribute `json:"attributes"               nullable:"false"`
	// Help points at the provider's own instructions for the credential.
	Help string `json:"help"`
}

// PostureProviderAttribute is one attribute a provider writes.
type PostureProviderAttribute struct {
	Name        string `doc:"The attribute name with the prefix, falcon:ztaScore." json:"name"`
	Type        string `enum:"string,number,boolean"                               json:"type"`
	Description string `json:"description"`
}

type (
	postureIntegrationIDInput struct {
		ID string `format:"uint64" path:"id"`
	}
	postureIntegrationBodyInput struct {
		Body PostureIntegrationRequestBody
	}
	postureIntegrationUpdateInput struct {
		ID   string `format:"uint64" path:"id"`
		Body PostureIntegrationRequestBody
	}
	postureIntegrationCheckInput struct {
		Body PostureIntegrationCheckBody
	}
	postureIntegrationOutput struct {
		Body struct {
			Integration PostureIntegration `json:"integration"`
		}
	}
	listPostureIntegrationsOutput struct {
		Body struct {
			Integrations []PostureIntegration `json:"integrations" nullable:"false"`
		}
	}
	postureProvidersOutput struct {
		Body struct {
			Providers []PostureProvider `json:"providers" nullable:"false"`
		}
	}
	postureIntegrationCheckOutput struct {
		Body struct {
			// OK is true when the credentials reached the provider.
			OK bool `json:"ok"`
		}
	}
)

func parsePostureIntegrationID(s string) (types.PostureIntegrationID, error) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, huma.Error400BadRequest("invalid posture integration id", err)
	}

	return types.PostureIntegrationID(id), nil
}

func postureIntegrationFromBody(body PostureIntegrationRequestBody) types.PostureIntegration {
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}

	return types.PostureIntegration{
		Provider: types.PostureProvider(body.Provider),
		Name:     body.Name,
		Enabled:  enabled,
		Config: types.PostureIntegrationConfig{
			BaseURL:      body.BaseURL,
			ClientID:     body.ClientID,
			ClientSecret: body.ClientSecret,
			APIToken:     body.APIToken,
			TenantID:     body.TenantID,
		},
	}
}

func postureIntegrationFrom(i types.PostureIntegration) PostureIntegration {
	out := PostureIntegration{
		ID:       i.ID.String(),
		Provider: string(i.Provider),
		Name:     i.Name,
		Prefix:   i.Provider.Prefix(),
		Enabled:  i.Enabled,
		Config: PostureIntegrationConfig{
			BaseURL:  i.Config.BaseURL,
			ClientID: i.Config.ClientID,
			TenantID: i.Config.TenantID,
		},
		HasSecret:   i.Config.HasSecret(),
		LastError:   i.LastError,
		LastMatched: i.LastMatched,
		CreatedAt:   i.CreatedAt,
		UpdatedAt:   i.UpdatedAt,
	}

	if !i.LastSyncAt.IsZero() {
		at := i.LastSyncAt
		out.LastSyncAt = &at
	}

	return out
}

// The attribute value kinds the catalog reports; attrString doubles as
// the OpenAPI schema type where a binary body is declared.
const (
	attrString  = "string"
	attrNumber  = "number"
	attrBoolean = "boolean"
)

// postureProviderCatalog is what the console shows for each provider;
// the attribute names are Tailscale's so a posture written for
// Tailscale works unchanged.
var postureProviderCatalog = []PostureProvider{
	{
		Provider: string(types.PostureProviderFalcon), Label: "CrowdStrike Falcon",
		Prefix: "falcon:", Fields: []string{"baseUrl", "clientId", "clientSecret"},
		BaseURLDefault: "https://api.crowdstrike.com",
		Attributes: []PostureProviderAttribute{
			{Name: "falcon:ztaScore", Type: attrNumber, Description: "The Zero Trust Assessment score, 0 to 100."},
		},
		Help: "https://falcon.crowdstrike.com/api-clients-and-keys/clients",
	},
	{
		Provider: string(types.PostureProviderSentinelOne), Label: "SentinelOne",
		Prefix: "sentinelOne:", Fields: []string{"baseUrl", "apiToken"},
		Attributes: []PostureProviderAttribute{
			{Name: "sentinelOne:operationalState", Type: attrString, Description: "The agent's operational state."},
			{Name: "sentinelOne:activeThreats", Type: attrNumber, Description: "Unresolved threats on the device."},
			{Name: "sentinelOne:agentVersion", Type: attrString, Description: "The installed agent version."},
			{
				Name:        "sentinelOne:encryptedApplications",
				Type:        attrBoolean,
				Description: "Whether disk encryption is on.",
			},
			{Name: "sentinelOne:firewallEnabled", Type: attrBoolean, Description: "Whether the firewall is on."},
			{Name: "sentinelOne:infected", Type: attrBoolean, Description: "Whether the agent reports an infection."},
		},
		Help: "https://www.sentinelone.com/",
	},
	{
		Provider: string(types.PostureProviderIntune), Label: "Microsoft Intune",
		Prefix: "intune:", Fields: []string{"tenantId", "clientId", "clientSecret"},
		BaseURLDefault: "https://graph.microsoft.com",
		Attributes: []PostureProviderAttribute{
			{
				Name:        "intune:complianceState",
				Type:        attrString,
				Description: "compliant, noncompliant, inGracePeriod, unknown.",
			},
			{
				Name:        "intune:azureADRegistered",
				Type:        attrBoolean,
				Description: "Whether the device is registered in Entra.",
			},
			{
				Name:        "intune:deviceRegistrationState",
				Type:        attrString,
				Description: "registered, notRegistered, revoked.",
			},
			{Name: "intune:isSupervised", Type: attrBoolean, Description: "Whether the device is supervised."},
			{Name: "intune:isEncrypted", Type: attrBoolean, Description: "Whether the disk is encrypted."},
			{Name: "intune:managedDeviceOwnerType", Type: attrString, Description: "company, personal or unknown."},
		},
		Help: "https://learn.microsoft.com/graph/api/intune-devices-manageddevice-list",
	},
	{
		Provider: string(types.PostureProviderJamf), Label: "Jamf Pro",
		Prefix: "jamfPro:", Fields: []string{"baseUrl", "clientId", "clientSecret"},
		Attributes: []PostureProviderAttribute{
			{Name: "jamfPro:remoteManaged", Type: attrBoolean, Description: "Whether MDM manages the computer."},
			{Name: "jamfPro:supervised", Type: attrBoolean, Description: "Whether the computer is supervised."},
			{Name: "jamfPro:firewallEnabled", Type: attrBoolean, Description: "Whether the firewall is on."},
			{
				Name:        "jamfPro:fileVaultStatus",
				Type:        attrString,
				Description: "ALL_ENCRYPTED, SOME_ENCRYPTED, NOT_ENCRYPTED.",
			},
			{Name: "jamfPro:SIPEnabled", Type: attrString, Description: "ENABLED or DISABLED."},
		},
		Help: "https://learn.jamf.com/bundle/jamf-pro-documentation-current/page/API_Roles_and_Clients.html",
	},
	{
		Provider: string(types.PostureProviderKandji), Label: "Kandji (Iru)",
		Prefix: "kandji:", Fields: []string{"baseUrl", "apiToken"},
		Attributes: []PostureProviderAttribute{
			{Name: "kandji:mdmEnabled", Type: attrBoolean, Description: "Whether MDM is enabled on the device."},
			{Name: "kandji:agentInstalled", Type: attrBoolean, Description: "Whether the Kandji agent is installed."},
		},
		Help: "https://support.kandji.io/kb/kandji-api",
	},
	{
		Provider: string(types.PostureProviderKolide), Label: "Kolide (1Password XAM)",
		Prefix: "kolide:", Fields: []string{"apiToken"},
		BaseURLDefault: "https://api.kolide.com",
		Attributes: []PostureProviderAttribute{
			{Name: "kolide:authState", Type: attrString, Description: "Good, Notified, Will Block or Blocked."},
		},
		Help: "https://www.kolide.com/docs/developers/api",
	},
}

func registerPostureIntegrations(api huma.API, b Backend) {
	registerPostureIntegrationWrites(api, b)

	huma.Register(api, withScope(huma.Operation{
		OperationID: "listPostureProviders",
		Method:      http.MethodGet,
		Path:        "/api/v1/posture-integrations/providers",
		Summary:     "List posture providers",
		Description: "The supported device management and endpoint security services, with the fields " +
			"each needs and the attributes it writes.",
		Tags:     []string{tagPostureIntegrations},
		Security: bearerAuth,
	}, scope.DevicesPostureAttributesRead), func(_ context.Context, _ *struct{}) (*postureProvidersOutput, error) {
		out := &postureProvidersOutput{}
		out.Body.Providers = postureProviderCatalog

		return out, nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "listPostureIntegrations",
		Method:      http.MethodGet,
		Path:        "/api/v1/posture-integrations",
		Summary:     "List posture integrations",
		Description: "Posture integrations ask a device management or endpoint security service about " +
			"every machine by serial number and write the answer as prefixed posture attributes.",
		Tags:     []string{tagPostureIntegrations},
		Security: bearerAuth,
	}, scope.DevicesPostureAttributesRead), func(
		_ context.Context, _ *struct{},
	) (*listPostureIntegrationsOutput, error) {
		out := &listPostureIntegrationsOutput{}
		out.Body.Integrations = []PostureIntegration{}

		for _, i := range b.State.PostureIntegrations() {
			out.Body.Integrations = append(out.Body.Integrations, postureIntegrationFrom(i))
		}

		return out, nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "getPostureIntegration",
		Method:      http.MethodGet,
		Path:        "/api/v1/posture-integration/{id}",
		Summary:     "Get posture integration",
		Tags:        []string{tagPostureIntegrations},
		Security:    bearerAuth,
	}, scope.DevicesPostureAttributesRead), func(
		_ context.Context, in *postureIntegrationIDInput,
	) (*postureIntegrationOutput, error) {
		id, err := parsePostureIntegrationID(in.ID)
		if err != nil {
			return nil, err
		}

		i, err := b.State.GetPostureIntegration(id)
		if err != nil {
			return nil, mapError("getting posture integration", err)
		}

		out := &postureIntegrationOutput{}
		out.Body.Integration = postureIntegrationFrom(i)

		return out, nil
	})
}

// registerPostureIntegrationWrites is the mutating half of
// [registerPostureIntegrations].
func registerPostureIntegrationWrites(api huma.API, b Backend) {
	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "createPostureIntegration",
		Method:      http.MethodPost,
		Path:        "/api/v1/posture-integrations",
		Summary:     "Create posture integration",
		Description: "Stores the integration and, when enabled, syncs it at once. One enabled integration " +
			"per provider.",
		Tags:          []string{tagPostureIntegrations},
		Security:      bearerAuth,
		DefaultStatus: http.StatusCreated,
	}, scope.DevicesPostureAttributes), "posture_integration.create", "posture_integration", ""), func(
		ctx context.Context, in *postureIntegrationBodyInput,
	) (*postureIntegrationOutput, error) {
		created, c, err := b.State.CreatePostureIntegration(ctx, postureIntegrationFromBody(in.Body))
		if err != nil {
			return nil, mapError("creating posture integration", err)
		}

		return b.postureIntegrationResponse(ctx, created, c), nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "updatePostureIntegration",
		Method:      http.MethodPut,
		Path:        "/api/v1/posture-integration/{id}",
		Summary:     "Update posture integration",
		Description: "Replaces the definition; an empty secret keeps the stored one. Disabling drops the " +
			"attributes the provider wrote.",
		Tags:     []string{tagPostureIntegrations},
		Security: bearerAuth,
	}, scope.DevicesPostureAttributes), "posture_integration.update", "posture_integration", "id"), func(
		ctx context.Context, in *postureIntegrationUpdateInput,
	) (*postureIntegrationOutput, error) {
		id, err := parsePostureIntegrationID(in.ID)
		if err != nil {
			return nil, err
		}

		i := postureIntegrationFromBody(in.Body)
		i.ID = id

		updated, c, err := b.State.UpdatePostureIntegration(ctx, i)
		if err != nil {
			return nil, mapError("updating posture integration", err)
		}

		return b.postureIntegrationResponse(ctx, updated, c), nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "deletePostureIntegration",
		Method:      http.MethodDelete,
		Path:        "/api/v1/posture-integration/{id}",
		Summary:     "Delete posture integration",
		Description: "Removes the integration and the attributes its provider wrote.",
		Tags:        []string{tagPostureIntegrations},
		Security:    bearerAuth,
	}, scope.DevicesPostureAttributes), "posture_integration.delete", "posture_integration", "id"), func(
		ctx context.Context, in *postureIntegrationIDInput,
	) (*struct{}, error) {
		id, err := parsePostureIntegrationID(in.ID)
		if err != nil {
			return nil, err
		}

		existing, err := b.State.GetPostureIntegration(id)
		if err != nil {
			return nil, mapError("deleting posture integration", err)
		}

		c, err := b.State.DeletePostureIntegration(id)
		if err != nil {
			return nil, mapError("deleting posture integration", err)
		}

		audit.Target(ctx, "posture_integration", existing.ID.String(), existing.Name)
		b.Change(c)

		return nil, nil //nolint:nilnil // 204 No Content
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "syncPostureIntegration",
		Method:      http.MethodPost,
		Path:        "/api/v1/posture-integration/{id}/sync",
		Summary:     "Sync posture integration",
		Description: "Asks the provider now instead of at the next scheduled sync. A provider failure is " +
			"recorded on the integration, not returned.",
		Tags:     []string{tagPostureIntegrations},
		Security: bearerAuth,
	}, scope.DevicesPostureAttributes), "posture_integration.sync", "posture_integration", "id"), func(
		ctx context.Context, in *postureIntegrationIDInput,
	) (*postureIntegrationOutput, error) {
		id, err := parsePostureIntegrationID(in.ID)
		if err != nil {
			return nil, err
		}

		synced, c, err := b.State.SyncPostureIntegration(ctx, id)
		if err != nil {
			return nil, mapError("syncing posture integration", err)
		}

		return b.postureIntegrationResponse(ctx, synced, c), nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "checkPostureIntegration",
		Method:      http.MethodPost,
		Path:        "/api/v1/posture-integrations/check",
		Summary:     "Check posture integration credentials",
		Description: "Tries the credentials against the provider without storing anything. A rejected " +
			"credential or unreachable provider is a 502 with the provider's answer.",
		Tags:     []string{tagPostureIntegrations},
		Security: bearerAuth,
	}, scope.DevicesPostureAttributes), func(
		ctx context.Context, in *postureIntegrationCheckInput,
	) (*postureIntegrationCheckOutput, error) {
		i := postureIntegrationFromBody(in.Body.PostureIntegrationRequestBody)

		if in.Body.ID != "" {
			id, err := parsePostureIntegrationID(in.Body.ID)
			if err != nil {
				return nil, err
			}

			i.ID = id
		}

		err := b.State.CheckPostureIntegration(ctx, i)
		if err != nil {
			return nil, mapError("checking posture integration", err)
		}

		out := &postureIntegrationCheckOutput{}
		out.Body.OK = true

		return out, nil
	})
}

func (b Backend) postureIntegrationResponse(
	ctx context.Context, i types.PostureIntegration, c change.Change,
) *postureIntegrationOutput {
	audit.Target(ctx, "posture_integration", i.ID.String(), i.Name)
	b.Change(c)

	out := &postureIntegrationOutput{}
	out.Body.Integration = postureIntegrationFrom(i)

	return out
}
