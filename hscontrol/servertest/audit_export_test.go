package servertest_test

import (
	"encoding/csv"
	"encoding/json"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// auditDownload performs one authenticated GET and returns the status, the
// response headers and the body as text, which apiCall cannot do because an
// export is a file rather than a JSON object.
func auditDownload(t *testing.T, client *http.Client, key, target string) (int, http.Header, string) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, target, http.NoBody)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+key)

	resp, err := client.Do(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return resp.StatusCode, resp.Header, string(body)
}

// TestAuditExport covers the audit export: the CSV carries a header and the
// matching events oldest first, the JSON is one array of the same events,
// the filters and the file name follow the query, and a member without the
// log scope is refused.
func TestAuditExport(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	export := srv.URL + "/api/v1/audit/export"

	owner := srv.CreateUser(t, "audit-owner")
	ownerKey := srv.CreateAPIKey(t, owner)
	member := srv.CreateUser(t, "audit-member")
	memberKey := srv.CreateAPIKey(t, member)

	// A window of its own, so the server's own events never land in it.
	base := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)

	const events = 5

	for i := range events {
		require.NoError(t, srv.State().RecordAuditEvent(&types.AuditEvent{
			CreatedAt:   base.Add(time.Duration(i) * time.Minute),
			ActorKind:   types.ActorAPIKey,
			ActorUserID: types.UserID(owner.ID),
			ActorName:   "audit-owner",
			Action:      "export.probe",
			TargetKind:  "node",
			TargetID:    "7",
			TargetName:  "probe-node",
			Outcome:     200,
			Detail:      map[string]any{"index": i},
			RemoteAddr:  "127.0.0.1",
		}))
	}

	// One event with another action, so the filters have something to
	// leave out.
	require.NoError(t, srv.State().RecordAuditEvent(&types.AuditEvent{
		CreatedAt:  base.Add(time.Minute),
		ActorKind:  types.ActorSystem,
		Action:     "export.other",
		TargetKind: "node",
		Outcome:    200,
	}))

	window := url.Values{
		"since": {base.Format(time.RFC3339)},
		"until": {base.Add(time.Hour).Format(time.RFC3339)},
	}

	t.Run("csv carries the header and the matching events", func(t *testing.T) {
		t.Parallel()

		query := url.Values{"action": {"export.probe"}}

		status, header, body := auditDownload(t, client, ownerKey, export+"?"+query.Encode())
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "text/csv; charset=utf-8", header.Get("Content-Type"))
		assert.Contains(t, header.Get("Content-Disposition"), `attachment; filename="audit-start-`)
		assert.Contains(t, header.Get("Content-Disposition"), `.csv"`)

		rows, err := csv.NewReader(strings.NewReader(body)).ReadAll()
		require.NoError(t, err)
		require.Len(t, rows, events+1, "a header and one row per event")

		assert.Equal(t, []string{
			"id", "time", "action", "actorKind", "actorUserId", "actorName",
			"targetKind", "targetId", "targetName", "outcome", "remoteAddr", "detail",
		}, rows[0])

		for i, row := range rows[1:] {
			require.Len(t, row, len(rows[0]))
			assert.Equal(t, "export.probe", row[2], "the action filter leaves the other event out")
			assert.Equal(t, "api_key", row[3])
			assert.Equal(t, userID(owner), row[4])
			assert.Equal(t, "probe-node", row[8])
			assert.Equal(t, "200", row[9])
			assert.JSONEq(t, `{"index":`+strconv.Itoa(i)+`}`, row[11], "the detail is one JSON cell")
		}

		assert.Equal(t, base.Format(time.RFC3339), rows[1][1], "oldest first, in UTC")
		assert.Equal(t, base.Add(4*time.Minute).Format(time.RFC3339), rows[events][1])
	})

	t.Run("json is one array", func(t *testing.T) {
		t.Parallel()

		query := url.Values{"action": {"export.probe"}, "format": {"json"}}
		maps.Copy(query, window)

		status, header, body := auditDownload(t, client, ownerKey, export+"?"+query.Encode())
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, "application/json", header.Get("Content-Type"))
		assert.Equal(t,
			`attachment; filename="audit-20260908T100000Z-20260908T110000Z.json"`,
			header.Get("Content-Disposition"),
			"the file is named after the window it covers",
		)

		var exported []map[string]any

		require.NoError(t, json.Unmarshal([]byte(body), &exported), "body: %s", body)
		require.Len(t, exported, events)

		assert.Equal(t, "export.probe", exported[0]["action"])
		assert.Equal(t, "probe-node", exported[0]["targetName"])
		assert.Equal(t, map[string]any{"index": float64(0)}, exported[0]["detail"])
		assert.Equal(t, map[string]any{"index": float64(events - 1)}, exported[events-1]["detail"],
			"oldest first",
		)
	})

	t.Run("the filters narrow the export", func(t *testing.T) {
		t.Parallel()

		query := url.Values{
			"action": {"export.probe"},
			"format": {"json"},
			"since":  {base.Add(3 * time.Minute).Format(time.RFC3339)},
			"until":  {base.Add(time.Hour).Format(time.RFC3339)},
		}

		status, _, body := auditDownload(t, client, ownerKey, export+"?"+query.Encode())
		require.Equal(t, http.StatusOK, status, body)

		var exported []map[string]any

		require.NoError(t, json.Unmarshal([]byte(body), &exported), "body: %s", body)
		assert.Len(t, exported, 2, "since is inclusive, until exclusive")
	})

	t.Run("the caller cannot set the size", func(t *testing.T) {
		t.Parallel()

		query := url.Values{"action": {"export.probe"}, "format": {"json"}, "limit": {"1"}}

		status, _, body := auditDownload(t, client, ownerKey, export+"?"+query.Encode())
		require.Equal(t, http.StatusOK, status, body)

		var exported []map[string]any

		require.NoError(t, json.Unmarshal([]byte(body), &exported), "body: %s", body)
		assert.Len(t, exported, events, "the export runs to the server's cap, not a page size")
	})

	t.Run("the list keeps the same filters", func(t *testing.T) {
		t.Parallel()

		status, body := apiCall(t, client, ownerKey, http.MethodGet,
			srv.URL+"/api/v1/audit?action=export.other", nil)
		require.Equal(t, http.StatusOK, status, body)

		listed, ok := body["events"].([]any)
		require.True(t, ok, "body: %v", body)
		assert.Len(t, listed, 1, "the list and the export share their filter struct")
	})

	t.Run("an unknown format is refused", func(t *testing.T) {
		t.Parallel()

		status, _, body := auditDownload(t, client, ownerKey, export+"?format=xlsx")
		assert.Equal(t, http.StatusUnprocessableEntity, status, body)
	})

	t.Run("a member without the log scope is refused", func(t *testing.T) {
		t.Parallel()

		status, _, body := auditDownload(t, client, memberKey, export)
		assert.Equal(t, http.StatusForbidden, status, body)
	})
}
