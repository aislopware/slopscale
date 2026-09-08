package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	clientv1 "github.com/juanfont/headscale/gen/client/v1"
	"github.com/spf13/cobra"
)

var errDNSFlagFormat = errors.New("invalid DNS flag format")

func init() {
	rootCmd.AddCommand(dnsCmd)
	dnsCmd.AddCommand(showDNSCmd)
	dnsCmd.AddCommand(setDNSCmd)
	dnsCmd.AddCommand(resetDNSCmd)

	setDNSCmd.Flags().StringSlice("nameserver", []string{}, "Global nameservers")
	setDNSCmd.Flags().Bool("override-local-dns", false, "Clients use the nameservers for every query")
	setDNSCmd.Flags().StringSlice(
		"split",
		[]string{},
		"Split DNS domain nameservers (domain=ns1,ns2, domain=ns1;ns2, or repeat)",
	)
	setDNSCmd.Flags().StringSlice(
		"use-with-exit-node",
		[]string{},
		"Global nameservers a machine keeps using while it has an exit node selected (needs --override-local-dns)",
	)
	setDNSCmd.Flags().StringSlice(
		"split-use-with-exit-node",
		[]string{},
		"Split DNS nameservers kept while an exit node is selected (domain=ns1,ns2 or repeat)",
	)
	setDNSCmd.Flags().StringSlice("search-domain", []string{}, "Search domains appended to base domain")
	setDNSCmd.Flags().StringArray("record", []string{}, "Extra DNS records (name=value or name=TYPE:value)")
	setDNSCmd.Flags().Bool("keep", false, "Keep existing settings and only replace specified fields")
}

var dnsCmd = &cobra.Command{
	Use:   "dns",
	Short: "Manage the tailnet's DNS settings",
}

var showDNSCmd = &cobra.Command{
	Use:     cmdShow,
	Short:   "Show DNS settings",
	Aliases: []string{"get", cmdList},
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.GetDNSWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("getting DNS settings: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printDNS(cmd, resp.JSON200, "")
		},
	),
}

var setDNSCmd = &cobra.Command{
	Use:   "set",
	Short: "Change tailnet DNS settings",
	Long: `Sets the tailnet's DNS settings. Without --keep, the request replaces all
settings, leaving omitted settings empty. Use --keep to start from the current
effective settings and only replace the fields whose flags were given.`,
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			var splitMap, splitKeepMap map[string]*[]string

			if cmd.Flags().Changed("split") {
				splitEntries, _ := cmd.Flags().GetStringSlice("split")

				parsed, err := parseSplitFlag(splitEntries)
				if err != nil {
					return err
				}

				splitMap = parsed
			}

			if cmd.Flags().Changed("split-use-with-exit-node") {
				splitEntries, _ := cmd.Flags().GetStringSlice("split-use-with-exit-node")

				parsed, err := parseSplitFlag(splitEntries)
				if err != nil {
					return err
				}

				splitKeepMap = parsed
			}

			var records []clientv1.DNSRecord

			if cmd.Flags().Changed("record") {
				recordEntries, _ := cmd.Flags().GetStringArray("record")

				parsed, err := parseRecordFlag(recordEntries)
				if err != nil {
					return err
				}

				records = parsed
			}

			var current *clientv1.DNS

			keep, _ := cmd.Flags().GetBool("keep")
			if keep {
				getResp, err := client.GetDNSWithResponse(ctx)
				if err != nil {
					return fmt.Errorf("getting DNS settings: %w", err)
				}

				if getResp.StatusCode() != http.StatusOK {
					return apiError(getResp.StatusCode(), getResp.ApplicationproblemJSONDefault)
				}

				current = getResp.JSON200
			}

			body := buildSetDNSBody(cmd, current, splitMap, splitKeepMap, records)

			resp, err := client.SetDNSWithResponse(ctx, body)
			if err != nil {
				return fmt.Errorf("setting DNS settings: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printDNS(cmd, resp.JSON200, "")
		},
	),
}

var resetDNSCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset DNS settings to the config file",
	RunE: clientRunE(
		func(ctx context.Context, client *clientv1.ClientWithResponses, cmd *cobra.Command, _ []string) error {
			resp, err := client.ResetDNSWithResponse(ctx)
			if err != nil {
				return fmt.Errorf("resetting DNS settings: %w", err)
			}

			if resp.StatusCode() != http.StatusOK {
				return apiError(resp.StatusCode(), resp.ApplicationproblemJSONDefault)
			}

			return printDNS(cmd, resp.JSON200, "DNS settings reset to the config file")
		},
	),
}

func buildSetDNSBody(
	cmd *cobra.Command,
	current *clientv1.DNS,
	splitMap, splitKeepMap map[string]*[]string,
	records []clientv1.DNSRecord,
) clientv1.SetDNSRequestBody {
	var body clientv1.SetDNSRequestBody

	if current != nil {
		body = clientv1.SetDNSRequestBody{
			Nameservers:          &current.Effective.Nameservers,
			OverrideLocalDns:     &current.Effective.OverrideLocalDns,
			SplitNameservers:     &current.Effective.SplitNameservers,
			UseWithExitNode:      &current.Effective.UseWithExitNode,
			SplitUseWithExitNode: &current.Effective.SplitUseWithExitNode,
			SearchDomains:        &current.Effective.SearchDomains,
			ExtraRecords:         &current.Effective.ExtraRecords,
		}
	}

	if cmd.Flags().Changed("use-with-exit-node") {
		keep, _ := cmd.Flags().GetStringSlice("use-with-exit-node")
		body.UseWithExitNode = &keep
	}

	if cmd.Flags().Changed("split-use-with-exit-node") {
		body.SplitUseWithExitNode = &splitKeepMap
	}

	if cmd.Flags().Changed("nameserver") {
		ns, _ := cmd.Flags().GetStringSlice("nameserver")
		body.Nameservers = &ns
	}

	if cmd.Flags().Changed("override-local-dns") {
		override, _ := cmd.Flags().GetBool("override-local-dns")
		body.OverrideLocalDns = &override
	}

	if cmd.Flags().Changed("split") {
		body.SplitNameservers = &splitMap
	}

	if cmd.Flags().Changed("search-domain") {
		sd, _ := cmd.Flags().GetStringSlice("search-domain")
		body.SearchDomains = &sd
	}

	if cmd.Flags().Changed("record") {
		body.ExtraRecords = &records
	}

	return body
}

func parseSplitFlag(entries []string) (map[string]*[]string, error) {
	result := make(map[string]*[]string)

	for _, entry := range entries {
		domain, nsList, err := parseSplitEntry(entry)
		if err != nil {
			return nil, err
		}

		existing, ok := result[domain]
		if !ok || existing == nil {
			result[domain] = &nsList

			continue
		}

		combined := append(*existing, nsList...)
		result[domain] = &combined
	}

	return result, nil
}

func parseSplitEntry(entry string) (string, []string, error) {
	domain, rest, found := strings.Cut(entry, "=")
	if !found || domain == "" || rest == "" {
		return "", nil, fmt.Errorf("%w: %q", errDNSFlagFormat, entry)
	}

	var servers []string

	for part := range strings.SplitSeq(rest, ";") {
		for s := range strings.SplitSeq(part, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				return "", nil, fmt.Errorf("%w: %q", errDNSFlagFormat, entry)
			}

			servers = append(servers, s)
		}
	}

	return domain, servers, nil
}

func parseRecordFlag(entries []string) ([]clientv1.DNSRecord, error) {
	var records []clientv1.DNSRecord

	for _, entry := range entries {
		for line := range strings.SplitSeq(entry, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			rec, err := parseRecordEntry(line)
			if err != nil {
				return nil, err
			}

			records = append(records, rec)
		}
	}

	return records, nil
}

func parseRecordEntry(entry string) (clientv1.DNSRecord, error) {
	name, rest, found := strings.Cut(entry, "=")
	if !found || name == "" || rest == "" {
		return clientv1.DNSRecord{}, fmt.Errorf("%w: %q", errDNSFlagFormat, entry)
	}

	typeStr, val, hasColon := strings.Cut(rest, ":")
	if hasColon {
		upper := strings.ToUpper(typeStr)
		if upper == "A" || upper == "AAAA" {
			if val == "" {
				return clientv1.DNSRecord{}, fmt.Errorf("%w: %q", errDNSFlagFormat, entry)
			}

			return clientv1.DNSRecord{
				Name:  name,
				Type:  clientv1.DNSRecordType(upper),
				Value: val,
			}, nil
		}
	}

	return clientv1.DNSRecord{
		Name:  name,
		Type:  "",
		Value: rest,
	}, nil
}

func printDNS(cmd *cobra.Command, dns *clientv1.DNS, prefix string) error {
	return printListOutput(cmd, dns, func() error {
		return printDNSHuman(dns, prefix)
	})
}

func printDNSHuman(dns *clientv1.DNS, prefix string) error {
	if prefix != "" {
		fmt.Println(prefix)
	}

	fmt.Printf("MagicDNS: %s\n", onOff(dns.MagicDns))

	if dns.BaseDomain != "" {
		fmt.Printf("Base domain: %s\n", dns.BaseDomain)
	} else {
		fmt.Println("Base domain: (none)")
	}

	if dns.Overridden {
		fmt.Println("Source: set through the API (reset with `headscale dns reset`)")
	} else {
		fmt.Println("Source: config file")
	}

	if dns.ExtraRecordsPath != "" {
		fmt.Printf("Extra records: read from %s\n", dns.ExtraRecordsPath)
	}

	if len(dns.Effective.Nameservers) > 0 {
		fmt.Printf("Nameservers: %s\n", strings.Join(dns.Effective.Nameservers, ", "))
	} else {
		fmt.Println("Nameservers: (none)")
	}

	fmt.Printf("Override local DNS: %s\n", onOff(dns.Effective.OverrideLocalDns))

	if len(dns.Effective.UseWithExitNode) > 0 {
		fmt.Printf("Kept with an exit node: %s\n", strings.Join(dns.Effective.UseWithExitNode, ", "))
	}

	err := printSplitDNS(dns.Effective.SplitNameservers)
	if err != nil {
		return err
	}

	if len(dns.Effective.SplitUseWithExitNode) > 0 {
		fmt.Println("Split DNS kept with an exit node:")

		err = printSplitDNS(dns.Effective.SplitUseWithExitNode)
		if err != nil {
			return err
		}
	}

	if len(dns.Effective.SearchDomains) > 0 {
		fmt.Printf("Search domains: %s\n", strings.Join(dns.Effective.SearchDomains, ", "))
	} else {
		fmt.Println("Search domains: (none)")
	}

	return printExtraRecords(dns.Effective.ExtraRecords)
}

func printSplitDNS(split map[string]*[]string) error {
	if len(split) == 0 {
		return nil
	}

	domains := make([]string, 0, len(split))
	for domain := range split {
		domains = append(domains, domain)
	}

	slices.Sort(domains)

	rows := make([][]string, 0, len(domains))
	for _, domain := range domains {
		ns := split[domain]

		var nsStr string
		if ns != nil {
			nsStr = strings.Join(*ns, ", ")
		}

		rows = append(rows, []string{domain, nsStr})
	}

	return renderTable([]string{"Domain", "Nameservers"}, rows)
}

func printExtraRecords(records []clientv1.DNSRecord) error {
	if len(records) == 0 {
		return nil
	}

	rows := make([][]string, 0, len(records))
	for _, r := range records {
		rows = append(rows, []string{r.Name, string(r.Type), r.Value})
	}

	return renderTable([]string{"Name", "Type", "Value"}, rows)
}
