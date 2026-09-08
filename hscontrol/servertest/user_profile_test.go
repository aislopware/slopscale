package servertest_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/types/netmap"
)

// TestUserProfileUpdateReachesClients covers juanfont/headscale#2166: an
// operator sets a user's display name and picture after creation, and the
// user's own machine and its peers show the new profile. A field left out
// of the request keeps its value.
func TestUserProfileUpdateReachesClients(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "profile-owner")
	ownerKey := srv.CreateAPIKey(t, owner)
	alice := srv.CreateUser(t, "alice")

	own := servertest.NewClient(t, srv, "alice-laptop", servertest.WithUser(alice))
	peer := servertest.NewClient(t, srv, "owner-laptop", servertest.WithUser(owner))
	own.WaitForPeers(t, 1, 10*time.Second)
	peer.WaitForPeers(t, 1, 10*time.Second)

	profileOf := func(nm *netmap.NetworkMap, login string) (string, string, bool) {
		for _, p := range nm.UserProfiles {
			if p.LoginName() == login {
				return p.DisplayName(), p.ProfilePicURL(), true
			}
		}

		return "", "", false
	}

	status, body := apiCall(t, client, ownerKey, http.MethodPatch, v1+"/user/"+userID(alice),
		map[string]any{"displayName": "Alice Liddell", "pictureUrl": "https://example.com/alice.png"})
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, "Alice Liddell", field(t, body, "user", "displayName"))
	assert.Equal(t, "https://example.com/alice.png", field(t, body, "user", "profilePicUrl"))

	for _, c := range []*servertest.TestClient{own, peer} {
		c.WaitForCondition(t, "the new profile in "+c.Name, 10*time.Second, func(nm *netmap.NetworkMap) bool {
			name, pic, ok := profileOf(nm, "alice")

			return ok && name == "Alice Liddell" && pic == "https://example.com/alice.png"
		})
	}

	status, body = apiCall(t, client, ownerKey, http.MethodPatch, v1+"/user/"+userID(alice),
		map[string]any{"pictureUrl": "http://example.com/alice.png"})
	assert.Equal(t, http.StatusBadRequest, status, "the clients fetch the picture over https only: %v", body)

	status, body = apiCall(t, client, ownerKey, http.MethodPatch, v1+"/user/"+userID(alice),
		map[string]any{"displayName": "Alice"})
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, "https://example.com/alice.png", field(t, body, "user", "profilePicUrl"),
		"a field left out keeps its value")

	peer.WaitForCondition(t, "the shorter name", 10*time.Second, func(nm *netmap.NetworkMap) bool {
		name, pic, ok := profileOf(nm, "alice")

		return ok && name == "Alice" && pic == "https://example.com/alice.png"
	})

	status, _ = apiCall(t, client, ownerKey, http.MethodPatch, v1+"/user/"+userID(alice), map[string]any{})
	assert.Equal(t, http.StatusBadRequest, status, "an empty update is refused")
}
