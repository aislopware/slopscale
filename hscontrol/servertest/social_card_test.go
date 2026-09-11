package servertest_test

import (
	"net/http"
	"testing"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSocialCardAnswersHead pins that the card image, the favicon and the
// root page answer HEAD as they answer GET: Slack's link unfurler asks
// HEAD for og:image before it fetches it, and a 405 there leaves a broken
// picture in the unfurl.
func TestSocialCardAnswersHead(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)

	for _, path := range []string{"/opengraph.png", "/favicon.ico"} {
		for _, method := range []string{http.MethodHead, http.MethodGet} {
			req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+path, http.NoBody)
			require.NoError(t, err)

			resp, err := client.Do(req)
			require.NoError(t, err)
			resp.Body.Close()

			assert.Equal(t, http.StatusOK, resp.StatusCode, "%s %s", method, path)
			assert.Equal(t, "image/png", resp.Header.Get("Content-Type"), "%s %s", method, path)
			assert.NotEqual(t, "0", resp.Header.Get("Content-Length"), "%s %s", method, path)
		}
	}

	req, err := http.NewRequestWithContext(t.Context(), http.MethodHead, srv.URL+"/", http.NoBody)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	resp.Body.Close()

	assert.NotEqual(t, http.StatusMethodNotAllowed, resp.StatusCode, "HEAD / follows GET /")
}
