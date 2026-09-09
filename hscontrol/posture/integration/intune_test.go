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

func TestIntune(t *testing.T) {
	t.Parallel()

	t.Run("check and lookup success with 6 keys, paging and token caching", func(t *testing.T) {
		t.Parallel()

		var tokenRequests atomic.Int32

		var ts *httptest.Server

		ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/my-tenant/oauth2/v2.0/token":
				tokenRequests.Add(1)

				_ = r.ParseForm()
				if r.FormValue("client_id") != "intune-client" || r.FormValue("client_secret") != "intune-secret" {
					w.WriteHeader(http.StatusUnauthorized)

					return
				}

				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tokenResponse{
					AccessToken: "intune-token",
					ExpiresIn:   3600,
				})

			case r.Method == http.MethodGet && r.URL.Path == "/v1.0/deviceManagement/managedDevices":
				if r.Header.Get("Authorization") != "Bearer intune-token" {
					w.WriteHeader(http.StatusUnauthorized)

					return
				}

				// Check request has $top=1
				if r.URL.Query().Get("$top") == "1" {
					w.WriteHeader(http.StatusOK)

					return
				}

				page := r.URL.Query().Get("page")

				w.Header().Set("Content-Type", "application/json")

				if page == "2" {
					// Page 2: returns SERIAL2 without next link
					_ = json.NewEncoder(w).Encode(map[string]any{
						"value": []map[string]any{
							{
								"serialNumber":            "SERIAL2",
								"complianceState":         "inGracePeriod",
								"azureADRegistered":       false,
								"deviceRegistrationState": "notRegistered",
								"isSupervised":            false,
								"isEncrypted":             false,
								"managedDeviceOwnerType":  "personal",
							},
						},
						"@odata.nextLink": "",
					})
				} else {
					// Page 1: returns SERIAL1 and next link
					_ = json.NewEncoder(w).Encode(map[string]any{
						"value": []map[string]any{
							{
								"serialNumber":            "SERIAL1",
								"complianceState":         "compliant",
								"azureADRegistered":       true,
								"deviceRegistrationState": "registered",
								"isSupervised":            true,
								"isEncrypted":             true,
								"managedDeviceOwnerType":  "company",
							},
						},
						"@odata.nextLink": ts.URL + "/v1.0/deviceManagement/managedDevices?page=2",
					})
				}

			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer ts.Close()

		c, err := New(types.PostureIntegration{
			Provider: types.PostureProviderIntune,
			Config: types.PostureIntegrationConfig{
				BaseURL:      ts.URL,
				TenantID:     "my-tenant",
				ClientID:     "intune-client",
				ClientSecret: "intune-secret",
			},
		}, ts.Client())
		require.NoError(t, err)

		// Check ok
		require.NoError(t, c.Check(t.Context()))
		assert.Equal(t, int32(1), tokenRequests.Load())

		// First lookup: token reused, paging traverses nextLink
		attrs, err := c.Lookup(t.Context(), []string{"SERIAL1", "SERIAL2", "UNKNOWN"})
		require.NoError(t, err)
		assert.Equal(t, int32(1), tokenRequests.Load(), "token should be cached across requests")

		// Unknown serial absent
		assert.NotContains(t, attrs, "UNKNOWN")

		// SERIAL1 has all 6 keys
		require.Contains(t, attrs, "SERIAL1")
		i1 := attrs["SERIAL1"]
		assert.Equal(t, "compliant", i1["complianceState"])
		assert.Equal(t, true, i1["azureADRegistered"])
		assert.Equal(t, "registered", i1["deviceRegistrationState"])
		assert.Equal(t, true, i1["isSupervised"])
		assert.Equal(t, true, i1["isEncrypted"])
		assert.Equal(t, "company", i1["managedDeviceOwnerType"])
		assert.Len(t, i1, 6)

		// SERIAL2 returned from page 2
		require.Contains(t, attrs, "SERIAL2")
		i2 := attrs["SERIAL2"]
		assert.Equal(t, "inGracePeriod", i2["complianceState"])
		assert.Equal(t, false, i2["azureADRegistered"])
		assert.Len(t, i2, 6)

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
			Provider: types.PostureProviderIntune,
			Config: types.PostureIntegrationConfig{
				BaseURL:      ts.URL,
				TenantID:     "tenant",
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
			Provider: types.PostureProviderIntune,
			Config: types.PostureIntegrationConfig{
				BaseURL:      ts.URL,
				TenantID:     "tenant",
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
