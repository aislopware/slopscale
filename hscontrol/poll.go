package hscontrol

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/aislopware/slopscale/hscontrol/util/zlog/zf"
	"github.com/aislopware/slopscale/hscontrol/wire"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"tailscale.com/tailcfg"
	"tailscale.com/util/zstdframe"
)

const (
	keepAliveInterval = 50 * time.Second
)

// errMapResponseTooLarge is returned when a marshalled map response would
// overflow the uint32 length prefix written ahead of it on the wire.
var errMapResponseTooLarge = errors.New("map response body exceeds uint32 length prefix")

type contextKey string

const nodeNameContextKey = contextKey("nodeName")

type mapSession struct {
	h      *Slopscale
	req    tailcfg.MapRequest
	ctx    context.Context //nolint:containedctx // mapSession is a per-stream session struct whose lifetime matches ctx
	capVer tailcfg.CapabilityVersion

	ch             chan *tailcfg.MapResponse
	cancelCh       chan struct{}
	cancelChClosed atomic.Bool

	keepAlive       time.Duration
	keepAliveTicker *time.Ticker

	node types.NodeView
	w    http.ResponseWriter

	log zerolog.Logger
}

func (h *Slopscale) newMapSession(
	ctx context.Context,
	req tailcfg.MapRequest,
	w http.ResponseWriter,
	node types.NodeView,
) *mapSession {
	//nolint:gosec // weak random is fine for jitter
	ka := keepAliveInterval + (time.Duration(rand.IntN(9000)) * time.Millisecond)

	return &mapSession{
		h:      h,
		ctx:    ctx,
		req:    req,
		w:      w,
		node:   node,
		capVer: req.Version,

		ch:       make(chan *tailcfg.MapResponse, h.cfg.Tuning.NodeMapSessionBufferedChanSize),
		cancelCh: make(chan struct{}),

		keepAlive:       ka,
		keepAliveTicker: nil,

		// The node is identified by id and name only: embedding the whole
		// record renders every key on every request, whether or not the
		// session ever logs.
		log: log.With().
			Str(zf.Component, "poll").
			Uint64(zf.NodeID, node.ID().Uint64()).
			Str(zf.NodeName, node.Hostname()).
			Bool(zf.OmitPeers, req.OmitPeers).
			Bool(zf.Stream, req.Stream).
			Logger(),
	}
}

func (m *mapSession) isStreaming() bool {
	return m.req.Stream
}

func (m *mapSession) isEndpointUpdate() bool {
	return !m.req.Stream && m.req.OmitPeers
}

func (m *mapSession) resetKeepAlive() {
	m.keepAliveTicker.Reset(m.keepAlive)
}

func (m *mapSession) stopFromBatcher() {
	if m.cancelChClosed.CompareAndSwap(false, true) {
		close(m.cancelCh)
	}
}

// nodeGone reports whether the state no longer knows the session's node,
// which is how a poll tells a cancel from its node's deletion apart from
// one for a replaced stream or a shutdown.
func (m *mapSession) nodeGone() bool {
	_, ok := m.h.state.GetNodeByID(m.node.ID())

	return !ok
}

// nodeGoneExpiry is the key expiry a deleted node is told: far enough in
// the past that no client clock reads it as still valid.
var nodeGoneExpiry = time.Unix(0, 0).UTC()

// nodeGoneResponse is the map response for a node the server no longer
// knows: its own entry with the key expired. tailscaled reads an expired
// self key as NeedsLogin and stops polling, where a bare HTTP error is a
// temporary failure it retries forever (aislopware/slopscale#3410). The
// hosted control plane answers a deleted device the same way. Only the
// fields the client needs to recognise itself are filled in; node may be
// a bare key when the server never knew it.
func nodeGoneResponse(node types.NodeView) *tailcfg.MapResponse {
	now := time.Now()

	return &tailcfg.MapResponse{
		ControlTime: &now,
		Node: &tailcfg.Node{
			//nolint:gosec // NodeID is a database autoincrement value, int64 on SQLite/PostgreSQL, so it fits
			ID:                tailcfg.NodeID(node.ID()),
			StableID:          node.ID().StableID(),
			Name:              node.GivenName(),
			Key:               node.NodeKey(),
			KeyExpiry:         nodeGoneExpiry,
			Machine:           node.MachineKey(),
			DiscoKey:          node.DiscoKey(),
			Addresses:         node.Prefixes(),
			AllowedIPs:        node.Prefixes(),
			MachineAuthorized: true,
			Expired:           true,
		},
	}
}

// afterServeLongPoll is called when a long-polling session ends and the node
// is disconnected.
func (m *mapSession) afterServeLongPoll() {
	if m.node.IsEphemeral() {
		m.h.ephemeralGC.Schedule(m.node.ID(), m.h.cfg.Node.Ephemeral.InactivityTimeout)
	}
}

// serve handles non-streaming requests.
func (m *mapSession) serve() {
	// This is the mechanism where the node gives us information about its
	// current configuration.
	//
	// Process the [tailcfg.MapRequest] to update node state (endpoints, hostinfo, etc.)
	c, err := m.h.state.UpdateNodeFromMapRequest(m.node.ID(), m.req)
	if err != nil {
		httpError(m.w, err)
		return
	}

	m.h.Change(c)
	m.h.collectServicesIfStale(m.ctx, m.node.ID())

	// If OmitPeers is true and Stream is false
	// then the server will let clients update their endpoints without
	// breaking existing long-polling (Stream == true) connections.
	// In this case, the server can omit the entire response; the client
	// only checks the HTTP response status code.
	//
	// This is what Tailscale calls a Lite update, the client ignores
	// the response and just wants a 200.
	// !req.stream && req.OmitPeers
	if m.isEndpointUpdate() {
		m.w.WriteHeader(http.StatusOK)
		mapResponseEndpointUpdates.WithLabelValues("ok").Inc()

		return
	}

	// Stream off without OmitPeers asks for one MapResponse and then the
	// end of the connection ([tailcfg.MapRequest.Stream]); an empty 200
	// reads as EOF on the client's size prefix.
	resp, err := m.h.mapBatcher.FullMapResponse(m.node.ID(), m.capVer)
	if err != nil {
		httpError(m.w, err)

		return
	}

	err = m.writeMap(resp)
	if err != nil {
		m.log.Error().Caller().Err(err).Msg("writing the one-shot map response")
	}
}

// cleanupAfterLongPoll releases the long-poll session's [state.State.Connect]
// reservation and disconnects the node. connectGen is 0 when the session
// never reached [state.State.Connect], in which case there is no session to
// release.
func (m *mapSession) cleanupAfterLongPoll(connectGen uint64) {
	m.stopFromBatcher()

	stillConnected := m.h.mapBatcher.RemoveNode(m.node.ID(), m.ch)

	// This session never reached [state.State.Connect]; there is no
	// session to release. A deleted node has nothing to release either,
	// and waiting for it to reconnect would only hold the stream count
	// up, which is what kept the server from shutting down.
	if connectGen == 0 || m.nodeGone() {
		return
	}

	// When a node disconnects, it might rapidly reconnect (e.g. mobile clients, network weather).
	// Instead of immediately marking the node as offline, we wait a few seconds to see if it reconnects.
	// If it reconnects during the wait, the new session's Connect raises the
	// session count, so the release below keeps the node online.
	//
	// This avoids flapping nodes in the UI and unnecessary churn in the network.
	// This is not my favourite solution, but it kind of works in our eventually consistent world.
	//
	// When another session already replaced this one (stillConnected), skip
	// the wait — but never the release itself. A cancelled map request whose
	// handler ran late is exactly such a session: if it kept its session
	// acquired on this path, the surviving session's release could never
	// take the node offline (the relogin flake).
	if !stillConnected {
		// Wait up to 10 seconds for the node to reconnect.
		// 10 seconds was arbitrary chosen as a reasonable time to reconnect.
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		for range 10 {
			if m.h.mapBatcher.IsConnected(m.node.ID()) {
				break
			}

			<-ticker.C
		}
	}

	// Release this session. The node goes offline exactly when the last
	// live session is released, so releases from replaced or stale
	// sessions are harmless regardless of the order they run in.
	disconnectChanges, err := m.h.state.Disconnect(m.node.ID(), connectGen)
	if err != nil {
		m.log.Error().Caller().Err(err).Msg("failed to disconnect node")
	}

	if len(disconnectChanges) == 0 {
		return
	}

	m.h.Change(disconnectChanges...)
	m.afterServeLongPoll()
	m.log.Info().Caller().Str(zf.Chan, fmt.Sprintf("%p", m.ch)).Msg("node has disconnected")
}

// serveLongPoll ensures the node gets the appropriate updates from either
// polling or immediate responses.
func (m *mapSession) serveLongPoll() {
	m.log.Trace().Caller().Msg("long poll session started")

	// connectGen is set by [state.State.Connect] below and captured by the deferred cleanup closure.
	// Each Connect acquires one live session in state; the cleanup must release
	// it with exactly one [state.State.Disconnect] call, in every exit path, or
	// the node's session count leaks and it stays online forever.
	var connectGen uint64

	// Clean up the session when the client disconnects
	defer func() {
		m.cleanupAfterLongPoll(connectGen)
	}()

	// Set up the client stream
	m.h.clientStreamsOpen.Add(1)
	defer m.h.clientStreamsOpen.Done()

	ctx, cancel := context.WithCancel(context.WithValue(m.ctx, nodeNameContextKey, m.node.Hostname()))
	defer cancel()

	m.keepAliveTicker = time.NewTicker(m.keepAlive)

	// Process the initial [tailcfg.MapRequest] to update node state (endpoints, hostinfo, etc.)
	// This must be done BEFORE calling [state.State.Connect] to ensure routes are properly synchronized.
	// When nodes reconnect, they send their hostinfo with announced routes in the [tailcfg.MapRequest].
	// We need this data in [state.NodeStore] before [state.State.Connect] sets up the primary routes, because
	// [types.NodeView.SubnetRoutes] calculates the intersection of announced and approved routes. If we
	// call [state.State.Connect] first, [types.NodeView.SubnetRoutes] returns empty (no announced routes yet), causing
	// the node to be incorrectly removed from AvailableRoutes.
	mapReqChange, err := m.h.state.UpdateNodeFromMapRequest(m.node.ID(), m.req)
	if err != nil {
		m.log.Error().Caller().Err(err).Msg("failed to update node from initial MapRequest")
		// Write an explicit error rather than returning silently: a bare
		// return leaves net/http to send an empty 200, which the client
		// reads as "unexpected EOF" and retries forever (issue #3346).
		httpError(m.w, err)

		return
	}

	// Connect the node after its state has been updated.
	// We send two separate change notifications because these are distinct operations:
	// 1. [state.State.UpdateNodeFromMapRequest]: processes the client's reported state (routes, endpoints, hostinfo)
	// 2. [state.State.Connect]: marks the node online and recalculates primary routes based on the updated state
	// While this results in two notifications, it ensures route data is synchronized before
	// primary route selection occurs, which is critical for proper HA subnet router failover.
	var connectChanges []change.Change

	connectChanges, connectGen = m.h.state.Connect(m.node.ID())

	// Cancel ephemeral GC only after Connect succeeds. Cancelling at the start
	// of serveLongPoll left departed nodes without a deletion timer when a
	// reconnect attempt failed before Connect (issue #3382).
	if m.node.IsEphemeral() {
		m.h.ephemeralGC.Cancel(m.node.ID())
	}

	m.log.Info().Caller().Str(zf.Chan, fmt.Sprintf("%p", m.ch)).Msg("node has connected")

	// TODO(kradalby): Redo the comments here
	// Add node to batcher so it can receive updates,
	// adding this before connecting it to the state ensure that
	// it does not miss any updates that might be sent in the split
	// time between the node connecting and the batcher being ready.
	err = m.h.mapBatcher.AddNode(m.node.ID(), m.ch, m.capVer, m.stopFromBatcher)
	if err != nil {
		m.log.Error().Caller().Err(err).Msg("failed to add node to batcher")
		// Write an explicit error rather than returning silently: a bare
		// return leaves net/http to send an empty 200, which the client
		// reads as "unexpected EOF" and retries forever (issue #3346).
		httpError(m.w, err)

		return
	}

	m.log.Debug().Caller().Msg("node added to batcher")

	m.h.Change(mapReqChange)
	m.h.Change(connectChanges...)

	m.h.collectPostureOnConnect(ctx, m.node.ID())
	m.h.collectServicesIfStale(ctx, m.node.ID())

	// Loop through updates and continuously send them to the
	// client.
	for {
		// consume channels with update, keep alives or "batch" blocking signals
		select {
		case <-m.cancelCh:
			m.log.Trace().Caller().Msg("poll cancelled received")
			mapResponseEnded.WithLabelValues("cancelled").Inc()

			// A cancel for a node the state no longer knows comes from
			// its deletion; the client learns so from the last frame.
			if m.nodeGone() {
				err := m.writeMap(nodeGoneResponse(m.node))
				if err != nil {
					m.log.Error().Caller().Err(err).Msg("cannot write final map to deleted node")
				}
			}

			return

		case <-ctx.Done():
			m.log.Trace().Caller().Str(zf.Chan, fmt.Sprintf("%p", m.ch)).Msg("poll context done")
			mapResponseEnded.WithLabelValues("done").Inc()

			return

		// Consume updates sent to node
		case update, ok := <-m.ch:
			m.log.Trace().Caller().Bool(zf.OK, ok).Msg("received update from channel")

			if !ok {
				m.log.Trace().Caller().Msg("update channel closed, streaming session is likely being replaced")
				return
			}

			err := m.writeMap(update)
			if err != nil {
				m.log.Error().Caller().Err(err).Msg("cannot write update to client")
				return
			}

			m.log.Trace().Caller().Msg("update sent")
			m.resetKeepAlive()

		case <-m.keepAliveTicker.C:
			err := m.writeMap(&keepAlive)
			if err != nil {
				m.log.Error().Caller().Err(err).Msg("cannot write keep alive")
				return
			}

			if debugHighCardinalityMetrics {
				mapResponseLastSentSeconds.WithLabelValues("keepalive", m.node.ID().String()).
					Set(float64(time.Now().Unix()))
			}

			mapResponseSent.WithLabelValues("ok", "keepalive").Inc()
			m.resetKeepAlive()
		}
	}
}

// mapBuffers pools the buffers map responses are encoded and compressed
// into, so a stream reuses one allocation across responses instead of
// paying for a netmap-sized slice on each.
var mapBuffers = sync.Pool{New: func() any { return new(bytes.Buffer) }}

// maxPooledMapBuffer keeps one unusually large netmap from pinning memory
// for the life of the process: a buffer that grew past it is dropped. A
// full map is about a kilobyte per peer, so the cap sits well above the
// largest tailnet the pool is meant to serve.
const maxPooledMapBuffer = 8 << 20

var mapHeaderPlaceholder [reservedResponseHeaderSize]byte

func getMapBuffer() *bytes.Buffer {
	buf, ok := mapBuffers.Get().(*bytes.Buffer)
	if !ok {
		buf = new(bytes.Buffer)
	}

	buf.Reset()

	return buf
}

func putMapBuffer(buf *bytes.Buffer) {
	if buf.Cap() <= maxPooledMapBuffer {
		mapBuffers.Put(buf)
	}
}

// encodeMap writes msg into out behind the length header, compressed when
// the client asked for zstd.
func (m *mapSession) encodeMap(out *bytes.Buffer, msg *tailcfg.MapResponse) error {
	out.Write(mapHeaderPlaceholder[:])

	if m.req.Compress != util.ZstdCompression {
		return wire.MarshalWrite(out, msg)
	}

	raw := getMapBuffer()
	defer putMapBuffer(raw)

	err := wire.MarshalWrite(raw, msg)
	if err != nil {
		return err
	}

	out.Write(zstdframe.AppendEncode(out.AvailableBuffer(), raw.Bytes(), zstdframe.FastestCompression))

	return nil
}

// writeMap writes the map response to the client.
// It handles compression if requested and any headers that need to be set.
// It also handles flushing the response if the [http.ResponseWriter]
// implements [http.Flusher].
func (m *mapSession) writeMap(msg *tailcfg.MapResponse) error {
	out := getMapBuffer()
	defer putMapBuffer(out)

	err := m.encodeMap(out, msg)
	if err != nil {
		return fmt.Errorf("marshalling map response: %w", err)
	}

	data := out.Bytes()

	body := len(data) - reservedResponseHeaderSize
	if int64(body) > math.MaxUint32 {
		return fmt.Errorf("%w: %d bytes", errMapResponseTooLarge, body)
	}

	binary.LittleEndian.PutUint32(data, uint32(body))

	startWrite := time.Now()

	_, err = m.w.Write(data)
	if err != nil {
		return fmt.Errorf("writing map response: %w", err)
	}

	if m.isStreaming() {
		if f, ok := m.w.(http.Flusher); ok {
			f.Flush()
		} else {
			m.log.Error().Caller().Msg("responseWriter does not implement http.Flusher, cannot flush")
		}
	}

	// Runs for every frame including keepalives, so the fields are only
	// built when trace logging is on.
	if e := m.log.Trace(); e.Enabled() {
		e.Caller().
			Str(zf.Chan, fmt.Sprintf("%p", m.ch)).
			TimeDiff("timeSpent", time.Now(), startWrite).
			Str(zf.MachineKey, m.node.MachineKey().String()).
			Bool("keepalive", msg.KeepAlive).
			Msg("finished writing mapresp to node")
	}

	return nil
}

var keepAlive = tailcfg.MapResponse{
	KeepAlive: true,
}
