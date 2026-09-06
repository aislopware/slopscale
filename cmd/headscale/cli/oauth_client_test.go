package cli

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	clientv2 "github.com/juanfont/headscale/gen/client/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// oauthFlags mirrors the flags init() registers on the oauth-clients
// subcommands.
func oauthFlags(cmd *cobra.Command) {
	cmd.Flags().StringArrayP("scope", "s", nil, "")
	cmd.Flags().StringArrayP("tag", "t", nil, "")
	cmd.Flags().StringP("description", "d", "", "")
	cmd.Flags().StringP("id", "i", "", "")
}

func oauthClientKey() clientv2.Key {
	return clientv2.Key{
		Id:          "k1",
		KeyType:     "client",
		Scopes:      &[]string{"devices:core"},
		Tags:        &[]string{"tag:k8s-operator"},
		Description: new("kubernetes operator"),
		Created:     time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
	}
}

func authKey() clientv2.Key {
	return clientv2.Key{
		Id:      "a1",
		KeyType: "auth",
		Created: time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
	}
}

func TestOAuthClientCommands(t *testing.T) {
	client := oauthClientKey()
	created := client
	created.Key = new("hskey-client-secret")

	listBoth := func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		t.Helper()
		assertBearer(t, r)
		assert.Equal(t, "-", r.PathValue("tailnet"))
		writeJSON(t, w, clientv2.ListKeysOutputBody{Keys: []clientv2.Key{client, authKey()}})
	}

	cases := []commandCase{
		{
			name: "create sends scopes, tags, and description and shows the secret once",
			src:  createOAuthClientCmd,
			flags: map[string]string{
				"scope":       "devices:core",
				"tag":         "tag:k8s-operator",
				"description": "kubernetes operator",
			},
			routes: map[string]apiHandler{
				"POST /api/v2/tailnet/{tailnet}/keys": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assertBearer(t, r)
					assert.Equal(t, "-", r.PathValue("tailnet"))

					var body clientv2.CreateKeyRequest

					decodeBody(t, r, &body)
					assert.Equal(t, "client", ptrStr(body.KeyType))
					assert.Equal(t, []string{"devices:core"}, ptrStrs(body.Scopes))
					assert.Equal(t, []string{"tag:k8s-operator"}, ptrStrs(body.Tags))
					assert.Equal(t, "kubernetes operator", ptrStr(body.Description))

					writeJSON(t, w, created)
				},
			},
			want: "OAuth client k1 created.\nSecret (shown once, store it now): hskey-client-secret\n",
		},
		{
			name:  "create prints the key as json",
			src:   createOAuthClientCmd,
			flags: map[string]string{"scope": "devices:core", "output": "json"},
			routes: map[string]apiHandler{
				"POST /api/v2/tailnet/{tailnet}/keys": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, created)
				},
			},
			want: indentJSON(t, created),
		},
		{
			name:    "create requires a scope",
			src:     createOAuthClientCmd,
			flags:   map[string]string{"tag": "tag:k8s-operator"},
			wantErr: "at least one --scope is required",
		},
		{
			name:  "create surfaces the v2 error message",
			src:   createOAuthClientCmd,
			flags: map[string]string{"scope": "devices:core"},
			routes: map[string]apiHandler{
				"POST /api/v2/tailnet/{tailnet}/keys": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"message":"tags are required for devices:core"}`))
				},
			},
			wantErr: "api error (400): tags are required for devices:core",
		},
		{
			name:   "list renders only oauth clients",
			src:    listOAuthClientsCmd,
			routes: map[string]apiHandler{"GET /api/v2/tailnet/{tailnet}/keys": listBoth},
			wantIn: []string{
				"Scopes", "k1", "devices:core", "tag:k8s-operator", "kubernetes operator", "2026-03-01 12:00:00",
			},
			wantNotIn: []string{"a1"},
		},
		{
			name:   "list prints only oauth clients as json",
			src:    listOAuthClientsCmd,
			flags:  map[string]string{"output": "json"},
			routes: map[string]apiHandler{"GET /api/v2/tailnet/{tailnet}/keys": listBoth},
			want:   indentJSON(t, []clientv2.Key{client}),
		},
		{
			name: "list surfaces a non-json error body",
			src:  listOAuthClientsCmd,
			routes: map[string]apiHandler{
				"GET /api/v2/tailnet/{tailnet}/keys": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					http.Error(w, "upstream unavailable", http.StatusBadGateway)
				},
			},
			wantErr: "api error (502): upstream unavailable",
		},
		{
			name:  "delete addresses the client by id",
			src:   deleteOAuthClientCmd,
			flags: map[string]string{"id": "k1"},
			routes: map[string]apiHandler{
				"DELETE /api/v2/tailnet/{tailnet}/keys/{id}": deleteOK("k1"),
			},
			want: "OAuth client k1 deleted\n",
		},
		{
			name:  "delete prints the id as json",
			src:   deleteOAuthClientCmd,
			flags: map[string]string{"id": "k1", "output": "json"},
			routes: map[string]apiHandler{
				"DELETE /api/v2/tailnet/{tailnet}/keys/{id}": deleteOK("k1"),
			},
			want: indentJSON(t, map[string]string{"id": "k1"}),
		},
		{
			name:    "delete requires an id",
			src:     deleteOAuthClientCmd,
			wantErr: "--id is required",
		},
		{
			name:  "delete surfaces the v2 error message",
			src:   deleteOAuthClientCmd,
			flags: map[string]string{"id": "nope"},
			routes: map[string]apiHandler{
				"DELETE /api/v2/tailnet/{tailnet}/keys/{id}": func(
					t *testing.T, w http.ResponseWriter, _ *http.Request,
				) {
					t.Helper()
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusNotFound)
					_, _ = w.Write([]byte(`{"message":"key not found"}`))
				},
			},
			wantErr: "api error (404): key not found",
		},
	}

	runCommandCases(t, oauthFlags, cases)
}

func TestNewV2ClientTransports(t *testing.T) {
	listClients := map[string]apiHandler{
		"GET /api/v2/tailnet/{tailnet}/keys": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
			t.Helper()
			writeJSON(t, w, clientv2.ListKeysOutputBody{Keys: []clientv2.Key{oauthClientKey()}})
		},
	}

	t.Run("remote address without an api key is rejected", func(t *testing.T) {
		pointCLIAt(t, "https://headscale.example.com")
		viper.Set("cli.api_key", "")

		_, _, _, err := newV2Client()
		require.ErrorIs(t, err, errAPIKeyNotSet)
	})

	t.Run("no address dials the unix socket", func(t *testing.T) {
		serveAPIOnSocket(t, listClients)

		cmd := newTestCommand(t, listOAuthClientsCmd, oauthFlags, map[string]string{"output": "json-line"})

		out, err := runCommand(t, cmd)
		require.NoError(t, err)
		assert.Contains(t, out, `"id":"k1"`)
	})

	t.Run("insecure skips certificate verification", func(t *testing.T) {
		mux := http.NewServeMux()
		for pattern, handler := range listClients {
			mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) { handler(t, w, r) })
		}

		server := httptest.NewTLSServer(mux)
		t.Cleanup(server.Close)

		pointCLIAt(t, server.URL)
		viper.Set("cli.insecure", true)

		cmd := newTestCommand(t, listOAuthClientsCmd, oauthFlags, map[string]string{"output": "json-line"})

		out, err := runCommand(t, cmd)
		require.NoError(t, err)
		assert.Contains(t, out, `"id":"k1"`)
	})
}

func TestV2Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{name: "200 is not an error", status: http.StatusOK, body: `{"message":"ignored"}`},
		{name: "204 is not an error", status: http.StatusNoContent},
		{
			name:   "message field is surfaced",
			status: http.StatusForbidden,
			body:   `{"message":"scope denied"}`,
			want:   "api error (403): scope denied",
		},
		{
			name:   "empty message falls back to the body",
			status: http.StatusForbidden,
			body:   `{"message":""}`,
			want:   `api error (403): {"message":""}`,
		},
		{
			name:   "non-json body is trimmed",
			status: http.StatusBadGateway,
			body:   "  bad gateway\n",
			want:   "api error (502): bad gateway",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := v2Error(tt.status, []byte(tt.body))
			if tt.want == "" {
				require.NoError(t, err)

				return
			}

			require.EqualError(t, err, tt.want)
		})
	}
}

func TestPtrHelpers(t *testing.T) {
	t.Parallel()

	assert.Empty(t, ptrStr(nil))
	assert.Equal(t, "x", ptrStr(new("x")))
	assert.Nil(t, ptrStrs(nil))
	assert.Equal(t, []string{"a", "b"}, ptrStrs(&[]string{"a", "b"}))
}
