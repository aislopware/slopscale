package hscontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
	errTrafficBusy            = errors.New("too many reports at once; retry later")
	errTrafficMalformed       = errors.New("malformed report")
	errTrafficTrailingData    = errors.New("data after the report")
)

// maxTrafficIngests bounds the reports applied at once, server-wide: a
// report can take a few hundred milliseconds of database writes, and a
// burst of gateways must not hold every database connection.
const maxTrafficIngests = 4

// trafficHeadSize is room for a report's fields other than its entries.
const trafficHeadSize = 1 << 10

// trafficTickInterval is how often the gateway resolvers are checked.
const trafficTickInterval = 15 * time.Second

// trafficRetryAfter is how many seconds a report refused for load is told
// to wait.
const trafficRetryAfter = "5"

// TrafficReportHandler takes a report from slopscale-flowd on a gateway.
// The agent's credential is the gateway's own identity token, which its
// tailscaled fetched for the traffic audience; see hscontrol/traffic. The
// credential is checked before a byte of the body is read.
func (h *Slopscale) TrafficReportHandler(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		writeTrafficError(w, r, http.StatusUnauthorized, errTrafficNoCredential)

		return
	}

	gateway, err := h.state.AuthenticateTrafficReporter(token, time.Now())
	if err != nil {
		writeTrafficError(w, r, trafficErrorStatus(err), err)

		return
	}

	if h.trafficIngests.Add(1) > maxTrafficIngests {
		h.trafficIngests.Add(-1)
		w.Header().Set("Retry-After", trafficRetryAfter)
		writeTrafficError(w, r, http.StatusTooManyRequests, errTrafficBusy)

		return
	}
	defer h.trafficIngests.Add(-1)

	report, status, err := decodeTrafficReport(r)
	if err != nil {
		writeTrafficError(w, r, status, err)

		return
	}

	resp, c, err := h.state.IngestTrafficReport(gateway, report, time.Now())
	if err != nil {
		writeTrafficError(w, r, trafficErrorStatus(err), err)

		return
	}

	if !c.IsEmpty() {
		h.Change(c)
	}

	// The first report is what makes the ASN table worth its memory.
	if h.state.ASNRanges() == 0 {
		go h.refreshASNIfDue(context.WithoutCancel(r.Context()))
	}

	log.Debug().Caller().
		Uint64("seq", resp.Seq).
		Int("flows", len(report.Flows)).
		Int("queries", len(report.Queries)).
		Msg("traffic report applied")

	writeJSON(w, resp)
}

// decodeTrafficReport reads the body, zstd-compressed or not, bounded
// both before and after decompression, and stops at the first entry past
// the per-report bounds rather than decoding the rest.
func decodeTrafficReport(r *http.Request) (traffic.Report, int, error) {
	body := http.MaxBytesReader(nil, r.Body, traffic.MaxReportBytes)

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

		body = http.MaxBytesReader(nil, io.NopCloser(dec), traffic.MaxReportBytes)
	default:
		return traffic.Report{}, http.StatusUnsupportedMediaType, fmt.Errorf("%w: %q",
			errTrafficEncodingUnknown, r.Header.Get("Content-Encoding"))
	}

	report, err := streamTrafficReport(body)

	_, overLimit := errors.AsType[*http.MaxBytesError](err)

	switch {
	case err == nil:
		return report, http.StatusOK, nil
	case errors.Is(err, state.ErrTrafficReportTooLarge):
		return traffic.Report{}, http.StatusRequestEntityTooLarge, err
	case overLimit || errors.Is(err, zstd.ErrDecoderSizeExceeded):
		return traffic.Report{}, http.StatusRequestEntityTooLarge, errTrafficBodyTooLarge
	default:
		return traffic.Report{}, http.StatusBadRequest, fmt.Errorf("decoding the report: %w", err)
	}
}

// streamTrafficReport decodes a report entry by entry, counting the flows
// and queries so it stops at the first one past the bounds. The other
// fields are small; they are collected and decoded as a report without
// entries, so a field added to the contract needs nothing here.
func streamTrafficReport(r io.Reader) (traffic.Report, error) {
	dec := json.NewDecoder(r)

	err := expectDelim(dec, '{')
	if err != nil {
		return traffic.Report{}, err
	}

	var report traffic.Report

	head := make([]byte, 1, trafficHeadSize)
	head[0] = '{'

	for dec.More() {
		var tok json.Token

		tok, err = dec.Token()
		if err != nil {
			return traffic.Report{}, fmt.Errorf("reading a field name: %w", err)
		}

		key, _ := tok.(string)

		switch key {
		case "flows":
			report.Flows, err = decodeTrafficEntries(dec, report.Flows, traffic.MaxFlowsPerReport, key)
		case "queries":
			report.Queries, err = decodeTrafficEntries(dec, report.Queries, traffic.MaxQueriesPerReport, key)
		default:
			head, err = appendTrafficField(dec, head, key)
		}

		if err != nil {
			return traffic.Report{}, err
		}
	}

	err = expectDelim(dec, '}')
	if err != nil {
		return traffic.Report{}, err
	}

	_, err = dec.Token()
	if !errors.Is(err, io.EOF) {
		return traffic.Report{}, errTrafficTrailingData
	}

	flows, queries := report.Flows, report.Queries

	err = json.Unmarshal(append(head, '}'), &report)
	if err != nil {
		return traffic.Report{}, fmt.Errorf("decoding the report's fields: %w", err)
	}

	report.Flows, report.Queries = flows, queries

	return report, nil
}

// appendTrafficField appends the next value, under key, to the JSON
// object being collected in head.
func appendTrafficField(dec *json.Decoder, head []byte, key string) ([]byte, error) {
	var value json.RawMessage

	err := dec.Decode(&value)
	if err != nil {
		return nil, fmt.Errorf("decoding %s: %w", key, err)
	}

	if len(head) > 1 {
		head = append(head, ',')
	}

	head = strconv.AppendQuote(head, key)
	head = append(head, ':')

	return append(head, value...), nil
}

// decodeTrafficEntries appends the entries of a JSON array, or null, to
// into, refusing the one past limit.
func decodeTrafficEntries[T any](dec *json.Decoder, into []T, limit int, what string) ([]T, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", what, err)
	}

	if tok == nil {
		return into, nil
	}

	if d, ok := tok.(json.Delim); !ok || d != '[' {
		return nil, fmt.Errorf("%w: %s is not an array", errTrafficMalformed, what)
	}

	for dec.More() {
		if len(into) >= limit {
			return nil, fmt.Errorf("%w: more than %d %s", state.ErrTrafficReportTooLarge, limit, what)
		}

		var entry T

		err = dec.Decode(&entry)
		if err != nil {
			return nil, fmt.Errorf("decoding %s entry %d: %w", what, len(into), err)
		}

		into = append(into, entry)
	}

	return into, expectDelim(dec, ']')
}

func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("reading the report: %w", err)
	}

	if d, ok := tok.(json.Delim); !ok || d != want {
		return fmt.Errorf("%w: expected %q", errTrafficMalformed, want)
	}

	return nil
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

// asnRetryInterval is how long a failed download waits before the next.
const asnRetryInterval = 15 * time.Minute

// refreshASNIfDue puts the ASN table in use once a gateway has reported,
// from the cache when there is one, and downloads it again when the
// cached copy is a day old; a table newly in use then names what was
// stored without one. It runs off the scheduler goroutine, one at a time.
func (h *Slopscale) refreshASNIfDue(ctx context.Context) {
	if h.cfg.Traffic.ASNDatabaseURL == "" || !h.state.TrafficInUse() {
		return
	}

	if !h.asnRefreshing.CompareAndSwap(false, true) {
		return
	}
	defer h.asnRefreshing.Store(false)
	defer h.backfillTrafficASN(ctx)

	last := h.state.EnsureASN()
	if p := h.asnRefreshedAt.Load(); p != nil && p.After(last) {
		last = *p
	}

	if time.Since(last) < asnRefreshInterval {
		return
	}

	err := h.state.RefreshASN(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("downloading the ASN table failed; keeping the one in use")

		// Dated so the next attempt comes after the retry interval
		// rather than with the next report.
		retry := time.Now().Add(asnRetryInterval - asnRefreshInterval)
		h.asnRefreshedAt.Store(&retry)

		return
	}

	now := time.Now()
	h.asnRefreshedAt.Store(&now)
}

// backfillTrafficASN names the destinations stored before the table in
// use was; a failure is retried with the next refresh.
func (h *Slopscale) backfillTrafficASN(ctx context.Context) {
	err := h.state.BackfillTrafficASN(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("naming the networks of traffic stored without one")
	}
}

// trafficTick takes the resolvers of gateways that stopped reporting or
// no longer qualify out of the clients' DNS. The scheduler starts it in
// its own goroutine every 15 seconds; while one still waits on the
// traffic lock, the next returns at once.
func (h *Slopscale) trafficTick(now time.Time) {
	if !h.trafficTicking.CompareAndSwap(false, true) {
		return
	}
	defer h.trafficTicking.Store(false)

	c, err := h.state.TrafficTick(now)
	if err != nil {
		log.Error().Err(err).Msg("updating the traffic resolvers")
	}

	if !c.IsEmpty() {
		h.Change(c)
	}
}

// trafficMaintenance prunes and folds the traffic rollups, off the
// scheduler goroutine; a run that finds another still going returns.
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
