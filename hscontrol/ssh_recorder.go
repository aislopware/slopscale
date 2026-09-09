package hscontrol

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strconv"
	"testing"
	"time"

	"github.com/juanfont/headscale/hscontrol/recorder"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/rs/zerolog/log"
	"tailscale.com/tsnet"
	"tailscale.com/types/logger"
)

// recorderKeyTTL is how long the pre-auth key minted for the embedded
// recorder's first login lives. tsnet ignores it once the node has
// state, so the key only matters on the very first start.
const recorderKeyTTL = 10 * time.Minute

// recorderBackoff is the pause before the recorder node is started
// again after it fails.
const recorderBackoff = 30 * time.Second

// newRecorder builds the recorder that indexes and serves recordings.
// It exists whether or not the embedded node runs, so old recordings
// stay reachable after the node is switched off.
func newRecorder(cfg *types.Config, st recorder.Store, nodes recorder.NodeLookup) *recorder.Recorder {
	return recorder.New(
		cfg.SSHRecording.Dir,
		cfg.SSHRecording.Retention,
		cfg.SSHRecording.SessionLimit(),
		st,
		nodes,
	)
}

// runSSHRecorder joins the tailnet as the embedded recorder node and
// serves the upload protocol on it until the context ends. A failure to
// come up is logged and retried, because the recorder must not take the
// control server down with it.
func (h *Headscale) runSSHRecorder(ctx context.Context) error {
	go h.recorder.RunSweeper(ctx)

	for {
		err := h.serveSSHRecorder(ctx)
		if err == nil {
			return nil
		}

		if ctx.Err() != nil {
			// Shutting down; the error is the listener closing.
			return nil //nolint:nilerr // the context ending is the normal exit
		}

		log.Error().Err(err).Dur("retryIn", recorderBackoff).Msg("SSH recorder node failed")

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(recorderBackoff):
		}
	}
}

// serveSSHRecorder runs one life of the recorder node.
func (h *Headscale) serveSSHRecorder(ctx context.Context) error {
	authKey, err := h.recorderAuthKey()
	if err != nil {
		return err
	}

	node := &tsnet.Server{
		Dir:        h.cfg.SSHRecording.StateDir,
		Hostname:   types.SSHRecorderHostname,
		ControlURL: h.cfg.ServerURL,
		AuthKey:    authKey,
		Logf:       logger.Discard,
	}
	defer node.Close()

	status, err := node.Up(ctx)
	if err != nil {
		return fmt.Errorf("starting recorder node: %w", err)
	}

	listener, err := node.Listen("tcp", ":"+strconv.Itoa(recorder.Port))
	if err != nil {
		return fmt.Errorf("listening on recorder node: %w", err)
	}

	addresses := make([]string, 0, len(status.TailscaleIPs))
	for _, addr := range status.TailscaleIPs {
		addresses = append(addresses, addr.String())
	}

	log.Info().
		Strs("addresses", addresses).
		Str("hostname", types.SSHRecorderHostname).
		Str("dir", h.cfg.SSHRecording.Dir).
		Msg("SSH session recorder listening")

	server := h.recorder.Server()

	go func() {
		<-ctx.Done()

		_ = server.Close()
	}()

	err = server.Serve(listener)
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serving recorder: %w", err)
	}

	return nil
}

// recorderAuthKey mints the tagged, single-use key the recorder node
// registers with. The key is pre-authorized so the node needs no
// approval, and short-lived so an unused one expires on its own.
func (h *Headscale) recorderAuthKey() (string, error) {
	expiry := time.Now().Add(recorderKeyTTL)

	key, err := h.state.CreatePreAuthKeyFromSpec(types.PreAuthKeySpec{
		Tags:          []string{types.SSHRecorderTag},
		Preauthorized: true,
		Expiration:    &expiry,
	})
	if err != nil {
		return "", fmt.Errorf("minting recorder auth key: %w", err)
	}

	return key.Key, nil
}

// SSHRecorderHandlerForTest is the upload service the embedded node
// serves, for tests that post a session without a tailnet. Such a test
// posts from loopback, which is no node, and the recorder takes uploads
// from nodes only; this handler admits the source without naming a node, so
// the recording is attributed to nothing exactly as it would be in
// production when the source cannot be named.
func (h *Headscale) SSHRecorderHandlerForTest() http.Handler {
	lookup := func(addr netip.Addr) (types.NodeView, bool) {
		node, ok := h.state.NodeByIP(addr)
		if ok {
			return node, true
		}

		return types.NodeView{}, true
	}

	return newRecorder(h.cfg, h.state, lookup).Handler()
}

// StartSSHRecorderForTest joins the tailnet as the embedded recorder,
// as [Headscale.Serve] does, until the test ends.
func (h *Headscale) StartSSHRecorderForTest(tb testing.TB) {
	tb.Helper()

	ctx, cancel := context.WithCancel(tb.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = h.runSSHRecorder(ctx)
	}()

	tb.Cleanup(func() {
		cancel()
		<-done
	})
}
