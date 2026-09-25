package cli

import (
	"net/http"
	"testing"
	"time"

	clientv1 "github.com/aislopware/slopscale/gen/client/v1"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// trafficFlags mirrors the flags init() registers on the traffic commands.
func trafficFlags(cmd *cobra.Command) {
	trafficRangeFlags(cmd)
	cmd.Flags().StringP("group-by", "g", "", "")
	cmd.Flags().StringP("search", "s", "", "")
	cmd.Flags().Int64("asn", 0, "")
	cmd.Flags().String("country", "", "")
	cmd.Flags().String("proto", "", "")
	cmd.Flags().Int64("port", 0, "")
	cmd.Flags().Int64P("limit", "l", 0, "")
	cmd.Flags().Uint64P("identifier", "i", 0, "")
	trafficSettingsFlags(cmd)
}

func TestTrafficCommands(t *testing.T) {
	start := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	summary := clientv1.TrafficSummaryOutputBody{
		Resolution: 3600,
		Start:      start,
		End:        start.Add(24 * time.Hour),
		Total:      clientv1.TrafficCounts{TxBytes: 3 << 30, RxBytes: 512, Conns: 7},
		Nodes: []clientv1.TrafficNode{
			{NodeId: "4", NodeName: "laptop", TxBytes: 3 << 30, RxBytes: 512, Conns: 7},
		},
		Reporters: []clientv1.TrafficNode{{NodeId: "1", NodeName: "exit-1", TxBytes: 3 << 30}},
	}
	settings := clientv1.TrafficSettings{
		Sni:       true,
		Retention: clientv1.TrafficRetention{MinuteHours: 48, HourDays: 14, DayDays: 180},
	}

	cases := []commandCase{
		{
			name:  "summary sends the range and renders both tables",
			src:   trafficSummaryCmd,
			flags: map[string]string{"since": "2026-09-24T00:00:00Z", "node": "4"},
			routes: map[string]apiHandler{
				"GET /api/v1/traffic/summary": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "2026-09-24T00:00:00Z", r.URL.Query().Get("start"))
					assert.Equal(t, "4", r.URL.Query().Get("nodeId"))
					assert.Empty(t, r.URL.Query().Get("end"))
					writeJSON(t, w, summary)
				},
			},
			wantIn: []string{"1h0m0s buckets", "Sent 3.0 GiB, received 512 B, 7 connections", "laptop", "exit-1"},
		},
		{
			name:    "summary refuses a time it cannot read",
			src:     trafficSummaryCmd,
			flags:   map[string]string{"since": "yesterday"},
			wantErr: "neither an RFC 3339 time nor a duration",
		},
		{
			name: "destinations send the filters and name each row",
			src:  trafficDestinationsCmd,
			flags: map[string]string{
				"group-by": "destination", "proto": "tcp", "port": "443", "search": "github", "reporter": "1",
			},
			routes: map[string]apiHandler{
				"GET /api/v1/traffic/destinations": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					q := r.URL.Query()
					assert.Equal(t, "destination", q.Get("groupBy"))
					assert.Equal(t, "6", q.Get("proto"))
					assert.Equal(t, "443", q.Get("port"))
					assert.Equal(t, "github", q.Get("q"))
					assert.Equal(t, "1", q.Get("reporterId"))
					assert.Empty(t, q.Get("asn"), "an unset filter is not sent")

					writeJSON(t, w, clientv1.TrafficDestinationsOutputBody{
						Destinations: []clientv1.TrafficDestination{
							{
								Dst: "140.82.112.3", Host: "github.com", Proto: 6, Port: 443,
								Asn: 36459, AsName: "GITHUB", Country: "US", Nodes: 2, TxBytes: 2048,
							},
							{Dst: "10.0.0.5", Proto: 17, Port: 53, Private: true, Nodes: 1},
							{Nodes: 3, RxBytes: 1},
						},
					})
				},
			},
			wantIn: []string{
				"github.com 140.82.112.3 tcp/443", "AS36459 GITHUB US", "2.0 KiB",
				"10.0.0.5 udp/53", "private", "(other)",
			},
		},
		{
			name:    "destinations refuse an unknown protocol",
			src:     trafficDestinationsCmd,
			flags:   map[string]string{"proto": "sctp-ish"},
			wantErr: "--proto must be",
		},
		{
			name:  "dns lists names and the folded remainder",
			src:   trafficDNSCmd,
			flags: map[string]string{"group-by": "name"},
			routes: map[string]apiHandler{
				"GET /api/v1/traffic/dns": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()
					assert.Equal(t, "name", r.URL.Query().Get("groupBy"))
					writeJSON(t, w, clientv1.TrafficDNSOutputBody{Names: []clientv1.TrafficName{
						{Name: "example.com", Queries: 9, Failed: 1, Nodes: 2},
						{Queries: 4, Nodes: 1},
					}})
				},
			},
			wantIn: []string{"example.com", "(other)"},
		},
		{
			name: "reporters show state, collectors and the resolvers in use",
			src:  listTrafficReportersCmd,
			routes: map[string]apiHandler{
				"GET /api/v1/traffic/reporters": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, clientv1.TrafficReportersOutputBody{
						Reporters: []clientv1.TrafficReporter{{
							NodeId: "1", NodeName: "exit-1", Version: "0.1.0", LastReportAt: start,
							ResolverActive: true, DnsListen: []string{"100.64.0.1:53"},
							Collectors: clientv1.TrafficCollectors{
								Conntrack: clientv1.TrafficCollector{Enabled: true},
								Sni:       clientv1.TrafficCollector{Enabled: true, Error: "no nfqueue"},
							},
						}},
						Resolvers: []string{"100.64.0.1"},
					})
				},
			},
			wantIn: []string{
				"exit-1", "reporting, resolving", "conntrack, sni: no nfqueue",
				"Exit node users resolve through 100.64.0.1",
			},
		},
		{
			name:  "reporters delete names the gateway",
			src:   deleteTrafficReporterCmd,
			flags: map[string]string{"identifier": "1"},
			routes: map[string]apiHandler{
				"DELETE /api/v1/traffic/reporters/{nodeId}": func(
					t *testing.T, w http.ResponseWriter, r *http.Request,
				) {
					t.Helper()
					assert.Equal(t, "1", r.PathValue("nodeId"))
					w.WriteHeader(http.StatusNoContent)
				},
			},
			want: "Traffic reporter removed\n",
		},
		{
			name:  "reporters delete surfaces a gateway that never reported",
			src:   deleteTrafficReporterCmd,
			flags: map[string]string{"identifier": "9"},
			routes: map[string]apiHandler{
				"DELETE /api/v1/traffic/reporters/{nodeId}": func(
					t *testing.T, w http.ResponseWriter, _ *http.Request,
				) {
					t.Helper()
					writeProblem(t, w, http.StatusNotFound, "traffic reporter not found")
				},
			},
			wantErr: "traffic reporter not found",
		},
		{
			name: "settings get renders every setting",
			src:  getTrafficSettingsCmd,
			routes: map[string]apiHandler{
				"GET /api/v1/traffic/settings": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeJSON(t, w, settings)
				},
			},
			wantIn: []string{"Names from handshakes", "on", "DNS logging", "off", "48 hours", "180 days"},
		},
		{
			name:  "settings set sends only what was given",
			src:   setTrafficSettingsCmd,
			flags: map[string]string{"dns-logging": "true", "hour-days": "30"},
			routes: map[string]apiHandler{
				"PATCH /api/v1/traffic/settings": func(t *testing.T, w http.ResponseWriter, r *http.Request) {
					t.Helper()

					var body clientv1.TrafficSettingsPatch

					decodeBody(t, r, &body)
					assert.Nil(t, body.Sni)

					if assert.NotNil(t, body.DnsLogging) {
						assert.True(t, *body.DnsLogging)
					}

					if assert.NotNil(t, body.Retention) {
						assert.Nil(t, body.Retention.MinuteHours)
						require.NotNil(t, body.Retention.HourDays)
						assert.Equal(t, int64(30), *body.Retention.HourDays)
					}

					next := settings
					next.DnsLogging, next.Retention.HourDays = true, 30
					writeJSON(t, w, next)
				},
			},
			wantIn: []string{"DNS logging", "30 days"},
		},
		{
			name:    "settings set without a flag is an error",
			src:     setTrafficSettingsCmd,
			wantErr: "at least one of",
		},
		{
			name:  "settings set surfaces a refused retention",
			src:   setTrafficSettingsCmd,
			flags: map[string]string{"minute-hours": "1000"},
			routes: map[string]apiHandler{
				"PATCH /api/v1/traffic/settings": func(t *testing.T, w http.ResponseWriter, _ *http.Request) {
					t.Helper()
					writeProblem(t, w, http.StatusBadRequest, "traffic retention is out of range")
				},
			},
			wantErr: "out of range",
		},
	}

	runCommandCases(t, trafficFlags, cases)
}

func TestFormatBytes(t *testing.T) {
	t.Parallel()

	for in, want := range map[int64]string{
		0:         "0 B",
		1023:      "1023 B",
		1024:      "1.0 KiB",
		1536:      "1.5 KiB",
		5 << 20:   "5.0 MiB",
		3 << 40:   "3.0 TiB",
		1<<63 - 1: "8.0 EiB",
	} {
		assert.Equal(t, want, formatBytes(in), "%d", in)
	}
}

func TestParseTrafficTime(t *testing.T) {
	t.Parallel()

	at, err := parseTrafficTime("since", "7d")
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().AddDate(0, 0, -7), at, time.Minute)

	at, err = parseTrafficTime("since", "90m")
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(-90*time.Minute), at, time.Minute)

	_, err = parseTrafficTime("since", "xd")
	require.Error(t, err)
}
