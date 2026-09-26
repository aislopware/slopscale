package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	clientv1 "github.com/aislopware/slopscale/gen/client/v1"
	"github.com/aislopware/slopscale/hscontrol/util"
	"github.com/spf13/cobra"
)

// The IP protocol numbers the CLI names.
const (
	protoICMP = 1
	protoTCP  = 6
	protoUDP  = 17
)

var (
	errNoTrafficSettingGiven = errors.New(
		"give at least one of --sni, --dns-logging, --minute-hours, --hour-days or --day-days")
	errTrafficProto = errors.New("--proto must be tcp, udp, icmp or a protocol number")
)

func init() {
	rootCmd.AddCommand(trafficCmd)

	for _, c := range []*cobra.Command{trafficSummaryCmd, trafficDestinationsCmd, trafficDNSCmd} {
		trafficRangeFlags(c)
		trafficCmd.AddCommand(c)
	}

	trafficSummaryCmd.Flags().Int64P("limit", "l", 0, "Top nodes to show (at most 100; default 10)")
	trafficDestinationFlags(trafficDestinationsCmd)
	trafficDNSFlags(trafficDNSCmd)

	trafficCmd.AddCommand(trafficReportersCmd)
	trafficReportersCmd.AddCommand(listTrafficReportersCmd)
	deleteTrafficReporterCmd.Flags().Uint64P("identifier", "i", 0, "The gateway's node ID")
	mustMarkRequired(deleteTrafficReporterCmd, "identifier")
	trafficReportersCmd.AddCommand(deleteTrafficReporterCmd)
	resolverTrafficReporterCmd.Flags().Uint64P("identifier", "i", 0, "The gateway's node ID")
	mustMarkRequired(resolverTrafficReporterCmd, "identifier")
	resolverTrafficReporterCmd.Flags().Bool("approve", true,
		"Let the gateway's exit node users use the resolver; false stops them")
	trafficReportersCmd.AddCommand(resolverTrafficReporterCmd)

	trafficCmd.AddCommand(trafficSettingsCmd)
	trafficSettingsCmd.AddCommand(getTrafficSettingsCmd)
	trafficSettingsFlags(setTrafficSettingsCmd)
	trafficSettingsCmd.AddCommand(setTrafficSettingsCmd)
}

// trafficRangeFlags registers the range and node filters every read takes.
func trafficRangeFlags(cmd *cobra.Command) {
	cmd.Flags().String("since", "", "Start of the range (RFC 3339 or a duration such as 7d or 24h); default a day ago")
	cmd.Flags().String("until", "", "End of the range (RFC 3339 or a duration back from now); default now")
	cmd.Flags().Uint64("node", 0, "Only the traffic of this node ID")
	cmd.Flags().Uint64("reporter", 0, "Only the traffic through this gateway's node ID")
}

func trafficDestinationFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("group-by", "g", "destination",
		"Group by destination, host, asn, country, port, node or reporter")
	cmd.Flags().StringP("search", "s", "", "Only hosts or addresses containing this")
	cmd.Flags().Int64("asn", 0, "Only this network (AS number)")
	cmd.Flags().String("country", "", "Only this country (ISO 3166 code)")
	cmd.Flags().String("proto", "", "Only this protocol: tcp, udp, icmp or a number")
	cmd.Flags().Int64("port", 0, "Only this port (with --proto)")
	cmd.Flags().Int64P("limit", "l", 0, "Rows to show (at most 1000; default 100)")
}

func trafficDNSFlags(cmd *cobra.Command) {
	cmd.Flags().StringP("group-by", "g", "name", "Group by name or node")
	cmd.Flags().StringP("search", "s", "", "Only names containing this")
	cmd.Flags().Int64P("limit", "l", 0, "Rows to show (at most 1000; default 100)")
}

func trafficSettingsFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("sni", false, "Name destinations from TLS and QUIC handshakes")
	cmd.Flags().Bool("dns-logging", false,
		"Point the nodes using a gateway as their exit node at its resolver and record the names they look up")
	cmd.Flags().Int64("minute-hours", 0, "Keep per-minute totals this many hours")
	cmd.Flags().Int64("hour-days", 0, "Keep hourly totals, destinations and names this many days")
	cmd.Flags().Int64("day-days", 0, "Keep daily ones this many days")
}

var trafficCmd = &cobra.Command{
	Use:   "traffic",
	Short: "Read what the gateways' traffic agents reported",
	Long: `The traffic monitor: how much each node sent and received through the exit
nodes, subnet routers and app connectors that run slopscale-flowd, and where it
went.`,
}

// trafficRange reads the range and node flags into the query parameters the
// reads share.
type trafficRange struct {
	start, end     *time.Time
	node, reporter *string
}

func trafficRangeFromFlags(cmd *cobra.Command) (trafficRange, error) {
	var r trafficRange

	for flag, dst := range map[string]**time.Time{"since": &r.start, "until": &r.end} {
		value, _ := cmd.Flags().GetString(flag)
		if value == "" {
			continue
		}

		at, err := parseTrafficTime(flag, value)
		if err != nil {
			return r, err
		}

		*dst = &at
	}

	for flag, dst := range map[string]**string{"node": &r.node, "reporter": &r.reporter} {
		id, _ := cmd.Flags().GetUint64(flag)
		if id != 0 {
			s := strconv.FormatUint(id, util.Base10)
			*dst = &s
		}
	}

	return r, nil
}

// parseTrafficTime is an RFC 3339 time or a duration back from now, which
// also takes days (7d) since traffic ranges are often counted in them.
func parseTrafficTime(flag, value string) (time.Time, error) {
	if days, ok := strings.CutSuffix(value, "d"); ok {
		n, err := strconv.Atoi(days)
		if err == nil {
			return time.Now().AddDate(0, 0, -n), nil
		}
	}

	return parseAuditTime(flag, value)
}

func int64FlagIfSet(cmd *cobra.Command, flag string) *int64 {
	if !cmd.Flags().Changed(flag) {
		return nil
	}

	v, _ := cmd.Flags().GetInt64(flag)

	return &v
}

func stringFlagIfSet(cmd *cobra.Command, flag string) *string {
	v, _ := cmd.Flags().GetString(flag)
	if v == "" {
		return nil
	}

	return &v
}

var trafficSummaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "Show the volume over a range, the top nodes and each gateway's share",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			r, err := trafficRangeFromFlags(cmd)
			if err != nil {
				return err
			}

			resp, err := client.GetTrafficSummaryWithResponse(ctx, &clientv1.GetTrafficSummaryParams{
				Start: r.start, End: r.end, NodeId: r.node, ReporterId: r.reporter,
				Limit: int64FlagIfSet(cmd, "limit"),
			})
			if err != nil {
				return fmt.Errorf("reading the traffic summary: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			s := resp.JSON200

			return printListOutput(cmd, s, func() error {
				fmt.Printf("%s to %s, %s buckets\n", s.Start.Format(SlopscaleDateTimeFormat),
					s.End.Format(SlopscaleDateTimeFormat), time.Duration(s.Resolution)*time.Second)
				fmt.Printf("Sent %s, received %s, %d connections\n\n",
					formatBytes(s.Total.TxBytes), formatBytes(s.Total.RxBytes), s.Total.Conns)

				err := renderTable(trafficNodeHeader("Node"), trafficNodeRows(s.Nodes))
				if err != nil {
					return err
				}

				fmt.Println()

				return renderTable(trafficNodeHeader("Gateway"), trafficNodeRows(s.Reporters))
			})
		},
	),
}

func trafficNodeHeader(kind string) []string {
	return []string{"ID", kind, "Sent", "Received", "Connections"}
}

func trafficNodeRows(nodes []clientv1.TrafficNode) [][]string {
	rows := make([][]string, 0, len(nodes))
	for _, n := range nodes {
		rows = append(rows, []string{
			n.NodeId, n.NodeName, formatBytes(n.TxBytes), formatBytes(n.RxBytes),
			strconv.FormatInt(n.Conns, util.Base10),
		})
	}

	return rows
}

var trafficDestinationsCmd = &cobra.Command{
	Use:     "destinations",
	Short:   "List where the traffic went, largest first",
	Aliases: []string{"dst"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			params, err := trafficDestinationParams(cmd)
			if err != nil {
				return err
			}

			resp, err := client.ListTrafficDestinationsWithResponse(ctx, params)
			if err != nil {
				return fmt.Errorf("listing traffic destinations: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printListOutput(cmd, resp.JSON200, func() error {
				return renderTable(
					[]string{"Destination", "Network", "Node", "Nodes", "Sent", "Received", "Connections"},
					trafficDestinationRows(resp.JSON200.Destinations),
				)
			})
		},
	),
}

func trafficDestinationParams(cmd *cobra.Command) (*clientv1.ListTrafficDestinationsParams, error) {
	r, err := trafficRangeFromFlags(cmd)
	if err != nil {
		return nil, err
	}

	group, _ := cmd.Flags().GetString("group-by")
	groupBy := clientv1.ListTrafficDestinationsParamsGroupBy(group)

	params := &clientv1.ListTrafficDestinationsParams{
		Start: r.start, End: r.end, NodeId: r.node, ReporterId: r.reporter,
		GroupBy: &groupBy,
		Q:       stringFlagIfSet(cmd, "search"),
		Asn:     int64FlagIfSet(cmd, "asn"),
		Country: stringFlagIfSet(cmd, "country"),
		Port:    int64FlagIfSet(cmd, "port"),
		Limit:   int64FlagIfSet(cmd, "limit"),
	}

	if proto := stringFlagIfSet(cmd, "proto"); proto != nil {
		n, err := parseProto(*proto)
		if err != nil {
			return nil, err
		}

		params.Proto = &n
	}

	return params, nil
}

func parseProto(s string) (int64, error) {
	switch strings.ToLower(s) {
	case "tcp":
		return protoTCP, nil
	case "udp":
		return protoUDP, nil
	case "icmp":
		return protoICMP, nil
	}

	n, err := strconv.ParseInt(s, util.Base10, 64)
	if err != nil || n < 0 || n > 255 {
		return 0, errTrafficProto
	}

	return n, nil
}

func protoName(n int64) string {
	switch n {
	case protoTCP:
		return "tcp"
	case protoUDP:
		return "udp"
	case protoICMP:
		return "icmp"
	}

	return strconv.FormatInt(n, util.Base10)
}

// trafficDestinationLabel names a destination row from the fields its
// grouping set.
func trafficDestinationLabel(d clientv1.TrafficDestination) string {
	var parts []string

	if d.Host != "" {
		parts = append(parts, d.Host)
	}

	if d.Dst != "" && d.Dst != d.Host {
		parts = append(parts, d.Dst)
	}

	if d.Proto != 0 {
		service := protoName(d.Proto)
		if d.Port != 0 {
			service += "/" + strconv.FormatInt(d.Port, util.Base10)
		}

		parts = append(parts, service)
	}

	if len(parts) == 0 && d.NodeId == "" && d.Asn == 0 && d.Country == "" {
		return "(other)"
	}

	return strings.Join(parts, " ")
}

func trafficDestinationRows(destinations []clientv1.TrafficDestination) [][]string {
	rows := make([][]string, 0, len(destinations))

	for _, d := range destinations {
		network := d.Country
		if d.Asn != 0 {
			network = strings.TrimSpace(fmt.Sprintf("AS%d %s %s", d.Asn, d.AsName, d.Country))
		}

		if d.Private {
			network = "private"
		}

		node := d.NodeName
		if node == "" {
			node = d.NodeId
		}

		rows = append(rows, []string{
			trafficDestinationLabel(d), network, node, strconv.FormatInt(d.Nodes, util.Base10),
			formatBytes(d.TxBytes), formatBytes(d.RxBytes), strconv.FormatInt(d.Conns, util.Base10),
		})
	}

	return rows
}

var trafficDNSCmd = &cobra.Command{
	Use:   "dns",
	Short: "List the names the nodes looked up, most asked first",
	Long: `Lists the questions the gateways' resolvers answered. Empty unless DNS logging
is on (slopscale traffic settings set --dns-logging).`,
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			r, err := trafficRangeFromFlags(cmd)
			if err != nil {
				return err
			}

			group, _ := cmd.Flags().GetString("group-by")
			groupBy := clientv1.ListTrafficNamesParamsGroupBy(group)

			resp, err := client.ListTrafficNamesWithResponse(ctx, &clientv1.ListTrafficNamesParams{
				Start: r.start, End: r.end, NodeId: r.node, ReporterId: r.reporter,
				GroupBy: &groupBy,
				Q:       stringFlagIfSet(cmd, "search"),
				Limit:   int64FlagIfSet(cmd, "limit"),
			})
			if err != nil {
				return fmt.Errorf("listing looked-up names: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printListOutput(cmd, resp.JSON200, func() error {
				rows := make([][]string, 0, len(resp.JSON200.Names))

				for _, n := range resp.JSON200.Names {
					label := n.Name
					switch {
					case n.NodeId != "":
						label = n.NodeName
					case label == "":
						label = "(other)"
					}

					rows = append(rows, []string{
						label, strconv.FormatInt(n.Queries, util.Base10),
						strconv.FormatInt(n.Failed, util.Base10), strconv.FormatInt(n.Nodes, util.Base10),
					})
				}

				return renderTable([]string{"Name", "Queries", "Failed", "Nodes"}, rows)
			})
		},
	),
}

var trafficReportersCmd = &cobra.Command{
	Use:     "reporters",
	Short:   "Manage the gateways that report traffic",
	Aliases: []string{"reporter", "gateways"},
}

var listTrafficReportersCmd = &cobra.Command{
	Use:     cmdList,
	Short:   "List the gateways whose agent has reported",
	Aliases: []string{"ls"},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.ListTrafficReportersWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("listing traffic reporters: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			body := resp.JSON200

			return printListOutput(cmd, body, func() error {
				rows := make([][]string, 0, len(body.Reporters))
				for _, r := range body.Reporters {
					rows = append(rows, []string{
						r.NodeId, r.NodeName, r.Version, r.LastReportAt.Format(SlopscaleDateTimeFormat),
						reporterState(r), collectorsText(r.Collectors),
						strings.Join(r.DnsListen, " "),
						strconv.FormatInt(r.Unattributed, util.Base10), strconv.FormatInt(r.Dropped, util.Base10),
					})
				}

				err := renderTable([]string{
					"ID", "Gateway", "Version", "Last report", "State", "Collectors", "Resolver",
					"Unattributed", "Dropped",
				}, rows)
				if err != nil {
					return err
				}

				if len(body.Resolvers) > 0 {
					fmt.Printf("\nExit node users resolve through %s\n", strings.Join(body.Resolvers, ", "))
				}

				if len(body.SkippedUpstreams) > 0 {
					fmt.Printf("\nThe resolvers cannot forward to %s\n", strings.Join(body.SkippedUpstreams, ", "))
				}

				return nil
			})
		},
	),
}

func reporterState(r clientv1.TrafficReporter) string {
	switch {
	case r.Stale:
		return "stale"
	case r.Refused != "":
		return "refused: " + r.Refused
	case r.ResolverActive:
		return "reporting, resolving"
	case r.ResolverApprovedAt != nil:
		return "reporting, resolver approved"
	}

	return "reporting"
}

// collectorsText lists the collectors that run, with the error of any that
// does not work.
func collectorsText(c clientv1.TrafficCollectors) string {
	var parts []string

	for _, col := range []struct {
		name string
		c    clientv1.TrafficCollector
	}{
		{"conntrack", c.Conntrack}, {"sni", c.Sni}, {"dns", c.Dns}, {"appc", c.AppConnector},
	} {
		switch {
		case col.c.Error != "":
			parts = append(parts, col.name+": "+col.c.Error)
		case col.c.Enabled:
			parts = append(parts, col.name)
		}
	}

	return strings.Join(parts, ", ")
}

var deleteTrafficReporterCmd = &cobra.Command{
	Use:     cmdDelete,
	Short:   "Forget a gateway's agent and take its resolver out of its exit node users' DNS",
	Aliases: []string{aliasDel},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")

			resp, err := client.DeleteTrafficReporterWithResponse(ctx, strconv.FormatUint(identifier, util.Base10))
			if err != nil {
				return fmt.Errorf("removing traffic reporter: %w", err)
			}

			if resp.StatusCode() != http.StatusOK && resp.StatusCode() != http.StatusNoContent {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printOutput(cmd, map[string]string{colResult: "Traffic reporter removed"},
				"Traffic reporter removed")
		},
	),
}

var resolverTrafficReporterCmd = &cobra.Command{
	Use:   "resolver",
	Short: "Let a gateway's exit node users use its resolver, or stop them",
	Long: `Approves a gateway's resolver for DNS logging, or withdraws the approval with
--approve=false. An approved resolver is used while DNS logging is on, the
gateway reports it working and still qualifies as a gateway, and only by the
nodes using the gateway as their exit node right now. Needs the dns scope as
well as logs:network.`,
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			identifier, _ := cmd.Flags().GetUint64("identifier")
			approve, _ := cmd.Flags().GetBool("approve")

			resp, err := client.UpdateTrafficReporterWithResponse(ctx, strconv.FormatUint(identifier, util.Base10),
				clientv1.TrafficReporterPatch{Resolver: approve})
			if err != nil {
				return fmt.Errorf("updating traffic reporter: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			msg := "Resolver approval withdrawn"
			if approve {
				msg = "Resolver approved"
			}

			return printOutput(cmd, resp.JSON200, msg)
		},
	),
}

var trafficSettingsCmd = &cobra.Command{
	Use:   "settings",
	Short: "Manage the traffic monitor's settings",
}

var getTrafficSettingsCmd = &cobra.Command{
	Use:     "get",
	Short:   "Show the traffic monitor's settings",
	Aliases: []string{cmdShow},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.GetTrafficSettingsWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("getting traffic settings: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printTrafficSettings(cmd, resp.JSON200)
		},
	),
}

var setTrafficSettingsCmd = &cobra.Command{
	Use:   "set",
	Short: "Change the traffic monitor's settings",
	Long: `Changes the given settings; the others keep their value. Turning DNS logging
on points the nodes using a gateway as their exit node at the gateway's
approved resolver while its agent reports, and records what they look up
while they do; turning it off points them back. Nodes that use no exit node,
and clients older than Tailscale 1.86, are never logged. Changing DNS logging
needs the dns scope as well as logs:network.`,
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			body, err := trafficSettingsPatch(cmd)
			if err != nil {
				return err
			}

			resp, err := client.UpdateTrafficSettingsWithResponse(ctx, body)
			if err != nil {
				return fmt.Errorf("updating traffic settings: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printTrafficSettings(cmd, resp.JSON200)
		},
	),
}

func trafficSettingsPatch(cmd *cobra.Command) (clientv1.TrafficSettingsPatch, error) {
	var body clientv1.TrafficSettingsPatch

	for flag, dst := range map[string]**bool{"sni": &body.Sni, "dns-logging": &body.DnsLogging} {
		if cmd.Flags().Changed(flag) {
			on, _ := cmd.Flags().GetBool(flag)
			*dst = &on
		}
	}

	retention := clientv1.TrafficRetentionPatch{
		MinuteHours: int64FlagIfSet(cmd, "minute-hours"),
		HourDays:    int64FlagIfSet(cmd, "hour-days"),
		DayDays:     int64FlagIfSet(cmd, "day-days"),
	}

	if retention.MinuteHours != nil || retention.HourDays != nil || retention.DayDays != nil {
		body.Retention = &retention
	}

	if body.Sni == nil && body.DnsLogging == nil && body.Retention == nil {
		return body, errNoTrafficSettingGiven
	}

	return body, nil
}

func printTrafficSettings(cmd *cobra.Command, s *clientv1.TrafficSettings) error {
	return printListOutput(cmd, s, func() error {
		return renderTable(
			[]string{"Setting", "Value"},
			[][]string{
				{"Names from handshakes", onOff(s.Sni)},
				{"DNS logging", onOff(s.DnsLogging)},
				{"Per-minute totals kept", fmt.Sprintf("%d hours", s.Retention.MinuteHours)},
				{"Hourly rows kept", fmt.Sprintf("%d days", s.Retention.HourDays)},
				{"Daily rows kept", fmt.Sprintf("%d days", s.Retention.DayDays)},
			},
		)
	})
}

// formatBytes renders a byte count in binary units, one decimal.
func formatBytes(n int64) string {
	const unit = 1024

	if n < unit {
		return strconv.FormatInt(n, util.Base10) + " B"
	}

	value, exp := float64(n)/unit, 0
	for value >= unit && exp < 5 {
		value /= unit
		exp++
	}

	return fmt.Sprintf("%.1f %ciB", value, "KMGTPE"[exp])
}
