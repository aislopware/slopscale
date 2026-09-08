package servertest_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/juanfont/headscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// derpCall is [apiCall] with the response headers, which the ETag contract
// needs, and an If-Match that is sent only when it is not empty.
func derpCall(
	t *testing.T, client *http.Client, key, method, url, ifMatch string, body any,
) (int, http.Header, map[string]any) {
	t.Helper()

	var reqBody io.Reader = http.NoBody

	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(t, err)

		reqBody = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(t.Context(), method, url, reqBody)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")

	if ifMatch != "" {
		req.Header.Set("If-Match", ifMatch)
	}

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var decoded map[string]any
	if len(raw) > 0 {
		require.NoError(t, json.Unmarshal(raw, &decoded), "body: %s", raw)
	}

	return resp.StatusCode, resp.Header, decoded
}

// derpSettingsBody is a complete settings document that needs no network:
// one custom region, the embedded relay off, and a refetch interval that
// is what each step varies.
func derpSettingsBody(frequency string) map[string]any {
	return map[string]any{
		"updateFrequency": frequency,
		"regions": []map[string]any{{
			"id":    20,
			"code":  "sgp",
			"nodes": []map[string]any{{"hostName": "sgp.derp.example"}},
		}},
		"server": map[string]any{"enabled": false},
	}
}

// TestDERPSettingsETag proves the optimistic concurrency of the DERP
// settings: a read carries an ETag of the settings in force, a write with a
// matching If-Match goes through and reports the new ETag, a write with the
// ETag of a read someone else has overtaken is refused without touching
// anything, and a write without the header behaves as it always did. The
// subtests carry the ETag they saw to the next one, so they run in order.
//
//nolint:tparallel // later steps use the etag and settings earlier ones leave
func TestDERPSettingsETag(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1/derp"

	owner := srv.CreateUser(t, "derp-etag-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	// fromFile is the etag of the config file's settings, stale from the
	// first write on; current is the etag of the settings in force.
	var fromFile, current string

	t.Run("get carries a stable etag", func(t *testing.T) {
		status, header, body := derpCall(t, client, ownerKey, http.MethodGet, v1, "", nil)
		require.Equal(t, http.StatusOK, status, body)

		fromFile = header.Get("ETag")
		require.NotEmpty(t, fromFile)
		assert.True(t,
			strings.HasPrefix(fromFile, `"`) && strings.HasSuffix(fromFile, `"`),
			"the etag is quoted per RFC 9110: %s", fromFile,
		)

		_, header, _ = derpCall(t, client, ownerKey, http.MethodGet, v1, "", nil)
		assert.Equal(t, fromFile, header.Get("ETag"), "settings that did not change keep their etag")
	})

	t.Run("a matching if-match writes and reports the new etag", func(t *testing.T) {
		status, header, body := derpCall(
			t, client, ownerKey, http.MethodPut, v1, fromFile, derpSettingsBody("2h"),
		)
		require.Equal(t, http.StatusOK, status, body)

		current = header.Get("ETag")
		require.NotEmpty(t, current)
		assert.NotEqual(t, fromFile, current, "the etag follows the settings")
		assert.Equal(t, "2h0m0s", field(t, body, "effective", "updateFrequency"))

		_, header, _ = derpCall(t, client, ownerKey, http.MethodGet, v1, "", nil)
		assert.Equal(t, current, header.Get("ETag"), "the write's etag is what the next read carries")
	})

	t.Run("a stale if-match is refused and changes nothing", func(t *testing.T) {
		status, _, body := derpCall(
			t, client, ownerKey, http.MethodPut, v1, fromFile, derpSettingsBody("3h"),
		)
		require.Equal(t, http.StatusPreconditionFailed, status, body)
		assert.Contains(t, body["detail"], "changed since they were read")

		status, header, body := derpCall(t, client, ownerKey, http.MethodGet, v1, "", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "2h0m0s", field(t, body, "effective", "updateFrequency"), "the refused write changed nothing")
		assert.Equal(t, current, header.Get("ETag"))
	})

	t.Run("a star if-match always matches", func(t *testing.T) {
		status, header, body := derpCall(
			t, client, ownerKey, http.MethodPut, v1, "*", derpSettingsBody("3h"),
		)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "3h0m0s", field(t, body, "effective", "updateFrequency"))

		current = header.Get("ETag")
		require.NotEmpty(t, current)
	})

	t.Run("a write without if-match goes through as before", func(t *testing.T) {
		status, header, body := derpCall(
			t, client, ownerKey, http.MethodPut, v1, "", derpSettingsBody("4h"),
		)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "4h0m0s", field(t, body, "effective", "updateFrequency"))
		assert.NotEqual(t, current, header.Get("ETag"))
	})

	t.Run("reset reports the etag of the file's settings again", func(t *testing.T) {
		status, header, body := derpCall(t, client, ownerKey, http.MethodDelete, v1, "", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, false, body["overridden"])
		assert.Equal(t, fromFile, header.Get("ETag"), "the etag is the settings, not a counter")
	})
}
