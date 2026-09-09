package cli

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	clientv1 "github.com/aislopware/slopscale/gen/client/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func postureIntegrationFlags(cmd *cobra.Command) {
	cmd.Flags().Uint64P("id", "i", 0, "")
	cmd.Flags().String("provider", "", "")
	cmd.Flags().StringP("name", "n", "", "")
	cmd.Flags().String("base-url", "", "")
	cmd.Flags().String("client-id", "", "")
	cmd.Flags().String("client-secret", "", "")
	cmd.Flags().String("api-token", "", "")
	cmd.Flags().String("tenant-id", "", "")
	cmd.Flags().Bool("disabled", false, "")
}

func samplePostureIntegration() clientv1.PostureIntegration {
	baseURL := "https://api.crowdstrike.com"
	clientID := "client-123"
	syncTime := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)

	return clientv1.PostureIntegration{
		Id:          "1",
		Name:        "falcon-prod",
		Provider:    "falcon",
		Prefix:      "falcon",
		Enabled:     true,
		HasSecret:   true,
		LastMatched: 42,
		LastSyncAt:  &syncTime,
		LastError:   "",
		Config: clientv1.PostureIntegrationConfig{
			BaseUrl:  &baseURL,
			ClientId: &clientID,
		},
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}
}

func samplePostureProvider() clientv1.PostureProvider {
	return clientv1.PostureProvider{
		Provider: "falcon",
		Label:    "CrowdStrike Falcon",
		Prefix:   "falcon",
		Fields:   []string{"clientId", "clientSecret", "baseUrl"},
		Attributes: []clientv1.PostureProviderAttribute{
			{
				Name:        "falcon:ztaScore",
				Description: "Zero Trust Assessment score",
				Type:        "number",
			},
		},
	}
}

func TestPostureIntegrationCommands(t *testing.T) {
	sample := samplePostureIntegration()
	provider := samplePostureProvider()

	sampleWithError := sample
	sampleWithError.Id = "2"
	sampleWithError.Name = "sentinelone-test"
	sampleWithError.Provider = "sentinelone"
	sampleWithError.Enabled = false
	sampleWithError.LastSyncAt = nil
	sampleWithError.LastError = "invalid token"
	sampleWithError.LastMatched = 0

	cases := []commandCase{
		{
			name: "providers catalog",
			src:  listPostureProvidersCmd,
			routes: map[string]apiHandler{
				"GET /api/v1/posture-integrations/providers": func(
					t *testing.T, w http.ResponseWriter, _ *http.Request,
				) {
					t.Helper()
					writeJSON(t, w, clientv1.PostureProvidersOutputBody{
						Providers: []clientv1.PostureProvider{provider},
					})
				},
			},
			wantIn: []string{
				"falcon",
				"CrowdStrike Falcon",
				"clientId, clientSecret, baseUrl",
				"falcon:ztaScore",
			},
		},
		{
			name: "list renders table with status",
			src:  listPostureIntegrationsCmd,
			routes: map[string]apiHandler{
				"GET /api/v1/posture-integrations": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, clientv1.ListPostureIntegrationsOutputBody{
						Integrations: []clientv1.PostureIntegration{sample, sampleWithError},
					})
				},
			},
			wantIn: []string{
				"1",
				"falcon-prod",
				"falcon",
				"yes",
				"2026-03-01 10:00",
				"ok",
				"42",
				"2",
				"sentinelone-test",
				"sentinelone",
				"no",
				"never",
				"invalid token",
				"0",
			},
		},
		{
			name:  "list as json",
			src:   listPostureIntegrationsCmd,
			flags: map[string]string{"output": "json"},
			routes: map[string]apiHandler{
				"GET /api/v1/posture-integrations": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, clientv1.ListPostureIntegrationsOutputBody{
						Integrations: []clientv1.PostureIntegration{sample},
					})
				},
			},
			want: indentJSON(t, []clientv1.PostureIntegration{sample}),
		},
		{
			name:  "show renders details",
			src:   showPostureIntegrationCmd,
			flags: map[string]string{"id": "1"},
			routes: map[string]apiHandler{
				"GET /api/v1/posture-integration/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					writeJSON(t, w, clientv1.PostureIntegrationOutputBody{Integration: sample})
				},
			},
			wantIn: []string{
				"ID: 1",
				"Name: falcon-prod",
				"Provider: falcon",
				"Prefix: falcon",
				"Enabled: yes",
				"Base URL: https://api.crowdstrike.com",
				"Client ID: client-123",
				"Has secret: yes",
				"Status: ok",
				"Matched machines: 42",
			},
		},
		{
			name: "create posture integration",
			src:  createPostureIntegrationCmd,
			flags: map[string]string{
				"provider":      "falcon",
				"name":          "falcon-prod",
				"base-url":      "https://api.crowdstrike.com",
				"client-id":     "client-123",
				"client-secret": "secret-xyz",
			},
			routes: map[string]apiHandler{
				"POST /api/v1/posture-integrations": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.CreatePostureIntegrationJSONRequestBody

					require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					assert.Equal(t, "falcon-prod", body.Name)
					assert.Equal(t, clientv1.PostureIntegrationRequestBodyProvider("falcon"), body.Provider)
					assert.Equal(t, "https://api.crowdstrike.com", *body.BaseUrl)
					assert.Equal(t, "client-123", *body.ClientId)
					assert.Equal(t, "secret-xyz", *body.ClientSecret)
					assert.True(t, *body.Enabled)

					writeCreated(t, w, clientv1.PostureIntegrationOutputBody{Integration: sample})
				},
			},
			wantIn: []string{"Name: falcon-prod"},
		},
		{
			name: "update posture integration keeping secret",
			src:  updatePostureIntegrationCmd,
			flags: map[string]string{
				"id":   "1",
				"name": "falcon-updated",
			},
			routes: map[string]apiHandler{
				"GET /api/v1/posture-integration/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					writeJSON(t, w, clientv1.PostureIntegrationOutputBody{Integration: sample})
				},
				"PUT /api/v1/posture-integration/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))

					var body clientv1.UpdatePostureIntegrationJSONRequestBody

					require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					assert.Equal(t, "falcon-updated", body.Name)
					assert.Nil(t, body.ClientSecret)

					updated := sample
					updated.Name = "falcon-updated"

					writeJSON(t, w, clientv1.PostureIntegrationOutputBody{Integration: updated})
				},
			},
			wantIn: []string{"Name: falcon-updated"},
		},
		{
			name:   "delete posture integration confirmed",
			src:    deletePostureIntegrationCmd,
			flags:  map[string]string{"id": "1"},
			prompt: "y",
			routes: map[string]apiHandler{
				"GET /api/v1/posture-integration/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					writeJSON(t, w, clientv1.PostureIntegrationOutputBody{Integration: sample})
				},
				"DELETE /api/v1/posture-integration/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					w.WriteHeader(http.StatusNoContent)
				},
			},
			wantIn: []string{"Posture integration deleted"},
		},
		{
			name:   "delete posture integration rejected at prompt",
			src:    deletePostureIntegrationCmd,
			flags:  map[string]string{"id": "1"},
			prompt: "n",
			routes: map[string]apiHandler{
				"GET /api/v1/posture-integration/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					writeJSON(t, w, clientv1.PostureIntegrationOutputBody{Integration: sample})
				},
			},
			wantIn: []string{"Posture integration not deleted"},
		},
		{
			name:  "sync posture integration",
			src:   syncPostureIntegrationCmd,
			flags: map[string]string{"id": "1"},
			routes: map[string]apiHandler{
				"POST /api/v1/posture-integration/{id}/sync": func(
					t *testing.T, w http.ResponseWriter, r *http.Request,
				) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					writeJSON(t, w, clientv1.PostureIntegrationOutputBody{Integration: sample})
				},
			},
			wantIn: []string{"Name: falcon-prod", "Status: ok"},
		},
		{
			name: "check new credentials prints ok",
			src:  checkPostureIntegrationCmd,
			flags: map[string]string{
				"provider":      "falcon",
				"name":          "falcon-test",
				"client-id":     "cid",
				"client-secret": "sec",
			},
			routes: map[string]apiHandler{
				"POST /api/v1/posture-integrations/check": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.CheckPostureIntegrationJSONRequestBody

					require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					assert.Equal(t, "falcon-test", body.Name)
					assert.Equal(t, clientv1.PostureIntegrationCheckBodyProvider("falcon"), body.Provider)
					assert.Equal(t, "cid", *body.ClientId)
					assert.Equal(t, "sec", *body.ClientSecret)

					writeJSON(t, w, clientv1.PostureIntegrationCheckOutputBody{Ok: true})
				},
			},
			wantIn: []string{"ok"},
		},
		{
			name:  "check reusing stored secret with id",
			src:   checkPostureIntegrationCmd,
			flags: map[string]string{"id": "1"},
			routes: map[string]apiHandler{
				"GET /api/v1/posture-integration/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					writeJSON(t, w, clientv1.PostureIntegrationOutputBody{Integration: sample})
				},
				"POST /api/v1/posture-integrations/check": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.CheckPostureIntegrationJSONRequestBody

					require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					assert.Equal(t, "1", *body.Id)
					assert.Equal(t, "falcon-prod", body.Name)
					assert.Equal(t, clientv1.PostureIntegrationCheckBodyProvider("falcon"), body.Provider)

					writeJSON(t, w, clientv1.PostureIntegrationCheckOutputBody{Ok: true})
				},
			},
			wantIn: []string{"ok"},
		},
		{
			name: "check surfaces provider error",
			src:  checkPostureIntegrationCmd,
			flags: map[string]string{
				"provider":      "falcon",
				"name":          "falcon-test",
				"client-id":     "cid",
				"client-secret": "bad-sec",
			},
			routes: map[string]apiHandler{
				"POST /api/v1/posture-integrations/check": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusBadGateway, "invalid OAuth credentials")
				},
			},
			wantErr: "invalid OAuth credentials",
		},
	}

	runCommandCases(t, postureIntegrationFlags, cases)
}
