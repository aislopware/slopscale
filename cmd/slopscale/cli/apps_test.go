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

func appFlags(cmd *cobra.Command) {
	cmd.Flags().Uint64P("id", "i", 0, "")
	cmd.Flags().StringP("name", "n", "", "")
	cmd.Flags().StringSliceP("domain", "d", []string{}, "")
	cmd.Flags().StringSliceP("connector", "c", []string{"*"}, "")
	cmd.Flags().StringSliceP("route", "r", []string{}, "")
	cmd.Flags().String("description", "", "")
}

func sampleApp() clientv1.App {
	return clientv1.App{
		Id:          "1",
		Name:        "internal-wiki",
		Description: "Company internal wiki",
		Domains:     []string{"wiki.internal", "*.wiki.internal"},
		Connectors:  []string{"tag:wiki-connector"},
		Routes:      []string{"10.20.0.0/16"},
		Nodes: []clientv1.AppNode{
			{
				NodeId:        "10",
				Name:          "connector-node-1",
				Connector:     true,
				Online:        true,
				LearnedRoutes: 3,
				Pending:       1,
			},
			{
				NodeId:        "11",
				Name:          "connector-node-2",
				Connector:     true,
				Online:        false,
				LearnedRoutes: 0,
				Pending:       0,
			},
		},
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}
}

func TestAppsCommands(t *testing.T) {
	sample := sampleApp()

	cases := []commandCase{
		{
			name: "list renders table with counts",
			src:  listAppsCmd,
			routes: map[string]apiHandler{
				"GET /api/v1/apps": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, clientv1.ListAppsOutputBody{
						Apps: []clientv1.App{sample},
					})
				},
			},
			wantIn: []string{
				"1",
				"internal-wiki",
				"wiki.internal, *.wiki.internal",
				"tag:wiki-connector",
				"2 (1 online)",
				"10.20.0.0/16",
			},
		},
		{
			name:  "list as json",
			src:   listAppsCmd,
			flags: map[string]string{"output": "json"},
			routes: map[string]apiHandler{
				"GET /api/v1/apps": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, clientv1.ListAppsOutputBody{
						Apps: []clientv1.App{sample},
					})
				},
			},
			want: indentJSON(t, []clientv1.App{sample}),
		},
		{
			name:  "show renders app details and connector nodes",
			src:   showAppCmd,
			flags: map[string]string{"id": "1"},
			routes: map[string]apiHandler{
				"GET /api/v1/app/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					writeJSON(t, w, clientv1.AppOutputBody{App: sample})
				},
			},
			wantIn: []string{
				"ID: 1",
				"Name: internal-wiki",
				"Description: Company internal wiki",
				"Domains: wiki.internal, *.wiki.internal",
				"Connectors: tag:wiki-connector",
				"Routes: 10.20.0.0/16",
				"connector-node-1",
				"online",
				"connector-node-2",
				"offline",
			},
		},
		{
			name: "create app",
			src:  createAppCmd,
			flags: map[string]string{
				"name":        "internal-wiki",
				"domain":      "wiki.internal",
				"connector":   "tag:wiki-connector",
				"route":       "10.20.0.0/16",
				"description": "Company internal wiki",
			},
			routes: map[string]apiHandler{
				"POST /api/v1/apps": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.CreateAppJSONRequestBody

					require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					assert.Equal(t, "internal-wiki", body.Name)
					assert.Equal(t, "Company internal wiki", *body.Description)
					assert.Equal(t, []string{"wiki.internal"}, *body.Domains)
					assert.Equal(t, []string{"tag:wiki-connector"}, *body.Connectors)
					assert.Equal(t, []string{"10.20.0.0/16"}, *body.Routes)

					writeCreated(t, w, clientv1.AppOutputBody{App: sample})
				},
			},
			wantIn: []string{"Name: internal-wiki"},
		},
		{
			name: "update app",
			src:  updateAppCmd,
			flags: map[string]string{
				"id":          "1",
				"description": "Updated wiki description",
			},
			routes: map[string]apiHandler{
				"GET /api/v1/app/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					writeJSON(t, w, clientv1.AppOutputBody{App: sample})
				},
				"PUT /api/v1/app/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))

					var body clientv1.UpdateAppJSONRequestBody

					require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					assert.Equal(t, "Updated wiki description", *body.Description)
					assert.Equal(t, "internal-wiki", body.Name)

					updated := sample
					updated.Description = "Updated wiki description"

					writeJSON(t, w, clientv1.AppOutputBody{App: updated})
				},
			},
			wantIn: []string{"Description: Updated wiki description"},
		},
		{
			name:   "delete app confirmed",
			src:    deleteAppCmd,
			flags:  map[string]string{"id": "1"},
			prompt: "y",
			routes: map[string]apiHandler{
				"GET /api/v1/app/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					writeJSON(t, w, clientv1.AppOutputBody{App: sample})
				},
				"DELETE /api/v1/app/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					w.WriteHeader(http.StatusNoContent)
				},
			},
			wantIn: []string{"App deleted"},
		},
		{
			name:   "delete app rejected at prompt",
			src:    deleteAppCmd,
			flags:  map[string]string{"id": "1"},
			prompt: "n",
			routes: map[string]apiHandler{
				"GET /api/v1/app/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					writeJSON(t, w, clientv1.AppOutputBody{App: sample})
				},
			},
			wantIn: []string{"App not deleted"},
		},
	}

	runCommandCases(t, appFlags, cases)
}
