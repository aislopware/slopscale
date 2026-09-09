package hscontrol

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/ingress"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/rs/zerolog/log"
)

// ingressKeyTTL is how long the pre-auth key minted for the embedded
// ingress node's first login lives; tsnet ignores it once the node has
// state.
const ingressKeyTTL = 10 * time.Minute

// ingressBackoff is the pause before the ingress node is started again
// after it fails.
const ingressBackoff = 30 * time.Second

// runFunnelIngress joins the tailnet as the embedded Funnel ingress and
// delivers public connections until the context ends. A failure to come
// up is logged and retried, because the ingress must not take the
// control server down with it.
func (h *Slopscale) runFunnelIngress(ctx context.Context) error {
	for {
		err := h.serveFunnelIngress(ctx)
		if err == nil || ctx.Err() != nil {
			// Either the ingress stopped on its own or the server is
			// shutting down; both are the normal exit.
			return nil //nolint:nilerr // the context ending is the normal exit
		}

		log.Error().Err(err).Dur("retryIn", ingressBackoff).Msg("Funnel ingress node failed")

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(ingressBackoff):
		}
	}
}

// serveFunnelIngress runs one life of the ingress node.
func (h *Slopscale) serveFunnelIngress(ctx context.Context) error {
	authKey, err := h.ingressAuthKey()
	if err != nil {
		return err
	}

	defer h.funnelAddrs.Store(nil)

	return ingress.Run(ctx, ingress.Node{
		ControlURL:  h.cfg.ServerURL,
		AuthKey:     authKey,
		Hostname:    types.FunnelIngressHostname,
		StateDir:    h.cfg.Funnel.StateDir,
		ListenAddrs: h.cfg.Funnel.ListenAddrs,
	}, func(addrs []net.Addr) {
		bound := make([]string, 0, len(addrs))
		for _, addr := range addrs {
			bound = append(bound, addr.String())
		}

		h.funnelAddrs.Store(&bound)
	})
}

// ingressAuthKey mints the tagged, single-use key the ingress node
// registers with: pre-authorized so it needs no approval, short-lived so
// an unused one expires on its own.
func (h *Slopscale) ingressAuthKey() (string, error) {
	expiry := time.Now().Add(ingressKeyTTL)

	key, err := h.state.CreatePreAuthKeyFromSpec(types.PreAuthKeySpec{
		Tags:          []string{types.FunnelIngressTag},
		Preauthorized: true,
		Expiration:    &expiry,
	})
	if err != nil {
		return "", err
	}

	return key.Key, nil
}

// FunnelIngressAddrs returns the addresses the embedded ingress is
// listening on, or nil while it is not.
func (h *Slopscale) FunnelIngressAddrs() []string {
	if addrs := h.funnelAddrs.Load(); addrs != nil {
		return *addrs
	}

	return nil
}

// StartFunnelIngressForTest joins the tailnet as the embedded ingress,
// as [Slopscale.Serve] does, until the test ends.
func (h *Slopscale) StartFunnelIngressForTest(tb testing.TB) {
	tb.Helper()

	ctx, cancel := context.WithCancel(tb.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		_ = h.runFunnelIngress(ctx)
	}()

	tb.Cleanup(func() {
		cancel()
		<-done
	})
}
