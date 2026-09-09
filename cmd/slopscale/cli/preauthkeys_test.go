package cli

import (
	"net/http"
	"testing"
	"time"

	clientv1 "github.com/aislopware/slopscale/gen/client/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

// preAuthKeyFlags mirrors the flags init() registers on the preauthkeys
// subcommands, including the default expiration.
func preAuthKeyFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("reusable", false, "")
	cmd.Flags().Bool("ephemeral", false, "")
	cmd.Flags().StringP("expiration", "e", DefaultPreAuthKeyExpiry, "")
	cmd.Flags().StringSlice("tags", []string{}, "")
	cmd.Flags().Uint64P("user", "u", 0, "")
	cmd.Flags().Uint64P("id", "i", 0, "")
	cmd.Flags().Bool("preauthorized", true, "")
}

func preAuthKeys() []clientv1.PreAuthKey {
	created := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	return []clientv1.PreAuthKey{
		{
			Id:         "5",
			Key:        "hskey-auth-userkey",
			Reusable:   true,
			Expiration: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
			CreatedAt:  created,
			User:       clientv1.User{Id: "1", Name: "alice", CreatedAt: created},
		},
		{
			Id:         "6",
			Key:        "hskey-auth-tagkey",
			Ephemeral:  true,
			Used:       true,
			AclTags:    []string{"tag:ci", "tag:dev"},
			Expiration: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
			CreatedAt:  created,
		},
		{
			Id:         "7",
			Key:        "hskey-auth-orphan",
			Expiration: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC),
			CreatedAt:  created,
		},
	}
}

func TestPreAuthKeyCommands(t *testing.T) {
	keys := preAuthKeys()

	listAll := func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		t.Helper()
		assertBearer(t, r)
		writeJSON(t, w, clientv1.ListPreAuthKeysOutputBody{PreAuthKeys: keys})
	}

	createdKey := func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
		t.Helper()
		writeJSON(t, w, clientv1.PreAuthKeyOutputBody{PreAuthKey: keys[0]})
	}

	cases := []commandCase{
		{
			name:   "list renders owner as user, tags, or dash",
			src:    listPreAuthKeys,
			routes: map[string]apiHandler{"GET /api/v1/preauthkey": listAll},
			wantIn: []string{
				"Key/Prefix", "Owner",
				"hskey-auth-userkey", "alice",
				"hskey-auth-tagkey", "tag:ci", "tag:dev",
				"hskey-auth-orphan", "-",
				"2026-03-01 12:00:00",
				ColourTime(keys[0].Expiration),
				ColourTime(keys[1].Expiration),
			},
		},
		{
			name:   "list prints json",
			src:    listPreAuthKeys,
			flags:  map[string]string{"output": "json"},
			routes: map[string]apiHandler{"GET /api/v1/preauthkey": listAll},
			want:   indentJSON(t, keys),
		},
		{
			name: "list surfaces the api error",
			src:  listPreAuthKeys,
			routes: map[string]apiHandler{
				"GET /api/v1/preauthkey": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusInternalServerError, "listing failed")
				},
			},
			wantErr: "listing failed",
		},
		{
			name: "create sends every option and prints the key",
			src:  createPreAuthKeyCmd,
			flags: map[string]string{
				"user": "1", "reusable": "true", "ephemeral": "true",
				"tags": "tag:ci,tag:dev", "expiration": "24h", "preauthorized": "false",
			},
			routes: map[string]apiHandler{
				"POST /api/v1/preauthkey": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.CreatePreAuthKeyRequestBody

					decodeBody(t, r, &body)
					assert.Equal(t, "1", ptrStr(body.User))
					assert.Equal(t, []string{"tag:ci", "tag:dev"}, ptrStrs(body.AclTags))

					if assert.NotNil(t, body.Preauthorized) {
						assert.False(t, *body.Preauthorized)
					}

					if assert.NotNil(t, body.Reusable) {
						assert.True(t, *body.Reusable)
					}

					if assert.NotNil(t, body.Ephemeral) {
						assert.True(t, *body.Ephemeral)
					}

					if assert.NotNil(t, body.Expiration) {
						assert.WithinDuration(t, time.Now().Add(24*time.Hour), *body.Expiration, time.Minute)
					}

					writeJSON(t, w, clientv1.PreAuthKeyOutputBody{PreAuthKey: keys[0]})
				},
			},
			want: "hskey-auth-userkey\n",
		},
		{
			name: "create defaults to a one hour single-use key",
			src:  createPreAuthKeyCmd,
			routes: map[string]apiHandler{
				"POST /api/v1/preauthkey": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.CreatePreAuthKeyRequestBody

					decodeBody(t, r, &body)
					assert.Equal(t, "0", ptrStr(body.User))
					assert.Empty(t, ptrStrs(body.AclTags))

					if assert.NotNil(t, body.Reusable) {
						assert.False(t, *body.Reusable)
					}

					if assert.NotNil(t, body.Ephemeral) {
						assert.False(t, *body.Ephemeral)
					}

					if assert.NotNil(t, body.Expiration) {
						assert.WithinDuration(t, time.Now().Add(time.Hour), *body.Expiration, time.Minute)
					}

					writeJSON(t, w, clientv1.PreAuthKeyOutputBody{PreAuthKey: keys[0]})
				},
			},
			want: "hskey-auth-userkey\n",
		},
		{
			name:   "create prints the key as json",
			src:    createPreAuthKeyCmd,
			flags:  map[string]string{"user": "1", "output": "json"},
			routes: map[string]apiHandler{"POST /api/v1/preauthkey": createdKey},
			want:   indentJSON(t, keys[0]),
		},
		{
			name:    "create rejects an unparsable expiration before calling the api",
			src:     createPreAuthKeyCmd,
			flags:   map[string]string{"user": "1", "expiration": "soon"},
			wantErr: "parsing duration",
		},
		{
			name:  "create surfaces the api error",
			src:   createPreAuthKeyCmd,
			flags: map[string]string{"user": "99"},
			routes: map[string]apiHandler{
				"POST /api/v1/preauthkey": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusNotFound, "user not found")
				},
			},
			wantErr: "user not found",
		},
		{
			name:  "expire sends the id",
			src:   expirePreAuthKeyCmd,
			flags: map[string]string{"id": "5"},
			routes: map[string]apiHandler{
				"POST /api/v1/preauthkey/expire": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.ExpirePreAuthKeyRequestBody

					decodeBody(t, r, &body)
					assert.Equal(t, "5", ptrStr(body.Id))

					writeJSON(t, w, map[string]any{})
				},
			},
			want: "Key expired\n",
		},
		{
			name:    "expire requires an id",
			src:     expirePreAuthKeyCmd,
			wantErr: "missing --id parameter",
		},
		{
			name:  "expire surfaces the api error",
			src:   expirePreAuthKeyCmd,
			flags: map[string]string{"id": "5"},
			routes: map[string]apiHandler{
				"POST /api/v1/preauthkey/expire": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusNotFound, "key not found")
				},
			},
			wantErr: "key not found",
		},
		{
			name:  "delete sends the id as a query parameter",
			src:   deletePreAuthKeyCmd,
			flags: map[string]string{"id": "5"},
			routes: map[string]apiHandler{
				"DELETE /api/v1/preauthkey": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "5", r.URL.Query().Get("id"))
					writeJSON(t, w, map[string]any{})
				},
			},
			want: "Key deleted\n",
		},
		{
			name:  "delete prints the response as json",
			src:   deletePreAuthKeyCmd,
			flags: map[string]string{"id": "5", "output": "json"},
			routes: map[string]apiHandler{
				"DELETE /api/v1/preauthkey": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, map[string]any{})
				},
			},
			want: "{}\n",
		},
		{
			name:    "delete requires an id",
			src:     deletePreAuthKeyCmd,
			wantErr: "missing --id parameter",
		},
		{
			name:  "delete surfaces the api error",
			src:   deletePreAuthKeyCmd,
			flags: map[string]string{"id": "5"},
			routes: map[string]apiHandler{
				"DELETE /api/v1/preauthkey": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusNotFound, "key not found")
				},
			},
			wantErr: "key not found",
		},
	}

	runCommandCases(t, preAuthKeyFlags, cases)
}
