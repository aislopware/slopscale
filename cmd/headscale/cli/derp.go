package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/spf13/cobra"
)

var (
	errDERPRegionNotFound = errors.New("no such relay region")
	errDERPRelayNotFound  = errors.New("no such relay in the region")
)

// defaultDERPRegionID is the config file's default region for the
// embedded relay, out of the way of Tailscale's regions.
const defaultDERPRegionID = 999

func init() {
	rootCmd.AddCommand(derpCmd)
	derpCmd.AddCommand(showDERPCmd)
	derpCmd.AddCommand(setDERPCmd)
	derpCmd.AddCommand(resetDERPCmd)
	derpCmd.AddCommand(refreshDERPCmd)
	derpCmd.AddCommand(derpRelayCmd)
	derpRelayCmd.AddCommand(addDERPRelayCmd)
	derpRelayCmd.AddCommand(removeDERPRelayCmd)

	setDERPCmd.Flags().StringSlice("url", []string{}, "DERP map URLs fetched and merged in order; empty clears them")
	setDERPCmd.Flags().Bool("auto-update", true, "Refetch the maps on a schedule")
	setDERPCmd.Flags().String("update-frequency", "3h", "How often the maps are refetched, at least 1m")
	setDERPCmd.Flags().Bool("server", true, "Run the embedded relay")
	setDERPCmd.Flags().Int64("region-id", defaultDERPRegionID, "Region the embedded relay is published as")
	setDERPCmd.Flags().String("region-code", "headscale", "Short code of the embedded relay's region")
	setDERPCmd.Flags().String("region-name", "", "Name of the embedded relay's region. If empty, the code is used")
	setDERPCmd.Flags().Bool("verify-clients", true, "Admit only this tailnet's machines to the embedded relay")
	setDERPCmd.Flags().String("stun", "0.0.0.0:3478", "UDP host:port the embedded relay's STUN listens on")
	setDERPCmd.Flags().
		String("ipv4", "", "Public IPv4 published for the embedded relay. If empty, machines rely on DNS")
	setDERPCmd.Flags().
		String("ipv6", "", "Public IPv6 published for the embedded relay. If empty, machines rely on DNS")

	addDERPRelayCmd.Flags().Int64("region", 0, "Region ID the relay belongs to; a new ID creates the region")
	addDERPRelayCmd.Flags().String("code", "", "Region code, needed for a new region")
	addDERPRelayCmd.Flags().String("name", "", "Region name. If empty, the code is used")
	addDERPRelayCmd.Flags().String("host", "", "DNS name the relay's certificate matches")
	addDERPRelayCmd.Flags().String("relay-name", "", "Name unique within the region. If empty, the host is used")
	addDERPRelayCmd.Flags().String("ipv4", "", "Fixed IPv4, or none")
	addDERPRelayCmd.Flags().String("ipv6", "", "Fixed IPv6, or none")
	addDERPRelayCmd.Flags().Int64("derp-port", 0, "HTTPS port. 0 selects 443")
	addDERPRelayCmd.Flags().Int64("stun-port", 0, "UDP STUN port. 0 selects 3478")
	addDERPRelayCmd.Flags().Bool("stun-only", false, "The relay answers STUN but never relays")
	addDERPRelayCmd.Flags().Bool("can-port80", false, "The relay also serves plain HTTP on port 80")
	_ = addDERPRelayCmd.MarkFlagRequired("region")
	_ = addDERPRelayCmd.MarkFlagRequired("host")

	removeDERPRelayCmd.Flags().Int64("region", 0, "Region ID")
	removeDERPRelayCmd.Flags().String("host", "", "Relay to remove. Without it the whole region is removed")
	_ = removeDERPRelayCmd.MarkFlagRequired("region")
}

var derpCmd = &cobra.Command{
	Use:   "derp",
	Short: "Manage the tailnet's DERP relays",
	Long: `DERP relays carry traffic between machines that cannot connect directly and
help them find each other. The map clients receive merges the fetched map
URLs, the config file's map files, the relays added here and the relay
headscale runs itself.`,
}

var showDERPCmd = &cobra.Command{
	Use:     cmdShow,
	Short:   "Show DERP settings and the map clients receive",
	Aliases: []string{"get", cmdList},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.GetDERPWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("getting DERP settings: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printDERP(cmd, resp.JSON200, "")
		},
	),
}

var setDERPCmd = &cobra.Command{
	Use:   "set",
	Short: "Change DERP settings",
	Long: `Changes the map sources, the refetch schedule and the embedded relay. It
starts from the settings in force and replaces only the fields whose flags
were given, so one flag at a time is safe. Relays the operator runs are
managed with "headscale derp relay".`,
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			current, err := currentDERP(ctx, client)
			if err != nil {
				return err
			}

			body := setDERPBody(current)
			applySetDERPFlags(cmd, &body)

			return putDERP(ctx, client, cmd, body, "")
		},
	),
}

var resetDERPCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset DERP settings to the config file",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.ResetDERPWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("resetting DERP settings: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printDERP(cmd, resp.JSON200, "DERP settings reset to the config file")
		},
	),
}

var refreshDERPCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Refetch the DERP maps now",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.RefreshDERPWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("refreshing DERP map: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printDERP(cmd, resp.JSON200, "DERP map refreshed")
		},
	),
}

var derpRelayCmd = &cobra.Command{
	Use:   "relay",
	Short: "Manage relays the operator runs",
}

var addDERPRelayCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a relay to a region, creating the region",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			current, err := currentDERP(ctx, client)
			if err != nil {
				return err
			}

			body := setDERPBody(current)
			regions := slices.Clone(current.Effective.Regions)

			regionID, _ := cmd.Flags().GetInt64("region")
			host, _ := cmd.Flags().GetString("host")
			relay := clientv1.DERPRelay{HostName: host}
			relay.Name = optionalString(cmd, "relay-name")
			relay.Ipv4 = optionalString(cmd, "ipv4")
			relay.Ipv6 = optionalString(cmd, "ipv6")
			relay.DerpPort = optionalInt(cmd, "derp-port")
			relay.StunPort = optionalInt(cmd, "stun-port")
			relay.StunOnly = optionalBool(cmd, "stun-only")
			relay.CanPort80 = optionalBool(cmd, "can-port80")

			idx := slices.IndexFunc(regions, func(r clientv1.DERPCustomRegion) bool { return r.Id == regionID })
			if idx == -1 {
				code, _ := cmd.Flags().GetString("code")
				region := clientv1.DERPCustomRegion{Id: regionID, Code: code, Name: optionalString(cmd, "name")}
				regions = append(regions, region)
				idx = len(regions) - 1
			} else {
				if cmd.Flags().Changed("code") {
					regions[idx].Code, _ = cmd.Flags().GetString("code")
				}

				if cmd.Flags().Changed("name") {
					regions[idx].Name = optionalString(cmd, "name")
				}
			}

			nodes := []clientv1.DERPRelay{}
			if regions[idx].Nodes != nil {
				nodes = slices.Clone(*regions[idx].Nodes)
			}

			nodes = slices.DeleteFunc(nodes, func(n clientv1.DERPRelay) bool { return n.HostName == host })
			nodes = append(nodes, relay)
			regions[idx].Nodes = &nodes
			body.Regions = &regions

			return putDERP(ctx, client, cmd, body, fmt.Sprintf("Relay %s added to region %d", host, regionID))
		},
	),
}

var removeDERPRelayCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove a relay, or a whole region",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			current, err := currentDERP(ctx, client)
			if err != nil {
				return err
			}

			body := setDERPBody(current)
			regions := slices.Clone(current.Effective.Regions)

			regionID, _ := cmd.Flags().GetInt64("region")
			host, _ := cmd.Flags().GetString("host")

			idx := slices.IndexFunc(regions, func(r clientv1.DERPCustomRegion) bool { return r.Id == regionID })
			if idx == -1 {
				return fmt.Errorf("%w: %d", errDERPRegionNotFound, regionID)
			}

			message := fmt.Sprintf("Region %d removed", regionID)

			if host == "" {
				regions = slices.Delete(regions, idx, idx+1)
			} else {
				nodes := []clientv1.DERPRelay{}
				if regions[idx].Nodes != nil {
					nodes = slices.Clone(*regions[idx].Nodes)
				}

				kept := slices.DeleteFunc(nodes, func(n clientv1.DERPRelay) bool { return n.HostName == host })
				if len(kept) == len(nodes) {
					return fmt.Errorf("%w: %s in %d", errDERPRelayNotFound, host, regionID)
				}

				message = fmt.Sprintf("Relay %s removed from region %d", host, regionID)

				if len(kept) == 0 {
					regions = slices.Delete(regions, idx, idx+1)
				} else {
					regions[idx].Nodes = &kept
				}
			}

			body.Regions = &regions

			return putDERP(ctx, client, cmd, body, message)
		},
	),
}

func currentDERP(ctx context.Context, client *clientv1.ClientWithResponses) (*clientv1.DERP, error) {
	resp, err := client.GetDERPWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting DERP settings: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return nil, apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
	}

	return resp.JSON200, nil
}

func putDERP(
	ctx context.Context,
	client *clientv1.ClientWithResponses,
	cmd *cobra.Command,
	body clientv1.SetDERPRequestBody,
	message string,
) error {
	resp, err := client.SetDERPWithResponse(ctx, body)
	if err != nil {
		return fmt.Errorf("setting DERP settings: %w", err)
	}

	if resp.StatusCode() != http.StatusOK {
		return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
	}

	return printDERP(cmd, resp.JSON200, message)
}

// setDERPBody is the settings in force as a request body, so a change
// sends the whole configuration back.
func setDERPBody(current *clientv1.DERP) clientv1.SetDERPRequestBody {
	e := current.Effective
	urls := slices.Clone(e.Urls)
	regions := slices.Clone(e.Regions)
	autoUpdate := e.AutoUpdate
	frequency := e.UpdateFrequency

	return clientv1.SetDERPRequestBody{
		Urls:            &urls,
		Regions:         &regions,
		AutoUpdate:      &autoUpdate,
		UpdateFrequency: &frequency,
		Server:          e.Server,
	}
}

func applySetDERPFlags(cmd *cobra.Command, body *clientv1.SetDERPRequestBody) {
	if cmd.Flags().Changed("url") {
		urls, _ := cmd.Flags().GetStringSlice("url")
		body.Urls = &urls
	}

	if cmd.Flags().Changed("auto-update") {
		on, _ := cmd.Flags().GetBool("auto-update")
		body.AutoUpdate = &on
	}

	if cmd.Flags().Changed("update-frequency") {
		f, _ := cmd.Flags().GetString("update-frequency")
		body.UpdateFrequency = &f
	}

	if cmd.Flags().Changed("server") {
		body.Server.Enabled, _ = cmd.Flags().GetBool("server")
	}

	if cmd.Flags().Changed("region-id") {
		body.Server.RegionId = optionalInt(cmd, "region-id")
	}

	if cmd.Flags().Changed("region-code") {
		body.Server.RegionCode = optionalString(cmd, "region-code")
	}

	if cmd.Flags().Changed("region-name") {
		body.Server.RegionName = optionalString(cmd, "region-name")
	}

	if cmd.Flags().Changed("verify-clients") {
		body.Server.VerifyClients = optionalBool(cmd, "verify-clients")
	}

	if cmd.Flags().Changed("stun") {
		body.Server.StunAddr = optionalString(cmd, "stun")
	}

	if cmd.Flags().Changed("ipv4") {
		body.Server.Ipv4 = optionalString(cmd, "ipv4")
	}

	if cmd.Flags().Changed("ipv6") {
		body.Server.Ipv6 = optionalString(cmd, "ipv6")
	}
}

func optionalString(cmd *cobra.Command, name string) *string {
	v, _ := cmd.Flags().GetString(name)

	return &v
}

func optionalInt(cmd *cobra.Command, name string) *int64 {
	v, _ := cmd.Flags().GetInt64(name)

	return &v
}

func optionalBool(cmd *cobra.Command, name string) *bool {
	v, _ := cmd.Flags().GetBool(name)

	return &v
}

func printDERP(cmd *cobra.Command, d *clientv1.DERP, prefix string) error {
	return printListOutput(cmd, d, func() error {
		printDERPHuman(d, prefix)

		return nil
	})
}

func printDERPHuman(d *clientv1.DERP, prefix string) {
	if prefix != "" {
		fmt.Println(prefix)
	}

	if d.Overridden {
		fmt.Println("Source: set through the API (reset with `headscale derp reset`)")
	} else {
		fmt.Println("Source: config file")
	}

	e := d.Effective

	if len(e.Urls) > 0 {
		fmt.Printf("Map URLs: %s\n", strings.Join(e.Urls, ", "))
	} else {
		fmt.Println("Map URLs: (none)")
	}

	if len(d.Paths) > 0 {
		fmt.Printf("Map files: %s\n", strings.Join(d.Paths, ", "))
	}

	if e.AutoUpdate {
		fmt.Printf("Auto update: every %s\n", e.UpdateFrequency)
	} else {
		fmt.Println("Auto update: off")
	}

	if !d.FetchedAt.IsZero() {
		fmt.Printf("Last fetch: %s\n", d.FetchedAt.Format(time.RFC3339))
	}

	if d.FetchError != "" {
		fmt.Printf("Last fetch error: %s\n", d.FetchError)
	}

	printEmbeddedDERP(d)
	printCustomDERPRegions(e.Regions)
	printDERPMap(d.Regions)
}

func printEmbeddedDERP(d *clientv1.DERP) {
	s := d.Effective.Server

	switch {
	case !d.RelayAvailable:
		fmt.Println("Embedded relay: unavailable (derp.server.private_key_path is not set)")
	case !s.Enabled:
		fmt.Println("Embedded relay: off")
	case d.RelayRunning:
		fmt.Printf("Embedded relay: running at %s as region %d (%s), STUN on %s\n",
			d.ServerUrl, derefInt(s.RegionId), derefString(s.RegionCode), d.StunAddr)
	default:
		fmt.Println("Embedded relay: on, not running")
	}

	if s.Enabled {
		fmt.Printf("  Verify clients: %s\n", onOff(derefBool(s.VerifyClients)))

		if ip := derefString(s.Ipv4); ip != "" {
			fmt.Printf("  IPv4: %s\n", ip)
		}

		if ip := derefString(s.Ipv6); ip != "" {
			fmt.Printf("  IPv6: %s\n", ip)
		}
	}
}

func printCustomDERPRegions(regions []clientv1.DERPCustomRegion) {
	if len(regions) == 0 {
		return
	}

	fmt.Println("Relays you run:")

	for _, r := range regions {
		fmt.Printf("  %d %s (%s)\n", r.Id, r.Code, derefString(r.Name))

		if r.Nodes == nil {
			continue
		}

		for _, n := range *r.Nodes {
			extra := []string{}

			if ip := derefString(n.Ipv4); ip != "" {
				extra = append(extra, ip)
			}

			if ip := derefString(n.Ipv6); ip != "" {
				extra = append(extra, ip)
			}

			if p := derefInt(n.DerpPort); p != 0 {
				extra = append(extra, fmt.Sprintf("port %d", p))
			}

			if p := derefInt(n.StunPort); p != 0 {
				extra = append(extra, fmt.Sprintf("stun %d", p))
			}

			if derefBool(n.StunOnly) {
				extra = append(extra, "stun only")
			}

			line := "    " + n.HostName
			if len(extra) > 0 {
				line += " (" + strings.Join(extra, ", ") + ")"
			}

			fmt.Println(line)
		}
	}
}

func printDERPMap(regions []clientv1.DERPMapRegion) {
	if len(regions) == 0 {
		fmt.Println("Map: empty, machines must connect directly")

		return
	}

	fmt.Printf("Map: %d regions\n", len(regions))

	for _, r := range regions {
		fmt.Printf("  %4d %-12s %-28s %2d relays  %s\n", r.Id, r.Code, r.Name, r.Nodes, r.Source)
	}
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}

func derefInt(i *int64) int64 {
	if i == nil {
		return 0
	}

	return *i
}

func derefBool(b *bool) bool {
	return b != nil && *b
}
