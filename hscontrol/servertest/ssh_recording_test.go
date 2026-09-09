package servertest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/servertest"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/sessionrecording"
	"tailscale.com/tailcfg"
	"tailscale.com/types/netmap"
)

const recordingWait = 10 * time.Second

// setPolicy installs a policy through the API, which stores it and
// pushes it to the clients.
func setPolicy(t *testing.T, client *http.Client, key, v1, policy string) {
	t.Helper()

	status, body := apiCall(t, client, key, http.MethodPut, v1+"/policy", map[string]any{"policy": policy})
	require.Equal(t, http.StatusOK, status, body)
}

// sshRules returns the SSH rules of the netmap, nil without a policy.
func sshRules(nm *netmap.NetworkMap) []*tailcfg.SSHRule {
	if nm.SSHPolicy == nil {
		return nil
	}

	return nm.SSHPolicy.Rules
}

// TestSSHRecordingPolicy proves that the tailnet default recorders reach
// every SSH rule's accept action with the recorder's tailnet address,
// that enforcement adds the reject and terminate messages, that a rule's
// own recorder wins over the default, that the packet filter opens the
// recorder port without a rule for it, and that the settings API refuses
// a recorder that is not a tag, host or address.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestSSHRecordingPolicy(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "recording-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	setPolicy(t, client, ownerKey, v1, `{
		"tagOwners": {"tag:recorder": ["recording-owner@"], "tag:server": ["recording-owner@"]},
		"acls": [{"action": "accept", "src": ["*"], "dst": ["tag:server:22"]}],
		"ssh": [{
			"action": "accept",
			"src": ["recording-owner@"],
			"dst": ["tag:server"],
			"users": ["autogroup:nonroot"]
		}]
	}`)

	laptop := servertest.NewClient(t, srv, "laptop", servertest.WithUser(owner))
	server := servertest.NewClient(t, srv, "server", servertest.WithUser(owner), servertest.WithTags("tag:server"))
	recorder := servertest.NewClient(t, srv, "recorder", servertest.WithUser(owner),
		servertest.WithTags("tag:recorder"))

	for _, c := range []*servertest.TestClient{laptop, server, recorder} {
		c.WaitForCondition(t, "a netmap", recordingWait, func(nm *netmap.NetworkMap) bool {
			return nm != nil && nm.SelfNode.Valid()
		})
	}

	recorderIP := netip.Addr{}

	for _, addr := range recorder.Netmap().SelfNode.Addresses().All() {
		if addr.Addr().Is4() {
			recorderIP = addr.Addr()
		}
	}

	require.True(t, recorderIP.IsValid())

	wantRecorder := netip.AddrPortFrom(recorderIP, types.SSHRecorderPort)

	server.WaitForCondition(t, "an SSH rule", recordingWait, func(nm *netmap.NetworkMap) bool {
		return len(sshRules(nm)) == 1
	})

	t.Run("no recorder before the setting", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/settings", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Empty(t, field(t, body, "sshRecorders"))
		assert.Equal(t, false, field(t, body, "sshRecordingEnforce"))
		assert.Equal(t, false, field(t, body, "embeddedRecorder"))

		rule := sshRules(server.Netmap())[0]
		assert.Empty(t, rule.Action.Recorders)
		assert.Nil(t, rule.Action.OnRecordingFailure)
	})

	t.Run("a bad recorder is refused", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/settings", map[string]any{
			"sshRecorders": []string{"group:ops"},
		})
		assert.Equal(t, http.StatusBadRequest, status, body)
	})

	t.Run("the default recorder reaches the accept action", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/settings", map[string]any{
			"sshRecorders": []string{"tag:recorder"},
		})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, []any{"tag:recorder"}, field(t, body, "sshRecorders"))

		server.WaitForCondition(t, "the recorder on the rule", recordingWait, func(nm *netmap.NetworkMap) bool {
			rules := sshRules(nm)

			return len(rules) == 1 && len(rules[0].Action.Recorders) == 2 &&
				rules[0].Action.Recorders[0] == wantRecorder
		})

		// The recorder's IPv6 address comes second; a client tries them in
		// order.
		assert.True(t, sshRules(server.Netmap())[0].Action.Recorders[1].Addr().Is6())

		failure := sshRules(server.Netmap())[0].Action.OnRecordingFailure
		require.NotNil(t, failure)
		assert.Equal(t, srv.URL+"/machine/ssh/event", failure.NotifyURL)
		assert.Empty(t, failure.RejectSessionWithMessage, "not enforced: the session goes on unrecorded")
		assert.Empty(t, failure.TerminateSessionWithMessage)

		// The packet filter admits the upload without a rule naming the
		// recorder.
		recorder.WaitForCondition(t, "the upload port open", recordingWait, func(nm *netmap.NetworkMap) bool {
			for _, match := range nm.PacketFilter {
				for _, dst := range match.Dsts {
					if dst.Net.Contains(recorderIP) && dst.Ports.Contains(types.SSHRecorderPort) {
						return true
					}
				}
			}

			return false
		})

		laptop.WaitForCondition(t, "the recorder as a peer", recordingWait, func(_ *netmap.NetworkMap) bool {
			_, ok := laptop.PeerByName("recorder")

			return ok
		})
	})

	t.Run("enforcement rejects sessions that cannot record", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/settings", map[string]any{
			"sshRecordingEnforce": true,
		})
		require.Equal(t, http.StatusOK, status, body)
		assert.Equal(t, true, field(t, body, "sshRecordingEnforce"))

		server.WaitForCondition(t, "the reject message", recordingWait, func(nm *netmap.NetworkMap) bool {
			rules := sshRules(nm)

			return len(rules) == 1 && rules[0].Action.OnRecordingFailure != nil &&
				rules[0].Action.OnRecordingFailure.RejectSessionWithMessage != ""
		})

		failure := sshRules(server.Netmap())[0].Action.OnRecordingFailure
		assert.NotEmpty(t, failure.TerminateSessionWithMessage)
	})

	t.Run("a rule's own recorder wins", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/settings", map[string]any{
			"sshRecorders": []string{}, "sshRecordingEnforce": false,
		})
		require.Equal(t, http.StatusOK, status, body)

		server.WaitForCondition(t, "no recorder again", recordingWait, func(nm *netmap.NetworkMap) bool {
			rules := sshRules(nm)

			return len(rules) == 1 && len(rules[0].Action.Recorders) == 0
		})

		setPolicy(t, client, ownerKey, v1, `{
			"tagOwners": {"tag:recorder": ["recording-owner@"], "tag:server": ["recording-owner@"]},
			"acls": [{"action": "accept", "src": ["*"], "dst": ["tag:server:22"]}],
			"ssh": [{
				"action": "accept",
				"src": ["recording-owner@"],
				"dst": ["tag:server"],
				"users": ["autogroup:nonroot"],
				"recorder": ["tag:recorder"],
				"enforceRecorder": true
			}]
		}`)

		server.WaitForCondition(t, "the rule's recorder", recordingWait, func(nm *netmap.NetworkMap) bool {
			rules := sshRules(nm)

			return len(rules) == 1 && len(rules[0].Action.Recorders) == 2 &&
				rules[0].Action.Recorders[0] == wantRecorder &&
				rules[0].Action.OnRecordingFailure != nil &&
				rules[0].Action.OnRecordingFailure.RejectSessionWithMessage != ""
		})
	})

	t.Run("a client reports a failed recording", func(t *testing.T) {
		receiver := newWebhookReceiver(t)

		status, body := apiCall(t, client, ownerKey, http.MethodPost, v1+"/webhook", map[string]any{
			"url":           receiver.URL,
			"description":   "recording failures",
			"subscriptions": []string{"sshRecordingFailed"},
		})
		require.Equal(t, http.StatusOK, status, body)

		event := tailcfg.SSHEventNotifyRequest{
			EventType: tailcfg.SSHSessionRecordingRejected,
			NodeKey:   server.Netmap().SelfNode.Key(),
			SrcNode:   laptop.Netmap().SelfNode.ID(),
			SSHUser:   "ubuntu",
			LocalUser: "ubuntu",
			RecordingAttempts: []*tailcfg.SSHRecordingAttempt{{
				Recorder: wantRecorder, FailureMessage: "dial tcp: connection refused",
			}},
		}

		assert.Equal(t, http.StatusNoContent, postSSHEvent(t, srv.URL, server, event))

		delivery := receiver.waitFor(t, types.EventSSHRecordingFailed)
		data, ok := delivery.events[0].Data.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "rejected", data["event"])
		assert.Equal(t, "ubuntu", data["sshUser"])
		assert.Equal(t, "laptop", data["srcNode"])

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/audit?action=ssh.recording.rejected", nil)
		require.Equal(t, http.StatusOK, status, body)

		events, ok := field(t, body, "events").([]any)
		require.True(t, ok)
		require.Len(t, events, 1)
		assert.Equal(t, server.NodeIDString(), field(t, events[0], "targetId"))

		// A client cannot report for another node.
		event.NodeKey = laptop.Netmap().SelfNode.Key()
		assert.Equal(t, http.StatusUnauthorized, postSSHEvent(t, srv.URL, server, event))
	})
}

// postSSHEvent sends an SSH event over the node's Noise connection, as
// tailscaled does when a recording fails.
func postSSHEvent(
	t *testing.T,
	serverURL string,
	node *servertest.TestClient,
	event tailcfg.SSHEventNotifyRequest,
) int {
	t.Helper()

	body, err := json.Marshal(event)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	eventURL := strings.Replace(serverURL+"/machine/ssh/event", "http://", "https://", 1)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, eventURL, bytes.NewReader(body))
	require.NoError(t, err)

	resp, err := node.Direct().DoNoiseRequest(req)
	require.NoError(t, err)

	defer resp.Body.Close()

	return resp.StatusCode
}

// TestSSHRecordingAPI proves a session posted to the recorder is listed,
// downloadable and deletable through the API, that the recording is
// attributed to the node it came from, and that the roles hold: a
// member cannot read recordings.
//
//nolint:tparallel // later steps depend on the state earlier ones leave behind
func TestSSHRecordingAPI(t *testing.T) {
	t.Parallel()

	srv := servertest.NewServer(t)
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "recording-api-owner")
	ownerKey := srv.CreateAPIKey(t, owner)
	member := srv.CreateUser(t, "recording-api-member")
	memberKey := srv.CreateAPIKey(t, member)

	server := servertest.NewClient(t, srv, "server", servertest.WithUser(owner))

	uploads := httptest.NewServer(srv.App.SSHRecorderHandlerForTest())
	t.Cleanup(uploads.Close)

	head, err := json.Marshal(sessionrecording.CastHeader{
		Version: 2, Width: 80, Height: 24, Timestamp: time.Now().Unix(),
		SrcNode: "laptop.example", SrcNodeID: "n1", SrcNodeUser: "alice@example.com",
		SSHUser: "ubuntu", LocalUser: "ubuntu",
	})
	require.NoError(t, err)

	cast := string(head) + "\n" + `[0.2,"o","$ uptime\r\n"]` + "\n"

	var id string

	t.Run("an upload is indexed", func(t *testing.T) {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, uploads.URL+"/record",
			strings.NewReader(cast))
		require.NoError(t, err)

		req.Header.Set("Expect", "100-continue")

		resp, err := uploads.Client().Do(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, http.StatusOK, resp.StatusCode)

		status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/ssh-recording", nil)
		require.Equal(t, http.StatusOK, status, body)

		recordings, ok := field(t, body, "recordings").([]any)
		require.True(t, ok)
		require.Len(t, recordings, 1)

		rec, ok := recordings[0].(map[string]any)
		require.True(t, ok)

		id, ok = rec["id"].(string)
		require.True(t, ok)

		assert.Equal(t, "laptop.example", rec["srcNode"])
		assert.Equal(t, "alice@example.com", rec["srcUser"])
		assert.Equal(t, "ubuntu", rec["sshUser"])
		assert.Equal(t, true, rec["complete"])
		assert.InDelta(t, float64(len(cast)), rec["size"], 0)
		assert.Empty(t, rec["dstNode"], "the upload came from loopback, no node holds it")
		assert.Empty(t, field(t, body, "nextBefore"))

		// Nothing beyond the page.
		_ = server
	})

	t.Run("the file is downloadable", func(t *testing.T) {
		req, err := http.NewRequestWithContext(
			t.Context(),
			http.MethodGet,
			v1+"/ssh-recording/"+id+"/cast",
			http.NoBody,
		)
		require.NoError(t, err)

		req.Header.Set("Authorization", "Bearer "+ownerKey)

		resp, err := client.Do(req)
		require.NoError(t, err)

		defer resp.Body.Close()

		require.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "application/x-asciicast", resp.Header.Get("Content-Type"))
		assert.Contains(t, resp.Header.Get("Content-Disposition"), "ssh-recording-"+id+".cast")

		got, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Equal(t, cast, string(got))
	})

	t.Run("a member sees nothing", func(t *testing.T) {
		status, _ := apiCall(t, client, memberKey, http.MethodGet, v1+"/ssh-recording", nil)
		assert.Equal(t, http.StatusForbidden, status)

		status, _ = apiCall(t, client, memberKey, http.MethodDelete, v1+"/ssh-recording/"+id, nil)
		assert.Equal(t, http.StatusForbidden, status)
	})

	t.Run("deleting removes the row and the file", func(t *testing.T) {
		status, body := apiCall(t, client, ownerKey, http.MethodDelete, v1+"/ssh-recording/"+id, nil)
		require.Equal(t, http.StatusOK, status, body)

		status, _ = apiCall(t, client, ownerKey, http.MethodGet, v1+"/ssh-recording/"+id, nil)
		assert.Equal(t, http.StatusNotFound, status)

		status, _ = apiCall(t, client, ownerKey, http.MethodGet, v1+"/ssh-recording/"+id+"/cast", nil)
		assert.Equal(t, http.StatusNotFound, status)

		status, body = apiCall(t, client, ownerKey, http.MethodGet, v1+"/audit?action=sshrecording.delete", nil)
		require.Equal(t, http.StatusOK, status, body)
		assert.Len(t, field(t, body, "events"), 1)
	})
}

// TestEmbeddedSSHRecorderJoins proves the embedded recorder joins the
// tailnet as a tagged, approved node under its own name, that the
// settings report it, and that it becomes a default recorder for every
// SSH rule without being named.
func TestEmbeddedSSHRecorderJoins(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	srv := servertest.NewServer(t, servertest.WithRealListener(), servertest.WithSSHRecording(types.SSHRecordingConfig{
		Enabled: true, Dir: dir + "/recordings", StateDir: dir + "/state",
	}))
	client := srv.HTTPClient(t)
	v1 := srv.URL + "/api/v1"

	owner := srv.CreateUser(t, "embedded-owner")
	ownerKey := srv.CreateAPIKey(t, owner)

	setPolicy(t, client, ownerKey, v1, `{
		"ssh": [{
			"action": "accept",
			"src": ["embedded-owner@"],
			"dst": ["autogroup:self"],
			"users": ["autogroup:nonroot"]
		}]
	}`)

	laptop := servertest.NewClient(t, srv, "laptop", servertest.WithUser(owner))

	srv.App.StartSSHRecorderForTest(t)

	laptop.WaitForCondition(t, "the recorder as a peer", 60*time.Second, func(_ *netmap.NetworkMap) bool {
		_, ok := laptop.PeerByName(types.SSHRecorderHostname)

		return ok
	})

	peer, _ := laptop.PeerByName(types.SSHRecorderHostname)
	assert.Equal(t, []string{types.SSHRecorderTag}, peer.Tags().AsSlice())
	assert.True(t, peer.MachineAuthorized())

	var recorderIP netip.Addr

	for _, addr := range peer.Addresses().All() {
		if addr.Addr().Is4() {
			recorderIP = addr.Addr()
		}
	}

	laptop.WaitForCondition(t, "the recorder on the SSH rule", recordingWait, func(nm *netmap.NetworkMap) bool {
		rules := sshRules(nm)

		return len(rules) == 1 && len(rules[0].Action.Recorders) > 0 &&
			rules[0].Action.Recorders[0] == netip.AddrPortFrom(recorderIP, types.SSHRecorderPort)
	})

	status, body := apiCall(t, client, ownerKey, http.MethodGet, v1+"/settings", nil)
	require.Equal(t, http.StatusOK, status, body)
	assert.Equal(t, true, field(t, body, "embeddedRecorder"))
}
