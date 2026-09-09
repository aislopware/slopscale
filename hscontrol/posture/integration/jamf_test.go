package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJamf(t *testing.T) {
	t.Parallel()

	t.Run("check and lookup success with 5 keys, paging and token caching", func(t *testing.T) {
		t.Parallel()

		var tokenRequests atomic.Int32

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/api/v1/oauth/token":
				tokenRequests.Add(1)

				_ = r.ParseForm()
				if r.FormValue("client_id") != "jamf-client" || r.FormValue("client_secret") != "jamf-secret" {
					w.WriteHeader(http.StatusUnauthorized)

					return
				}

				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tokenResponse{
					AccessToken: "jamf-token",
					ExpiresIn:   3600,
				})

			case r.Method == http.MethodGet && r.URL.Path == "/api/v1/computers-inventory":
				if r.Header.Get("Authorization") != "Bearer jamf-token" {
					w.WriteHeader(http.StatusUnauthorized)

					return
				}

				// Check request has page-size=1 and section=GENERAL
				if r.URL.Query().Get("page-size") == "1" {
					w.WriteHeader(http.StatusOK)

					return
				}

				page := r.URL.Query().Get("page")

				w.Header().Set("Content-Type", "application/json")

				// Paging simulation: totalCount = 150. Page 0 returns SERIAL1, Page 1 returns SERIAL2.
				if page == "1" {
					_ = json.NewEncoder(w).Encode(map[string]any{
						"totalCount": 150,
						"results": []map[string]any{
							{
								"general": map[string]any{
									"supervised": false,
									"remoteManagement": map[string]any{
										"managed": false,
									},
								},
								"hardware": map[string]any{
									"serialNumber": "SERIAL2",
								},
								"security": map[string]any{
									"firewallEnabled":  false,
									"fileVault2Status": "NOT_ENCRYPTED",
									"sipStatus":        "DISABLED",
								},
							},
						},
					})
				} else {
					_ = json.NewEncoder(w).Encode(map[string]any{
						"totalCount": 150,
						"results": []map[string]any{
							{
								"general": map[string]any{
									"supervised": true,
									"remoteManagement": map[string]any{
										"managed": true,
									},
								},
								"hardware": map[string]any{
									"serialNumber": "SERIAL1",
								},
								"security": map[string]any{
									"firewallEnabled":  true,
									"fileVault2Status": "ALL_PARTITIONS_ENCRYPTED",
									"sipStatus":        "ENABLED",
								},
							},
						},
					})
				}

			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer ts.Close()

		c, err := New(types.PostureIntegration{
			Provider: types.PostureProviderJamf,
			Config: types.PostureIntegrationConfig{
				BaseURL:      ts.URL,
				ClientID:     "jamf-client",
				ClientSecret: "jamf-secret",
			},
		}, ts.Client())
		require.NoError(t, err)

		// Check ok
		require.NoError(t, c.Check(t.Context()))
		assert.Equal(t, int32(1), tokenRequests.Load())

		// First lookup: token reused, paging traverses both pages
		attrs, err := c.Lookup(t.Context(), []string{"SERIAL1", "SERIAL2", "UNKNOWN"})
		require.NoError(t, err)
		assert.Equal(t, int32(1), tokenRequests.Load(), "token should be cached across requests")

		// Unknown serial absent
		assert.NotContains(t, attrs, "UNKNOWN")

		// SERIAL1 has 5 keys
		require.Contains(t, attrs, "SERIAL1")
		j1 := attrs["SERIAL1"]
		assert.Equal(t, true, j1["remoteManaged"])
		assert.Equal(t, true, j1["supervised"])
		assert.Equal(t, true, j1["firewallEnabled"])
		assert.Equal(t, "ALL_PARTITIONS_ENCRYPTED", j1["fileVaultStatus"])
		assert.Equal(t, "ENABLED", j1["SIPEnabled"])
		assert.Len(t, j1, 5)

		// SERIAL2 received via page 1
		require.Contains(t, attrs, "SERIAL2")
		j2 := attrs["SERIAL2"]
		assert.Equal(t, false, j2["remoteManaged"])
		assert.Equal(t, "NOT_ENCRYPTED", j2["fileVaultStatus"])
		assert.Len(t, j2, 5)

		// Second lookup: token still cached
		attrs2, err := c.Lookup(t.Context(), []string{"SERIAL1"})
		require.NoError(t, err)
		assert.Equal(t, int32(1), tokenRequests.Load(), "token should remain cached")
		assert.Contains(t, attrs2, "SERIAL1")
	})

	t.Run("auth error", func(t *testing.T) {
		t.Parallel()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer ts.Close()

		c, err := New(types.PostureIntegration{
			Provider: types.PostureProviderJamf,
			Config: types.PostureIntegrationConfig{
				BaseURL:      ts.URL,
				ClientID:     "bad-client",
				ClientSecret: "bad-secret",
			},
		}, ts.Client())
		require.NoError(t, err)

		err = c.Check(t.Context())
		require.ErrorIs(t, err, ErrAuth)

		_, err = c.Lookup(t.Context(), []string{"SERIAL1"})
		require.ErrorIs(t, err, ErrAuth)
	})

	t.Run("server error", func(t *testing.T) {
		t.Parallel()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tokenResponse{
					AccessToken: "token",
					ExpiresIn:   3600,
				})

				return
			}

			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		c, err := New(types.PostureIntegration{
			Provider: types.PostureProviderJamf,
			Config: types.PostureIntegrationConfig{
				BaseURL:      ts.URL,
				ClientID:     "client",
				ClientSecret: "secret",
			},
		}, ts.Client())
		require.NoError(t, err)

		err = c.Check(t.Context())
		require.ErrorIs(t, err, ErrUpstream)

		_, err = c.Lookup(t.Context(), []string{"SERIAL1"})
		require.ErrorIs(t, err, ErrUpstream)
	})
}
