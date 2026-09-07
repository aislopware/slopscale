package servertest_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/servertest"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// webhookReceiver is an HTTP server that records every delivery it gets.
type webhookReceiver struct {
	*httptest.Server

	mu         sync.Mutex
	deliveries []webhookDelivery
}

type webhookDelivery struct {
	signature string
	body      []byte
	events    []types.WebhookEvent
}

func newWebhookReceiver(t *testing.T) *webhookReceiver {
	t.Helper()

	r := &webhookReceiver{}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		var events []types.WebhookEvent

		err = json.Unmarshal(body, &events)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)

			return
		}

		r.mu.Lock()
		r.deliveries = append(r.deliveries, webhookDelivery{
			signature: req.Header.Get(webhook.SignatureHeader),
			body:      body,
			events:    events,
		})
		r.mu.Unlock()

		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(r.Close)

	return r
}

// waitFor blocks until a delivery carrying an event of the given type
// has arrived and returns it.
func (r *webhookReceiver) waitFor(t *testing.T, eventType types.WebhookEventType) webhookDelivery {
	t.Helper()

	var found webhookDelivery

	require.Eventually(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()

		for _, d := range r.deliveries {
			for _, e := range d.events {
				if e.Type == eventType {
					found = d

					return true
				}
			}
		}

		return false
	}, 10*time.Second, 20*time.Millisecond, "no %s event delivered", eventType)

	return found
}

func (r *webhookReceiver) types(t *testing.T) []types.WebhookEventType {
	t.Helper()

	r.mu.Lock()
	defer r.mu.Unlock()

	var out []types.WebhookEventType

	for _, d := range r.deliveries {
		for _, e := range d.events {
			out = append(out, e.Type)
		}
	}

	return out
}

// TestWebhookDeliveries registers an endpoint through the v1 API and
// checks that node and user events reach it signed, that the test,
// rotate and delete operations behave, and that unsubscribed events stay
// away.
func TestWebhookDeliveries(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "hook-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	receiver := newWebhookReceiver(t)

	status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/webhook/event-types", nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.NotEmpty(t, body["types"])

	status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/webhook", map[string]any{
		"url":           receiver.URL,
		"description":   "ops channel",
		"subscriptions": []string{"nodeCreated", "nodeDeleted", "userCreated", "userRoleUpdated", "policyUpdate"},
	})
	require.Equal(t, http.StatusOK, status, body)

	w := hook(t, body)
	secret, _ := w["secret"].(string)
	require.NotEmpty(t, secret, "the secret is returned once, on create")

	id, _ := w["id"].(string)
	require.NotEmpty(t, id)
	assert.Equal(t, "ops channel", w["description"])
	assert.Equal(t, owner.StringID(), w["createdByUserId"])

	status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/webhook/"+id, nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.Nil(t, hook(t, body)["secret"], "reads never carry the secret")

	// A node registering fires nodeCreated with a valid signature.
	servertest.NewClient(t, srv, "hooked", servertest.WithUser(owner))
	created := receiver.waitFor(t, types.EventNodeCreated)
	assert.True(t, webhook.Verify(secret, created.signature, created.body, time.Now(), time.Minute),
		"signature must verify with the create-time secret")

	require.Len(t, created.events, 1)
	assert.Equal(t, types.WebhookEventVersion, created.events[0].Version)
	assert.NotEmpty(t, created.events[0].Tailnet)
	assert.Contains(t, created.events[0].Message, "hooked")

	nodeData, ok := created.events[0].Data.(map[string]any)
	require.True(t, ok, "node events carry an object")
	assert.Equal(t, "hooked", nodeData["deviceName"], "no base domain, so the name alone")
	assert.Equal(t, owner.Username(), nodeData["managedBy"])
	assert.NotEmpty(t, nodeData["nodeID"])
	assert.Contains(t, nodeData["url"], "/admin/machines/")

	// Unsubscribed events stay away: a policy update is not delivered.
	status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/webhook/"+id+"/test", nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, true, body["delivered"])
	receiver.waitFor(t, types.EventTest)

	status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/webhook/"+id, nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, "204", hook(t, body)["lastDeliveryStatus"])
	assert.NotEmpty(t, hook(t, body)["lastDeliveryAt"])

	status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/webhook/"+id+"/deliveries", nil)
	require.Equal(t, http.StatusOK, status, body)

	deliveries, _ := body["deliveries"].([]any)
	require.Len(t, deliveries, 2, "the node event and the test event")

	newest, _ := deliveries[0].(map[string]any)
	assert.Equal(t, "test", newest["eventType"], "newest first")
	assert.Equal(t, true, newest["ok"])
	assert.Equal(t, "204", newest["status"])
	assert.InDelta(t, 1, newest["attempts"], 0)

	oldest, _ := deliveries[1].(map[string]any)
	assert.Equal(t, "nodeCreated", oldest["eventType"])

	// User events.
	status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/user", map[string]any{"name": "hook-member"})
	require.Equal(t, http.StatusOK, status, body)

	userEvent := receiver.waitFor(t, types.EventUserCreated)
	assert.True(t, webhook.Verify(secret, userEvent.signature, userEvent.body, time.Now(), time.Minute))

	// Groups and rules are policy to the tailnet, so creating a group is a
	// policy update.
	status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/group", map[string]any{"name": "hooked"})
	require.Equal(t, http.StatusOK, status, body)
	receiver.waitFor(t, types.EventPolicyUpdate)

	// Rotating the secret returns a new one and signs later events with it.
	status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/webhook/"+id+"/rotate", nil)
	require.Equal(t, http.StatusOK, status, body)

	rotated, _ := hook(t, body)["secret"].(string)
	require.NotEmpty(t, rotated)
	require.NotEqual(t, secret, rotated)

	nodeID := findNodeID(t, srv, "hooked")
	status, body = apiCall(t, client, ownerKey, http.MethodDelete, v1+"/node/"+nodeID.String(), nil)
	require.Equal(t, http.StatusOK, status, body)

	deleted := receiver.waitFor(t, types.EventNodeDeleted)
	assert.False(t, webhook.Verify(secret, deleted.signature, deleted.body, time.Now(), time.Minute),
		"the old secret no longer verifies")
	assert.True(t, webhook.Verify(rotated, deleted.signature, deleted.body, time.Now(), time.Minute))

	// Updating the subscriptions narrows what is delivered.
	status, body = apiCall(t, client, ownerKey, http.MethodPut, v1+"/webhook/"+id, map[string]any{
		"url":           receiver.URL,
		"description":   "ops channel",
		"subscriptions": []string{"nodeDeleted"},
	})
	require.Equal(t, http.StatusOK, status, body)
	assert.ElementsMatch(t, []any{"nodeDeleted"}, hook(t, body)["subscriptions"])

	status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/user", map[string]any{"name": "hook-quiet"})
	require.Equal(t, http.StatusOK, status, body)

	// Unsubscribed events are dropped before any delivery starts, so once
	// the next test event has landed the user event cannot still be on
	// its way.
	status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/webhook/"+id+"/test", nil)
	require.Equal(t, http.StatusOK, status, body)
	require.Eventually(t, func() bool {
		return countType(receiver.types(t), types.EventTest) == 2
	}, 10*time.Second, 20*time.Millisecond)

	seen := receiver.types(t)
	assert.Equal(t, 1, countType(seen, types.EventUserCreated), "only the first user event was subscribed")

	status, body = apiCall(t, client, ownerKey, http.MethodDelete, v1+"/webhook/"+id, nil)
	require.Equal(t, http.StatusOK, status, body)

	status, _ = apiCall(t, client, ownerKey, http.MethodGet, v1+"/webhook/"+id, nil)
	assert.Equal(t, http.StatusNotFound, status)

	status, _ = apiCall(t, client, ownerKey, http.MethodGet, v1+"/webhook/"+id+"/deliveries", nil)
	assert.Equal(t, http.StatusNotFound, status, "history goes with the webhook")

	status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/webhook", nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.Empty(t, body["webhooks"])
}

// hook unwraps the v1 response envelope.
func hook(t *testing.T, body map[string]any) map[string]any {
	t.Helper()

	w, ok := body["webhook"].(map[string]any)
	require.True(t, ok, "response carries a webhook: %v", body)

	return w
}

func countType(seen []types.WebhookEventType, want types.WebhookEventType) int {
	n := 0

	for _, s := range seen {
		if s == want {
			n++
		}
	}

	return n
}

// TestWebhookValidation covers the refusals: a bad URL, no subscriptions,
// an unknown event and an unknown provider.
func TestWebhookValidation(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "hook-validator")
	ownerKey := srv.CreateAPIKey(t, owner)

	cases := []struct {
		name string
		body map[string]any
		want int
	}{
		{"bad url", map[string]any{"url": "ftp://x", "subscriptions": []string{"nodeCreated"}}, http.StatusBadRequest},
		{"no subscriptions", map[string]any{"url": "https://example.com/hook"}, http.StatusUnprocessableEntity},
		{
			"unknown event",
			map[string]any{"url": "https://example.com/hook", "subscriptions": []string{"nodeExploded"}},
			http.StatusBadRequest,
		},
		{
			"unknown provider",
			map[string]any{
				"url": "https://example.com/hook", "providerType": "pager", "subscriptions": []string{"nodeCreated"},
			},
			http.StatusUnprocessableEntity,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/webhook", tc.body)
			assert.Equal(t, tc.want, status, body)
		})
	}
}

// TestWebhookTailscaleAPI drives the same feature through the Tailscale
// compatible v2 endpoints.
func TestWebhookTailscaleAPI(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v2 := srv.URL + "/api/v2"

	owner := srv.CreateUser(t, "hook-v2")
	ownerKey := srv.CreateAPIKey(t, owner)
	receiver := newWebhookReceiver(t)

	status, body := apiCall(t, client, ownerKey, http.MethodPost, v2+"/tailnet/-/webhooks", map[string]any{
		"endpointUrl":   receiver.URL,
		"providerType":  "slack",
		"subscriptions": []string{"nodeCreated"},
	})
	require.Equal(t, http.StatusOK, status, body)

	id, _ := body["endpointId"].(string)
	require.NotEmpty(t, id)
	assert.NotEmpty(t, body["secret"])
	assert.NotEmpty(t, body["created"])
	assert.NotEmpty(t, body["lastModified"])
	assert.Equal(t, "slack", body["providerType"])

	status, body = apiCall(t, client, ownerKey, http.MethodPatch, v2+"/webhooks/"+id, map[string]any{
		"subscriptions": []string{"nodeCreated", "nodeDeleted"},
	})
	require.Equal(t, http.StatusOK, status, body)
	assert.ElementsMatch(t, []any{"nodeCreated", "nodeDeleted"}, body["subscriptions"])

	status, body = apiCall(t, client, ownerKey, http.MethodGet, v2+"/tailnet/-/webhooks", nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.Len(t, body["webhooks"], 1)

	status, body = apiCall(t, client, ownerKey, http.MethodPost, v2+"/webhooks/"+id+"/rotate", nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.NotEmpty(t, body["secret"])

	status, _ = apiCall(t, client, ownerKey, http.MethodDelete, v2+"/webhooks/"+id, nil)
	require.Equal(t, http.StatusOK, status)

	status, _ = apiCall(t, client, ownerKey, http.MethodGet, v2+"/webhooks/"+id, nil)
	assert.Equal(t, http.StatusNotFound, status)
}

// TestWebhookRoles follows Tailscale: every admin role manages webhooks,
// an auditor reads, a member sees nothing.
func TestWebhookRoles(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "hook-roles-owner")
	ownerKey := srv.CreateAPIKey(t, owner)
	receiver := newWebhookReceiver(t)

	keys := map[types.Role]string{}

	for _, role := range []types.Role{types.RoleNetworkAdmin, types.RoleITAdmin, types.RoleAuditor, types.RoleMember} {
		user := srv.CreateUser(t, "hook-roles-"+string(role))
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/user/"+userID(user)+"/role",
			map[string]string{"role": string(role)})
		require.Equal(t, http.StatusOK, status, body)

		keys[role] = srv.CreateAPIKey(t, user)
	}

	create := map[string]any{"url": receiver.URL, "subscriptions": []string{"nodeCreated"}}

	for _, role := range []types.Role{types.RoleNetworkAdmin, types.RoleITAdmin} {
		status, body := apiCall(t, client, keys[role], http.MethodPost, v1+"/webhook", create)
		assert.Equal(t, http.StatusOK, status, "%s creates: %v", role, body)
	}

	status, body := apiCall(t, client, keys[types.RoleAuditor], http.MethodGet, v1+"/webhook", nil)
	assert.Equal(t, http.StatusOK, status, body)
	assert.Len(t, body["webhooks"], 2)

	status, body = apiCall(t, client, keys[types.RoleAuditor], http.MethodPost, v1+"/webhook", create)
	assert.Equal(t, http.StatusForbidden, status, body)

	status, body = apiCall(t, client, keys[types.RoleMember], http.MethodGet, v1+"/webhook", nil)
	assert.Equal(t, http.StatusForbidden, status, body)
}
