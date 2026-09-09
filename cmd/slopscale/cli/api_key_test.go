package cli

import (
	"net/http"
	"testing"
	"time"

	clientv1 "github.com/aislopware/slopscale/gen/client/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// apiKeyFlags mirrors the flags init() registers on the apikeys subcommands,
// including the default expiration.
func apiKeyFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("expiration", "e", DefaultAPIKeyExpiry, "")
	cmd.Flags().StringP("prefix", "p", "", "")
	cmd.Flags().Uint64P("id", "i", 0, "")
	cmd.Flags().Uint64P("user", "u", 0, "")
}

func apiKeys() []clientv1.ApiKey {
	created := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	expiration := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)

	owner := "7"

	return []clientv1.ApiKey{
		{Id: "2", Prefix: "abcd1234", Expiration: &expiration, CreatedAt: &created, UserId: &owner},
		{Id: "3", Prefix: "wxyz9876"},
	}
}

func TestAPIKeyCommands(t *testing.T) {
	keys := apiKeys()

	listAll := func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		t.Helper()
		assertBearer(t, r)
		writeJSON(t, w, clientv1.ListAPIKeysOutputBody{ApiKeys: keys})
	}

	deleteByPrefix := func(prefix string) apiHandler {
		return func(t *testing.T, w http.ResponseWriter, r *http.Request) {
			t.Helper()
			assert.Equal(t, prefix, r.PathValue("prefix"))
			writeJSON(t, w, map[string]any{})
		}
	}

	cases := []commandCase{
		{
			name:   "list renders expiration and created, dash when unset",
			src:    listAPIKeys,
			routes: map[string]apiHandler{"GET /api/v1/apikey": listAll},
			wantIn: []string{
				"Prefix", "User", "abcd1234", "wxyz9876", "7", "2026-03-01 12:00:00",
				ColourTime(*keys[0].Expiration), "-",
			},
		},
		{
			name:   "list prints json",
			src:    listAPIKeys,
			flags:  map[string]string{"output": "json"},
			routes: map[string]apiHandler{"GET /api/v1/apikey": listAll},
			want:   indentJSON(t, keys),
		},
		{
			name: "list surfaces the api error",
			src:  listAPIKeys,
			routes: map[string]apiHandler{
				"GET /api/v1/apikey": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusInternalServerError, "listing failed")
				},
			},
			wantErr: "listing failed",
		},
		{
			name: "create defaults to a 90 day expiry and prints the key",
			src:  createAPIKeyCmd,
			routes: map[string]apiHandler{
				"POST /api/v1/apikey": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.CreateApiKeyRequestBody

					decodeBody(t, r, &body)

					if assert.NotNil(t, body.Expiration) {
						assert.WithinDuration(t, time.Now().Add(90*24*time.Hour), *body.Expiration, time.Minute)
					}

					writeJSON(t, w, clientv1.CreateAPIKeyOutputBody{ApiKey: "abcd1234.supersecret"})
				},
			},
			want: "abcd1234.supersecret\n",
		},
		{
			name:  "create honours --expiration and prints json",
			src:   createAPIKeyCmd,
			flags: map[string]string{"expiration": "1h", "output": "json"},
			routes: map[string]apiHandler{
				"POST /api/v1/apikey": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.CreateApiKeyRequestBody

					decodeBody(t, r, &body)

					if assert.NotNil(t, body.Expiration) {
						assert.WithinDuration(t, time.Now().Add(time.Hour), *body.Expiration, time.Minute)
					}

					writeJSON(t, w, clientv1.CreateAPIKeyOutputBody{ApiKey: "abcd1234.supersecret"})
				},
			},
			want: "\"abcd1234.supersecret\"\n",
		},
		{
			name:  "create sends the owning user",
			src:   createAPIKeyCmd,
			flags: map[string]string{"user": "7"},
			routes: map[string]apiHandler{
				"POST /api/v1/apikey": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.CreateApiKeyRequestBody

					decodeBody(t, r, &body)
					assert.Equal(t, "7", ptrStr(body.UserId))

					writeJSON(t, w, clientv1.CreateAPIKeyOutputBody{ApiKey: "abcd1234.supersecret"})
				},
			},
			want: "abcd1234.supersecret\n",
		},
		{
			name:    "create rejects an unparsable expiration before calling the api",
			src:     createAPIKeyCmd,
			flags:   map[string]string{"expiration": "soon"},
			wantErr: "parsing duration",
		},
		{
			name: "create surfaces the api error",
			src:  createAPIKeyCmd,
			routes: map[string]apiHandler{
				"POST /api/v1/apikey": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusInternalServerError, "creating failed")
				},
			},
			wantErr: "creating failed",
		},
		{
			name:  "expire by prefix",
			src:   expireAPIKeyCmd,
			flags: map[string]string{"prefix": "abcd1234"},
			routes: map[string]apiHandler{
				"POST /api/v1/apikey/expire": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.ExpireApiKeyRequestBody

					decodeBody(t, r, &body)
					assert.Nil(t, body.Id)
					assert.Equal(t, "abcd1234", ptrStr(body.Prefix))

					writeJSON(t, w, map[string]any{})
				},
			},
			want: "Key expired\n",
		},
		{
			name:  "expire by id",
			src:   expireAPIKeyCmd,
			flags: map[string]string{"id": "2"},
			routes: map[string]apiHandler{
				"POST /api/v1/apikey/expire": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.ExpireApiKeyRequestBody

					decodeBody(t, r, &body)
					assert.Nil(t, body.Prefix)
					assert.Equal(t, "2", ptrStr(body.Id))

					writeJSON(t, w, map[string]any{})
				},
			},
			want: "Key expired\n",
		},
		{
			name:    "expire requires an id or prefix",
			src:     expireAPIKeyCmd,
			wantErr: "either --id or --prefix must be provided",
		},
		{
			name:    "expire rejects both id and prefix",
			src:     expireAPIKeyCmd,
			flags:   map[string]string{"id": "2", "prefix": "abcd1234"},
			wantErr: "only one of --id or --prefix can be provided",
		},
		{
			name:  "expire surfaces the api error",
			src:   expireAPIKeyCmd,
			flags: map[string]string{"prefix": "nope"},
			routes: map[string]apiHandler{
				"POST /api/v1/apikey/expire": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusNotFound, "api key not found")
				},
			},
			wantErr: "api key not found",
		},
		{
			name:   "delete by prefix addresses the key in the path",
			src:    deleteAPIKeyCmd,
			flags:  map[string]string{"prefix": "abcd1234"},
			routes: map[string]apiHandler{"DELETE /api/v1/apikey/{prefix}": deleteByPrefix("abcd1234")},
			want:   "Key deleted\n",
		},
		{
			name:  "delete by id resolves the prefix through the list",
			src:   deleteAPIKeyCmd,
			flags: map[string]string{"id": "3", "output": "json"},
			routes: map[string]apiHandler{
				"GET /api/v1/apikey":             listAll,
				"DELETE /api/v1/apikey/{prefix}": deleteByPrefix("wxyz9876"),
			},
			want: "{}\n",
		},
		{
			name:    "delete by an unknown id fails after the list",
			src:     deleteAPIKeyCmd,
			flags:   map[string]string{"id": "9"},
			routes:  map[string]apiHandler{"GET /api/v1/apikey": listAll},
			wantErr: "api key 9 not found",
		},
		{
			name:  "delete by id surfaces a failing list",
			src:   deleteAPIKeyCmd,
			flags: map[string]string{"id": "2"},
			routes: map[string]apiHandler{
				"GET /api/v1/apikey": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusInternalServerError, "listing failed")
				},
			},
			wantErr: "listing failed",
		},
		{
			name:    "delete requires an id or prefix",
			src:     deleteAPIKeyCmd,
			wantErr: "either --id or --prefix must be provided",
		},
		{
			name:  "delete surfaces the api error",
			src:   deleteAPIKeyCmd,
			flags: map[string]string{"prefix": "abcd1234"},
			routes: map[string]apiHandler{
				"DELETE /api/v1/apikey/{prefix}": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusNotFound, "api key not found")
				},
			},
			wantErr: "api key not found",
		},
	}

	runCommandCases(t, apiKeyFlags, cases)
}

func TestAPIKeyIDOrPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		id         string
		prefix     string
		wantID     uint64
		wantPrefix string
		wantErr    string
	}{
		{name: "id only", id: "2", wantID: 2},
		{name: "prefix only", prefix: "abcd", wantPrefix: "abcd"},
		{name: "neither", wantErr: "either --id or --prefix"},
		{name: "both", id: "2", prefix: "abcd", wantErr: "only one of --id or --prefix"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := &cobra.Command{}
			apiKeyFlags(cmd)

			if tt.id != "" {
				require.NoError(t, cmd.Flags().Set("id", tt.id))
			}

			if tt.prefix != "" {
				require.NoError(t, cmd.Flags().Set("prefix", tt.prefix))
			}

			id, prefix, err := apiKeyIDOrPrefix(cmd)
			if tt.wantErr != "" {
				require.ErrorIs(t, err, errMissingParameter)
				require.ErrorContains(t, err, tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantID, id)
			assert.Equal(t, tt.wantPrefix, prefix)
		})
	}
}

// apiKeyRotateFlags mirrors the flags init() registers on "apikeys rotate",
// where --expiration is empty by default so the key keeps its own.
func apiKeyRotateFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("prefix", "p", "", "")
	cmd.Flags().StringP("expiration", "e", "", "")
}

func TestAPIKeyRotateCommand(t *testing.T) {
	rotated := clientv1.RotateAPIKeyOutputBody{ApiKey: "abcd1234.newsecret", Prefix: "abcd1234"}

	rotateRoute := func(assertBody func(t *testing.T, body clientv1.RotateApiKeyRequestBody)) apiHandler {
		return func(t *testing.T, w http.ResponseWriter, r *http.Request) {
			t.Helper()
			assertBearer(t, r)
			assert.Equal(t, "abcd1234", r.PathValue("prefix"))

			var body clientv1.RotateApiKeyRequestBody

			decodeBody(t, r, &body)
			assertBody(t, body)

			writeJSON(t, w, rotated)
		}
	}

	cases := []commandCase{
		{
			name:  "rotate keeps the current expiry when the flag is unset",
			src:   rotateAPIKeyCmd,
			flags: map[string]string{"prefix": "abcd1234"},
			routes: map[string]apiHandler{
				"POST /api/v1/apikey/{prefix}/rotate": rotateRoute(
					func(t *testing.T, body clientv1.RotateApiKeyRequestBody) {
						t.Helper()
						assert.Nil(t, body.Expiration)
					},
				),
			},
			want: "abcd1234.newsecret\n",
		},
		{
			name:  "rotate sends a new expiry and prints json",
			src:   rotateAPIKeyCmd,
			flags: map[string]string{"prefix": "abcd1234", "expiration": "1h", "output": "json"},
			routes: map[string]apiHandler{
				"POST /api/v1/apikey/{prefix}/rotate": rotateRoute(
					func(t *testing.T, body clientv1.RotateApiKeyRequestBody) {
						t.Helper()

						if assert.NotNil(t, body.Expiration) {
							assert.WithinDuration(t, time.Now().Add(time.Hour), *body.Expiration, time.Minute)
						}
					},
				),
			},
			want: indentJSON(t, rotated),
		},
		{
			name:    "rotate requires a prefix",
			src:     rotateAPIKeyCmd,
			wantErr: "--prefix must be provided",
		},
		{
			name:    "rotate rejects an unparsable expiration before calling the api",
			src:     rotateAPIKeyCmd,
			flags:   map[string]string{"prefix": "abcd1234", "expiration": "soon"},
			wantErr: "parsing duration",
		},
		{
			name:  "rotate surfaces the api error",
			src:   rotateAPIKeyCmd,
			flags: map[string]string{"prefix": "abcd1234"},
			routes: map[string]apiHandler{
				"POST /api/v1/apikey/{prefix}/rotate": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusConflict, "an expired key cannot be rotated")
				},
			},
			wantErr: "an expired key cannot be rotated",
		},
	}

	runCommandCases(t, apiKeyRotateFlags, cases)
}
