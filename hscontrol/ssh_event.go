package hscontrol

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/juanfont/headscale/hscontrol/audit"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/rs/zerolog/log"
	"tailscale.com/tailcfg"
)

// ErrSSHEventNodeMismatch is returned when the node key in an SSH event
// does not belong to the Noise session it came over.
var ErrSSHEventNodeMismatch = errors.New("SSH event node key does not match the session")

const maxSSHEventBody = 64 << 10

// SSHEventHandler takes a [tailcfg.SSHEventNotifyRequest], which a client
// sends when a session recording could not start or broke off, and
// records it in the audit log and as a webhook event. The client only
// ever reports on itself: the node key must belong to the Noise session.
func (ns *noiseServer) SSHEventHandler(writer http.ResponseWriter, req *http.Request) {
	var event tailcfg.SSHEventNotifyRequest

	err := json.NewDecoder(http.MaxBytesReader(writer, req.Body, maxSSHEventBody)).Decode(&event)
	if err != nil {
		httpError(writer, NewHTTPError(http.StatusBadRequest, "invalid SSH event", err))

		return
	}

	node, ok := ns.headscale.state.GetNodeByNodeKey(event.NodeKey)
	if !ok || node.MachineKey() != ns.machineKey {
		httpError(writer, NewHTTPError(http.StatusUnauthorized, "node key does not match the session",
			fmt.Errorf("%w: %s", ErrSSHEventNodeMismatch, event.NodeKey.ShortString())))

		return
	}

	ns.headscale.recordSSHEvent(node, event)

	writer.WriteHeader(http.StatusNoContent)
}

// recordSSHEvent writes the event to the audit log and hands it to the
// webhooks.
func (h *Headscale) recordSSHEvent(node types.NodeView, event tailcfg.SSHEventNotifyRequest) {
	attempts := make([]map[string]string, 0, len(event.RecordingAttempts))
	for _, attempt := range event.RecordingAttempts {
		if attempt == nil {
			continue
		}

		attempts = append(attempts, map[string]string{
			"recorder": attempt.Recorder.String(),
			"error":    attempt.FailureMessage,
		})
	}

	srcName := ""

	if event.SrcNode > 0 {
		if src, ok := h.state.GetNodeByID(types.NodeID(event.SrcNode)); ok {
			srcName = src.GivenName()
		}
	}

	detail := map[string]any{
		"event":     sshEventName(event.EventType),
		"srcNodeId": int64(event.SrcNode),
		"srcNode":   srcName,
		"sshUser":   event.SSHUser,
		"localUser": event.LocalUser,
		"attempts":  attempts,
	}

	audit.Record(h.state, &types.AuditEvent{
		Action:     "ssh.recording." + sshEventName(event.EventType),
		TargetKind: "node",
		TargetID:   node.ID().String(),
		TargetName: node.GivenName(),
		Detail:     detail,
	})

	h.state.EmitSSHRecordingFailed(node, srcName, event.SSHUser, sshEventName(event.EventType), attempts)

	log.Warn().
		Str("event", sshEventName(event.EventType)).
		Uint64("node.id", uint64(node.ID())).
		Str("ssh_user", event.SSHUser).
		Int("attempts", len(attempts)).
		Msg("SSH session recording failed")
}

// sshEventName is the event as the audit log and webhooks name it.
func sshEventName(t tailcfg.SSHEventType) string {
	switch t {
	case tailcfg.SSHSessionRecordingRejected:
		return "rejected"
	case tailcfg.SSHSessionRecordingTerminated:
		return "terminated"
	case tailcfg.SSHSessionRecordingFailed:
		return "failed"
	case tailcfg.UnspecifiedSSHEventType:
		return "unspecified"
	default:
		return "unknown"
	}
}
