package cli

import (
	"net/http"
	"testing"
	"time"

	clientv1 "github.com/aislopware/slopscale/gen/client/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func networkFlags(cmd *cobra.Command) {
	cmd.Flags().Uint64P("identifier", "i", 0, "")
	cmd.Flags().StringP("name", "n", "", "")
	cmd.Flags().StringP("description", "d", "", "")
	cmd.Flags().StringSliceP("prefix", "p", []string{}, "")
	cmd.Flags().StringSliceP("router", "r", []string{}, "")
	cmd.Flags().StringSliceP("group", "g", []string{}, "")
	cmd.Flags().Bool("disabled", false, "")
}

func sampleNetwork() clientv1.Network {
	return clientv1.Network{
		Id:            "1",
		Name:          "corp-net",
		Description:   "Corporate network",
		Enabled:       true,
		ExitNode:      true,
		Prefixes:      []string{"10.0.0.0/24", "10.0.1.0/24"},
		GroupIds:      []string{"1", "2"},
		RouterNodeIds: []string{"10", "20"},
		Routers: []clientv1.NetworkRouter{
			{
				NodeId:          "10",
				Name:            "gw-1",
				Online:          true,
				PrimaryPrefixes: []string{"10.0.0.0/24"},
				MissingPrefixes: []string{},
			},
			{
				NodeId:          "20",
				Name:            "gw-2",
				Online:          false,
				PrimaryPrefixes: []string{"10.0.1.0/24"},
				MissingPrefixes: []string{"192.168.1.0/24"},
			},
		},
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}
}

func TestNetworkCommands(t *testing.T) {
	net := sampleNetwork()

	noRoutersNet := net
	noRoutersNet.Routers = []clientv1.NetworkRouter{}

	cases := []commandCase{
		{
			name: "list renders row with name prefix offline router and exit node",
			src:  listNetworksCmd,
			routes: map[string]apiHandler{
				"GET /api/v1/network": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, clientv1.ListNetworksOutputBody{Networks: []clientv1.Network{net}})
				},
			},
			wantIn: []string{
				"corp-net",
				"10.0.0.0/24",
				"gw-2 (offline)",
				"yes",
			},
		},
		{
			name:  "list as json",
			src:   listNetworksCmd,
			flags: map[string]string{"output": "json"},
			routes: map[string]apiHandler{
				"GET /api/v1/network": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, clientv1.ListNetworksOutputBody{Networks: []clientv1.Network{net}})
				},
			},
			want: indentJSON(t, []clientv1.Network{net}),
		},
		{
			name:  "show renders routers table including missing prefix",
			src:   showNetworkCmd,
			flags: map[string]string{"identifier": "1"},
			routes: map[string]apiHandler{
				"GET /api/v1/network/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					writeJSON(t, w, clientv1.NetworkOutputBody{Network: net})
				},
			},
			wantIn: []string{
				"Name: corp-net",
				"Description: Corporate network",
				"Enabled: on",
				"10.0.0.0/24",
				"gw-1",
				"gw-2",
				"192.168.1.0/24",
			},
		},
		{
			name:  "show with no routers says no routers",
			src:   showNetworkCmd,
			flags: map[string]string{"identifier": "1"},
			routes: map[string]apiHandler{
				"GET /api/v1/network/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					writeJSON(t, w, clientv1.NetworkOutputBody{Network: noRoutersNet})
				},
			},
			wantIn: []string{
				"no routers",
			},
		},
		{
			name: "create builds the right body",
			src:  createNetworkCmd,
			flags: map[string]string{
				"name":        "new-net",
				"description": "New corporate network",
				"prefix":      "10.0.0.0/24,10.0.1.0/24",
				"router":      "10,20",
				"group":       "1,2",
				"disabled":    "true",
			},
			routes: map[string]apiHandler{
				"POST /api/v1/network": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.NetworkRequestBody

					decodeBody(t, r, &body)
					assert.Equal(t, "new-net", body.Name)

					if assert.NotNil(t, body.Description) {
						assert.Equal(t, "New corporate network", *body.Description)
					}

					if assert.NotNil(t, body.Enabled) {
						assert.False(t, *body.Enabled)
					}

					if assert.NotNil(t, body.Prefixes) {
						assert.Equal(t, []string{"10.0.0.0/24", "10.0.1.0/24"}, *body.Prefixes)
					}

					if assert.NotNil(t, body.RouterNodeIds) {
						assert.Equal(t, []string{"10", "20"}, *body.RouterNodeIds)
					}

					if assert.NotNil(t, body.GroupIds) {
						assert.Equal(t, []string{"1", "2"}, *body.GroupIds)
					}

					created := net
					created.Name = body.Name
					created.Enabled = *body.Enabled

					writeJSON(t, w, clientv1.NetworkOutputBody{Network: created})
				},
			},
			wantIn: []string{"Name: new-net"},
		},
		{
			name: "update merges untouched fields and replaces given one",
			src:  updateNetworkCmd,
			flags: map[string]string{
				"identifier": "1",
				"name":       "updated-net",
			},
			routes: map[string]apiHandler{
				"GET /api/v1/network/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					writeJSON(t, w, clientv1.NetworkOutputBody{Network: net})
				},
				"PUT /api/v1/network/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))

					var body clientv1.NetworkRequestBody

					decodeBody(t, r, &body)
					assert.Equal(t, "updated-net", body.Name)

					if assert.NotNil(t, body.Description) {
						assert.Equal(t, net.Description, *body.Description)
					}

					if assert.NotNil(t, body.Enabled) {
						assert.Equal(t, net.Enabled, *body.Enabled)
					}

					if assert.NotNil(t, body.Prefixes) {
						assert.Equal(t, net.Prefixes, *body.Prefixes)
					}

					if assert.NotNil(t, body.GroupIds) {
						assert.Equal(t, net.GroupIds, *body.GroupIds)
					}

					if assert.NotNil(t, body.RouterNodeIds) {
						assert.Equal(t, net.RouterNodeIds, *body.RouterNodeIds)
					}

					updated := net
					updated.Name = body.Name

					writeJSON(t, w, clientv1.NetworkOutputBody{Network: updated})
				},
			},
			wantIn: []string{"Name: updated-net"},
		},
		{
			name:  "enable sends the right PATCH body",
			src:   enableNetworkCmd,
			flags: map[string]string{"identifier": "1"},
			routes: map[string]apiHandler{
				"PATCH /api/v1/network/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))

					var body clientv1.NetworkEnabledInputBody

					decodeBody(t, r, &body)
					assert.True(t, body.Enabled)

					enabledNet := net
					enabledNet.Enabled = true

					writeJSON(t, w, clientv1.NetworkOutputBody{Network: enabledNet})
				},
			},
			want: "Network corp-net enabled\n",
		},
		{
			name:  "disable sends the right PATCH body",
			src:   disableNetworkCmd,
			flags: map[string]string{"identifier": "1"},
			routes: map[string]apiHandler{
				"PATCH /api/v1/network/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))

					var body clientv1.NetworkEnabledInputBody

					decodeBody(t, r, &body)
					assert.False(t, body.Enabled)

					disabledNet := net
					disabledNet.Enabled = false

					writeJSON(t, w, clientv1.NetworkOutputBody{Network: disabledNet})
				},
			},
			want: "Network corp-net disabled\n",
		},
		{
			name:  "delete with --force skips the prompt",
			src:   deleteNetworkCmd,
			flags: map[string]string{"identifier": "1", "force": "true"},
			routes: map[string]apiHandler{
				"GET /api/v1/network/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					writeJSON(t, w, clientv1.NetworkOutputBody{Network: net})
				},
				"DELETE /api/v1/network/{id}": deleteOK("1"),
			},
			want: "Network deleted\n",
		},
		{
			name:   "delete confirmed at the prompt",
			src:    deleteNetworkCmd,
			flags:  map[string]string{"identifier": "1"},
			prompt: "y",
			routes: map[string]apiHandler{
				"GET /api/v1/network/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					writeJSON(t, w, clientv1.NetworkOutputBody{Network: net})
				},
				"DELETE /api/v1/network/{id}": deleteOK("1"),
			},
			want: "Network deleted\n",
		},
		{
			name:  "show surfaces api error",
			src:   showNetworkCmd,
			flags: map[string]string{"identifier": "99"},
			routes: map[string]apiHandler{
				"GET /api/v1/network/{id}": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusNotFound, "network not found")
				},
			},
			wantErr: "network not found",
		},
		{
			name: "create surfaces api error",
			src:  createNetworkCmd,
			flags: map[string]string{
				"name":   "bad-net",
				"group":  "1",
				"prefix": "invalid-cidr",
			},
			routes: map[string]apiHandler{
				"POST /api/v1/network": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusBadRequest, "invalid prefix")
				},
			},
			wantErr: "invalid prefix",
		},
		{
			name:  "delete surfaces api error",
			src:   deleteNetworkCmd,
			flags: map[string]string{"identifier": "1", "force": "true"},
			routes: map[string]apiHandler{
				"GET /api/v1/network/{id}": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, clientv1.NetworkOutputBody{Network: net})
				},
				"DELETE /api/v1/network/{id}": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusInternalServerError, "delete failed")
				},
			},
			wantErr: "delete failed",
		},
	}

	runCommandCases(t, networkFlags, cases)
}
