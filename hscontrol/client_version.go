package hscontrol

import (
	"context"
	"net/http"
	"time"

	"github.com/aislopware/slopscale/hscontrol/clientversion"
	"github.com/aislopware/slopscale/hscontrol/egress"
	"github.com/rs/zerolog/log"
)

// clientVersionTimeout bounds one fetch of the package listing.
const clientVersionTimeout = 30 * time.Second

// runClientVersionCheck reads the latest stable client version at start
// and every client_updates.interval, and tells every node when it
// changes; see [tailcfg.MapResponse.ClientVersion].
func (h *Slopscale) runClientVersionCheck(ctx context.Context) error {
	client := &http.Client{Transport: egress.Transport(), Timeout: clientVersionTimeout}

	ticker := time.NewTicker(h.cfg.ClientUpdates.Interval)
	defer ticker.Stop()

	for {
		h.checkClientVersion(ctx, client)

		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// checkClientVersion fetches the latest version once; a failed fetch
// keeps the last one.
func (h *Slopscale) checkClientVersion(ctx context.Context, client *http.Client) {
	latest, err := clientversion.Latest(ctx, client, clientversion.DefaultURL)
	if err != nil {
		if ctx.Err() == nil {
			log.Warn().Caller().Err(err).Msg("checking the latest client version failed; keeping the last one")
		}

		return
	}

	c, changed := h.state.SetLatestClientVersion(latest)
	if !changed {
		return
	}

	log.Info().Caller().Str("version", latest).Msg("latest client version")
	h.Change(c)
}
