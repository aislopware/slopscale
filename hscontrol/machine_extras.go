package hscontrol

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/juanfont/headscale/hscontrol/audit"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/wire"
	"github.com/rs/zerolog/log"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
	"tailscale.com/types/key"
)

// ErrMachineKeyMismatch is returned when a machine request names a node
// key that does not belong to the Noise session it arrived on.
var ErrMachineKeyMismatch = errors.New("node key does not match the session")

// featureCaps lists, per feature the CLI can ask about, the self caps the
// client needs before it stops asking (cmd/tailscale/cli enableFeatureInteractive).
// Funnel is not here: it needs Tailscale's public ingress servers, which
// this server does not run, so the policy refuses the attribute and the
// answer says so.
var featureCaps = map[string][]nodecap.Cap{
	"serve": {nodecap.HTTPS},
}

const funnelUnavailable = "Funnel is not available on this server: it needs Tailscale's public ingress " +
	"relays. tailscale serve still shares the service with the tailnet."

// sessionNode returns the node a machine request speaks for, refusing a
// node key that is unknown or not bound to the session's machine key.
func (ns *noiseServer) sessionNode(nodeKey key.NodePublic) (types.NodeView, error) {
	node, ok := ns.headscale.state.GetNodeByNodeKey(nodeKey)
	if !ok || node.MachineKey() != ns.machineKey {
		return types.NodeView{}, NewHTTPError(http.StatusUnauthorized, "node key does not match the session",
			fmt.Errorf("%w: %s", ErrMachineKeyMismatch, nodeKey.ShortString()))
	}

	return node, nil
}

// AuditLogHandler records a [tailcfg.AuditLogRequest], an action the
// device's user took on the device, as an audit event whose actor is the
// machine. The client sends one per audited action and does not retry a
// refusal, so anything but a 200 loses the entry.
func (ns *noiseServer) AuditLogHandler(writer http.ResponseWriter, req *http.Request) {
	var request tailcfg.AuditLogRequest

	err := wire.UnmarshalRead(req.Body, &request)
	if err != nil {
		httpError(writer, NewHTTPError(http.StatusBadRequest, "invalid audit-log request", err))

		return
	}

	node, err := ns.sessionNode(request.NodeKey)
	if err != nil {
		httpError(writer, err)

		return
	}

	event := &types.AuditEvent{
		ActorKind:  types.ActorNode,
		ActorName:  node.GivenName(),
		Action:     clientAuditAction(request.Action),
		TargetKind: "node",
		TargetID:   node.ID().String(),
		TargetName: node.GivenName(),
		Detail:     map[string]any{"details": request.Details},
		RemoteAddr: req.RemoteAddr,
	}

	if uid, ok := node.UserID().GetOk(); ok {
		event.ActorUserID = types.UserID(uid)
	}

	if !request.Timestamp.IsZero() {
		event.Detail["reportedAt"] = request.Timestamp.UTC().Format(time.RFC3339)
	}

	audit.Record(ns.headscale.state, event)

	writer.WriteHeader(http.StatusOK)
}

// clientAuditAction maps the client's action names onto the audit log's
// dotted form: DISCONNECT_NODE becomes node.client.disconnect.
func clientAuditAction(action tailcfg.ClientAuditAction) string {
	name := strings.ToLower(strings.TrimSuffix(string(action), "_NODE"))

	return "node.client." + strings.ReplaceAll(name, "_", "-")
}

// FeatureQueryHandler answers `tailscale serve` and `tailscale funnel` when
// the node lacks the caps a feature needs: whether it is already on, and
// otherwise where the operator turns it on. The policy's nodeAttrs grant
// the caps, so the answer points at the console's policy page.
func (ns *noiseServer) FeatureQueryHandler(writer http.ResponseWriter, req *http.Request) {
	var request tailcfg.QueryFeatureRequest

	err := wire.UnmarshalRead(req.Body, &request)
	if err != nil {
		httpError(writer, NewHTTPError(http.StatusBadRequest, "invalid feature query", err))

		return
	}

	node, err := ns.sessionNode(request.NodeKey)
	if err != nil {
		httpError(writer, err)

		return
	}

	response := ns.featureQueryResponse(node, request.Feature)

	writer.Header().Set("Content-Type", "application/json; charset=utf-8")

	err = wire.MarshalWrite(writer, &response)
	if err != nil {
		log.Error().Caller().Err(err).Msg("encoding the feature query response")
	}
}

func (ns *noiseServer) featureQueryResponse(node types.NodeView, feature string) tailcfg.QueryFeatureResponse {
	if feature == "funnel" {
		return tailcfg.QueryFeatureResponse{Text: funnelUnavailable}
	}

	needed, known := featureCaps[feature]
	if !known {
		return tailcfg.QueryFeatureResponse{Text: fmt.Sprintf("%q is not a feature this server knows.", feature)}
	}

	capMap := ns.headscale.state.NodeCapMap(node.ID())

	var missing []string

	for _, c := range needed {
		if _, ok := capMap[c]; !ok {
			missing = append(missing, string(c))
		}
	}

	if len(missing) == 0 {
		return tailcfg.QueryFeatureResponse{Complete: true}
	}

	return tailcfg.QueryFeatureResponse{
		Text: fmt.Sprintf(
			"%s is off for this machine. An administrator turns it on by granting the %s node attribute "+
				"to the machine in the policy file, under Access controls.",
			featureTitle(feature), strings.Join(missing, " and "),
		),
		URL: ns.headscale.cfg.ServerURL + "/admin/policy",
	}
}

func featureTitle(feature string) string {
	if feature == "" {
		return "The feature"
	}

	return strings.ToUpper(feature[:1]) + feature[1:]
}
