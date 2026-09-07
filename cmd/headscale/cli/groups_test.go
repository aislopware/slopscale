package cli

import (
	"net/http"
	"testing"
	"time"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func groupFlags(cmd *cobra.Command) {
	cmd.Flags().Uint64P("identifier", "i", 0, "")
	cmd.Flags().StringP("name", "n", "", "")
	cmd.Flags().StringP("description", "d", "", "")
	cmd.Flags().String("node", "", "")
	cmd.Flags().StringP("user", "u", "", "")
}

func sampleGroup() clientv1.Group {
	return clientv1.Group{
		Id:          "1",
		Name:        "engineers",
		Description: "Engineering team",
		Builtin:     "",
		NodeIds:     []string{"10", "11"},
		UserIds:     []string{"1", "2", "3"},
		CreatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC),
	}
}

func builtinAllGroup() clientv1.Group {
	return clientv1.Group{
		Id:          "2",
		Name:        "All",
		Description: "Builtin group holding all nodes",
		Builtin:     "all",
		NodeIds:     []string{},
		UserIds:     []string{},
		CreatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

func TestGroupCommands(t *testing.T) {
	group := sampleGroup()
	all := builtinAllGroup()

	cases := []commandCase{
		{
			name: "groups list renders a table row per group",
			src:  listGroupsCmd,
			routes: map[string]apiHandler{
				"GET /api/v1/group": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assertBearer(t, r)
					writeJSON(t, w, clientv1.ListGroupsOutputBody{Groups: []clientv1.Group{group, all}})
				},
			},
			wantIn: []string{"engineers", "Engineering team", "All", "yes"},
		},
		{
			name:  "create posts name and description",
			src:   createGroupCmd,
			flags: map[string]string{"name": "devs", "description": "Developers"},
			routes: map[string]apiHandler{
				"POST /api/v1/group": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assertBearer(t, r)

					var body clientv1.CreateGroupJSONRequestBody
					decodeBody(t, r, &body)
					assert.Equal(t, "devs", body.Name)
					require.NotNil(t, body.Description)
					assert.Equal(t, "Developers", *body.Description)

					created := sampleGroup()
					created.Name = body.Name
					created.Description = *body.Description
					writeJSON(t, w, clientv1.GroupOutputBody{Group: created})
				},
			},
			want: "Group created\n",
		},
		{
			name:  "rename calls UpdateGroup with name and description",
			src:   renameGroupCmd,
			flags: map[string]string{"identifier": "1", "name": "core-devs", "description": "Core developers"},
			routes: map[string]apiHandler{
				"PATCH /api/v1/group/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))

					var body clientv1.UpdateGroupJSONRequestBody
					decodeBody(t, r, &body)
					assert.Equal(t, "core-devs", body.Name)
					require.NotNil(t, body.Description)
					assert.Equal(t, "Core developers", *body.Description)

					updated := sampleGroup()
					updated.Name = body.Name
					updated.Description = *body.Description
					writeJSON(t, w, clientv1.GroupOutputBody{Group: updated})
				},
			},
			want: "Group updated\n",
		},
		{
			name:  "delete with --force skips the prompt",
			src:   deleteGroupCmd,
			flags: map[string]string{"identifier": "1", "force": "true"},
			routes: map[string]apiHandler{
				"GET /api/v1/group/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					writeJSON(t, w, clientv1.GroupOutputBody{Group: group})
				},
				"DELETE /api/v1/group/{id}": deleteOK("1"),
			},
			want: "Group deleted\n",
		},
		{
			name:   "delete confirmed at the prompt",
			src:    deleteGroupCmd,
			flags:  map[string]string{"identifier": "1"},
			prompt: "y",
			routes: map[string]apiHandler{
				"GET /api/v1/group/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					writeJSON(t, w, clientv1.GroupOutputBody{Group: group})
				},
				"DELETE /api/v1/group/{id}": deleteOK("1"),
			},
			want: "Group deleted\n",
		},
		{
			name:   "delete declined at the prompt never calls DELETE",
			src:    deleteGroupCmd,
			flags:  map[string]string{"identifier": "1"},
			prompt: "n",
			routes: map[string]apiHandler{
				"GET /api/v1/group/{id}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					writeJSON(t, w, clientv1.GroupOutputBody{Group: group})
				},
			},
			want: "Group not deleted\n",
		},
		{
			name:  "add-node posts NodeId",
			src:   addGroupNodeCmd,
			flags: map[string]string{"identifier": "1", "node": "42"},
			routes: map[string]apiHandler{
				"POST /api/v1/group/{id}/member": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))

					var body clientv1.AddGroupMemberJSONRequestBody
					decodeBody(t, r, &body)
					require.NotNil(t, body.NodeId)
					assert.Equal(t, "42", *body.NodeId)
					assert.Nil(t, body.UserId)

					g := sampleGroup()
					g.NodeIds = append(g.NodeIds, "42")
					writeJSON(t, w, clientv1.GroupOutputBody{Group: g})
				},
			},
			want: "Machine added to group\n",
		},
		{
			name:    "add-node rejects a non-numeric --node",
			src:     addGroupNodeCmd,
			flags:   map[string]string{"identifier": "1", "node": "server-one"},
			wantErr: "--node must be a node id",
		},
		{
			name:  "remove-node hits the right path",
			src:   removeGroupNodeCmd,
			flags: map[string]string{"identifier": "1", "node": "10"},
			routes: map[string]apiHandler{
				"DELETE /api/v1/group/{id}/node/{nodeId}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					assert.Equal(t, "10", r.PathValue("nodeId"))
					writeJSON(t, w, clientv1.GroupOutputBody{Group: group})
				},
			},
			want: "Machine removed from group\n",
		},
		{
			name:  "add-user posts UserId",
			src:   addGroupUserCmd,
			flags: map[string]string{"identifier": "1", "user": "5"},
			routes: map[string]apiHandler{
				"POST /api/v1/group/{id}/member": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))

					var body clientv1.AddGroupMemberJSONRequestBody
					decodeBody(t, r, &body)
					require.NotNil(t, body.UserId)
					assert.Equal(t, "5", *body.UserId)
					assert.Nil(t, body.NodeId)

					g := sampleGroup()
					g.UserIds = append(g.UserIds, "5")
					writeJSON(t, w, clientv1.GroupOutputBody{Group: g})
				},
			},
			want: "User added to group\n",
		},
		{
			name:    "add-user rejects a non-numeric --user",
			src:     addGroupUserCmd,
			flags:   map[string]string{"identifier": "1", "user": "alice"},
			wantErr: "--user must be a user id",
		},
		{
			name:  "remove-user hits the right path",
			src:   removeGroupUserCmd,
			flags: map[string]string{"identifier": "1", "user": "2"},
			routes: map[string]apiHandler{
				"DELETE /api/v1/group/{id}/user/{userId}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("id"))
					assert.Equal(t, "2", r.PathValue("userId"))
					writeJSON(t, w, clientv1.GroupOutputBody{Group: group})
				},
			},
			want: "User removed from group\n",
		},
		{
			name:    "remove-user rejects a non-numeric --user",
			src:     removeGroupUserCmd,
			flags:   map[string]string{"identifier": "1", "user": "bob"},
			wantErr: "--user must be a user id",
		},
	}

	runCommandCases(t, groupFlags, cases)
}
