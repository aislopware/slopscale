package hscontrol

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/wire"
	"github.com/rs/zerolog/log"
	"tailscale.com/tailcfg"
	"tailscale.com/tailcfg/nodecap"
	"tailscale.com/types/key"
)

// ErrMachineKeyMismatch is returned when a machine request names a node
// key that does not belong to the Noise session it arrived on.
var ErrMachineKeyMismatch = errors.New("node key does not match the session")

// ErrFeatureUnavailable is returned for a feature query this server cannot
// satisfy at all, as opposed to one the policy has not granted yet.
var ErrFeatureUnavailable = errors.New("feature unavailable on this server")

// ErrUnknownAuditAction is returned for a client audit action outside the
// closed set the audit log records.
var ErrUnknownAuditAction = errors.New("unknown client audit action")

// featureCaps lists, per feature the CLI can ask about, the self caps the
// client needs before it stops asking (cmd/tailscale/cli enableFeatureInteractive).
var featureCaps = map[string][]nodecap.Cap{
	"serve":  {nodecap.HTTPS},
	"funnel": {nodecap.HTTPS, nodecap.Funnel},
}

const funnelNoIngress = "Funnel has no ingress on this server: run the embedded ingress (funnel.enabled in " +
	"the config file) or `slopscale ingress` on a machine with a public address; see docs/ref/funnel."

// sessionNode returns the node a machine request speaks for, refusing a
// node key that is unknown or not bound to the session's machine key.
func (ns *noiseServer) sessionNode(nodeKey key.NodePublic) (types.NodeView, error) {
	node, ok := ns.slopscale.state.GetNodeByNodeKey(nodeKey)
	if !ok || node.MachineKey() != ns.machineKey {
		return types.NodeView{}, NewHTTPError(http.StatusUnauthorized, "node key does not match the session",
			fmt.Errorf("%w: %s", ErrMachineKeyMismatch, nodeKey.ShortString()))
	}

	return node, nil
}

// AuditLogHandler records a [tailcfg.AuditLogRequest], an action the
// device's user took on the device, as an audit event whose actor is the
// machine. The client queues entries on disk and retries a 5xx with
// backoff; a 4xx drops the entry, so only a request that cannot be
// recorded is refused.
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

	action, ok := clientAuditAction(request.Action)
	if !ok {
		httpError(writer, NewHTTPError(http.StatusBadRequest, "unknown audit action",
			fmt.Errorf("%w: %q", ErrUnknownAuditAction, request.Action)))

		return
	}

	event := &types.AuditEvent{
		ActorKind:  types.ActorNode,
		ActorName:  node.GivenName(),
		Action:     action,
		TargetKind: "node",
		TargetID:   node.ID().String(),
		TargetName: node.GivenName(),
		Detail:     map[string]any{"details": request.Details},
		RemoteAddr: req.RemoteAddr,
	}

	if uid, ok := node.UserID().GetOk(); ok {
		event.ActorUserID = types.UserID(uid)
	}

	audit.Record(ns.slopscale.state, event)

	writer.WriteHeader(http.StatusOK)
}

// clientAuditAction maps the client's action names onto the audit log's
// dotted form. The set is closed so a client cannot write names of its
// own into the log; [tailcfg.ClientAuditAction] lists the ones control
// planes must know.
func clientAuditAction(action tailcfg.ClientAuditAction) (string, bool) {
	if action == tailcfg.AuditNodeDisconnect {
		return "node.client.disconnect", true
	}

	return "", false
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

	// Funnel needs an ingress node; without one nothing could ever reach
	// the machine. An error, not a text answer, keeps `tailscale funnel`
	// on its own check, which fails loudly; a text answer makes the CLI
	// print it and exit 0 with nothing set up.
	if request.Feature == "funnel" && !ns.slopscale.state.HasFunnelIngress() {
		httpError(writer, NewHTTPError(http.StatusNotFound, funnelNoIngress, ErrFeatureUnavailable))

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
	needed, known := featureCaps[feature]
	if !known {
		return tailcfg.QueryFeatureResponse{Text: fmt.Sprintf("%q is not a feature this server knows.", feature)}
	}

	capMap := ns.slopscale.state.NodeCapMap(node.ID())

	var missing []string

	for _, c := range needed {
		if _, ok := capMap[c]; !ok {
			missing = append(missing, string(c))
		}
	}

	if len(missing) == 0 {
		return tailcfg.QueryFeatureResponse{Complete: true}
	}

	// ShouldWait keeps the command open until the policy grants the
	// attribute; without it the CLI prints the text and exits 0 having
	// set nothing up.
	return tailcfg.QueryFeatureResponse{
		Text: fmt.Sprintf(
			"%s is off for this machine. An administrator turns it on by granting the %s node attribute "+
				"to the machine in the policy file, under Access controls. This command waits for that.",
			featureTitle(feature), strings.Join(missing, " and "),
		),
		URL:        ns.slopscale.cfg.ServerURL + "/console/policy",
		ShouldWait: true,
	}
}

func featureTitle(feature string) string {
	if feature == "" {
		return "The feature"
	}

	return strings.ToUpper(feature[:1]) + feature[1:]
}
