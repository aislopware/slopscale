package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/aislopware/slopscale/hscontrol/util/zlog/zf"
	"github.com/rs/zerolog/log"
	"tailscale.com/tailcfg"
)

// ErrPostureCollectionOff is returned while the posture identity
// setting is off.
var ErrPostureCollectionOff = errors.New("posture identity collection is off")

// PostureMaxAge is how old a report may be before a collection cycle
// asks again.
const PostureMaxAge = 24 * time.Hour

// CollectPosture asks a connected node for its device identity and
// records the answer. It needs the posture identity setting on; a node
// whose client has posture checking off answers with an empty, disabled
// report, which is recorded too so the console can say so. The returned
// change is non-empty when the serials changed and the policy may need
// recomputing.
func (s *State) CollectPosture(
	ctx context.Context, nodeID types.NodeID, connected bool, dispatch func(...change.Change),
) (types.PostureIdentity, change.Change, error) {
	if !s.Settings().PostureIdentityOn {
		return types.PostureIdentity{}, change.Change{}, ErrPostureCollectionOff
	}

	if !connected {
		return types.PostureIdentity{}, change.Change{}, ErrNodeNotConnected
	}

	resp, err := s.c2nRoundTrip(ctx, nodeID, c2nCall{method: http.MethodGet, path: "/posture/identity"}, dispatch)
	if err != nil {
		return types.PostureIdentity{}, change.Change{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return types.PostureIdentity{}, change.Change{}, fmt.Errorf("%w: %s", ErrC2NFailed, resp.Status)
	}

	var identity tailcfg.C2NPostureIdentityResponse

	err = readJSON(resp.Body, &identity)
	if err != nil {
		return types.PostureIdentity{}, change.Change{}, fmt.Errorf("decoding posture identity: %w", err)
	}

	posture := types.PostureFromResponse(identity, time.Now().UTC())

	return s.setPosture(nodeID, posture)
}

// setPosture stores a report and recomputes the policy when the serials
// changed.
func (s *State) setPosture(nodeID types.NodeID, posture types.PostureIdentity) (
	types.PostureIdentity, change.Change, error,
) {
	var previous []string

	_, ok := s.nodeStore.UpdateNode(nodeID, func(node *types.Node) {
		if node.Posture != nil {
			previous = node.Posture.SerialNumbers
		}

		node.Posture = &posture
	})
	if !ok {
		return types.PostureIdentity{}, change.Change{}, fmt.Errorf("%w: %d", ErrNodeNotInNodeStore, nodeID)
	}

	err := s.db.NodeSetPosture(nodeID, &posture)
	if err != nil {
		return types.PostureIdentity{}, change.Change{}, fmt.Errorf("storing posture: %w", err)
	}

	if slices.Equal(previous, posture.SerialNumbers) {
		return posture, change.Change{}, nil
	}

	c, err := s.updatePolicyManagerNodes()
	if err != nil {
		return posture, change.Change{}, err
	}

	return posture, c, nil
}

// CollectStalePostures asks every connected node whose report is missing
// or older than a day, one at a time so a large tailnet does not burst.
// The scheduled worker calls it; it does nothing while the setting is off.
func (s *State) CollectStalePostures(
	ctx context.Context,
	connected func(types.NodeID) bool,
	dispatch func(...change.Change),
) {
	if !s.Settings().PostureIdentityOn {
		return
	}

	cutoff := time.Now().Add(-PostureMaxAge)

	for _, node := range s.ListNodes().All() {
		if !connected(node.ID()) {
			continue
		}

		if node.Posture().Valid() && node.Posture().CollectedAt().After(cutoff) {
			continue
		}

		_, c, err := s.CollectPosture(ctx, node.ID(), true, dispatch)
		if err != nil {
			log.Debug().Err(err).Uint64(zf.NodeID, node.ID().Uint64()).Msg("posture collection failed")

			continue
		}

		if !c.IsEmpty() {
			dispatch(c)
		}
	}
}

// PostureCollectionInterval is how often the scheduled worker looks for
// stale reports.
const PostureCollectionInterval = 10 * time.Minute

// readJSON decodes a bounded body.
func readJSON(r io.Reader, v any) error {
	const maxBody = 64 << 10

	data, err := io.ReadAll(io.LimitReader(r, maxBody))
	if err != nil {
		return fmt.Errorf("reading body: %w", err)
	}

	err = json.Unmarshal(data, v)
	if err != nil {
		return fmt.Errorf("decoding body: %w", err)
	}

	return nil
}

// SetNodeAttribute stores a custom posture attribute on the node,
// replacing one with the same key, and recomputes the policy.
func (s *State) SetNodeAttribute(nodeID types.NodeID, attr types.NodeAttribute) (
	types.NodeView, change.Change, error,
) {
	err := types.ValidateNodeAttribute(attr, time.Now())
	if err != nil {
		return types.NodeView{}, change.Change{}, err
	}

	if !attr.ExpiresAt.IsZero() {
		attr.ExpiresAt = attr.ExpiresAt.UTC()
	}

	err = s.db.SetNodeAttribute(nodeID, attr)
	if err != nil {
		return types.NodeView{}, change.Change{}, err
	}

	n, ok := s.nodeStore.UpdateNode(nodeID, func(node *types.Node) {
		node.Attributes = slices.DeleteFunc(
			node.Attributes,
			func(a types.NodeAttribute) bool { return a.Key == attr.Key },
		)
		node.Attributes = append(node.Attributes, attr)
		types.SortAttributes(node.Attributes)
	})
	if !ok {
		return types.NodeView{}, change.Change{}, fmt.Errorf("%w: %d", ErrNodeNotInNodeStore, nodeID)
	}

	c, err := s.updatePolicyManagerNodes()
	if err != nil {
		return n, change.Change{}, err
	}

	return n, c, nil
}

// DeleteNodeAttribute removes a custom posture attribute and recomputes
// the policy.
func (s *State) DeleteNodeAttribute(nodeID types.NodeID, key string) (types.NodeView, change.Change, error) {
	err := s.db.DeleteNodeAttribute(nodeID, key)
	if err != nil {
		return types.NodeView{}, change.Change{}, err
	}

	n, ok := s.nodeStore.UpdateNode(nodeID, func(node *types.Node) {
		node.Attributes = slices.DeleteFunc(node.Attributes, func(a types.NodeAttribute) bool { return a.Key == key })
	})
	if !ok {
		return types.NodeView{}, change.Change{}, fmt.Errorf("%w: %d", ErrNodeNotInNodeStore, nodeID)
	}

	c, err := s.updatePolicyManagerNodes()
	if err != nil {
		return n, change.Change{}, err
	}

	return n, c, nil
}

// ExpireNodeAttributes drops every custom attribute whose expiry has
// passed and returns the recompute that follows, empty when none had.
// The scheduled worker calls it every minute.
func (s *State) ExpireNodeAttributes(now time.Time) (change.Change, error) {
	ids, err := s.db.DeleteExpiredNodeAttributes(now)
	if err != nil {
		return change.Change{}, fmt.Errorf("expiring node attributes: %w", err)
	}

	if len(ids) == 0 {
		return change.Change{}, nil
	}

	for _, id := range ids {
		s.nodeStore.UpdateNode(id, func(node *types.Node) {
			node.Attributes = slices.DeleteFunc(node.Attributes, func(a types.NodeAttribute) bool {
				return a.Expired(now)
			})
		})
	}

	return s.updatePolicyManagerNodes()
}

// NextAttributeExpiry returns the earliest expiry among every node's
// attributes, or the zero time when none expires; the sweeper wakes up
// for it.
func (s *State) NextAttributeExpiry() time.Time {
	var next time.Time

	for _, node := range s.ListNodes().All() {
		for _, a := range node.Attributes().All() {
			if !a.ExpiresAt.IsZero() && (next.IsZero() || a.ExpiresAt.Before(next)) {
				next = a.ExpiresAt
			}
		}
	}

	return next
}
