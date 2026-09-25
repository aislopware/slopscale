package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/aislopware/slopscale/hscontrol/traffic"
)

const (
	// reportMargin is room left under [traffic.MaxReportBytes] for what the
	// size estimate does not see exactly.
	reportMargin = 16 << 10
	// maxSkew is how far the gateway's clock may be off the server's before
	// the status says so; buckets follow the server's clock either way.
	maxSkew = 2 * time.Minute
	// userspaceNote explains why a gateway in userspace networking mode
	// reports no traffic.
	userspaceNote = "tailscaled runs with --tun=userspace-networking: forwarded traffic never reaches " +
		"the kernel's connection tracking, so none of it can be counted"
)

// report spools the buckets before before as one or more reports, each
// under the server's size and entry limits, and asks the uploader to send
// them. It reports even when there is no traffic: the server learns from
// it that the gateway and its resolver are alive.
func (a *Agent) report(before int64) {
	flows, queries, dropped := a.table.Drain(before)

	a.mu.Lock()
	dnsListen := slices.Clone(a.dnsListen)
	a.mu.Unlock()

	base := traffic.Report{
		Version:   a.opts.Version,
		SentAt:    time.Now().UTC(),
		Status:    a.reportStatus(),
		DNSListen: dnsListen,
	}

	for i, r := range splitReport(base, flows, queries) {
		if i == 0 {
			r.Dropped = dropped
		}

		err := a.spool.Enqueue(r)
		if err != nil {
			a.log.Error("spooling a report failed; its entries are lost", "err", err,
				"flows", len(r.Flows), "queries", len(r.Queries))
		}
	}

	a.uploader.Kick()
}

// reportStatus is the collectors' status, with what else the server should
// show about the gateway added to the connection tracking collector, whose
// counts it concerns: a userspace tailscaled, a clock far off the server's
// and a report the server refused.
func (a *Agent) reportStatus() traffic.Status {
	a.mu.Lock()
	status := a.status
	userspace := a.userspace
	a.mu.Unlock()

	var notes []string

	if status.Conntrack.Error != "" {
		notes = append(notes, status.Conntrack.Error)
	}

	if userspace {
		notes = append(notes, userspaceNote)
	}

	if skew := a.uploader.Skew(); skew > maxSkew || skew < -maxSkew {
		notes = append(notes, fmt.Sprintf(
			"the gateway's clock is %s off the server's; traffic is filed by the server's clock",
			skew.Round(time.Second)))
	}

	if rejected := a.uploader.Rejection(); rejected != "" {
		notes = append(notes, rejected)
	}

	status.Conntrack.Error = strings.Join(notes, "; ")

	return status
}

// splitReport spreads flows and queries over as few reports as the
// server's limits allow, all with base's header. The size of each entry is
// measured as it will be encoded, so no report goes over
// [traffic.MaxReportBytes] and gets refused; there is always at least one
// report.
func splitReport(base traffic.Report, flows []traffic.Flow, queries []traffic.Query) []*traffic.Report {
	// The header as the spool will stamp it, at its widest.
	header := base
	header.Instance = strings.Repeat("0", 32) //nolint:mnd // the spool's hex instance id
	header.Seq, header.Dropped = math.MaxUint64, math.MaxUint64

	budget := traffic.MaxReportBytes - reportMargin - encodedLen(header)

	var (
		out  []*traffic.Report
		cur  *traffic.Report
		used int
	)

	next := func() {
		r := base
		cur, used = &r, 0
		out = append(out, cur)
	}

	next()

	for _, f := range flows {
		size := encodedLen(f) + 1
		if used+size > budget || len(cur.Flows) == traffic.MaxFlowsPerReport {
			next()
		}

		cur.Flows = append(cur.Flows, f)
		used += size
	}

	for _, q := range queries {
		size := encodedLen(q) + 1
		if used+size > budget || len(cur.Queries) == traffic.MaxQueriesPerReport {
			next()
		}

		cur.Queries = append(cur.Queries, q)
		used += size
	}

	return out
}

func encodedLen(v any) int {
	raw, err := json.Marshal(v)
	if err != nil {
		return 0
	}

	return len(raw)
}

// runModeChecks watches whether tailscaled forwards through the
// kernel: in userspace networking mode no forwarded packet passes
// connection tracking, and the status should say why nothing is counted.
func (a *Agent) runModeChecks(ctx context.Context) {
	for {
		status, err := a.opts.Local.StatusWithoutPeers(ctx)
		if err == nil {
			a.mu.Lock()
			a.userspace = !status.TUN
			a.mu.Unlock()
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(recheckInterval):
		}
	}
}
