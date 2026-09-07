package cli

import (
	"net/http"
	"testing"
	"time"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func webhookFlags(cmd *cobra.Command) {
	cmd.Flags().Uint64P("identifier", "i", 0, "")
	cmd.Flags().StringP("url", "u", "", "")
	cmd.Flags().StringP("description", "d", "", "")
	cmd.Flags().StringP("provider", "p", "", "")
	cmd.Flags().StringSliceP("event", "e", []string{}, "")
}

func sampleWebhook() clientv1.Webhook {
	deliveryTime := time.Date(2026, 3, 1, 15, 4, 0, 0, time.UTC)

	return clientv1.Webhook{
		Id:                 "1",
		Url:                "https://example.com/webhook",
		Description:        "Production alerts",
		ProviderType:       "slack",
		Subscriptions:      []string{"nodeCreated", "userCreated"},
		CreatedByUserId:    "42",
		CreatedAt:          time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		UpdatedAt:          time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC),
		LastDeliveryAt:     &deliveryTime,
		LastDeliveryStatus: "204",
	}
}

func neverDeliveredWebhook() clientv1.Webhook {
	return clientv1.Webhook{
		Id:              "2",
		Url:             "https://example.com/empty",
		Description:     "No delivery webhook",
		ProviderType:    "",
		Subscriptions:   []string{"nodeCreated"},
		CreatedByUserId: "42",
		CreatedAt:       time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		UpdatedAt:       time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC),
	}
}

func TestWebhookCommands(t *testing.T) {
	sample := sampleWebhook()
	neverDelivered := neverDeliveredWebhook()

	cases := []commandCase{
		{
			name: "list renders URL provider subscriptions last delivery and never",
			src:  listWebhooksCmd,
			routes: map[string]apiHandler{
				"GET /api/v1/webhook": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, clientv1.ListWebhooksOutputBody{
						Webhooks: []clientv1.Webhook{sample, neverDelivered},
					})
				},
			},
			wantIn: []string{
				"https://example.com/webhook",
				"slack",
				"nodeCreated, userCreated",
				"204 (2026-03-01 15:04)",
				"https://example.com/empty",
				"json",
				"never",
			},
		},
		{
			name:  "list as json",
			src:   listWebhooksCmd,
			flags: map[string]string{"output": "json"},
			routes: map[string]apiHandler{
				"GET /api/v1/webhook": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, clientv1.ListWebhooksOutputBody{
						Webhooks: []clientv1.Webhook{sample},
					})
				},
			},
			want: indentJSON(t, []clientv1.Webhook{sample}),
		},
		{
			name:  "show renders the subscriptions",
			src:   showWebhookCmd,
			flags: map[string]string{"identifier": "1"},
			routes: map[string]apiHandler{
				"GET /api/v1/webhook/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					writeJSON(t, w, clientv1.WebhookOutputBody{Webhook: sample})
				},
			},
			wantIn: []string{
				"URL: https://example.com/webhook",
				"Description: Production alerts",
				"Provider: slack",
				"nodeCreated",
				"userCreated",
			},
		},
		{
			name: "event-types prints the names",
			src:  listWebhookEventTypesCmd,
			routes: map[string]apiHandler{
				"GET /api/v1/webhook/event-types": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, clientv1.WebhookEventTypes{
						Types: []string{"nodeCreated", "nodeDeleted", "userCreated"},
					})
				},
			},
			wantIn: []string{
				"nodeCreated",
				"nodeDeleted",
				"userCreated",
			},
		},
		{
			name: "create builds the right body and prints secret",
			src:  createWebhookCmd,
			flags: map[string]string{
				"url":         "https://example.com/hook",
				"description": "Alerts hook",
				"provider":    "slack",
				"event":       "nodeCreated,userCreated",
			},
			routes: map[string]apiHandler{
				"POST /api/v1/webhook": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.WebhookRequestBody

					decodeBody(t, r, &body)
					assert.Equal(t, "https://example.com/hook", body.Url)

					if assert.NotNil(t, body.Description) {
						assert.Equal(t, "Alerts hook", *body.Description)
					}

					if assert.NotNil(t, body.ProviderType) {
						assert.Equal(t, clientv1.WebhookRequestBodyProviderTypeSlack, *body.ProviderType)
					}

					if assert.NotNil(t, body.Subscriptions) {
						assert.Equal(t, []string{"nodeCreated", "userCreated"}, *body.Subscriptions)
					}

					secret := "whsec_xyz123"
					created := sample
					created.Url = body.Url
					created.Secret = &secret

					writeJSON(t, w, clientv1.WebhookOutputBody{Webhook: created})
				},
			},
			wantIn: []string{
				"Secret (shown once, store it now): whsec_xyz123",
				"URL: https://example.com/hook",
			},
		},
		{
			name: "create with unknown provider errors before request",
			src:  createWebhookCmd,
			flags: map[string]string{
				"url":      "https://example.com/hook",
				"event":    "nodeCreated",
				"provider": "unknown",
			},
			routes:  map[string]apiHandler{},
			wantErr: "invalid provider",
		},
		{
			name: "update merges untouched fields and replaces given one",
			src:  updateWebhookCmd,
			flags: map[string]string{
				"identifier": "1",
				"url":        "https://example.com/updated",
			},
			routes: map[string]apiHandler{
				"GET /api/v1/webhook/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					writeJSON(t, w, clientv1.WebhookOutputBody{Webhook: sample})
				},
				"PUT /api/v1/webhook/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))

					var body clientv1.WebhookRequestBody

					decodeBody(t, r, &body)
					assert.Equal(t, "https://example.com/updated", body.Url)

					if assert.NotNil(t, body.Description) {
						assert.Equal(t, sample.Description, *body.Description)
					}

					if assert.NotNil(t, body.ProviderType) {
						assert.Equal(
							t,
							clientv1.WebhookRequestBodyProviderType(sample.ProviderType),
							*body.ProviderType,
						)
					}

					if assert.NotNil(t, body.Subscriptions) {
						assert.Equal(t, sample.Subscriptions, *body.Subscriptions)
					}

					updated := sample
					updated.Url = body.Url

					writeJSON(t, w, clientv1.WebhookOutputBody{Webhook: updated})
				},
			},
			wantIn: []string{"URL: https://example.com/updated"},
		},
		{
			name:  "rotate prints the new secret",
			src:   rotateWebhookCmd,
			flags: map[string]string{"identifier": "1"},
			routes: map[string]apiHandler{
				"POST /api/v1/webhook/{id}/rotate": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))

					newSecret := "whsec_rotated999"
					rotated := sample
					rotated.Secret = &newSecret

					writeJSON(t, w, clientv1.WebhookOutputBody{Webhook: rotated})
				},
			},
			wantIn: []string{"Secret (shown once, store it now): whsec_rotated999"},
		},
		{
			name:  "test prints delivered",
			src:   testWebhookCmd,
			flags: map[string]string{"identifier": "1"},
			routes: map[string]apiHandler{
				"POST /api/v1/webhook/{id}/test": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					writeJSON(t, w, clientv1.WebhookTestOutputBody{
						Delivered: true,
						Status:    "204",
					})
				},
			},
			want: "Delivered (status 204)\n",
		},
		{
			name:  "test with failed delivery returns error",
			src:   testWebhookCmd,
			flags: map[string]string{"identifier": "1"},
			routes: map[string]apiHandler{
				"POST /api/v1/webhook/{id}/test": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					writeJSON(t, w, clientv1.WebhookTestOutputBody{
						Delivered: false,
						Status:    "500 Internal Server Error",
					})
				},
			},
			wantErr: "delivery failed: 500 Internal Server Error",
		},
		{
			name:  "delete with --force skips the prompt",
			src:   deleteWebhookCmd,
			flags: map[string]string{"identifier": "1", "force": "true"},
			routes: map[string]apiHandler{
				"GET /api/v1/webhook/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					writeJSON(t, w, clientv1.WebhookOutputBody{Webhook: sample})
				},
				"DELETE /api/v1/webhook/{id}": deleteOK("1"),
			},
			want: "Webhook deleted\n",
		},
		{
			name:   "delete confirmed at the prompt",
			src:    deleteWebhookCmd,
			flags:  map[string]string{"identifier": "1"},
			prompt: "y",
			routes: map[string]apiHandler{
				"GET /api/v1/webhook/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					writeJSON(t, w, clientv1.WebhookOutputBody{Webhook: sample})
				},
				"DELETE /api/v1/webhook/{id}": deleteOK("1"),
			},
			want: "Webhook deleted\n",
		},
		{
			name:  "api problem response surfaces as error",
			src:   showWebhookCmd,
			flags: map[string]string{"identifier": "1"},
			routes: map[string]apiHandler{
				"GET /api/v1/webhook/{id}": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusForbidden, "missing scope")
				},
			},
			wantErr: "missing scope",
		},
	}

	runCommandCases(t, webhookFlags, cases)
}
