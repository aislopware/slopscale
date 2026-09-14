package dnsprovider

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"codeberg.org/miekg/dns"
	"codeberg.org/miekg/dns/dnsutil"
)

const (
	defaultPollInterval = 2 * time.Second
	queryTimeout        = 5 * time.Second
	minZoneLabels       = 2
)

var (
	// ErrNoNameservers is returned when no zone above the name has NS
	// records, so there is nothing to check the record against.
	ErrNoNameservers = errors.New("no authoritative nameservers found")
	// ErrRecordNotVisible is returned when the wait ends before every
	// nameserver serves the record.
	ErrRecordNotVisible = errors.New("record not yet visible")
)

// Checker asks the zone's authoritative nameservers for a record. A
// client accepts its ACME challenge the moment set-dns answers, and the
// CA looks the record up right away, so the answer has to wait until
// the record is actually served: a provider's API accepting a record is
// seconds ahead of its nameservers.
type Checker struct {
	// LookupNS finds a zone's nameservers; nil uses the system resolver.
	LookupNS func(ctx context.Context, zone string) ([]*net.NS, error)
	// Port is the nameservers' port; empty is 53.
	Port string
	// Interval is the time between polls; zero is two seconds.
	Interval time.Duration
}

// WaitVisible polls until every authoritative nameserver of the zone
// holding name serves a TXT record with value, or ctx ends.
func WaitVisible(ctx context.Context, name, value string) error {
	return Checker{}.WaitVisible(ctx, name, value)
}

// WaitVisible polls until every authoritative nameserver of the zone
// holding name serves a TXT record with value, or ctx ends.
func (c Checker) WaitVisible(ctx context.Context, name, value string) error {
	fqdn := dnsutil.Fqdn(strings.ToLower(name))

	servers, err := c.nameservers(ctx, fqdn)
	if err != nil {
		return err
	}

	interval := c.Interval
	if interval <= 0 {
		interval = defaultPollInterval
	}

	pending := servers

	for {
		pending = c.missing(ctx, pending, fqdn, value)
		if len(pending) == 0 {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("%w on %s: %w", ErrRecordNotVisible, strings.Join(pending, ", "), ctx.Err())
		case <-time.After(interval):
		}
	}
}

// nameservers walks up from the name's parent until a zone with NS
// records is found and returns its servers as host:port.
func (c Checker) nameservers(ctx context.Context, fqdn string) ([]string, error) {
	lookup := c.LookupNS
	if lookup == nil {
		lookup = net.DefaultResolver.LookupNS
	}

	port := c.Port
	if port == "" {
		port = "53"
	}

	labels := strings.Split(strings.TrimSuffix(fqdn, "."), ".")

	for start := 1; len(labels)-start >= minZoneLabels; start++ {
		zone := strings.Join(labels[start:], ".")

		records, err := lookup(ctx, zone)
		if err != nil || len(records) == 0 {
			continue
		}

		servers := make([]string, 0, len(records))
		for _, ns := range records {
			servers = append(servers, net.JoinHostPort(strings.TrimSuffix(ns.Host, "."), port))
		}

		return servers, nil
	}

	return nil, fmt.Errorf("%w for %s", ErrNoNameservers, fqdn)
}

// missing returns the servers that do not serve the record yet.
func (c Checker) missing(ctx context.Context, servers []string, fqdn, value string) []string {
	var pending []string

	for _, server := range servers {
		if !c.serves(ctx, server, fqdn, value) {
			pending = append(pending, server)
		}
	}

	return pending
}

// serves reports whether the server answers a TXT query for fqdn with
// value among the records. TCP, because a name collects one value per
// challenge and the answer can outgrow a plain UDP message.
func (c Checker) serves(ctx context.Context, server, fqdn, value string) bool {
	client := dns.NewClient()
	client.Dialer = &net.Dialer{Timeout: queryTimeout}
	client.ReadTimeout = queryTimeout
	client.WriteTimeout = queryTimeout

	msg := dns.NewMsg(fqdn, dns.TypeTXT)
	msg.RecursionDesired = false

	reply, _, err := client.Exchange(ctx, msg, "tcp", server)
	if err != nil || reply.Rcode != dns.RcodeSuccess {
		return false
	}

	for _, rr := range reply.Answer {
		txt, ok := rr.(*dns.TXT)
		if ok && strings.Join(txt.Txt, "") == value {
			return true
		}
	}

	return false
}
