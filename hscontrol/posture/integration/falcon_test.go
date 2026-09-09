package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFalcon(t *testing.T) {
	t.Parallel()

	t.Run("check and lookup success with token caching", func(t *testing.T) {
		t.Parallel()

		var tokenRequests atomic.Int32

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == http.MethodPost && r.URL.Path == "/oauth2/token":
				tokenRequests.Add(1)

				_ = r.ParseForm()
				if r.FormValue("client_id") != "test-client" || r.FormValue("client_secret") != "test-secret" {
					w.WriteHeader(http.StatusUnauthorized)

					return
				}

				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tokenResponse{
					AccessToken: "falcon-token",
					ExpiresIn:   3600,
				})

			case r.Method == http.MethodGet && r.URL.Path == "/devices/queries/devices/v1":
				if r.Header.Get("Authorization") != "Bearer falcon-token" {
					w.WriteHeader(http.StatusUnauthorized)

					return
				}

				filter := r.URL.Query().Get("filter")

				w.Header().Set("Content-Type", "application/json")

				if strings.Contains(filter, "KNOWN1") {
					_ = json.NewEncoder(w).Encode(map[string]any{
						"resources": []string{"aid-known-1"},
					})
				} else {
					_ = json.NewEncoder(w).Encode(map[string]any{
						"resources": []string{},
					})
				}

			case r.Method == http.MethodGet && r.URL.Path == "/devices/entities/devices/v2":
				if r.Header.Get("Authorization") != "Bearer falcon-token" {
					w.WriteHeader(http.StatusUnauthorized)

					return
				}

				ids := r.URL.Query()["ids"]
				resources := []map[string]string{}

				for _, id := range ids {
					if id == "aid-known-1" {
						resources = append(resources, map[string]string{
							"device_id":     "aid-known-1",
							"serial_number": "KNOWN1",
						})
					}
				}

				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"resources": resources,
				})

			case r.Method == http.MethodGet && r.URL.Path == "/zero-trust-assessment/entities/assessments/v1":
				if r.Header.Get("Authorization") != "Bearer falcon-token" {
					w.WriteHeader(http.StatusUnauthorized)

					return
				}

				ids := r.URL.Query()["ids"]
				resources := []map[string]any{}

				for _, id := range ids {
					if id == "aid-known-1" {
						resources = append(resources, map[string]any{
							"aid": "aid-known-1",
							"assessment": map[string]any{
								"overall": 87.5,
							},
						})
					}
				}

				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"resources": resources,
				})

			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer ts.Close()

		c, err := New(types.PostureIntegration{
			Provider: types.PostureProviderFalcon,
			Config: types.PostureIntegrationConfig{
				BaseURL:      ts.URL,
				ClientID:     "test-client",
				ClientSecret: "test-secret",
			},
		}, ts.Client())
		require.NoError(t, err)

		// Check ok
		require.NoError(t, c.Check(t.Context()))
		assert.Equal(t, int32(1), tokenRequests.Load())

		// First lookup: token is valid and should be reused
		attrs, err := c.Lookup(t.Context(), []string{"KNOWN1", "UNKNOWN"})
		require.NoError(t, err)
		assert.Equal(t, int32(1), tokenRequests.Load(), "token should be cached across requests")

		// Known serial has ztaScore number
		require.Contains(t, attrs, "KNOWN1")
		assert.Equal(t, Attributes{"ztaScore": 87.5}, attrs["KNOWN1"])

		// Unknown serial is absent
		assert.NotContains(t, attrs, "UNKNOWN")

		// Second lookup: token is still cached
		attrs2, err := c.Lookup(t.Context(), []string{"KNOWN1"})
		require.NoError(t, err)
		assert.Equal(t, int32(1), tokenRequests.Load(), "token should remain cached")
		assert.Equal(t, Attributes{"ztaScore": 87.5}, attrs2["KNOWN1"])
	})

	t.Run("auth error on token", func(t *testing.T) {
		t.Parallel()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer ts.Close()

		c, err := New(types.PostureIntegration{
			Provider: types.PostureProviderFalcon,
			Config: types.PostureIntegrationConfig{
				BaseURL:      ts.URL,
				ClientID:     "bad-client",
				ClientSecret: "bad-secret",
			},
		}, ts.Client())
		require.NoError(t, err)

		err = c.Check(t.Context())
		require.ErrorIs(t, err, ErrAuth)

		_, err = c.Lookup(t.Context(), []string{"KNOWN1"})
		require.ErrorIs(t, err, ErrAuth)
	})

	t.Run("server error on lookup", func(t *testing.T) {
		t.Parallel()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/oauth2/token" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tokenResponse{
					AccessToken: "falcon-token",
					ExpiresIn:   3600,
				})

				return
			}

			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		c, err := New(types.PostureIntegration{
			Provider: types.PostureProviderFalcon,
			Config: types.PostureIntegrationConfig{
				BaseURL:      ts.URL,
				ClientID:     "client",
				ClientSecret: "secret",
			},
		}, ts.Client())
		require.NoError(t, err)

		_, err = c.Lookup(t.Context(), []string{"KNOWN1"})
		require.ErrorIs(t, err, ErrUpstream)
	})
}
