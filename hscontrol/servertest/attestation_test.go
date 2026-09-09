package servertest_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/types/netmap"
)

// TestHardwareAttestation proves that a client signing its map requests
// with an attestation key is recorded as attested, that a posture on
// node:hardwareAttested admits it and nobody else, that the API shows the
// record and that resetting it makes the next map request start again.
// A client whose signature does not verify never counts as attested.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestHardwareAttestation(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "attestation-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	// Clients live on the parent test so a subtest's cleanup does not
	// take them away from the ones that follow.
	tpm := servertest.NewClient(t, srv, "tpm", servertest.WithUser(owner),
		servertest.WithHardwareAttestation())
	plain := servertest.NewClient(t, srv, "plain", servertest.WithUser(owner))
	forged := servertest.NewClient(t, srv, "forged", servertest.WithUser(owner),
		servertest.WithHardwareAttestationSigner(func(_ []byte) []byte { return make([]byte, 64) }))
	target := servertest.NewClient(t, srv, "target", servertest.WithUser(owner))

	for _, c := range []*servertest.TestClient{tpm, plain, forged, target} {
		c.WaitForPeerCount(t, 3, postureWait)
	}

	var targetsID, postureID string

	t.Run("a signed map request attests the machine", func(t *testing.T) {
		requireAttested(t, client, ownerKey, v1, tpm.NodeIDString())

		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/node/"+plain.NodeIDString(), nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Nil(t, field(t, body, "node", "hardwareAttestation"),
			"a client that sends no key has no record")

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/node/"+forged.NodeIDString(), nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Nil(t, field(t, body, "node", "hardwareAttestation"),
			"a signature that does not verify leaves the node unattested")
	})

	t.Run("a posture on the attribute admits only the attested machine", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/posture", map[string]any{
			"name": "Attested hardware", "expressions": []string{"node:hardwareAttested == true"},
		})
		require.Equal(t, http.StatusOK, status, body)

		id, ok := field(t, body, "posture", "id").(string)
		require.True(t, ok)

		postureID = id

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/group", map[string]any{"name": "Targets"})
		require.Equal(t, http.StatusOK, status, body)

		id, ok = field(t, body, "group", "id").(string)
		require.True(t, ok)

		targetsID = id

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/group/"+targetsID+"/member",
			map[string]any{"nodeId": target.NodeIDString()})
		require.Equal(t, http.StatusOK, status, body)

		status, body = apiCall(t, client, ownerKey, http.MethodPost, v1+"/access-rule", map[string]any{
			"name":                "SSH from attested machines",
			"protocol":            "tcp",
			"ports":               "22",
			"sourceGroupIds":      []string{groupNamed(t, client, ownerKey, v1, "All")},
			"destinationGroupIds": []string{targetsID},
			"postureIds":          []string{postureID},
		})
		require.Equal(t, http.StatusOK, status, body)

		tpm.WaitForCondition(t, "the target as the only peer", postureWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.Peers) == 1 && nm.Peers[0].ID() == target.Netmap().SelfNode.ID()
		})
		plain.WaitForCondition(t, "no peers", postureWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.Peers) == 0
		})
		forged.WaitForCondition(t, "no peers", postureWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.Peers) == 0
		})
	})

	t.Run("a reset drops the attestation until the next map request", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodDelete,
			v1+"/node/"+tpm.NodeIDString()+"/hardware-attestation", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Nil(t, field(t, body, "node", "hardwareAttestation"), "the record is gone")

		tpm.WaitForCondition(t, "no peers once the record is gone", postureWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.Peers) == 0
		})

		// The client keeps its key, so the map request its reconnect
		// sends attests it again.
		tpm.Reconnect(t)
		tpm.WaitForCondition(t, "the target back", postureWait, func(nm *netmap.NetworkMap) bool {
			return len(nm.Peers) == 1
		})

		requireAttested(t, client, ownerKey, v1, tpm.NodeIDString())
	})
}

// requireAttested waits for the node's API record to report attestation;
// the NodeStore batches its writes, so the first read after a map request
// may still be the state before it.
func requireAttested(t *testing.T, client *http.Client, key, v1, nodeID string) {
	t.Helper()

	require.EventuallyWithT(t, func(collect *assert.CollectT) {
		status, body := apiCall(t, client, key, http.MethodGet, v1+"/node/"+nodeID, nil)
		assert.Equal(collect, http.StatusOK, status, body)

		node, _ := body["node"].(map[string]any)
		attestation, _ := node["hardwareAttestation"].(map[string]any)

		assert.Equal(collect, true, attestation["attested"])
		assert.NotEmpty(collect, attestation["key"])
	}, postureWait, 100*time.Millisecond)
}
