package apiv2

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/aislopware/slopscale/hscontrol/api/principal"
	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
)

func init() {
	registrations = append(registrations, registerWebhooks, registerWebhookItem, registerWebhookActions)
}

// WebhookEndpoint is Tailscale's webhook endpoint shape. secret is only
// present in the response that created or rotated it.
type WebhookEndpoint struct {
	EndpointID       string     `json:"endpointId"`
	EndpointURL      string     `json:"endpointUrl"`
	ProviderType     string     `json:"providerType"`
	CreatorLoginName string     `json:"creatorLoginName"`
	Created          time.Time  `json:"created"`
	LastModified     time.Time  `json:"lastModified"`
	Subscriptions    []string   `json:"subscriptions"            nullable:"false"`
	Secret           string     `json:"secret,omitempty"`
	LastDeliveryAt   *time.Time `json:"lastDeliveryAt,omitempty"`
	// LastDeliveryStatus is a Slopscale addition: the HTTP status of the
	// newest delivery, or the error when none came back.
	LastDeliveryStatus string `json:"lastDeliveryStatus,omitempty"`
}

// CreateWebhookRequest is Tailscale's create body.
type CreateWebhookRequest struct {
	EndpointURL   string   `format:"uri"                                json:"endpointUrl"`
	ProviderType  string   `enum:",slack,mattermost,googlechat,discord" json:"providerType,omitempty"`
	Subscriptions []string `json:"subscriptions"`
}

// UpdateWebhookRequest is Tailscale's PATCH body; absent fields keep
// their value.
type UpdateWebhookRequest struct {
	EndpointURL   *string   `format:"uri"                                json:"endpointUrl,omitempty"`
	ProviderType  *string   `enum:",slack,mattermost,googlechat,discord" json:"providerType,omitempty"`
	Subscriptions *[]string `json:"subscriptions,omitempty"`
}

type (
	webhookIDInput struct {
		EndpointID string `path:"endpointId"`
	}
	createWebhookInput struct {
		Tailnet string `path:"tailnet"`
		Body    CreateWebhookRequest
	}
	updateWebhookInput struct {
		EndpointID string `path:"endpointId"`
		Body       UpdateWebhookRequest
	}
	webhookOutput struct {
		Body WebhookEndpoint
	}
	listWebhooksOutput struct {
		Body struct {
			Webhooks []WebhookEndpoint `json:"webhooks" nullable:"false"`
		}
	}
)

var webhookTags = []string{"Webhooks", tagTailscaleCompat}

func parseWebhookID(s string) (types.WebhookID, error) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, huma.Error404NotFound("webhook not found", err)
	}

	return types.WebhookID(id), nil
}

func webhookEndpointFrom(b Backend, w types.Webhook, withSecret bool) WebhookEndpoint {
	out := WebhookEndpoint{
		EndpointID:         strconv.FormatUint(uint64(w.ID), 10),
		EndpointURL:        w.URL,
		ProviderType:       string(w.ProviderType),
		Created:            w.CreatedAt,
		LastModified:       w.UpdatedAt,
		Subscriptions:      make([]string, 0, len(w.Subscriptions)),
		LastDeliveryAt:     w.LastDeliveryAt,
		LastDeliveryStatus: w.LastDeliveryStatus,
	}

	for _, s := range w.Subscriptions {
		out.Subscriptions = append(out.Subscriptions, string(s))
	}

	if w.CreatedBy != 0 {
		user, err := b.State.GetUserByID(w.CreatedBy)
		if err == nil {
			out.CreatorLoginName = user.Name
		}
	}

	if withSecret {
		out.Secret = w.Secret
	}

	return out
}

func registerWebhooks(api huma.API, b Backend) {
	huma.Register(api, principal.RequireScope(huma.Operation{
		OperationID: "listWebhooks",
		Method:      http.MethodGet,
		Path:        "/api/v2/tailnet/{tailnet}/webhooks",
		Summary:     "List webhooks",
		Tags:        webhookTags,
		Security:    security,
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.WebhooksRead), func(_ context.Context, in *tailnetInput) (*listWebhooksOutput, error) {
		err := requireDefaultTailnet(in.Tailnet)
		if err != nil {
			return nil, err
		}

		hooks, err := b.State.ListWebhooks()
		if err != nil {
			return nil, mapError("listing webhooks", err)
		}

		out := &listWebhooksOutput{}
		out.Body.Webhooks = make([]WebhookEndpoint, 0, len(hooks))

		for _, w := range hooks {
			out.Body.Webhooks = append(out.Body.Webhooks, webhookEndpointFrom(b, w, false))
		}

		return out, nil
	})

	huma.Register(api, audit.Declare(principal.RequireScope(huma.Operation{
		OperationID: "createWebhook",
		Method:      http.MethodPost,
		Path:        "/api/v2/tailnet/{tailnet}/webhooks",
		Summary:     "Create webhook",
		Description: "The response carries the signing secret; it is not shown again.",
		Tags:        webhookTags,
		Security:    security,
		Errors:      []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.Webhooks), "webhook.create", "webhook", ""), func(
		ctx context.Context, in *createWebhookInput,
	) (*webhookOutput, error) {
		err := requireDefaultTailnet(in.Tailnet)
		if err != nil {
			return nil, err
		}

		subs, err := types.ParseWebhookSubscriptions(in.Body.Subscriptions)
		if err != nil {
			return nil, mapError("parsing subscriptions", err)
		}

		w := types.Webhook{
			URL:           in.Body.EndpointURL,
			ProviderType:  types.WebhookProvider(in.Body.ProviderType),
			Subscriptions: subs,
		}

		if p, ok := principal.From(ctx); ok {
			w.CreatedBy = p.UserID
		}

		created, err := b.State.CreateWebhook(w)
		if err != nil {
			return nil, mapError("creating webhook", err)
		}

		audit.Target(ctx, "", strconv.FormatUint(uint64(created.ID), 10), created.Host())
		audit.Detail(ctx, "subscriptions", in.Body.Subscriptions)

		return &webhookOutput{Body: webhookEndpointFrom(b, created, true)}, nil
	})
}

// registerWebhookItem adds the get and patch operations on one endpoint.
func registerWebhookItem(api huma.API, b Backend) {
	huma.Register(api, principal.RequireScope(huma.Operation{
		OperationID: "getWebhook",
		Method:      http.MethodGet,
		Path:        "/api/v2/webhooks/{endpointId}",
		Summary:     "Get webhook",
		Tags:        webhookTags,
		Security:    security,
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.WebhooksRead), func(_ context.Context, in *webhookIDInput) (*webhookOutput, error) {
		id, err := parseWebhookID(in.EndpointID)
		if err != nil {
			return nil, err
		}

		w, err := b.State.GetWebhook(id)
		if err != nil {
			return nil, mapError("getting webhook", err)
		}

		return &webhookOutput{Body: webhookEndpointFrom(b, w, false)}, nil
	})

	huma.Register(api, audit.Declare(principal.RequireScope(huma.Operation{
		OperationID: "updateWebhook",
		Method:      http.MethodPatch,
		Path:        "/api/v2/webhooks/{endpointId}",
		Summary:     "Update webhook",
		Tags:        webhookTags,
		Security:    security,
		Errors:      []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.Webhooks), "webhook.update", "webhook", "endpointId"), func(
		ctx context.Context, in *updateWebhookInput,
	) (*webhookOutput, error) {
		id, err := parseWebhookID(in.EndpointID)
		if err != nil {
			return nil, err
		}

		w, err := b.State.GetWebhook(id)
		if err != nil {
			return nil, mapError("updating webhook", err)
		}

		if in.Body.EndpointURL != nil {
			w.URL = *in.Body.EndpointURL
		}

		if in.Body.ProviderType != nil {
			w.ProviderType = types.WebhookProvider(*in.Body.ProviderType)
		}

		if in.Body.Subscriptions != nil {
			w.Subscriptions, err = types.ParseWebhookSubscriptions(*in.Body.Subscriptions)
			if err != nil {
				return nil, mapError("parsing subscriptions", err)
			}
		}

		updated, err := b.State.UpdateWebhook(w)
		if err != nil {
			return nil, mapError("updating webhook", err)
		}

		audit.Target(ctx, "", "", updated.Host())

		return &webhookOutput{Body: webhookEndpointFrom(b, updated, false)}, nil
	})
}

func registerWebhookActions(api huma.API, b Backend) {
	huma.Register(api, audit.Declare(principal.RequireScope(huma.Operation{
		OperationID: "deleteWebhook",
		Method:      http.MethodDelete,
		Path:        "/api/v2/webhooks/{endpointId}",
		Summary:     "Delete webhook",
		Tags:        webhookTags,
		Security:    security,
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.Webhooks), "webhook.delete", "webhook", "endpointId"), func(
		ctx context.Context, in *webhookIDInput,
	) (*emptyOutput, error) {
		id, err := parseWebhookID(in.EndpointID)
		if err != nil {
			return nil, err
		}

		w, err := b.State.GetWebhook(id)
		if err != nil {
			return nil, mapError("deleting webhook", err)
		}

		err = b.State.DeleteWebhook(id)
		if err != nil {
			return nil, mapError("deleting webhook", err)
		}

		audit.Target(ctx, "", "", w.Host())

		return &emptyOutput{}, nil
	})

	huma.Register(api, audit.Declare(principal.RequireScope(huma.Operation{
		OperationID: "testWebhook",
		Method:      http.MethodPost,
		Path:        "/api/v2/webhooks/{endpointId}/test",
		Summary:     "Test webhook",
		Description: "Posts a test event now. Tailscale queues it; Slopscale delivers it before " +
			"answering, so a failing receiver shows as a 502.",
		Tags:     webhookTags,
		Security: security,
		Errors:   []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusBadGateway},
	}, scope.Webhooks), "webhook.test", "webhook", "endpointId"), func(
		ctx context.Context, in *webhookIDInput,
	) (*emptyOutput, error) {
		id, err := parseWebhookID(in.EndpointID)
		if err != nil {
			return nil, err
		}

		err = b.State.TestWebhook(ctx, id)
		if err != nil {
			_, getErr := b.State.GetWebhook(id)
			if getErr != nil {
				return nil, mapError("testing webhook", getErr)
			}

			return nil, huma.Error502BadGateway("testing webhook", err)
		}

		return &emptyOutput{}, nil
	})

	huma.Register(api, audit.Declare(principal.RequireScope(huma.Operation{
		OperationID: "rotateWebhookSecret",
		Method:      http.MethodPost,
		Path:        "/api/v2/webhooks/{endpointId}/rotate",
		Summary:     "Rotate webhook secret",
		Tags:        webhookTags,
		Security:    security,
		Errors:      []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, scope.Webhooks), "webhook.rotate", "webhook", "endpointId"), func(
		ctx context.Context, in *webhookIDInput,
	) (*webhookOutput, error) {
		id, err := parseWebhookID(in.EndpointID)
		if err != nil {
			return nil, err
		}

		rotated, err := b.State.RotateWebhookSecret(id)
		if err != nil {
			return nil, mapError("rotating webhook secret", err)
		}

		audit.Target(ctx, "", "", rotated.Host())

		return &webhookOutput{Body: webhookEndpointFrom(b, rotated, true)}, nil
	})
}
