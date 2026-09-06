package cli

import (
	"net/http"
	"testing"
	"time"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testMachineKey = "mkey:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testNodeKey    = "nodekey:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
)

// nodeFlags mirrors the flags init() registers on the node subcommands.
func nodeFlags(cmd *cobra.Command) {
	cmd.Flags().Uint64P("identifier", "i", 0, "")
	cmd.Flags().StringP("user", "u", "", "")
	cmd.Flags().StringP("key", "k", "", "")
	cmd.Flags().StringP("expiry", "e", "", "")
	cmd.Flags().BoolP("disable", "d", false, "")
	cmd.Flags().StringSliceP("tags", "t", []string{}, "")
	cmd.Flags().StringSliceP("routes", "r", []string{}, "")
}

// laptopNode is a user-owned, online node with no routes. Timestamps are
// fixed so table output is deterministic.
func laptopNode() clientv1.Node {
	lastSeen := time.Date(2026, 3, 1, 11, 30, 0, 0, time.UTC)
	expiry := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)

	return clientv1.Node{
		Id:             "7",
		Name:           "laptop",
		GivenName:      "laptop",
		MachineKey:     testMachineKey,
		NodeKey:        testNodeKey,
		User:           clientv1.User{Id: "1", Name: "alice", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		IpAddresses:    []string{"100.64.0.7", "fd7a:115c:a1e0::7"},
		LastSeen:       &lastSeen,
		Expiry:         &expiry,
		Online:         true,
		CreatedAt:      time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		RegisterMethod: "REGISTER_METHOD_AUTH_KEY",
	}
}

// routerNode is a tagged, offline, expired, ephemeral node advertising routes.
func routerNode() clientv1.Node {
	expiry := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

	return clientv1.Node{
		Id:              "8",
		Name:            "router",
		GivenName:       "router",
		MachineKey:      testMachineKey,
		NodeKey:         testNodeKey,
		Tags:            []string{"tag:router"},
		IpAddresses:     []string{"100.64.0.8"},
		Expiry:          &expiry,
		PreAuthKey:      clientv1.NodePreAuthKey{Id: "3", Ephemeral: true},
		ApprovedRoutes:  []string{"10.0.0.0/8"},
		AvailableRoutes: []string{"10.0.0.0/8", "192.168.1.0/24"},
		SubnetRoutes:    []string{"10.0.0.0/8"},
		CreatedAt:       time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		RegisterMethod:  "REGISTER_METHOD_AUTH_KEY",
	}
}

func nodeResponse(node clientv1.Node) apiHandler {
	return func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
		t.Helper()
		writeJSON(t, w, clientv1.NodeOutputBody{Node: node})
	}
}

func TestNodeCommands(t *testing.T) {
	laptop := laptopNode()
	router := routerNode()
	future := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)

	listBoth := func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		t.Helper()
		assertBearer(t, r)
		assert.Empty(t, r.URL.Query().Get("user"))
		writeJSON(t, w, clientv1.ListNodesOutputBody{Nodes: []clientv1.Node{laptop, router}})
	}

	cases := []commandCase{
		{
			name:   "list renders a table",
			src:    listNodesCmd,
			routes: map[string]apiHandler{"GET /api/v1/node": listBoth},
			wantIn: []string{"Hostname", "laptop", "router", "100.64.0.7", "alice", "tag:router"},
		},
		{
			name:  "list filters by user and prints json",
			src:   listNodesCmd,
			flags: map[string]string{"user": "alice", "output": "json"},
			routes: map[string]apiHandler{
				"GET /api/v1/node": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "alice", r.URL.Query().Get("user"))
					writeJSON(t, w, clientv1.ListNodesOutputBody{Nodes: []clientv1.Node{laptop}})
				},
			},
			want: indentJSON(t, []clientv1.Node{laptop}),
		},
		{
			name:   "list prints yaml",
			src:    listNodesCmd,
			flags:  map[string]string{"output": "yaml"},
			routes: map[string]apiHandler{"GET /api/v1/node": listBoth},
			wantIn: []string{"name: laptop", "name: router"},
		},
		{
			name: "list surfaces the api error detail",
			src:  listNodesCmd,
			routes: map[string]apiHandler{
				"GET /api/v1/node": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusInternalServerError, "database is on fire")
				},
			},
			wantErr: "database is on fire",
		},
		{
			name:      "list-routes keeps only nodes with routes",
			src:       listNodeRoutesCmd,
			routes:    map[string]apiHandler{"GET /api/v1/node": listBoth},
			wantIn:    []string{"Serving (Primary)", "router", "10.0.0.0/8", "192.168.1.0/24"},
			wantNotIn: []string{"laptop"},
		},
		{
			name:   "list-routes selects one node by identifier",
			src:    listNodeRoutesCmd,
			flags:  map[string]string{"identifier": "8", "output": "json"},
			routes: map[string]apiHandler{"GET /api/v1/node": listBoth},
			want:   indentJSON(t, []clientv1.Node{router}),
		},
		{
			name:   "list-routes for a node without routes is empty",
			src:    listNodeRoutesCmd,
			flags:  map[string]string{"identifier": "7", "output": "json"},
			routes: map[string]apiHandler{"GET /api/v1/node": listBoth},
			want:   "[]\n",
		},
		{
			name:  "register passes user and key as query parameters",
			src:   registerNodeCmd,
			flags: map[string]string{"user": "alice", "key": "mkey:abc"},
			routes: map[string]apiHandler{
				"POST /api/v1/node/register": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "alice", r.URL.Query().Get("user"))
					assert.Equal(t, "mkey:abc", r.URL.Query().Get("key"))
					writeJSON(t, w, clientv1.NodeOutputBody{Node: laptop})
				},
			},
			want: "Node laptop registered\n",
		},
		{
			name:  "register surfaces the api error",
			src:   registerNodeCmd,
			flags: map[string]string{"user": "alice", "key": "mkey:abc"},
			routes: map[string]apiHandler{
				"POST /api/v1/node/register": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusNotFound, "registration id not found")
				},
			},
			wantErr: "registration id not found",
		},
		{
			name:  "expire without a time expires now",
			src:   expireNodeCmd,
			flags: map[string]string{"identifier": "7"},
			routes: map[string]apiHandler{
				"POST /api/v1/node/{id}/expire": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "7", r.PathValue("id"))

					var body clientv1.ExpireNodeRequestBody

					decodeBody(t, r, &body)
					assert.Nil(t, body.DisableExpiry)

					if assert.NotNil(t, body.Expiry) {
						assert.WithinDuration(t, time.Now(), *body.Expiry, time.Minute)
					}

					writeJSON(t, w, clientv1.NodeOutputBody{Node: laptop})
				},
			},
			want: "Node expired\n",
		},
		{
			name:  "expire with a future time updates the expiration",
			src:   expireNodeCmd,
			flags: map[string]string{"identifier": "7", "expiry": future.Format(time.RFC3339)},
			routes: map[string]apiHandler{
				"POST /api/v1/node/{id}/expire": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.ExpireNodeRequestBody

					decodeBody(t, r, &body)

					if assert.NotNil(t, body.Expiry) {
						assert.True(t, future.Equal(*body.Expiry), "expiry %s", body.Expiry)
					}

					writeJSON(t, w, clientv1.NodeOutputBody{Node: laptop})
				},
			},
			want: "Node expiration updated\n",
		},
		{
			name:  "expire with --disable turns expiry off",
			src:   expireNodeCmd,
			flags: map[string]string{"identifier": "7", "disable": "true"},
			routes: map[string]apiHandler{
				"POST /api/v1/node/{id}/expire": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.ExpireNodeRequestBody

					decodeBody(t, r, &body)
					assert.Nil(t, body.Expiry)

					if assert.NotNil(t, body.DisableExpiry) {
						assert.True(t, *body.DisableExpiry)
					}

					writeJSON(t, w, clientv1.NodeOutputBody{Node: laptop})
				},
			},
			want: "Node expiry disabled\n",
		},
		{
			name:    "expire rejects an unparsable time before calling the api",
			src:     expireNodeCmd,
			flags:   map[string]string{"identifier": "7", "expiry": "tomorrow"},
			wantErr: "parsing expiry time",
		},
		{
			name:   "expire prints the node as json",
			src:    expireNodeCmd,
			flags:  map[string]string{"identifier": "7", "output": "json"},
			routes: map[string]apiHandler{"POST /api/v1/node/{id}/expire": nodeResponse(laptop)},
			want:   indentJSON(t, laptop),
		},
		{
			name:  "expire surfaces the api error",
			src:   expireNodeCmd,
			flags: map[string]string{"identifier": "7", "disable": "true"},
			routes: map[string]apiHandler{
				"POST /api/v1/node/{id}/expire": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusNotFound, "node not found")
				},
			},
			wantErr: "node not found",
		},
		{
			name:  "rename puts the new name in the path",
			src:   renameNodeCmd,
			flags: map[string]string{"identifier": "7"},
			args:  []string{"laptop-2"},
			routes: map[string]apiHandler{
				"POST /api/v1/node/{id}/rename/{name}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "7", r.PathValue("id"))
					assert.Equal(t, "laptop-2", r.PathValue("name"))
					writeJSON(t, w, clientv1.NodeOutputBody{Node: laptop})
				},
			},
			want: "Node renamed\n",
		},
		{
			name:  "rename surfaces the api error",
			src:   renameNodeCmd,
			flags: map[string]string{"identifier": "7"},
			args:  []string{"x"},
			routes: map[string]apiHandler{
				"POST /api/v1/node/{id}/rename/{name}": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusBadRequest, "name is too short")
				},
			},
			wantErr: "name is too short",
		},
		{
			name:  "delete with --force skips the prompt",
			src:   deleteNodeCmd,
			flags: map[string]string{"identifier": "7", "force": "true"},
			routes: map[string]apiHandler{
				"GET /api/v1/node/{id}":    nodeResponse(laptop),
				"DELETE /api/v1/node/{id}": deleteOK("7"),
			},
			want: "Node deleted\n",
		},
		{
			name:   "delete confirmed at the prompt",
			src:    deleteNodeCmd,
			flags:  map[string]string{"identifier": "7"},
			prompt: "y",
			routes: map[string]apiHandler{
				"GET /api/v1/node/{id}":    nodeResponse(laptop),
				"DELETE /api/v1/node/{id}": deleteOK("7"),
			},
			want: "Node deleted\n",
		},
		{
			name:   "delete declined at the prompt never calls DELETE",
			src:    deleteNodeCmd,
			flags:  map[string]string{"identifier": "7"},
			prompt: "n",
			routes: map[string]apiHandler{"GET /api/v1/node/{id}": nodeResponse(laptop)},
			want:   "Node not deleted\n",
		},
		{
			name:  "delete prints the result as json",
			src:   deleteNodeCmd,
			flags: map[string]string{"identifier": "7", "force": "true", "output": "json"},
			routes: map[string]apiHandler{
				"GET /api/v1/node/{id}":    nodeResponse(laptop),
				"DELETE /api/v1/node/{id}": deleteOK("7"),
			},
			want: indentJSON(t, map[string]string{colResult: "Node deleted"}),
		},
		{
			name:  "delete of an unknown node fails on the lookup",
			src:   deleteNodeCmd,
			flags: map[string]string{"identifier": "9", "force": "true"},
			routes: map[string]apiHandler{
				"GET /api/v1/node/{id}": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusNotFound, "node not found")
				},
			},
			wantErr: "node not found",
		},
		{
			name:  "delete surfaces the api error from DELETE",
			src:   deleteNodeCmd,
			flags: map[string]string{"identifier": "7", "force": "true"},
			routes: map[string]apiHandler{
				"GET /api/v1/node/{id}": nodeResponse(laptop),
				"DELETE /api/v1/node/{id}": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusInternalServerError, "delete failed")
				},
			},
			wantErr: "delete failed",
		},
		{
			name:  "tag sends the tag list",
			src:   tagCmd,
			flags: map[string]string{"identifier": "7", "tags": "tag:a,tag:b"},
			routes: map[string]apiHandler{
				"POST /api/v1/node/{id}/tags": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "7", r.PathValue("id"))

					var body clientv1.SetTagsRequestBody

					decodeBody(t, r, &body)

					if assert.NotNil(t, body.Tags) {
						assert.Equal(t, []string{"tag:a", "tag:b"}, *body.Tags)
					}

					writeJSON(t, w, clientv1.NodeOutputBody{Node: laptop})
				},
			},
			want: "Node updated\n",
		},
		{
			name:  "tag surfaces the api error",
			src:   tagCmd,
			flags: map[string]string{"identifier": "7", "tags": "notatag"},
			routes: map[string]apiHandler{
				"POST /api/v1/node/{id}/tags": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusBadRequest, "tag must start with tag:")
				},
			},
			wantErr: "tag must start with tag:",
		},
		{
			name:  "approve-routes sends the route list",
			src:   approveRoutesCmd,
			flags: map[string]string{"identifier": "8", "routes": "10.0.0.0/8,192.168.1.0/24"},
			routes: map[string]apiHandler{
				"POST /api/v1/node/{id}/approve_routes": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "8", r.PathValue("id"))

					var body clientv1.SetApprovedRoutesRequestBody

					decodeBody(t, r, &body)

					if assert.NotNil(t, body.Routes) {
						assert.Equal(t, []string{"10.0.0.0/8", "192.168.1.0/24"}, *body.Routes)
					}

					writeJSON(t, w, clientv1.NodeOutputBody{Node: router})
				},
			},
			want: "Node updated\n",
		},
		{
			name:  "approve-routes with an empty list clears approvals",
			src:   approveRoutesCmd,
			flags: map[string]string{"identifier": "8", "routes": ""},
			routes: map[string]apiHandler{
				"POST /api/v1/node/{id}/approve_routes": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.SetApprovedRoutesRequestBody

					decodeBody(t, r, &body)

					if assert.NotNil(t, body.Routes) {
						assert.Empty(t, *body.Routes)
					}

					writeJSON(t, w, clientv1.NodeOutputBody{Node: router})
				},
			},
			want: "Node updated\n",
		},
		{
			name:  "approve-routes surfaces the api error",
			src:   approveRoutesCmd,
			flags: map[string]string{"identifier": "8", "routes": "10.0.0.0/8"},
			routes: map[string]apiHandler{
				"POST /api/v1/node/{id}/approve_routes": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusBadRequest, "route not advertised")
				},
			},
			wantErr: "route not advertised",
		},
		{
			name:  "backfillips with --force confirms to the api",
			src:   backfillNodeIPsCmd,
			flags: map[string]string{"force": "true"},
			routes: map[string]apiHandler{
				"POST /api/v1/node/backfillips": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "true", r.URL.Query().Get("confirmed"))
					writeJSON(t, w, clientv1.BackfillNodeIPsOutputBody{Changes: []string{"node 7: added fd7a:115c:a1e0::7"}})
				},
			},
			want: "Node IPs backfilled successfully\n",
		},
		{
			name:  "backfillips prints the changes as json",
			src:   backfillNodeIPsCmd,
			flags: map[string]string{"force": "true", "output": "json"},
			routes: map[string]apiHandler{
				"POST /api/v1/node/backfillips": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, clientv1.BackfillNodeIPsOutputBody{Changes: []string{"node 7: added fd7a:115c:a1e0::7"}})
				},
			},
			want: indentJSON(t, clientv1.BackfillNodeIPsOutputBody{Changes: []string{"node 7: added fd7a:115c:a1e0::7"}}),
		},
		{
			name:   "backfillips declined at the prompt does nothing",
			src:    backfillNodeIPsCmd,
			prompt: "n",
			want:   "",
		},
		{
			name:  "backfillips surfaces the api error",
			src:   backfillNodeIPsCmd,
			flags: map[string]string{"force": "true"},
			routes: map[string]apiHandler{
				"POST /api/v1/node/backfillips": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusInternalServerError, "no prefixes configured")
				},
			},
			wantErr: "no prefixes configured",
		},
	}

	runCommandCases(t, nodeFlags, cases)
}

func TestNodesToPtables(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mutate   func(n *clientv1.Node)
		wantRow  []string
		wantErr  string
		baseNode func() clientv1.Node
	}{
		{
			name:     "user-owned online node",
			baseNode: laptopNode,
			wantRow: []string{
				"7", "laptop", "laptop", "[ASNFZ]", "[/ty6m]", "alice", "",
				"100.64.0.7\nfd7a:115c:a1e0::7", "false",
				"2026-03-01 11:30:00", "2100-01-01 00:00:00",
				pterm.LightGreen("online"), pterm.LightGreen("no"),
			},
		},
		{
			name:     "tagged ephemeral expired offline node",
			baseNode: routerNode,
			wantRow: []string{
				"8", "router", "router", "[ASNFZ]", "[/ty6m]", "", "tag:router",
				"100.64.0.8", "true",
				"", "2000-01-01 00:00:00",
				pterm.LightRed("offline"), pterm.LightRed("yes"),
			},
		},
		{
			name:     "missing expiry renders N/A and never counts as expired",
			baseNode: laptopNode,
			mutate:   func(n *clientv1.Node) { n.Expiry = nil },
			wantRow: []string{
				"7", "laptop", "laptop", "[ASNFZ]", "[/ty6m]", "alice", "",
				"100.64.0.7\nfd7a:115c:a1e0::7", "false",
				"2026-03-01 11:30:00", "N/A",
				pterm.LightGreen("online"), pterm.LightGreen("no"),
			},
		},
		{
			name:     "unparsable machine key falls back to an empty short string",
			baseNode: laptopNode,
			mutate:   func(n *clientv1.Node) { n.MachineKey = "garbage" },
			wantRow: []string{
				"7", "laptop", "laptop", "", "[/ty6m]", "alice", "",
				"100.64.0.7\nfd7a:115c:a1e0::7", "false",
				"2026-03-01 11:30:00", "2100-01-01 00:00:00",
				pterm.LightGreen("online"), pterm.LightGreen("no"),
			},
		},
		{
			name:     "unparsable ip addresses are skipped",
			baseNode: laptopNode,
			mutate:   func(n *clientv1.Node) { n.IpAddresses = []string{"not-an-ip", "100.64.0.7"} },
			wantRow: []string{
				"7", "laptop", "laptop", "[ASNFZ]", "[/ty6m]", "alice", "",
				"100.64.0.7", "false",
				"2026-03-01 11:30:00", "2100-01-01 00:00:00",
				pterm.LightGreen("online"), pterm.LightGreen("no"),
			},
		},
		{
			name:     "unparsable node key is an error",
			baseNode: laptopNode,
			mutate:   func(n *clientv1.Node) { n.NodeKey = "garbage" },
			wantErr:  "nodekey",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			node := tt.baseNode()
			if tt.mutate != nil {
				tt.mutate(&node)
			}

			table, err := nodesToPtables([]clientv1.Node{node})
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			require.Len(t, table, 2)
			assert.Equal(t, "ID", table[0][0])
			assert.Equal(t, "Expired", table[0][len(table[0])-1])
			assert.Equal(t, tt.wantRow, table[1])
		})
	}
}

func TestNodesToPtablesEmpty(t *testing.T) {
	t.Parallel()

	table, err := nodesToPtables(nil)
	require.NoError(t, err)
	require.Len(t, table, 1, "header only")
	assert.Len(t, table[0], 13)
}

func TestNodeRoutesToPtables(t *testing.T) {
	t.Parallel()

	table := nodeRoutesToPtables([]clientv1.Node{routerNode(), laptopNode()})

	require.Len(t, table, 3)
	assert.Equal(t, []string{"ID", "Hostname", "Approved", "Available", "Serving (Primary)"}, table[0])
	assert.Equal(t, []string{"8", "router", "10.0.0.0/8", "10.0.0.0/8\n192.168.1.0/24", "10.0.0.0/8"}, table[1])
	assert.Equal(t, []string{"7", "laptop", "", "", ""}, table[2])
}
