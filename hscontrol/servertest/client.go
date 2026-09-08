package servertest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/types"
	"tailscale.com/control/controlclient"
	_ "tailscale.com/feature/c2n" // answers c2n pings
	"tailscale.com/health"
	"tailscale.com/net/netmon"
	"tailscale.com/net/tsdial"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
	"tailscale.com/types/netmap"
	"tailscale.com/types/persist"
	"tailscale.com/util/eventbus"
)

// errUnexpectedAuthURL is returned when a pre-auth-key login is answered
// with an interactive AuthURL instead of a registered node.
var errUnexpectedAuthURL = errors.New(
	"servertest: unexpected auth URL (expected auto-auth with preauth key)",
)

// TestClient wraps a Tailscale [controlclient.Direct] connected to a
// [TestServer]. It tracks all received [netmap.NetworkMap] updates, providing
// helpers to wait for convergence and inspect the client's view of
// the network.
type TestClient struct {
	// Name is a human-readable identifier for this client.
	Name string

	server  *TestServer
	direct  *controlclient.Direct
	authKey string
	user    *types.User

	// Connection lifecycle.
	pollCtx    context.Context //nolint:containedctx // test-only; context stored for cancel control
	pollCancel context.CancelFunc
	pollDone   chan struct{}

	// Accumulated state from [tailcfg.MapResponse] callbacks.
	mu      sync.RWMutex
	netmap  *netmap.NetworkMap
	history []*netmap.NetworkMap

	// updates is a buffered channel that receives a signal
	// each time a new [netmap.NetworkMap] arrives.
	updates chan *netmap.NetworkMap

	bus     *eventbus.Bus
	dialer  *tsdial.Dialer
	tracker *health.Tracker
}

// ClientOption configures a [TestClient].
type ClientOption func(*clientConfig)

type clientConfig struct {
	ephemeral bool
	hostname  string
	tags      []string
	user      *types.User
	authKey   string
	hostinfo  func(*tailcfg.Hostinfo)
	serials   []string
	posture   bool
}

// WithHostinfo lets a test shape the [tailcfg.Hostinfo] the client
// registers with: OS, versions, device model and the like.
func WithHostinfo(f func(*tailcfg.Hostinfo)) ClientOption {
	return func(c *clientConfig) { c.hostinfo = f }
}

// WithSerialNumbers makes the client answer the server's c2n posture
// identity request with these serial numbers, as a client with posture
// checking on does. Without it the client answers that posture checking
// is off.
func WithSerialNumbers(serials ...string) ClientOption {
	return func(c *clientConfig) {
		c.serials = serials
		c.posture = true
	}
}

// WithAuthKey registers with the given pre-auth key instead of one the
// server mints for the client, for keys with non-default properties such
// as preauthorized=false.
func WithAuthKey(authKey string) ClientOption {
	return func(cc *clientConfig) { cc.authKey = authKey }
}

// WithEphemeral makes the client register as an ephemeral node.
func WithEphemeral() ClientOption {
	return func(c *clientConfig) { c.ephemeral = true }
}

// WithHostname sets the client's hostname in [tailcfg.Hostinfo].
func WithHostname(name string) ClientOption {
	return func(c *clientConfig) { c.hostname = name }
}

// WithTags sets ACL tags on the pre-auth key.
func WithTags(tags ...string) ClientOption {
	return func(c *clientConfig) { c.tags = tags }
}

// WithUser sets the user for the client. If not set, the harness
// creates a default user.
func WithUser(user *types.User) ClientOption {
	return func(c *clientConfig) { c.user = user }
}

// NewClient creates a [TestClient], registers it with the [TestServer]
// using a pre-auth key, and starts long-polling for map updates.
func NewClient(tb testing.TB, server *TestServer, name string, opts ...ClientOption) *TestClient {
	tb.Helper()

	cc := &clientConfig{
		hostname: name,
	}
	for _, o := range opts {
		o(cc)
	}

	// Resolve user.
	user := cc.user
	if user == nil {
		// Create a per-client user if none specified.
		user = server.CreateUser(tb, "user-"+name)
	}

	// Create pre-auth key.
	uid := types.UserID(user.ID)

	var authKey string

	switch {
	case cc.authKey != "":
		authKey = cc.authKey
	case cc.ephemeral:
		authKey = server.CreateEphemeralPreAuthKey(tb, uid)
	case len(cc.tags) > 0:
		authKey = server.CreateTaggedPreAuthKey(tb, uid, cc.tags)
	default:
		authKey = server.CreatePreAuthKey(tb, uid)
	}

	tc := newTestClient(tb, server, name, cc.hostname, authKey, cc)
	tc.user = user

	// Register with the server.
	tc.register(tb)

	// Start long-polling in the background.
	tc.startPoll(tb)

	return tc
}

// newTestClient builds the Tailscale client infrastructure for a
// [TestClient] wired to the server's in-memory network. An empty authKey
// leaves the client to log in interactively. The client is not registered
// and not polling yet; cleanup is registered on tb.
func newTestClient(tb testing.TB, server *TestServer, name, hostname, authKey string, cc *clientConfig) *TestClient {
	tb.Helper()

	if cc == nil {
		cc = &clientConfig{}
	}

	bus := eventbus.New()
	tracker := health.NewTracker(bus)
	dialer := tsdial.NewDialer(netmon.NewStatic())
	dialer.SetBus(bus)

	// Route all connections through the server's in-memory network
	// so that no real TCP sockets are used, unless the server listens
	// on a real port.
	if !server.realListener {
		dialer.SetSystemDialerForTest(server.MemNet().Dial)
	}

	machineKey := key.NewMachine()

	hostinfo := &tailcfg.Hostinfo{
		BackendLogID: "servertest-" + name,
		Hostname:     hostname,
	}
	if cc.hostinfo != nil {
		cc.hostinfo(hostinfo)
	}

	direct, err := controlclient.NewDirect(controlclient.Options{
		Persist:              persist.Persist{},
		GetMachinePrivateKey: func() (key.MachinePrivate, error) { return machineKey, nil },
		ServerURL:            server.URL,
		AuthKey:              authKey,
		Hostinfo:             hostinfo,
		DiscoPublicKey:       key.NewDisco().Public(),
		Logf:                 tb.Logf,
		HealthTracker:        tracker,
		Dialer:               dialer,
		Bus:                  bus,
		C2NHandler:           c2nHandler(cc),
	})
	if err != nil {
		tb.Fatalf("servertest: NewDirect(%s): %v", name, err)
	}

	tc := &TestClient{
		Name:    name,
		server:  server,
		direct:  direct,
		authKey: authKey,
		updates: make(chan *netmap.NetworkMap, 64),
		bus:     bus,
		dialer:  dialer,
		tracker: tracker,
	}

	tb.Cleanup(func() {
		tc.cleanup()
	})

	return tc
}

// PendingLogin is a Tailscale client that has started an interactive
// registration and is waiting for it to be completed out of band. The server
// answered the first register request with AuthURL and is now holding the
// client's follow-up request open until an operator, or an identity provider
// callback, finishes the registration for AuthID.
type PendingLogin struct {
	// AuthURL is the URL the server asked the user to visit, as a browser
	// would receive it from `tailscale up`.
	AuthURL string

	// AuthID is the registration id embedded in AuthURL. It keys the
	// server's auth cache entry for this registration.
	AuthID types.AuthID

	client *TestClient
	done   chan loginResult
}

type loginResult struct {
	url string
	err error
}

// NewPendingLogin creates a client with no pre-auth key and sends its first
// register request, which the server answers with an AuthURL. The client then
// long-polls the follow-up request in the background, the way tailscaled
// does while `tailscale up` prints the login URL. Complete the registration
// (for example by driving the OIDC flow with [TestServer.HTTPClient]) and
// call [PendingLogin.Wait] to obtain the connected client.
func NewPendingLogin(tb testing.TB, server *TestServer, name string) *PendingLogin {
	tb.Helper()

	tc := newTestClient(tb, server, name, name, "", nil)

	return tc.startPendingLogin(tb)
}

// StartInteractiveRelogin sends a fresh register request for a client that
// has logged out with [TestClient.LogoutAndDisconnect]. Without a pre-auth
// key the server answers with an AuthURL, and the returned [PendingLogin]
// long-polls the follow-up until the registration is completed out of band.
func (c *TestClient) StartInteractiveRelogin(tb testing.TB) *PendingLogin {
	tb.Helper()

	return c.startPendingLogin(tb)
}

// Wait blocks until the server has answered the follow-up request with a
// registered node, then starts the map poll and returns the connected
// client. It fails the test if the follow-up errors, if the server hands out
// a fresh AuthURL instead (the registration was lost), or if timeout passes.
func (p *PendingLogin) Wait(tb testing.TB, timeout time.Duration) *TestClient {
	tb.Helper()

	select {
	case res := <-p.done:
		if res.err != nil {
			tb.Fatalf("servertest: WaitLoginURL(%s): %v", p.client.Name, res.err)
		}

		if res.url != "" {
			tb.Fatalf("servertest: WaitLoginURL(%s): server restarted the login with %s", p.client.Name, res.url)
		}
	case <-time.After(timeout):
		tb.Fatalf("servertest: PendingLogin(%s): registration not completed after %v", p.client.Name, timeout)
	}

	// A relogin after [TestClient.LogoutAndDisconnect] still holds the old
	// session's netmap; drop it so waits observe only the new session.
	p.client.resetNetmapState()
	p.client.startPollLoop()

	return p.client
}

// UpdateFullNetmap implements [controlclient.NetmapUpdater].
// Called by [controlclient.Direct] when a new [netmap.NetworkMap] is received.
func (c *TestClient) UpdateFullNetmap(nm *netmap.NetworkMap) {
	// The control client hands every netmap the same DisplayMessages map
	// and edits it in place when the next response arrives, so a test
	// reading a stored netmap would race that write. Detach it here, on
	// the client's goroutine, before the next response can touch it.
	nm.DisplayMessages = maps.Clone(nm.DisplayMessages)

	c.mu.Lock()
	c.netmap = nm
	c.history = append(c.history, nm)
	c.mu.Unlock()

	// Non-blocking send to the updates channel.
	select {
	case c.updates <- nm:
	default:
	}
}

// --- Lifecycle methods ---

// Disconnect cancels the long-poll context, simulating a clean
// client disconnect.
func (c *TestClient) Disconnect(tb testing.TB) {
	tb.Helper()

	if c.pollCancel != nil {
		c.pollCancel()
		<-c.pollDone
	}
}

// Reconnect registers and starts a new long-poll session.
// Call [TestClient.Disconnect] first, or this will disconnect automatically.
func (c *TestClient) Reconnect(tb testing.TB) {
	tb.Helper()

	// Cancel any existing poll.
	if c.pollCancel != nil {
		c.pollCancel()

		select {
		case <-c.pollDone:
		case <-time.After(5 * time.Second):
			tb.Fatalf("servertest: Reconnect(%s): old poll did not exit", c.Name)
		}
	}

	// Clear stale netmap data and drain pending updates so that callers
	// like [TestClient.WaitForPeers] actually wait for the new session's
	// map instead of returning immediately based on the old session's
	// cached state.
	c.resetNetmapState()

	// Re-register and start polling again.
	c.register(tb)

	c.startPoll(tb)
}

// LogoutAndDisconnect sends a logout [tailcfg.RegisterRequest] (expiry in
// the past) and tears down the long-poll session, mirroring what
// tailscaled does on `tailscale logout`. The server marks the node
// expired; the poll teardown then triggers the server's disconnect
// grace period, after which the node goes offline.
//
// Safe to call from non-test goroutines: errors are returned, not
// fataled, so many clients can log out concurrently.
func (c *TestClient) LogoutAndDisconnect(ctx context.Context) error {
	err := c.direct.TryLogout(ctx)
	if err != nil {
		return fmt.Errorf("servertest: TryLogout(%s): %w", c.Name, err)
	}

	if c.pollCancel != nil {
		c.pollCancel()

		select {
		case <-c.pollDone:
		case <-ctx.Done():
			return fmt.Errorf("servertest: LogoutAndDisconnect(%s): poll did not exit: %w", c.Name, ctx.Err())
		}
	}

	return nil
}

// ReloginAndPoll logs the client back in after [TestClient.LogoutAndDisconnect]
// and starts a fresh long-poll session. [controlclient.Direct.TryLogout] cleared
// the persisted node key, so this generates a new NodeKey and re-registers with
// the same pre-auth key and machine key — the same shape as a real client
// running `tailscale up --authkey=...` after a logout.
//
// Safe to call from non-test goroutines.
func (c *TestClient) ReloginAndPoll(ctx context.Context) error {
	url, err := c.direct.TryLogin(ctx, controlclient.LoginDefault)
	if err != nil {
		return fmt.Errorf("servertest: TryLogin(%s): %w", c.Name, err)
	}

	if url != "" {
		return fmt.Errorf("%w: TryLogin(%s): %q", errUnexpectedAuthURL, c.Name, url)
	}

	c.resetNetmapState()
	c.startPollLoop()

	return nil
}

// RestartPoll tears down the current long-poll session and immediately
// starts a new one without re-registering, the way tailscaled restarts
// its map poll on state-machine transitions (pause/unpause around
// login). The node key is unchanged; the server sees a rapid
// disconnect/reconnect.
//
// Safe to call from non-test goroutines.
func (c *TestClient) RestartPoll(ctx context.Context) error {
	if c.pollCancel != nil {
		c.pollCancel()

		select {
		case <-c.pollDone:
		case <-ctx.Done():
			return fmt.Errorf("servertest: RestartPoll(%s): old poll did not exit: %w", c.Name, ctx.Err())
		}
	}

	c.startPollLoop()

	return nil
}

// ReconnectAfter disconnects, waits for d, then reconnects.
// The timer works correctly with testing/synctest for
// time-controlled tests.
func (c *TestClient) ReconnectAfter(tb testing.TB, d time.Duration) {
	tb.Helper()
	c.Disconnect(tb)

	timer := time.NewTimer(d)
	defer timer.Stop()

	<-timer.C
	c.Reconnect(tb)
}

// --- State accessors ---

// Netmap returns the latest [netmap.NetworkMap], or nil if none received yet.
func (c *TestClient) Netmap() *netmap.NetworkMap {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return c.netmap
}

// WaitForPeers blocks until the client sees at least n peers,
// or until timeout expires.
func (c *TestClient) WaitForPeers(tb testing.TB, n int, timeout time.Duration) {
	tb.Helper()

	c.waitForPeers(tb, n, timeout, "WaitForPeers", func(got int) bool { return got >= n })
}

// WaitForUpdate blocks until the next netmap update arrives or timeout.
func (c *TestClient) WaitForUpdate(tb testing.TB, timeout time.Duration) *netmap.NetworkMap {
	tb.Helper()

	select {
	case nm := <-c.updates:
		return nm
	case <-time.After(timeout):
		tb.Fatalf("servertest: WaitForUpdate(%s): timeout after %v", c.Name, timeout)

		return nil
	}
}

// NodeIDString returns the client's node id as the HTTP APIs render it,
// read from the self node of the current netmap.
func (c *TestClient) NodeIDString() string {
	nm := c.Netmap()
	if nm == nil || !nm.SelfNode.Valid() {
		return ""
	}

	return strconv.FormatInt(int64(nm.SelfNode.ID()), 10)
}

// Peers returns the current peer list, or nil.
func (c *TestClient) Peers() []tailcfg.NodeView {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.netmap == nil {
		return nil
	}

	return c.netmap.Peers
}

// PeerByName finds a peer by hostname. Returns the peer and true
// if found, zero value and false otherwise.
func (c *TestClient) PeerByName(hostname string) (tailcfg.NodeView, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.netmap == nil {
		return tailcfg.NodeView{}, false
	}

	for _, p := range c.netmap.Peers {
		hi := p.Hostinfo()
		if hi.Valid() && hi.Hostname() == hostname {
			return p, true
		}
	}

	return tailcfg.NodeView{}, false
}

// PeerNames returns the hostnames of all current peers.
func (c *TestClient) PeerNames() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.netmap == nil {
		return nil
	}

	names := make([]string, 0, len(c.netmap.Peers))
	for _, p := range c.netmap.Peers {
		hi := p.Hostinfo()
		if hi.Valid() {
			names = append(names, hi.Hostname())
		}
	}

	return names
}

// UpdateCount returns the total number of full netmap updates received.
func (c *TestClient) UpdateCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return len(c.history)
}

// History returns a copy of all [netmap.NetworkMap] snapshots in order.
func (c *TestClient) History() []*netmap.NetworkMap {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]*netmap.NetworkMap, len(c.history))
	copy(out, c.history)

	return out
}

// SelfName returns the self node's hostname from the latest netmap.
func (c *TestClient) SelfName() string {
	nm := c.Netmap()
	if nm == nil || !nm.SelfNode.Valid() {
		return ""
	}

	return nm.SelfNode.Hostinfo().Hostname()
}

// WaitForPeerCount blocks until the client sees exactly n peers.
func (c *TestClient) WaitForPeerCount(tb testing.TB, n int, timeout time.Duration) {
	tb.Helper()

	c.waitForPeers(tb, n, timeout, "WaitForPeerCount", func(got int) bool { return got == n })
}

// WaitForCondition blocks until condFn returns true on the latest
// netmap, or until timeout expires. This is useful for waiting for
// specific state changes (e.g., peer going offline).
func (c *TestClient) WaitForCondition(
	tb testing.TB,
	desc string,
	timeout time.Duration,
	condFn func(*netmap.NetworkMap) bool,
) {
	tb.Helper()

	deadline := time.After(timeout)

	for {
		if nm := c.Netmap(); nm != nil && condFn(nm) {
			return
		}

		select {
		case <-c.updates:
			// Check again.
		case <-deadline:
			tb.Fatalf("servertest: WaitForCondition(%s, %q): timeout after %v; last netmap:\n%s",
				c.Name, desc, timeout, describeNetmap(c.Netmap()))
		}
	}
}

// describeNetmap renders a netmap for a failure message: the self node,
// every peer, and the packet filter, so a wait that timed out says what
// the client was looking at.
func describeNetmap(nm *netmap.NetworkMap) string {
	if nm == nil {
		return "(none)"
	}

	return nm.Concise() + fmt.Sprintf("filter: %d rules\n", len(nm.PacketFilter))
}

// Direct returns the underlying [controlclient.Direct] for
// advanced operations like [controlclient.Direct.SetHostinfo] or SendUpdate.
func (c *TestClient) Direct() *controlclient.Direct {
	return c.direct
}

// String implements [fmt.Stringer] for debug output.
func (c *TestClient) String() string {
	nm := c.Netmap()
	if nm == nil {
		return fmt.Sprintf("TestClient(%s, no netmap)", c.Name)
	}

	return fmt.Sprintf("TestClient(%s, %d peers)", c.Name, len(nm.Peers))
}

// startPendingLogin performs the first register request of an interactive
// login and starts the follow-up long-poll in the background. The follow-up
// is cancelled when the test ends if nothing completes it before.
func (c *TestClient) startPendingLogin(tb testing.TB) *PendingLogin {
	tb.Helper()

	ctx, cancel := context.WithTimeout(tb.Context(), 10*time.Second)
	defer cancel()

	authURL, err := c.direct.TryLogin(ctx, controlclient.LoginDefault)
	if err != nil {
		tb.Fatalf("servertest: TryLogin(%s): %v", c.Name, err)
	}

	if authURL == "" {
		tb.Fatalf("servertest: TryLogin(%s): expected an auth URL for an interactive login", c.Name)
	}

	authID, err := types.AuthIDFromString(strings.TrimPrefix(authURL, c.server.URL+"/register/"))
	if err != nil {
		tb.Fatalf("servertest: TryLogin(%s): auth URL %q does not carry an auth id: %v", c.Name, authURL, err)
	}

	followupCtx, followupCancel := context.WithCancel(tb.Context())
	tb.Cleanup(followupCancel)

	pl := &PendingLogin{
		AuthURL: authURL,
		AuthID:  authID,
		client:  c,
		done:    make(chan loginResult, 1),
	}

	go func() {
		newURL, err := c.direct.WaitLoginURL(followupCtx, authURL)
		pl.done <- loginResult{url: newURL, err: err}
	}()

	return pl
}

// register performs the initial [controlclient.Direct.TryLogin] to register the client.
func (c *TestClient) register(tb testing.TB) {
	tb.Helper()

	ctx, cancel := context.WithTimeout(tb.Context(), 10*time.Second)
	defer cancel()

	url, err := c.direct.TryLogin(ctx, controlclient.LoginDefault)
	if err != nil {
		tb.Fatalf("servertest: TryLogin(%s): %v", c.Name, err)
	}

	if url != "" {
		tb.Fatalf(
			"servertest: TryLogin(%s): unexpected auth URL: %s (expected auto-auth with preauth key)",
			c.Name,
			url,
		)
	}
}

// startPoll begins the long-poll [tailcfg.MapRequest] loop.
func (c *TestClient) startPoll(tb testing.TB) {
	tb.Helper()

	c.startPollLoop()
}

// startPollLoop creates a fresh poll context and launches the background
// [controlclient.Direct.PollNetMap] goroutine, which blocks until the
// context is cancelled or the server closes the connection.
func (c *TestClient) startPollLoop() {
	c.pollCtx, c.pollCancel = context.WithCancel(context.Background())
	c.pollDone = make(chan struct{})

	go func() {
		defer close(c.pollDone)

		_ = c.direct.PollNetMap(c.pollCtx, c)
	}()
}

// resetNetmapState clears the cached netmap and drains any pending
// updates from a previous session so that convergence waits observe
// only the new session's maps.
func (c *TestClient) resetNetmapState() {
	c.mu.Lock()
	c.netmap = nil
	c.mu.Unlock()

	for {
		select {
		case <-c.updates:
		default:
			return
		}
	}
}

// cleanup releases all resources.
func (c *TestClient) cleanup() {
	if c.pollCancel != nil {
		c.pollCancel()
	}

	if c.pollDone != nil {
		// Wait for PollNetMap to exit, but don't hang.
		select {
		case <-c.pollDone:
		case <-time.After(5 * time.Second):
		}
	}

	if c.direct != nil {
		c.direct.Close()
	}

	if c.dialer != nil {
		c.dialer.Close()
	}

	if c.bus != nil {
		c.bus.Close()
	}
}

// waitForPeers blocks until match reports the current peer count
// satisfies the caller's predicate, or until timeout expires. op
// names the caller for the timeout failure message.
func (c *TestClient) waitForPeers(
	tb testing.TB,
	n int,
	timeout time.Duration,
	op string,
	match func(got int) bool,
) {
	tb.Helper()

	deadline := time.After(timeout)

	for {
		if nm := c.Netmap(); nm != nil && match(len(nm.Peers)) {
			return
		}

		select {
		case <-c.updates:
			// Check again.
		case <-deadline:
			nm := c.Netmap()

			got := 0
			if nm != nil {
				got = len(nm.Peers)
			}

			tb.Fatalf("servertest: %s(%s, %d): timeout after %v (got %d peers)", op, c.Name, n, timeout, got)
		}
	}
}

// c2nHandler answers the control-to-node requests the server sends
// through the map stream, the way the real client's handler does for the
// posture identity request.
func c2nHandler(cc *clientConfig) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /posture/identity", func(w http.ResponseWriter, _ *http.Request) {
		resp := tailcfg.C2NPostureIdentityResponse{PostureDisabled: !cc.posture}
		if cc.posture {
			resp.SerialNumbers = cc.serials
		}

		w.Header().Set("Content-Type", "application/json")

		_ = json.NewEncoder(w).Encode(resp)
	})

	return mux
}
