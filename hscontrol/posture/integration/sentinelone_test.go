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

func TestSentinelOne(t *testing.T) {
	t.Parallel()

	t.Run("check and lookup success with 6 keys and paging", func(t *testing.T) {
		t.Parallel()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "ApiToken s1-token" {
				w.WriteHeader(http.StatusUnauthorized)

				return
			}

			if r.URL.Path != "/web/api/v2.1/agents" {
				w.WriteHeader(http.StatusNotFound)

				return
			}

			// Check request: limit=1
			if r.URL.Query().Get("limit") == "1" && r.URL.Query().Get("serialNumberContains") == "" {
				w.WriteHeader(http.StatusOK)

				return
			}

			cursor := r.URL.Query().Get("cursor")

			w.Header().Set("Content-Type", "application/json")

			switch cursor {
			case "":
				// First page: returns SERIAL1 and nextCursor
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data": []map[string]any{
						{
							"serialNumber":          "SERIAL1",
							"operationalState":      "unlocked",
							"activeThreats":         0,
							"agentVersion":          "23.4.1",
							"encryptedApplications": true,
							"firewallEnabled":       true,
							"infected":              false,
						},
					},
					"pagination": map[string]any{
						"nextCursor": "page-2-cursor",
					},
				})

			case "page-2-cursor":
				// Second page: returns SERIAL2 and empty nextCursor
				_ = json.NewEncoder(w).Encode(map[string]any{
					"data": []map[string]any{
						{
							"serialNumber":          "SERIAL2",
							"operationalState":      "unlocked",
							"activeThreats":         2,
							"agentVersion":          "23.4.2",
							"encryptedApplications": false,
							"firewallEnabled":       false,
							"infected":              true,
						},
					},
					"pagination": map[string]any{
						"nextCursor": "",
					},
				})

			default:
				w.WriteHeader(http.StatusBadRequest)
			}
		}))
		defer ts.Close()

		c, err := New(types.PostureIntegration{
			Provider: types.PostureProviderSentinelOne,
			Config: types.PostureIntegrationConfig{
				BaseURL:  ts.URL,
				APIToken: "s1-token",
			},
		}, ts.Client())
		require.NoError(t, err)

		// Check ok
		require.NoError(t, c.Check(t.Context()))

		// Lookup with known serials and an unknown serial
		attrs, err := c.Lookup(t.Context(), []string{"SERIAL1", "SERIAL2", "UNKNOWN"})
		require.NoError(t, err)

		// Unknown serial absent
		assert.NotContains(t, attrs, "UNKNOWN")

		// SERIAL1 has all 6 keys
		require.Contains(t, attrs, "SERIAL1")
		s1 := attrs["SERIAL1"]
		assert.Equal(t, "unlocked", s1["operationalState"])
		assert.Equal(t, 0, s1["activeThreats"])
		assert.Equal(t, "23.4.1", s1["agentVersion"])
		assert.Equal(t, true, s1["encryptedApplications"])
		assert.Equal(t, true, s1["firewallEnabled"])
		assert.Equal(t, false, s1["infected"])
		assert.Len(t, s1, 6)

		// SERIAL2 received via paging
		require.Contains(t, attrs, "SERIAL2")
		s2 := attrs["SERIAL2"]
		assert.Equal(t, 2, s2["activeThreats"])
		assert.Equal(t, true, s2["infected"])
		assert.Len(t, s2, 6)
	})

	t.Run("auth error", func(t *testing.T) {
		t.Parallel()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		defer ts.Close()

		c, err := New(types.PostureIntegration{
			Provider: types.PostureProviderSentinelOne,
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
			Provider: types.PostureProviderSentinelOne,
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

	t.Run("forbidden error", func(t *testing.T) {
		t.Parallel()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer ts.Close()

		c, err := New(types.PostureIntegration{
			Provider: types.PostureProviderSentinelOne,
			Config: types.PostureIntegrationConfig{
				BaseURL:  ts.URL,
				APIToken: "forbidden-token",
			},
		}, ts.Client())
		require.NoError(t, err)

		err = c.Check(t.Context())
		require.ErrorIs(t, err, ErrAuth)
	})
}
