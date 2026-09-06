package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// filterUsersServer serves GET /api/v1/user responses filtered like the
// real API: by the id, name, and email query parameters.
func filterUsersServer(t *testing.T, users []clientv1.User) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()

		filtered := make([]clientv1.User, 0, len(users))
		for _, user := range users {
			if name := query.Get("name"); name != "" && user.Name != name {
				continue
			}

			if id := query.Get("id"); id != "" && user.Id != id {
				continue
			}

			if email := query.Get("email"); email != "" && user.Email != email {
				continue
			}

			filtered = append(filtered, user)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		err := json.NewEncoder(w).Encode(clientv1.ListUsersOutputBody{Users: filtered})
		if err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
}

func commandWithUserFlags(t *testing.T, identifier, name string) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{}
	usernameAndIDFlag(cmd)

	if identifier != "" {
		err := cmd.Flags().Set("identifier", identifier)
		if err != nil {
			t.Fatalf("setting identifier flag: %v", err)
		}
	}

	if name != "" {
		err := cmd.Flags().Set("name", name)
		if err != nil {
			t.Fatalf("setting name flag: %v", err)
		}
	}

	return cmd
}

func TestResolveSingleUser(t *testing.T) {
	lukas := clientv1.User{Id: "6", Name: "lukas", Email: "login@lukasrunge.de"}
	hannes := clientv1.User{Id: "9", Name: "hannes@rueger.events", Email: "hannes@rueger.events"}
	hannesDup := clientv1.User{Id: "10", Name: "hannes@rueger.events", Email: "other@example.com"}

	tests := []struct {
		name       string
		users      []clientv1.User
		identifier string
		flagName   string
		wantID     string
		wantErr    bool
	}{
		{
			// Regression: renaming by name used to return the raw flag
			// id (0 when unset), so the rename request targeted user 0.
			name:     "resolves by name to the matched user's identifier",
			users:    []clientv1.User{lukas, hannes},
			flagName: "hannes@rueger.events",
			wantID:   "9",
		},
		{
			name:       "resolves by identifier",
			users:      []clientv1.User{hannes},
			identifier: "9",
			wantID:     "9",
		},
		{
			name:     "no match is an error",
			users:    []clientv1.User{lukas},
			flagName: "nobody@example.com",
			wantErr:  true,
		},
		{
			// OIDC users can share a name, see issue #3429.
			name:     "multiple matches are an error",
			users:    []clientv1.User{hannes, hannesDup},
			flagName: "hannes@rueger.events",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := filterUsersServer(t, tt.users)
			defer server.Close()

			client, err := clientv1.NewClientWithResponses(server.URL)
			if err != nil {
				t.Fatalf("creating client: %v", err)
			}

			cmd := commandWithUserFlags(t, tt.identifier, tt.flagName)

			id, user, err := resolveSingleUser(t.Context(), client, cmd)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveSingleUser() error = nil, want error")
				}

				return
			}

			if err != nil {
				t.Fatalf("resolveSingleUser() error = %v", err)
			}

			if id != tt.wantID {
				t.Errorf("resolveSingleUser() id = %q, want %q", id, tt.wantID)
			}

			if user.Id != tt.wantID {
				t.Errorf("resolveSingleUser() user.Id = %q, want %q", user.Id, tt.wantID)
			}
		})
	}
}

func TestResolveSingleUserAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeProblem(t, w, http.StatusInternalServerError, "listing failed")
	}))
	t.Cleanup(server.Close)

	client, err := clientv1.NewClientWithResponses(server.URL)
	require.NoError(t, err)

	_, _, err = resolveSingleUser(t.Context(), client, commandWithUserFlags(t, "1", ""))
	require.ErrorContains(t, err, "listing failed")
}

// userFlags mirrors the flags init() registers on the user subcommands.
func userFlags(cmd *cobra.Command) {
	usernameAndIDFlag(cmd)
	cmd.Flags().StringP("email", "e", "", "")
	cmd.Flags().StringP("display-name", "d", "", "")
	cmd.Flags().StringP("picture-url", "p", "", "")
	cmd.Flags().StringP("new-name", "r", "", "")
	cmd.Flags().String("role", "", "")
}

func TestUserCommands(t *testing.T) {
	alice := clientv1.User{
		Id:          "1",
		Name:        "alice",
		DisplayName: "Alice",
		Email:       "alice@example.com",
		Role:        "owner",
		CreatedAt:   time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
	}
	bob := clientv1.User{Id: "2", Name: "bob", Role: "member", CreatedAt: time.Date(2026, 3, 2, 12, 0, 0, 0, time.UTC)}

	listFiltered := func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		t.Helper()
		assertBearer(t, r)

		query := r.URL.Query()

		users := make([]clientv1.User, 0, 2)

		for _, user := range []clientv1.User{alice, bob} {
			if id := query.Get("id"); id != "" && user.Id != id {
				continue
			}

			if name := query.Get("name"); name != "" && user.Name != name {
				continue
			}

			if email := query.Get("email"); email != "" && user.Email != email {
				continue
			}

			users = append(users, user)
		}

		writeJSON(t, w, clientv1.ListUsersOutputBody{Users: users})
	}

	cases := []commandCase{
		{
			name: "create sends the optional fields",
			src:  createUserCmd,
			args: []string{"alice"},
			flags: map[string]string{
				"display-name": "Alice",
				"email":        "alice@example.com",
				"picture-url":  "https://example.com/a.png",
			},
			routes: map[string]apiHandler{
				"POST /api/v1/user": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.CreateUserRequestBody

					decodeBody(t, r, &body)
					assert.Equal(t, "alice", ptrStr(body.Name))
					assert.Equal(t, "Alice", ptrStr(body.DisplayName))
					assert.Equal(t, "alice@example.com", ptrStr(body.Email))
					assert.Equal(t, "https://example.com/a.png", ptrStr(body.PictureUrl))

					writeJSON(t, w, clientv1.UserOutputBody{User: alice})
				},
			},
			want: "User created\n",
		},
		{
			name:  "create omits unset fields and prints json",
			src:   createUserCmd,
			args:  []string{"bob"},
			flags: map[string]string{"output": "json"},
			routes: map[string]apiHandler{
				"POST /api/v1/user": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.CreateUserRequestBody

					decodeBody(t, r, &body)
					assert.Equal(t, "bob", ptrStr(body.Name))
					assert.Nil(t, body.DisplayName)
					assert.Nil(t, body.Email)
					assert.Nil(t, body.PictureUrl)

					writeJSON(t, w, clientv1.UserOutputBody{User: bob})
				},
			},
			want: indentJSON(t, bob),
		},
		{
			name:    "create rejects an invalid picture url before calling the api",
			src:     createUserCmd,
			args:    []string{"alice"},
			flags:   map[string]string{"picture-url": "://not-a-url"},
			wantErr: "invalid picture URL",
		},
		{
			name: "create surfaces the api error",
			src:  createUserCmd,
			args: []string{"alice"},
			routes: map[string]apiHandler{
				"POST /api/v1/user": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusConflict, "user already exists")
				},
			},
			wantErr: "user already exists",
		},
		{
			name:   "list renders a table",
			src:    listUsersCmd,
			routes: map[string]apiHandler{"GET /api/v1/user": listFiltered},
			wantIn: []string{
				"Username", "Role", "alice", "Alice", "alice@example.com", "owner", "bob", "member",
				"2026-03-01 12:00:00",
			},
		},
		{
			name:  "set-role resolves by name and posts the role",
			src:   setUserRoleCmd,
			flags: map[string]string{"name": "bob", "role": "auditor"},
			routes: map[string]apiHandler{
				"GET /api/v1/user": listFiltered,
				"POST /api/v1/user/{id}/role": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "2", r.PathValue("id"))

					var body clientv1.SetUserRoleRequestBody

					decodeBody(t, r, &body)
					assert.Equal(t, "auditor", body.Role)

					promoted := bob
					promoted.Role = "auditor"
					writeJSON(t, w, clientv1.UserOutputBody{User: promoted})
				},
			},
			want: "User role set to auditor\n",
		},
		{
			name:  "set-role surfaces the api error",
			src:   setUserRoleCmd,
			flags: map[string]string{"identifier": "1", "role": "member"},
			routes: map[string]apiHandler{
				"GET /api/v1/user": listFiltered,
				"POST /api/v1/user/{id}/role": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusForbidden, "the owner's role only changes by transferring ownership")
				},
			},
			wantErr: "transferring ownership",
		},
		{
			name:   "list filters by identifier",
			src:    listUsersCmd,
			flags:  map[string]string{"identifier": "2", "output": "json"},
			routes: map[string]apiHandler{"GET /api/v1/user": listFiltered},
			want:   indentJSON(t, []clientv1.User{bob}),
		},
		{
			name:   "list filters by name",
			src:    listUsersCmd,
			flags:  map[string]string{"name": "alice", "output": "json"},
			routes: map[string]apiHandler{"GET /api/v1/user": listFiltered},
			want:   indentJSON(t, []clientv1.User{alice}),
		},
		{
			name:   "list filters by email",
			src:    listUsersCmd,
			flags:  map[string]string{"email": "alice@example.com", "output": "json"},
			routes: map[string]apiHandler{"GET /api/v1/user": listFiltered},
			want:   indentJSON(t, []clientv1.User{alice}),
		},
		{
			name: "list surfaces the api error",
			src:  listUsersCmd,
			routes: map[string]apiHandler{
				"GET /api/v1/user": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusInternalServerError, "listing failed")
				},
			},
			wantErr: "listing failed",
		},
		{
			name:  "destroy resolves by name and deletes with --force",
			src:   destroyUserCmd,
			flags: map[string]string{"name": "alice", "force": "true"},
			routes: map[string]apiHandler{
				"GET /api/v1/user":         listFiltered,
				"DELETE /api/v1/user/{id}": deleteOK("1"),
			},
			want: "User destroyed\n",
		},
		{
			name:   "destroy declined at the prompt never calls DELETE",
			src:    destroyUserCmd,
			flags:  map[string]string{"identifier": "2"},
			prompt: "n",
			routes: map[string]apiHandler{"GET /api/v1/user": listFiltered},
			want:   "User not destroyed\n",
		},
		{
			name:    "destroy requires a name or identifier",
			src:     destroyUserCmd,
			wantErr: errFlagRequired.Error(),
		},
		{
			name:  "destroy surfaces the api error",
			src:   destroyUserCmd,
			flags: map[string]string{"identifier": "1", "force": "true"},
			routes: map[string]apiHandler{
				"GET /api/v1/user": listFiltered,
				"DELETE /api/v1/user/{id}": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusInternalServerError, "user has nodes")
				},
			},
			wantErr: "user has nodes",
		},
		{
			name:  "rename resolves the user and puts the new name in the path",
			src:   renameUserCmd,
			flags: map[string]string{"name": "bob", "new-name": "robert"},
			routes: map[string]apiHandler{
				"GET /api/v1/user": listFiltered,
				"POST /api/v1/user/{id}/rename/{name}": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "2", r.PathValue("id"))
					assert.Equal(t, "robert", r.PathValue("name"))

					renamed := bob
					renamed.Name = "robert"

					writeJSON(t, w, clientv1.UserOutputBody{User: renamed})
				},
			},
			want: "User renamed\n",
		},
		{
			name:  "rename surfaces the api error",
			src:   renameUserCmd,
			flags: map[string]string{"identifier": "2", "new-name": "alice"},
			routes: map[string]apiHandler{
				"GET /api/v1/user": listFiltered,
				"POST /api/v1/user/{id}/rename/{name}": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusConflict, "name already taken")
				},
			},
			wantErr: "name already taken",
		},
	}

	runCommandCases(t, userFlags, cases)
}
