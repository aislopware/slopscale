package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKandji(t *testing.T) {
	t.Parallel()

	t.Run("check and lookup success with 2 keys", func(t *testing.T) {
		t.Parallel()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer kandji-token" {
				w.WriteHeader(http.StatusUnauthorized)

				return
			}

			if r.URL.Path != "/api/v1/devices" {
				w.WriteHeader(http.StatusNotFound)

				return
			}

			// Check request has limit=1 and no serial_number
			if r.URL.Query().Get("limit") == "1" && r.URL.Query().Get("serial_number") == "" {
				w.WriteHeader(http.StatusOK)

				return
			}

			serial := r.URL.Query().Get("serial_number")

			w.Header().Set("Content-Type", "application/json")

			if serial == "SERIAL1" {
				_ = json.NewEncoder(w).Encode([]map[string]any{
					{
						"serial_number":   "SERIAL1",
						"mdm_enabled":     true,
						"agent_installed": true,
					},
				})
			} else {
				_ = json.NewEncoder(w).Encode([]map[string]any{})
			}
		}))
		defer ts.Close()

		c, err := New(types.PostureIntegration{
			Provider: types.PostureProviderKandji,
			Config: types.PostureIntegrationConfig{
				BaseURL:  ts.URL,
				APIToken: "kandji-token",
			},
		}, ts.Client())
		require.NoError(t, err)

		// Check ok
		require.NoError(t, c.Check(t.Context()))

		// Lookup with known and unknown serials
		attrs, err := c.Lookup(t.Context(), []string{"SERIAL1", "UNKNOWN"})
		require.NoError(t, err)

		// Unknown serial absent
		assert.NotContains(t, attrs, "UNKNOWN")

		// SERIAL1 has 2 keys
		require.Contains(t, attrs, "SERIAL1")
		k1 := attrs["SERIAL1"]
		assert.Equal(t, true, k1["mdmEnabled"])
		assert.Equal(t, true, k1["agentInstalled"])
		assert.Len(t, k1, 2)
	})

	t.Run("auth error", func(t *testing.T) {
		t.Parallel()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer ts.Close()

		c, err := New(types.PostureIntegration{
			Provider: types.PostureProviderKandji,
			Config: types.PostureIntegrationConfig{
				BaseURL:  ts.URL,
				APIToken: "bad-token",
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

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		c, err := New(types.PostureIntegration{
			Provider: types.PostureProviderKandji,
			Config: types.PostureIntegrationConfig{
				BaseURL:  ts.URL,
				APIToken: "token",
			},
		}, ts.Client())
		require.NoError(t, err)

		err = c.Check(t.Context())
		require.ErrorIs(t, err, ErrUpstream)

		_, err = c.Lookup(t.Context(), []string{"SERIAL1"})
		require.ErrorIs(t, err, ErrUpstream)
	})
}
