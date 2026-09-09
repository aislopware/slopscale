package hscontrol

import (
	"cmp"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/aislopware/slopscale/hscontrol/wire"
	"github.com/rs/zerolog/log"
	"tailscale.com/tailcfg"
)

// ErrDeviceAttributesOff is returned when a machine sets its own
// attributes while the deviceAttributesOn setting is off.
var ErrDeviceAttributesOff = errors.New("machines may not set their own attributes")

// deviceAttributeComment marks a custom attribute the machine set itself,
// so an operator can tell it from one an administrator set.
const deviceAttributeComment = "Set by the machine"

const deviceAttributesOffText = "This server does not let machines set their own attributes; " +
	"an administrator turns it on with the deviceAttributesOn setting (Settings, Device trust in the console)."

// deviceAttrOp is one key of the patch: a value to set, or nil to delete.
type deviceAttrOp struct {
	key  string
	attr *types.NodeAttribute
}

// SetDeviceAttrHandler applies a [tailcfg.SetDeviceAttributesRequest],
// the patch of custom posture attributes a machine sends for itself
// through the client's alpha-set-device-attrs local API; see
// docs/ref/device-trust.md. Every key is validated before any is
// written, so a bad patch changes nothing.
func (ns *noiseServer) SetDeviceAttrHandler(writer http.ResponseWriter, req *http.Request) {
	var request tailcfg.SetDeviceAttributesRequest

	err := wire.UnmarshalRead(req.Body, &request)
	if err != nil {
		httpError(writer, NewHTTPError(http.StatusBadRequest, "invalid device attributes request", err))

		return
	}

	node, err := ns.sessionNode(request.NodeKey)
	if err != nil {
		httpError(writer, err)

		return
	}

	if !ns.slopscale.state.Settings().DeviceAttributesOn {
		httpError(writer, NewHTTPError(http.StatusForbidden, deviceAttributesOffText, ErrDeviceAttributesOff))

		return
	}

	ops, err := deviceAttrOps(request.Update, time.Now())
	if err != nil {
		httpError(writer, NewHTTPError(http.StatusBadRequest, err.Error(), err))

		return
	}

	changes, err := ns.applyDeviceAttrs(node, ops, req.RemoteAddr)
	if err != nil {
		httpError(writer, NewHTTPError(http.StatusInternalServerError, "storing the device attributes", err))

		return
	}

	ns.slopscale.Change(changes...)

	writeJSON(writer, struct{}{})
}

// deviceAttrOps validates the patch and orders it by key.
func deviceAttrOps(update tailcfg.AttrUpdate, now time.Time) ([]deviceAttrOp, error) {
	ops := make([]deviceAttrOp, 0, len(update))

	for key, value := range update {
		if value == nil {
			ops = append(ops, deviceAttrOp{key: key})

			continue
		}

		v, err := types.AttributeValueOf(value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}

		attr := types.NodeAttribute{Key: key, Value: v, Comment: deviceAttributeComment}

		err = types.ValidateNodeAttribute(attr, now)
		if err != nil {
			return nil, err
		}

		ops = append(ops, deviceAttrOp{key: key, attr: &attr})
	}

	slices.SortFunc(ops, func(a, b deviceAttrOp) int { return cmp.Compare(a.key, b.key) })

	return ops, nil
}

// applyDeviceAttrs writes the patch and records each key in the audit
// log with the machine as the actor.
func (ns *noiseServer) applyDeviceAttrs(
	node types.NodeView,
	ops []deviceAttrOp,
	remoteAddr string,
) ([]change.Change, error) {
	changes := make([]change.Change, 0, len(ops))

	for _, op := range ops {
		var (
			c      change.Change
			err    error
			action string
			detail map[string]any
		)

		if op.attr == nil {
			action = "node.attribute.delete"
			detail = map[string]any{"key": op.key}
			_, c, err = ns.slopscale.state.DeleteNodeAttribute(node.ID(), op.key)

			if errors.Is(err, types.ErrAttributeNotFound) {
				continue
			}
		} else {
			action = "node.attribute.set"
			detail = map[string]any{"key": op.key, "value": op.attr.Value.Any()}
			_, c, err = ns.slopscale.state.SetNodeAttribute(node.ID(), *op.attr)
		}

		if err != nil {
			return changes, err
		}

		changes = append(changes, c)

		ns.recordDeviceAttr(node, action, detail, remoteAddr)
	}

	log.Info().Caller().
		Uint64("node.id", node.ID().Uint64()).
		Str("node.name", node.GivenName()).
		Int("attributes", len(ops)).
		Msg("device set its own attributes")

	return changes, nil
}

func (ns *noiseServer) recordDeviceAttr(node types.NodeView, action string, detail map[string]any, remoteAddr string) {
	event := &types.AuditEvent{
		ActorKind:  types.ActorNode,
		ActorName:  node.GivenName(),
		Action:     action,
		TargetKind: "node",
		TargetID:   node.ID().String(),
		TargetName: node.GivenName(),
		Detail:     detail,
		RemoteAddr: remoteAddr,
	}

	if uid, ok := node.UserID().GetOk(); ok {
		event.ActorUserID = types.UserID(uid)
	}

	audit.Record(ns.slopscale.state, event)
}
