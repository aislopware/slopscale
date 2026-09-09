package state

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/aislopware/slopscale/hscontrol/util/zlog/zf"
	"github.com/rs/zerolog/log"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

// hardwareAttestationSkew is how far the client's signature timestamp may
// sit from the server's clock. The client stamps its own time, so the
// window has to survive a machine whose clock is off; a day is what it
// takes to make the timestamp a replay bound rather than a clock check.
const hardwareAttestationSkew = 24 * time.Hour

// verifyHardwareAttestation checks the signature a client made with its
// hardware attestation key over its node key, as
// tailscale.com/control/controlclient produces it on every map request:
// an ASN.1 ECDSA signature over the SHA-256 of "<unix seconds>|<node
// key>". It returns the key that verified, so the caller can tell a new
// key from the stored one.
//
// The key itself is validated as a P-256 point when the map request is
// decoded, so Verifier cannot fail here.
func verifyHardwareAttestation(
	req tailcfg.MapRequest,
	nodeKey key.NodePublic,
	now time.Time,
) (key.HardwareAttestationPublic, bool) {
	pub := req.HardwareAttestationKey
	if pub.IsZero() || len(req.HardwareAttestationKeySignature) == 0 {
		return key.HardwareAttestationPublic{}, false
	}

	stamped := req.HardwareAttestationKeySignatureTimestamp
	if now.Sub(stamped).Abs() > hardwareAttestationSkew {
		log.Debug().
			Time("signed_at", stamped).
			Msg("hardware attestation signature timestamp is too far from the server clock")

		return key.HardwareAttestationPublic{}, false
	}

	digest := sha256.Sum256(fmt.Appendf(nil, "%d|%s", stamped.Unix(), nodeKey.String()))

	if !ecdsa.VerifyASN1(pub.Verifier(), digest[:], req.HardwareAttestationKeySignature) {
		return key.HardwareAttestationPublic{}, false
	}

	return pub, true
}

// attestationMove is what one map request did to a node's attestation.
// Gaining attestation under a new key sets both flags, because both are
// worth an audit entry.
type attestationMove struct {
	gained     bool
	lost       bool
	keyChanged bool
}

func (m attestationMove) moved() bool {
	return m.gained || m.lost || m.keyChanged
}

// applyHardwareAttestation folds the result of [verifyHardwareAttestation]
// into the node and reports what moved. A request that restates the state
// the node is already in changes nothing, so the map request path's
// "nothing changed" exit still holds for the steady state, where every
// request of an attesting client carries a fresh valid signature.
func applyHardwareAttestation(
	node *types.Node,
	verified key.HardwareAttestationPublic,
	ok bool,
	now time.Time,
) attestationMove {
	var move attestationMove

	current := node.HardwareAttestation

	if !ok {
		if current == nil || !current.Attested {
			return move
		}

		// The key that last verified stays, so an operator can still see
		// which one the machine used.
		next := *current
		next.Attested = false
		node.HardwareAttestation = &next
		move.lost = true

		return move
	}

	if current == nil {
		node.HardwareAttestation = &types.HardwareAttestation{
			Key:        verified,
			Attested:   true,
			AttestedAt: now,
		}
		move.gained = true

		return move
	}

	next := *current

	if !next.Key.Equal(verified) {
		next.Key = verified
		next.KeyChangedAt = now
		move.keyChanged = true
	}

	if !next.Attested {
		next.Attested = true
		next.AttestedAt = now
		move.gained = true
	}

	if !move.moved() {
		return move
	}

	node.HardwareAttestation = &next

	return move
}

// recordHardwareAttestation persists a transition and writes it to the
// audit log. The record is a column of its own, like the posture, so the
// map request path's full-row update never carries it.
func (s *State) recordHardwareAttestation(node types.NodeView, move attestationMove) error {
	err := s.db.NodeSetHardwareAttestation(node.ID(), node.HardwareAttestation().AsStruct())
	if err != nil {
		return fmt.Errorf("storing hardware attestation: %w", err)
	}

	if move.keyChanged {
		s.auditAttestation(node, "node.attestation.key_changed")
	}

	if move.gained {
		s.auditAttestation(node, "node.attestation.attested")
	}

	if move.lost {
		s.auditAttestation(node, "node.attestation.lost")
	}

	log.Info().
		Uint64(zf.NodeID, node.ID().Uint64()).
		Bool("attested", node.HardwareAttestation().Attested()).
		Bool("key_changed", move.keyChanged).
		Msg("hardware attestation changed")

	return nil
}

func (s *State) auditAttestation(node types.NodeView, action string) {
	audit.Record(s, &types.AuditEvent{
		ActorKind:  types.ActorSystem,
		Action:     action,
		TargetKind: "node",
		TargetID:   node.ID().String(),
		TargetName: node.GivenName(),
		Detail:     map[string]any{"key": node.HardwareAttestation().Key().String()},
	})
}

// ResetHardwareAttestation forgets what a node's attestation key proved,
// so the next map request that carries a valid signature starts the
// record again. The client keeps its key; nothing is asked of it.
func (s *State) ResetHardwareAttestation(nodeID types.NodeID) (types.NodeView, change.Change, error) {
	var had bool

	node, ok := s.nodeStore.UpdateNode(nodeID, func(node *types.Node) {
		had = node.HardwareAttestation != nil
		node.HardwareAttestation = nil
	})
	if !ok {
		return types.NodeView{}, change.Change{}, fmt.Errorf("%w: %d", ErrNodeNotInNodeStore, nodeID)
	}

	if !had {
		return node, change.Change{}, nil
	}

	err := s.db.NodeSetHardwareAttestation(nodeID, nil)
	if err != nil {
		return types.NodeView{}, change.Change{}, fmt.Errorf("clearing hardware attestation: %w", err)
	}

	c, err := s.updatePolicyManagerNodes()
	if err != nil {
		return types.NodeView{}, change.Change{}, err
	}

	return node, c, nil
}
