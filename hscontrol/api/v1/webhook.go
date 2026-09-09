package apiv1

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/webhook"
	"github.com/danielgtaylor/huma/v2"
)

func init() {
	registrations = append(registrations, registerWebhooks, registerWebhookActions, registerWebhookDeliveries)
}

const tagWebhooks = "Webhooks"

// Webhook is an endpoint the server posts events to. The secret is only
// present in the response that created or rotated it.
type Webhook struct {
	ID          string `format:"uint64"    json:"id"`
	URL         string `json:"url"`
	Description string `json:"description"`
	// ProviderType is empty for the signed JSON payload, a chat service
	// (slack, mattermost, googlechat, discord, teams), telegram, ntfy or
	// email; see docs/ref/webhooks.md for what each expects in the URL.
	ProviderType  string   `json:"providerType"`
	Subscriptions []string `json:"subscriptions" nullable:"false"`
	// Secret signs every delivery; shown once, on create and rotate.
	Secret          string     `json:"secret,omitempty"`
	CreatedByUserID string     `format:"uint64"         json:"createdByUserId"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	LastDeliveryAt  *time.Time `json:"lastDeliveryAt"`
	// LastDeliveryStatus is the HTTP status of the newest delivery, or
	// the error when none came back; empty before the first.
	LastDeliveryStatus string `json:"lastDeliveryStatus"`
}

// WebhookRequestBody creates or replaces a webhook.
type WebhookRequestBody struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
	// ProviderType shapes the payload and picks the transport: an
	// email endpoint's URL is "mailto:" and the recipients, a telegram
	// one is the Bot API's sendMessage URL with a chat_id query.
	//nolint:lll // the enum tag lists every provider
	ProviderType  string   `enum:",slack,mattermost,googlechat,discord,teams,telegram,ntfy,email" json:"providerType,omitempty"`
	Subscriptions []string `json:"subscriptions"`
}

// WebhookDelivery is one attempt at delivering an event, after retries.
type WebhookDelivery struct {
	ID        string `format:"uint64"  json:"id"`
	EventType string `json:"eventType"`
	// Status is the HTTP status, or the error text when no response came.
	Status string `json:"status"`
	// Ok is whether the receiver answered 2xx in the end.
	Ok bool `json:"ok"`
	// Attempts counts the requests made, retries included.
	Attempts   int       `json:"attempts"`
	DurationMs int64     `json:"durationMs"`
	At         time.Time `json:"at"`
}

// WebhookEventTypes lists what a webhook may subscribe to.
type WebhookEventTypes struct {
	Types []string `json:"types" nullable:"false"`
}

type (
	webhookIDInput struct {
		ID string `format:"uint64" path:"id"`
	}
	webhookDeliveriesOutput struct {
		Body struct {
			Deliveries []WebhookDelivery `json:"deliveries" nullable:"false"`
		}
	}
	webhookBodyInput struct {
		Body WebhookRequestBody
	}
	webhookUpdateInput struct {
		ID   string `format:"uint64" path:"id"`
		Body WebhookRequestBody
	}
	webhookOutput struct {
		Body struct {
			Webhook Webhook `json:"webhook"`
		}
	}
	listWebhooksOutput struct {
		Body struct {
			Webhooks []Webhook `json:"webhooks" nullable:"false"`
		}
	}
	webhookEventTypesOutput struct {
		Body WebhookEventTypes
	}
	webhookTestOutput struct {
		Body struct {
			// Delivered reports whether the receiver answered 2xx.
			Delivered bool   `json:"delivered"`
			Status    string `doc:"HTTP status or the error text." json:"status"`
		}
	}
)

func parseWebhookID(s string) (types.WebhookID, error) {
	id, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, huma.Error400BadRequest("invalid webhook id", err)
	}

	return types.WebhookID(id), nil
}

func webhookFromBody(body WebhookRequestBody) (types.Webhook, error) {
	subs, err := types.ParseWebhookSubscriptions(body.Subscriptions)
	if err != nil {
		return types.Webhook{}, mapError("parsing subscriptions", err)
	}

	return types.Webhook{
		URL:           body.URL,
		Description:   body.Description,
		ProviderType:  types.WebhookProvider(body.ProviderType),
		Subscriptions: subs,
	}, nil
}

// webhookFrom renders the record without its secret; withSecret adds it.
func webhookFrom(w types.Webhook, withSecret bool) Webhook {
	out := Webhook{
		ID:                 formatID(uint64(w.ID)),
		URL:                w.URL,
		Description:        w.Description,
		ProviderType:       string(w.ProviderType),
		Subscriptions:      make([]string, 0, len(w.Subscriptions)),
		CreatedByUserID:    formatID(uint64(w.CreatedBy)),
		CreatedAt:          w.CreatedAt,
		UpdatedAt:          w.UpdatedAt,
		LastDeliveryAt:     w.LastDeliveryAt,
		LastDeliveryStatus: w.LastDeliveryStatus,
	}

	for _, s := range w.Subscriptions {
		out.Subscriptions = append(out.Subscriptions, string(s))
	}

	if withSecret {
		out.Secret = w.Secret
	}

	return out
}

func registerWebhooks(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "listWebhookEventTypes",
		Method:      http.MethodGet,
		Path:        "/api/v1/webhook/event-types",
		Summary:     "List webhook event types",
		Tags:        []string{tagWebhooks},
		Security:    bearerAuth,
	}, scope.WebhooksRead), func(_ context.Context, _ *struct{}) (*webhookEventTypesOutput, error) {
		out := &webhookEventTypesOutput{}
		out.Body.Types = make([]string, 0, len(types.WebhookEventTypes))

		for _, t := range types.WebhookEventTypes {
			out.Body.Types = append(out.Body.Types, string(t))
		}

		return out, nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "listWebhooks",
		Method:      http.MethodGet,
		Path:        "/api/v1/webhook",
		Summary:     "List webhooks",
		Description: "Endpoints the server posts events to, signed with each endpoint's secret in " +
			"the " + webhook.SignatureHeader + " header. Secrets are not listed.",
		Tags:     []string{tagWebhooks},
		Security: bearerAuth,
	}, scope.WebhooksRead), func(_ context.Context, _ *struct{}) (*listWebhooksOutput, error) {
		hooks, err := b.State.ListWebhooks()
		if err != nil {
			return nil, mapError("listing webhooks", err)
		}

		out := &listWebhooksOutput{}
		out.Body.Webhooks = make([]Webhook, 0, len(hooks))

		for _, w := range hooks {
			out.Body.Webhooks = append(out.Body.Webhooks, webhookFrom(w, false))
		}

		return out, nil
	})

	huma.Register(api, withScope(huma.Operation{
		OperationID: "getWebhook",
		Method:      http.MethodGet,
		Path:        "/api/v1/webhook/{id}",
		Summary:     "Get webhook",
		Tags:        []string{tagWebhooks},
		Security:    bearerAuth,
	}, scope.WebhooksRead), func(_ context.Context, in *webhookIDInput) (*webhookOutput, error) {
		id, err := parseWebhookID(in.ID)
		if err != nil {
			return nil, err
		}

		w, err := b.State.GetWebhook(id)
		if err != nil {
			return nil, mapError("getting webhook", err)
		}

		out := &webhookOutput{}
		out.Body.Webhook = webhookFrom(w, false)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "createWebhook",
		Method:      http.MethodPost,
		Path:        "/api/v1/webhook",
		Summary:     "Create webhook",
		Description: "The response carries the signing secret; it is not shown again.",
		Tags:        []string{tagWebhooks},
		Security:    bearerAuth,
	}, scope.Webhooks), "webhook.create", "webhook", ""), func(
		ctx context.Context, in *webhookBodyInput,
	) (*webhookOutput, error) {
		w, err := webhookFromBody(in.Body)
		if err != nil {
			return nil, err
		}

		w.CreatedBy = caller(ctx).UserID

		created, err := b.State.CreateWebhook(w)
		if err != nil {
			return nil, mapError("creating webhook", err)
		}

		audit.Target(ctx, "", formatID(uint64(created.ID)), created.Host())
		audit.Detail(ctx, "subscriptions", in.Body.Subscriptions)

		out := &webhookOutput{}
		out.Body.Webhook = webhookFrom(created, true)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "updateWebhook",
		Method:      http.MethodPut,
		Path:        "/api/v1/webhook/{id}",
		Summary:     "Replace webhook",
		Description: "The secret stays; rotate it separately.",
		Tags:        []string{tagWebhooks},
		Security:    bearerAuth,
	}, scope.Webhooks), "webhook.update", "webhook", "id"), func(
		ctx context.Context, in *webhookUpdateInput,
	) (*webhookOutput, error) {
		id, err := parseWebhookID(in.ID)
		if err != nil {
			return nil, err
		}

		w, err := webhookFromBody(in.Body)
		if err != nil {
			return nil, err
		}

		w.ID = id

		updated, err := b.State.UpdateWebhook(w)
		if err != nil {
			return nil, mapError("updating webhook", err)
		}

		audit.Target(ctx, "", "", updated.Host())
		audit.Detail(ctx, "subscriptions", in.Body.Subscriptions)

		out := &webhookOutput{}
		out.Body.Webhook = webhookFrom(updated, false)

		return out, nil
	})
}

// registerWebhookActions adds delete, rotate and test.
func registerWebhookActions(api huma.API, b Backend) {
	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "deleteWebhook",
		Method:      http.MethodDelete,
		Path:        "/api/v1/webhook/{id}",
		Summary:     "Delete webhook",
		Tags:        []string{tagWebhooks},
		Security:    bearerAuth,
	}, scope.Webhooks), "webhook.delete", "webhook", "id"), func(
		ctx context.Context, in *webhookIDInput,
	) (*emptyOutput, error) {
		id, err := parseWebhookID(in.ID)
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

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "rotateWebhookSecret",
		Method:      http.MethodPost,
		Path:        "/api/v1/webhook/{id}/rotate",
		Summary:     "Rotate webhook secret",
		Description: "Replaces the signing secret and returns it; deliveries signed with the old " +
			"one stop at once.",
		Tags:     []string{tagWebhooks},
		Security: bearerAuth,
	}, scope.Webhooks), "webhook.rotate", "webhook", "id"), func(
		ctx context.Context, in *webhookIDInput,
	) (*webhookOutput, error) {
		id, err := parseWebhookID(in.ID)
		if err != nil {
			return nil, err
		}

		rotated, err := b.State.RotateWebhookSecret(id)
		if err != nil {
			return nil, mapError("rotating webhook secret", err)
		}

		audit.Target(ctx, "", "", rotated.Host())

		out := &webhookOutput{}
		out.Body.Webhook = webhookFrom(rotated, true)

		return out, nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "testWebhook",
		Method:      http.MethodPost,
		Path:        "/api/v1/webhook/{id}/test",
		Summary:     "Test webhook",
		Description: "Posts a test event now and reports the receiver's answer.",
		Tags:        []string{tagWebhooks},
		Security:    bearerAuth,
	}, scope.Webhooks), "webhook.test", "webhook", "id"), func(
		ctx context.Context, in *webhookIDInput,
	) (*webhookTestOutput, error) {
		id, err := parseWebhookID(in.ID)
		if err != nil {
			return nil, err
		}

		err = b.State.TestWebhook(ctx, id)

		w, getErr := b.State.GetWebhook(id)
		if getErr != nil {
			return nil, mapError("testing webhook", getErr)
		}

		audit.Target(ctx, "", "", w.Host())
		audit.Detail(ctx, "status", w.LastDeliveryStatus)

		out := &webhookTestOutput{}
		out.Body.Delivered = err == nil
		out.Body.Status = w.LastDeliveryStatus

		return out, nil
	})
}

func registerWebhookDeliveries(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "listWebhookDeliveries",
		Method:      http.MethodGet,
		Path:        "/api/v1/webhook/{id}/deliveries",
		Summary:     "List webhook deliveries",
		Description: "The newest deliveries to the webhook, most recent first; the server keeps the last " +
			strconv.Itoa(types.WebhookDeliveryHistory) + " per webhook.",
		Tags:     []string{tagWebhooks},
		Security: bearerAuth,
	}, scope.WebhooksRead), func(_ context.Context, in *webhookIDInput) (*webhookDeliveriesOutput, error) {
		id, err := parseWebhookID(in.ID)
		if err != nil {
			return nil, err
		}

		deliveries, err := b.State.ListWebhookDeliveries(id)
		if err != nil {
			return nil, mapError("listing webhook deliveries", err)
		}

		out := &webhookDeliveriesOutput{}
		out.Body.Deliveries = make([]WebhookDelivery, 0, len(deliveries))

		for _, d := range deliveries {
			out.Body.Deliveries = append(out.Body.Deliveries, WebhookDelivery{
				ID:         formatID(uint64(d.ID)),
				EventType:  string(d.EventType),
				Status:     d.Status,
				Ok:         d.OK,
				Attempts:   d.Attempts,
				DurationMs: d.Duration.Milliseconds(),
				At:         d.At,
			})
		}

		return out, nil
	})
}
