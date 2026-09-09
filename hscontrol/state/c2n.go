package state

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"tailscale.com/tailcfg"
	"tailscale.com/util/rands"
)

// Control-to-node failures an operator sees.
var (
	ErrNodeNotConnected = errors.New("node is not connected")
	ErrC2NTimeout       = errors.New("the node did not answer in time")
	ErrC2NFailed        = errors.New("the node refused the request")
)

// c2nResponseTimeout bounds how long a request waits for the client.
const c2nResponseTimeout = 15 * time.Second

// c2nMaxErrorBody is how much of a failing client's message is kept for
// the operator; the client's handlers answer with one line.
const c2nMaxErrorBody = 4 << 10

// c2nTracker correlates the c2n requests the server sends as
// [tailcfg.PingRequest]s with the responses the clients post back; see
// [State.c2nRoundTrip]. Like [pingTracker], entries have no TTL: the
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

// c2nCall is one control-to-node request: the method, the path, which may
// carry a query, and an optional body with the content type the client
// should read it as.
type c2nCall struct {
	method      string
	path        string
	body        []byte
	contentType string
}

// c2nRequest serialises a control-to-node request the way the client's
// c2n answerer reads it back.
func c2nRequest(ctx context.Context, call c2nCall) ([]byte, error) {
	target, err := url.Parse(call.path)
	if err != nil {
		return nil, fmt.Errorf("parsing c2n path: %w", err)
	}

	var body io.Reader = http.NoBody
	if len(call.body) > 0 {
		body = bytes.NewReader(call.body)
	}

	req, err := http.NewRequestWithContext(ctx, call.method, call.path, body)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	req.Host = "c2n"
	req.URL = target

	if call.contentType != "" {
		req.Header.Set("Content-Type", call.contentType)
	}

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
	ctx context.Context, nodeID types.NodeID, call c2nCall, dispatch func(...change.Change),
) (*http.Response, error) {
	payload, err := c2nRequest(ctx, call)
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
		return "https://slopscale/machine/c2n-response?id=" + id
	}

	return "https://" + u.Host + "/machine/c2n-response?id=" + id
}

// c2nFailure describes a client's refusal to the operator: the status the
// handler answered with and the message it wrote, which is the only place
// the reason appears for a client built without a feature.
func c2nFailure(resp *http.Response) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, c2nMaxErrorBody))
	if err != nil {
		return fmt.Errorf("%w: %s", ErrC2NFailed, resp.Status)
	}

	message := strings.TrimSpace(string(body))
	if message == "" {
		return fmt.Errorf("%w: %s", ErrC2NFailed, resp.Status)
	}

	return fmt.Errorf("%w: %s: %s", ErrC2NFailed, resp.Status, message)
}
