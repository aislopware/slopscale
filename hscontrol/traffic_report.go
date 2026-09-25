package hscontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aislopware/slopscale/hscontrol/state"
	"github.com/aislopware/slopscale/hscontrol/traffic"
	"github.com/klauspost/compress/zstd"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	errTrafficNoCredential    = errors.New("no bearer credential")
	errTrafficBodyTooLarge    = errors.New("report body too large")
	errTrafficEncodingUnknown = errors.New("unsupported content encoding")
)

// TrafficReportHandler takes a report from slopscale-flowd on a gateway.
// The agent's credential is the gateway's own identity token, which its
// tailscaled fetched for the traffic audience; see hscontrol/traffic.
func (h *Slopscale) TrafficReportHandler(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		writeTrafficError(w, r, http.StatusUnauthorized, errTrafficNoCredential)

		return
	}

	report, status, err := decodeTrafficReport(r)
	if err != nil {
		writeTrafficError(w, r, status, err)

		return
	}

	resp, c, err := h.state.IngestTrafficReport(token, report, time.Now())
	if err != nil {
		writeTrafficError(w, r, trafficErrorStatus(err), err)

		return
	}

	if !c.IsEmpty() {
		h.Change(c)
	}

	log.Debug().Caller().
		Uint64("seq", resp.Seq).
		Int("flows", len(report.Flows)).
		Int("queries", len(report.Queries)).
		Msg("traffic report applied")

	writeJSON(w, resp)
}

// decodeTrafficReport reads the body, zstd-compressed or not, bounded
// both before and after decompression.
func decodeTrafficReport(r *http.Request) (traffic.Report, int, error) {
	body := io.Reader(http.MaxBytesReader(nil, r.Body, traffic.MaxReportBytes))

	switch r.Header.Get("Content-Encoding") {
	case "", "identity":
	case "zstd":
		dec, err := zstd.NewReader(body,
			zstd.WithDecoderConcurrency(1),
			zstd.WithDecoderMaxMemory(traffic.MaxReportBytes),
		)
		if err != nil {
			return traffic.Report{}, http.StatusBadRequest, fmt.Errorf("opening the zstd stream: %w", err)
		}
		defer dec.Close()

		body = dec
	default:
		return traffic.Report{}, http.StatusUnsupportedMediaType, fmt.Errorf("%w: %q",
			errTrafficEncodingUnknown, r.Header.Get("Content-Encoding"))
	}

	raw, err := io.ReadAll(io.LimitReader(body, traffic.MaxReportBytes+1))
	if err != nil {
		if _, tooLarge := errors.AsType[*http.MaxBytesError](err); tooLarge {
			return traffic.Report{}, http.StatusRequestEntityTooLarge, errTrafficBodyTooLarge
		}

		return traffic.Report{}, http.StatusBadRequest, fmt.Errorf("reading the report: %w", err)
	}

	if len(raw) > traffic.MaxReportBytes {
		return traffic.Report{}, http.StatusRequestEntityTooLarge, errTrafficBodyTooLarge
	}

	var report traffic.Report

	err = json.Unmarshal(raw, &report)
	if err != nil {
		return traffic.Report{}, http.StatusBadRequest, fmt.Errorf("decoding the report: %w", err)
	}

	return report, http.StatusOK, nil
}

func trafficErrorStatus(err error) int {
	switch {
	case errors.Is(err, state.ErrTrafficUnauthenticated):
		return http.StatusUnauthorized
	case errors.Is(err, state.ErrTrafficForbidden):
		return http.StatusForbidden
	case errors.Is(err, state.ErrTrafficReportTooLarge):
		return http.StatusRequestEntityTooLarge
	case errors.Is(err, state.ErrTrafficReportInvalid):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// asnRefreshInterval is how often the ASN table is downloaded again. The
// table changes hourly, but a day-old one names nearly every destination
// right, and each download is a few seconds of parsing on a small host.
const asnRefreshInterval = 24 * time.Hour

// refreshASNIfDue downloads the ASN table when the last download is a day
// old, or has never happened. It runs off the scheduler goroutine.
func (h *Slopscale) refreshASNIfDue(ctx context.Context) {
	if h.cfg.Traffic.ASNDatabaseURL == "" {
		return
	}

	last := h.asnRefreshedAt.Load()
	if last != nil && time.Since(*last) < asnRefreshInterval {
		return
	}

	if !h.asnRefreshing.CompareAndSwap(false, true) {
		return
	}
	defer h.asnRefreshing.Store(false)

	err := h.state.RefreshASN(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("downloading the ASN table failed; keeping the one in use")

		return
	}

	now := time.Now()
	h.asnRefreshedAt.Store(&now)
}

// trafficTick takes gateway resolvers that stopped reporting out of the
// clients' DNS.
func (h *Slopscale) trafficTick(now time.Time) {
	c, err := h.state.TrafficTick(now)
	if err != nil {
		log.Error().Err(err).Msg("updating the traffic resolvers")
	}

	if !c.IsEmpty() {
		h.Change(c)
	}
}

// trafficMaintenance prunes and folds the traffic rollups, off the
// scheduler goroutine.
func (h *Slopscale) trafficMaintenance() {
	err := h.state.TrafficMaintenance(time.Now())
	if err != nil {
		log.Error().Err(err).Msg("traffic maintenance")
	}
}

// writeTrafficError answers with the reason in plain text, which the
// agent logs; the server logs refusals as warnings, since an agent left
// misconfigured repeats them every minute until someone looks.
func writeTrafficError(w http.ResponseWriter, r *http.Request, status int, err error) {
	level := zerolog.WarnLevel
	if status >= http.StatusInternalServerError {
		level = zerolog.ErrorLevel
	}

	log.WithLevel(level).Caller().Err(err).Int("code", status).Str("remote", r.RemoteAddr).
		Msg("traffic report refused")

	msg := err.Error()
	if status >= http.StatusInternalServerError {
		msg = "internal server error"
	}

	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", `Bearer realm="slopscale-flowd"`)
	}

	http.Error(w, msg, status)
}
