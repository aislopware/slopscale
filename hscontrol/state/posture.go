package state

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/juanfont/headscale/hscontrol/types/change"
	"github.com/juanfont/headscale/hscontrol/util/zlog/zf"
	"github.com/rs/zerolog/log"
	"tailscale.com/tailcfg"
	"tailscale.com/util/rands"
)

// Posture collection errors.
var (
	ErrPostureCollectionOff = errors.New("posture identity collection is off")
	ErrNodeNotConnected     = errors.New("node is not connected")
	ErrC2NTimeout           = errors.New("the node did not answer in time")
	ErrC2NFailed            = errors.New("the node refused the request")
)

// c2nResponseTimeout bounds how long a collection waits for the client.
const c2nResponseTimeout = 15 * time.Second

// PostureMaxAge is how old a report may be before a collection cycle
// asks again.
const PostureMaxAge = 24 * time.Hour

// c2nTracker correlates the c2n requests the server sends as
// [tailcfg.PingRequest]s with the responses the clients post back; see
// [State.CollectPosture]. Like [pingTracker], entries have no TTL: the
// sender cancels or reads within its own timeout.
type c2nTracker struct {
	mu      sync.Mutex
	pending map[string]chan []byte
}

func newC2NTracker() *c2nTracker {
	return &c2nTracker{pending: make(map[string]chan []byte)}
}

func (t *c2nTracker) register() (string, <-chan []byte) {
	id := rands.HexString(pingIDLength)
	ch := make(chan []byte, 1)

	t.mu.Lock()
	t.pending[id] = ch
	t.mu.Unlock()

	return id, ch
}

func (t *c2nTracker) complete(id string, body []byte) bool {
	t.mu.Lock()

	ch, ok := t.pending[id]
	if ok {
		delete(t.pending, id)
	}

	t.mu.Unlock()

	if ok {
		ch <- body
	}

	return ok
}

func (t *c2nTracker) cancel(id string) {
	t.mu.Lock()
	delete(t.pending, id)
	t.mu.Unlock()
}

// CompleteC2N delivers a client's answer to the sender. It reports
// whether the id was known.
func (s *State) CompleteC2N(id string, body []byte) bool {
	return s.c2n.complete(id, body)
}

// c2nRequest serialises a control-to-node request the way the client's
// c2n answerer reads it back.
func c2nRequest(ctx context.Context, method, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, path, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	req.Host = "c2n"
	req.URL = &url.URL{Path: path}

	var buf bytes.Buffer

	err = req.Write(&buf)
	if err != nil {
		return nil, fmt.Errorf("serialising request: %w", err)
	}

	return buf.Bytes(), nil
}

// c2nRoundTrip sends a c2n request to a connected node through its map
// stream and waits for the answer. The client posts the serialised HTTP
// response back over noise to the URL the request names; the id in it is
// the only authentication, as with pings.
//
// The URL carries an https scheme whatever the server URL says: the
// client's noise transport only tunnels https requests and would dial a
// plain http one directly, and over the tunnel only the path matters.
func (s *State) c2nRoundTrip(
	ctx context.Context, nodeID types.NodeID, method, path string, dispatch func(...change.Change),
) (*http.Response, error) {
	payload, err := c2nRequest(ctx, method, path)
	if err != nil {
		return nil, fmt.Errorf("building c2n request: %w", err)
	}

	id, ch := s.c2n.register()
	defer s.c2n.cancel(id)

	dispatch(change.PingNode(nodeID, &tailcfg.PingRequest{
		URL:     c2nResponseURL(s.cfg.ServerURL, id),
		Types:   "c2n",
		Payload: payload,
	}))

	var body []byte

	select {
	case body = <-ch:
	case <-time.After(c2nResponseTimeout):
		return nil, ErrC2NTimeout
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(body)), nil)
	if err != nil {
		return nil, fmt.Errorf("reading c2n response: %w", err)
	}

	return resp, nil
}

// c2nResponseURL is where the client posts its answer; see [c2nRoundTrip].
func c2nResponseURL(serverURL, id string) string {
	u, err := url.Parse(serverURL)
	if err != nil || u.Host == "" {
		return "https://headscale/machine/c2n-response?id=" + id
	}

	return "https://" + u.Host + "/machine/c2n-response?id=" + id
}

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

	resp, err := s.c2nRoundTrip(ctx, nodeID, http.MethodGet, "/posture/identity", dispatch)
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
