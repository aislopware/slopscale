package integration

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKolide(t *testing.T) {
	t.Parallel()

	t.Run("check and lookup success with authState rendering", func(t *testing.T) {
		t.Parallel()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer kolide-token" {
				w.WriteHeader(http.StatusUnauthorized)

				return
			}

			if r.URL.Path != "/devices" {
				w.WriteHeader(http.StatusNotFound)

				return
			}

			// Check request has per_page=1 and no query
			if r.URL.Query().Get("per_page") == "1" && r.URL.Query().Get("query") == "" {
				w.WriteHeader(http.StatusOK)

				return
			}

			q := r.URL.Query().Get("query")

			w.Header().Set("Content-Type", "application/json")

			switch {
			case strings.Contains(q, "SERIAL1"):
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data": []map[string]any{
						{
							"serial":     "SERIAL1",
							"auth_state": "will_block",
						},
					},
				})

			case strings.Contains(q, "SERIAL2"):
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data": []map[string]any{
						{
							"serial":     "SERIAL2",
							"auth_state": "good",
						},
					},
				})

			default:
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data": []map[string]any{},
				})
			}
		}))
		defer ts.Close()

		c, err := New(types.PostureIntegration{
			Provider: types.PostureProviderKolide,
			Config: types.PostureIntegrationConfig{
				BaseURL:  ts.URL,
				APIToken: "kolide-token",
			},
		}, ts.Client())
		require.NoError(t, err)

		// Check ok
		require.NoError(t, c.Check(t.Context()))

		// Lookup with known and unknown serials
		attrs, err := c.Lookup(t.Context(), []string{"SERIAL1", "SERIAL2", "UNKNOWN"})
		require.NoError(t, err)

		// Unknown serial absent
		assert.NotContains(t, attrs, "UNKNOWN")

		// SERIAL1 has rendered "Will Block"
		require.Contains(t, attrs, "SERIAL1")
		assert.Equal(t, Attributes{"authState": "Will Block"}, attrs["SERIAL1"])

		// SERIAL2 has rendered "Good"
		require.Contains(t, attrs, "SERIAL2")
		assert.Equal(t, Attributes{"authState": "Good"}, attrs["SERIAL2"])
	})

	t.Run("auth error", func(t *testing.T) {
		t.Parallel()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer ts.Close()

		c, err := New(types.PostureIntegration{
			Provider: types.PostureProviderKolide,
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
			Provider: types.PostureProviderKolide,
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
