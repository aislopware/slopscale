package servertest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"tailscale.com/control/controlclient"
	_ "tailscale.com/feature/c2n" // answers c2n pings
	"tailscale.com/health"
	"tailscale.com/ipn"
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

	server     *TestServer
	direct     *controlclient.Direct
	authKey    string
	loginFlags controlclient.LoginFlags
	user       *types.User

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
	// loginFlags go with every register request; LoginEphemeral asks the
	// server to make the node ephemeral, as a tailscaled with mem: state does.
	loginFlags controlclient.LoginFlags
	hostname   string
	tags       []string
	user       *types.User
	authKey    string
	hostinfo   func(*tailcfg.Hostinfo)
	// debugFlags go with every map request, the way tailscaled reports
	// warn-ip-forwarding-off and its kin.
	debugFlags []string
	serials    []string
	posture    bool
	// attestationKey signs every map request the way a TPM-backed key
	// does; see [WithHardwareAttestation].
	attestationKey key.HardwareAttestationKey
	// services is what the client answers to the server's c2n
	// /vip-services request, with the hash it stamps in its Hostinfo.
	services []tailcfg.VIPService

	// c2nMu guards the answers the c2n handler mutates: an update the
	// server starts and the preferences it edits.
	c2nMu sync.Mutex
	// clientUpdate is what the client answers to /update; nil makes it
	// answer as a build without the client update feature does.
	clientUpdate *tailcfg.C2NUpdateResponse
	// clientHealth is what the client answers to /debug/health.
	clientHealth health.State
	// sshUsernames are the login hints the client suggests.
	sshUsernames []string
	// appConnectorRoutes are the domains the client answers for as an app
	// connector, with the addresses it resolved for each.
	appConnectorRoutes map[string][]netip.Addr
	// tlsCert is what the client answers to /tls-cert-status; nil makes it
	// report a certificate it never fetched.
	tlsCert *tailcfg.C2NTLSCertInfo
	// prefs are the preferences the client serves and, while remoteConfig
	// is on, lets the server edit through its local API.
	prefs        ipn.Prefs
	remoteConfig bool
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

// WithVIPServices makes the client report the services as its serve
// configuration: it stamps their hash in its Hostinfo, the way tailscaled
// does, and answers the server's c2n request with the list.
func WithVIPServices(services ...tailcfg.VIPService) ClientOption {
	return func(c *clientConfig) {
		c.services = services
	}
}

// WithClientUpdate makes the client answer the server's c2n /update
// request with resp. A POST flips Started when the answer says the update
// is both enabled and supported, the way the real client does once it
// starts one. Without the option the client answers as a build without
// the client update feature does.
func WithClientUpdate(resp tailcfg.C2NUpdateResponse) ClientOption {
	return func(c *clientConfig) { c.clientUpdate = &resp }
}

// WithClientHealth makes the client report these warnings when the server
// asks for its health.
func WithClientHealth(state health.State) ClientOption {
	return func(c *clientConfig) { c.clientHealth = state }
}

// WithSSHUsernames makes the client suggest these logins for a Tailscale
// SSH session to it.
func WithSSHUsernames(usernames ...string) ClientOption {
	return func(c *clientConfig) { c.sshUsernames = usernames }
}

// WithAppConnectorRoutes makes the client report these learned routes as
// an app connector.
func WithAppConnectorRoutes(domains map[string][]netip.Addr) ClientOption {
	return func(c *clientConfig) { c.appConnectorRoutes = domains }
}

// WithTLSCert makes the client report info about the certificate it
// caches for its own name.
func WithTLSCert(info tailcfg.C2NTLSCertInfo) ClientOption {
	return func(c *clientConfig) { c.tlsCert = &info }
}

// WithClientPrefs gives the client the preferences it serves. With
// remoteConfig the client reports the opt-in in its Hostinfo and lets the
// server edit them through the c2n local API proxy, as a machine that ran
// `tailscale set --remote-config` does; without it the proxy answers 403.
func WithClientPrefs(prefs ipn.Prefs, remoteConfig bool) ClientOption {
	return func(c *clientConfig) {
		c.prefs = prefs
		c.remoteConfig = remoteConfig
	}
}

// WithAuthKey registers with the given pre-auth key instead of one the
// server mints for the client, for keys with non-default properties such
// as preauthorized=false.
func WithAuthKey(authKey string) ClientOption {
	return func(cc *clientConfig) { cc.authKey = authKey }
}

// WithEphemeral makes the client register as an ephemeral node through
// an ephemeral pre-auth key.
func WithEphemeral() ClientOption {
	return func(c *clientConfig) { c.ephemeral = true }
}

// WithEphemeralLogin makes the client ask to be ephemeral in its register
// request ([tailcfg.RegisterRequest.Ephemeral]), as a tailscaled with mem:
// state or a tsnet server with Ephemeral does, whatever key it uses.
func WithEphemeralLogin() ClientOption {
	return func(c *clientConfig) { c.loginFlags |= controlclient.LoginEphemeral }
}

// WithDebugFlags sets the [tailcfg.MapRequest.DebugFlags] the client sends
// with every map request, such as warn-ip-forwarding-off.
func WithDebugFlags(flags ...string) ClientOption {
	return func(c *clientConfig) { c.debugFlags = flags }
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

	if len(cc.services) > 0 {
		hostinfo.ServicesHash = VIPServicesHash(cc.services)
	}

	if cc.remoteConfig {
		hostinfo.RemoteConfig = true
	}

	direct, err := controlclient.NewDirect(controlclient.Options{
		Persist:              persist.Persist{AttestationKey: cc.attestationKey},
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
		DebugFlags:           cc.debugFlags,
	})
	if err != nil {
		tb.Fatalf("servertest: NewDirect(%s): %v", name, err)
	}

	tc := &TestClient{
		Name:       name,
		server:     server,
		direct:     direct,
		authKey:    authKey,
		loginFlags: cc.loginFlags,
		updates:    make(chan *netmap.NetworkMap, 64),
		bus:        bus,
		dialer:     dialer,
		tracker:    tracker,
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
func NewPendingLogin(tb testing.TB, server *TestServer, name string, opts ...ClientOption) *PendingLogin {
	tb.Helper()

	cc := &clientConfig{hostname: name}
	for _, o := range opts {
		o(cc)
	}

	tc := newTestClient(tb, server, name, cc.hostname, "", cc)

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

// PollEnded is closed when the current long poll returns, whether the
// server ended it or the client cancelled it.
func (c *TestClient) PollEnded() <-chan struct{} {
	return c.pollDone
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

// NodePrivateKey is the client's node key, what a relay client
// identifies itself with.
func (c *TestClient) NodePrivateKey() key.NodePrivate {
	return c.direct.GetPersist().PrivateNodeKey()
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

	authURL, err := c.direct.TryLogin(ctx, c.loginFlags)
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

	url, err := c.direct.TryLogin(ctx, c.loginFlags)
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
	mux.HandleFunc("GET /vip-services", func(w http.ResponseWriter, _ *http.Request) {
		resp := tailcfg.C2NVIPServicesResponse{ServicesHash: VIPServicesHash(cc.services)}
		for i := range cc.services {
			resp.VIPServices = append(resp.VIPServices, &cc.services[i])
		}

		w.Header().Set("Content-Type", "application/json")

		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("GET /posture/identity", func(w http.ResponseWriter, _ *http.Request) {
		resp := tailcfg.C2NPostureIdentityResponse{PostureDisabled: !cc.posture}
		if cc.posture {
			resp.SerialNumbers = cc.serials
		}

		w.Header().Set("Content-Type", "application/json")

		_ = json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("GET /debug/health", func(w http.ResponseWriter, _ *http.Request) {
		writeC2NJSON(w, cc.clientHealth)
	})
	mux.HandleFunc("GET /ssh/usernames", func(w http.ResponseWriter, _ *http.Request) {
		writeC2NJSON(w, tailcfg.C2NSSHUsernamesResponse{Usernames: cc.sshUsernames})
	})
	mux.HandleFunc("GET /appconnector/routes", func(w http.ResponseWriter, _ *http.Request) {
		writeC2NJSON(w, tailcfg.C2NAppConnectorDomainRoutesResponse{Domains: cc.appConnectorRoutes})
	})
	mux.HandleFunc("GET /tls-cert-status", func(w http.ResponseWriter, _ *http.Request) {
		info := tailcfg.C2NTLSCertInfo{Missing: true}
		if cc.tlsCert != nil {
			info = *cc.tlsCert
		}

		writeC2NJSON(w, info)
	})

	registerC2NUpdate(mux, cc)
	registerC2NPrefs(mux, cc)
	registerC2NDiagnostics(mux)

	return mux
}

// registerC2NUpdate answers the remote update request. A POST starts the
// update when the machine allows one, and records that it did so a later
// GET reports it, the way the real client does.
func registerC2NUpdate(mux *http.ServeMux, cc *clientConfig) {
	mux.HandleFunc("/update", func(w http.ResponseWriter, r *http.Request) {
		cc.c2nMu.Lock()
		defer cc.c2nMu.Unlock()

		if cc.clientUpdate == nil {
			http.Error(w, "clientupdate extension not found", http.StatusInternalServerError)

			return
		}

		resp := *cc.clientUpdate

		if r.Method == http.MethodPost {
			switch {
			case !resp.Enabled:
				resp.Err = "not enabled"
			case !resp.Supported:
				resp.Err = "not supported"
			default:
				cc.clientUpdate.Started = true
				resp.Started = true
				resp.Err = ""
			}
		}

		writeC2NJSON(w, resp)
	})
}

// registerC2NPrefs answers the preferences the client serves for debugging
// and, when the machine opted into remote configuration, the local API
// proxy the server edits them through.
func registerC2NPrefs(mux *http.ServeMux, cc *clientConfig) {
	mux.HandleFunc("GET /debug/prefs", func(w http.ResponseWriter, _ *http.Request) {
		cc.c2nMu.Lock()
		defer cc.c2nMu.Unlock()

		writeC2NJSON(w, cc.prefs)
	})
	mux.HandleFunc("/remoteapi/localapi/v0/prefs", func(w http.ResponseWriter, r *http.Request) {
		if !cc.remoteConfig {
			http.Error(w, "remote config not enabled by local machine", http.StatusForbidden)

			return
		}

		cc.c2nMu.Lock()
		defer cc.c2nMu.Unlock()

		if r.Method == http.MethodPatch {
			masked := new(ipn.MaskedPrefs)

			err := json.NewDecoder(r.Body).Decode(masked)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)

				return
			}

			cc.prefs.ApplyEdits(masked)
		}

		writeC2NJSON(w, cc.prefs)
	})
}

// registerC2NDiagnostics answers the support dumps with stand-ins shaped
// like the real ones: the server hands them to the operator untouched, so
// only the content type and the fact that they arrive matter.
func registerC2NDiagnostics(mux *http.ServeMux) {
	mux.HandleFunc("GET /debug/netmap", func(w http.ResponseWriter, _ *http.Request) {
		writeC2NJSON(w, tailcfg.C2NDebugNetmapResponse{Current: json.RawMessage(`{"Peers":[]}`)})
	})
	mux.HandleFunc("GET /debug/tka/log", func(w http.ResponseWriter, _ *http.Request) {
		writeC2NJSON(w, map[string]any{"updates": []any{}})
	})
	mux.HandleFunc("GET /debug/metrics", func(w http.ResponseWriter, _ *http.Request) {
		writeC2NText(w, "servertest_metric 1\n")
	})
	mux.HandleFunc("GET /debug/goroutines", func(w http.ResponseWriter, _ *http.Request) {
		writeC2NText(w, "goroutine 1 [running]:\n")
	})
	mux.HandleFunc("POST /sockstats", func(w http.ResponseWriter, _ *http.Request) {
		writeC2NText(w, "logid: servertest\n")
	})
}

func writeC2NJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")

	_ = json.NewEncoder(w).Encode(v)
}

func writeC2NText(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/plain")

	_, _ = io.WriteString(w, body)
}

// VIPServicesHash is the hash tailscaled stamps in Hostinfo.ServicesHash
// for a service list: the SHA-256 of its JSON encoding, hex encoded, and
// empty for no services.
func VIPServicesHash(services []tailcfg.VIPService) string {
	if len(services) == 0 {
		return ""
	}

	ptrs := make([]*tailcfg.VIPService, 0, len(services))
	for i := range services {
		ptrs = append(ptrs, &services[i])
	}

	h := sha256.New()

	_ = json.NewEncoder(h).Encode(ptrs)

	return hex.EncodeToString(h.Sum(nil))
}
