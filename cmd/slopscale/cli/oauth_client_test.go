package cli

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	clientv1 "github.com/aislopware/slopscale/gen/client/v1"
	clientv2 "github.com/aislopware/slopscale/gen/client/v2"
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
	cmd.Flags().Bool("federated", false, "")
	cmd.Flags().Bool("clear-tags", false, "")
	cmd.Flags().Bool("clear-claims", false, "")
	federatedFlags(cmd)
}

const (
	testIssuer   = "https://token.actions.githubusercontent.com"
	testAudience = "slopscale"
	testSubject  = "repo:acme/app:ref:refs/heads/main"
)

func federatedKey() clientv2.Key {
	return clientv2.Key{
		Id:               "f1",
		KeyType:          "federated",
		Scopes:           &[]string{"auth_keys"},
		Tags:             &[]string{"tag:ci"},
		Issuer:           new(testIssuer),
		Audience:         new(testAudience),
		Subject:          new(testSubject),
		CustomClaimRules: &map[string]string{"repository": "acme/app"},
		Description:      new("github actions"),
		Created:          time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
	}
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

// updatedIdentity is what the v1 patch answers with; the CLI only reads the
// client id from it.
func updatedIdentity() clientv1.OAuthClient {
	return clientv1.OAuthClient{
		ClientId:         "f1",
		KeyType:          clientv1.OAuthClientKeyTypeFederated,
		Scopes:           []string{"auth_keys"},
		Tags:             []string{"tag:ci"},
		Issuer:           testIssuer,
		Audience:         testAudience,
		Subject:          testSubject,
		CustomClaimRules: map[string]string{},
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

	federated := federatedKey()

	listBoth := func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		t.Helper()
		assertBearer(t, r)
		assert.Equal(t, "-", r.PathValue("tailnet"))
		writeJSON(t, w, clientv2.ListKeysOutputBody{Keys: []clientv2.Key{client, federated, authKey()}})
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
			name: "create --federated sends the trust conditions and shows no secret",
			src:  createOAuthClientCmd,
			flags: map[string]string{
				"scope": "auth_keys", "tag": "tag:ci", "federated": "true",
				"issuer": testIssuer, "audience": testAudience, "subject": testSubject,
				"claim": "repository=acme/app",
			},
			routes: map[string]apiHandler{
				"POST /api/v2/tailnet/{tailnet}/keys": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv2.CreateKeyRequest

					decodeBody(t, r, &body)
					assert.Equal(t, "federated", ptrStr(body.KeyType))
					assert.Equal(t, testIssuer, ptrStr(body.Issuer))
					assert.Equal(t, testAudience, ptrStr(body.Audience))
					assert.Equal(t, testSubject, ptrStr(body.Subject))
					require.NotNil(t, body.CustomClaimRules)
					assert.Equal(t, map[string]string{"repository": "acme/app"}, *body.CustomClaimRules)

					writeJSON(t, w, federatedKey())
				},
			},
			want: "Federated identity f1 created.\n",
		},
		{
			name:    "a trust condition without --federated is refused",
			src:     createOAuthClientCmd,
			flags:   map[string]string{"scope": "auth_keys", "issuer": testIssuer},
			wantErr: "need --federated",
		},
		{
			name: "--federated needs every trust condition",
			src:  createOAuthClientCmd,
			flags: map[string]string{
				"scope": "auth_keys", "federated": "true", "issuer": testIssuer, "audience": testAudience,
			},
			wantErr: "--federated needs --issuer, --audience and --subject",
		},
		{
			name: "a malformed claim is refused",
			src:  createOAuthClientCmd,
			flags: map[string]string{
				"scope": "auth_keys", "federated": "true", "issuer": testIssuer,
				"audience": testAudience, "subject": testSubject, "claim": "repository",
			},
			wantErr: `--claim "repository" must be name=value`,
		},
		{
			name:  "update sends only the flags that were set",
			src:   updateOAuthClientCmd,
			flags: map[string]string{"id": "f1", "subject": testSubject, "tag": "tag:ci"},
			routes: map[string]apiHandler{
				"PATCH /api/v1/oauth-client/{clientId}": func(
					t *testing.T, w http.ResponseWriter, r *http.Request,
				) {
					t.Helper()
					assertBearer(t, r)
					assert.Equal(t, "f1", r.PathValue("clientId"))

					var body clientv1.UpdateOAuthClientRequestBody

					decodeBody(t, r, &body)
					assert.Equal(t, testSubject, ptrStr(body.Subject))
					assert.Equal(t, []string{"tag:ci"}, ptrStrs(body.Tags))
					assert.Nil(t, body.Scopes, "an unset flag is not sent")
					assert.Nil(t, body.Description)
					assert.Nil(t, body.Issuer)

					writeJSON(t, w, clientv1.UpdateOAuthClientOutputBody{OauthClient: updatedIdentity()})
				},
			},
			want: "OAuth client f1 updated\n",
		},
		{
			name:  "update clears the tags and the claim rules",
			src:   updateOAuthClientCmd,
			flags: map[string]string{"id": "f1", "clear-tags": "true", "clear-claims": "true"},
			routes: map[string]apiHandler{
				"PATCH /api/v1/oauth-client/{clientId}": func(
					t *testing.T, w http.ResponseWriter, r *http.Request,
				) {
					t.Helper()

					var body clientv1.UpdateOAuthClientRequestBody

					decodeBody(t, r, &body)
					assert.Equal(t, []string{}, ptrStrs(body.Tags))
					require.NotNil(t, body.CustomClaimRules)
					assert.Empty(t, *body.CustomClaimRules)

					writeJSON(t, w, clientv1.UpdateOAuthClientOutputBody{OauthClient: updatedIdentity()})
				},
			},
			want: "OAuth client f1 updated\n",
		},
		{
			name:    "update requires an id",
			src:     updateOAuthClientCmd,
			flags:   map[string]string{"subject": testSubject},
			wantErr: "--id is required",
		},
		{
			name:    "update needs something to change",
			src:     updateOAuthClientCmd,
			flags:   map[string]string{"id": "f1"},
			wantErr: "nothing to update",
		},
		{
			name:  "update surfaces the v1 error",
			src:   updateOAuthClientCmd,
			flags: map[string]string{"id": "k1", "issuer": testIssuer},
			routes: map[string]apiHandler{
				"PATCH /api/v1/oauth-client/{clientId}": func(
					t *testing.T, w http.ResponseWriter, _ *http.Request,
				) {
					t.Helper()
					w.Header().Set("Content-Type", "application/problem+json")
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"title":"Bad Request","detail":"issuer belongs to a federated identity"}`))
				},
			},
			wantErr: "issuer belongs to a federated identity",
		},
		{
			name:   "list renders both kinds and no auth keys",
			src:    listOAuthClientsCmd,
			routes: map[string]apiHandler{"GET /api/v2/tailnet/{tailnet}/keys": listBoth},
			wantIn: []string{
				"Kind", "k1", "client", "devices:core", "tag:k8s-operator", "kubernetes operator",
				"2026-03-01 12:00:00", "f1", "federated", testSubject,
			},
			wantNotIn: []string{"a1"},
		},
		{
			name:   "list prints only oauth clients as json",
			src:    listOAuthClientsCmd,
			flags:  map[string]string{"output": "json"},
			routes: map[string]apiHandler{"GET /api/v2/tailnet/{tailnet}/keys": listBoth},
			want:   indentJSON(t, []clientv2.Key{client, federated}),
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
		pointCLIAt(t, "https://slopscale.example.com")
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
