package dnsprovider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/miekg/dns"
)

const rfc2136Timeout = 30 * time.Second

var (
	// ErrTSIGAlgorithm is returned for an algorithm the resolver library
	// does not know.
	ErrTSIGAlgorithm = errors.New("unknown TSIG algorithm")
	// ErrUpdateRefused wraps the server's verdict on an update.
	ErrUpdateRefused = errors.New("dns update refused")
)

// tsigAlgorithms maps the config's names to the wire names.
var tsigAlgorithms = map[string]string{
	"hmac-md5":    dns.HmacMD5,
	"hmac-sha1":   dns.HmacSHA1,
	"hmac-sha224": dns.HmacSHA224,
	"hmac-sha256": dns.HmacSHA256,
	"hmac-sha384": dns.HmacSHA384,
	"hmac-sha512": dns.HmacSHA512,
}

// RFC2136 publishes with dynamic updates to an authoritative server,
// signed with TSIG when a key is configured.
type RFC2136 struct {
	server    string
	zone      string
	ttl       uint32
	keyName   string
	secret    string
	algorithm string
}

// NewRFC2136 builds the provider; the zone is where the records go.
func NewRFC2136(cfg types.RFC2136Config, zone string, ttl time.Duration) (*RFC2136, error) {
	p := &RFC2136{
		server: cfg.Server,
		zone:   dns.Fqdn(strings.ToLower(zone)),
		ttl:    uint32(ttl.Seconds()),
	}

	if cfg.TSIGKeyName != "" {
		algorithm, ok := tsigAlgorithms[strings.ToLower(strings.TrimSuffix(cfg.TSIGAlgorithm, "."))]
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrTSIGAlgorithm, cfg.TSIGAlgorithm)
		}

		p.keyName = dns.Fqdn(cfg.TSIGKeyName)
		p.secret = cfg.TSIGSecret
		p.algorithm = algorithm
	}

	return p, nil
}

// SetTXT sends one update inserting the record.
func (p *RFC2136) SetTXT(ctx context.Context, name, value string) error {
	fqdn := dns.Fqdn(strings.ToLower(name))
	if !inZone(fqdn, p.zone) {
		return fmt.Errorf("%w: %s not under %s", ErrNameOutsideZone, name, p.zone)
	}

	msg := new(dns.Msg)
	msg.SetUpdate(p.zone)
	msg.Insert([]dns.RR{&dns.TXT{
		Hdr: dns.RR_Header{Name: fqdn, Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: p.ttl},
		Txt: []string{value},
	}})

	client := &dns.Client{Net: "tcp", Timeout: rfc2136Timeout}

	if p.keyName != "" {
		client.TsigSecret = map[string]string{p.keyName: p.secret}
		msg.SetTsig(p.keyName, p.algorithm, 300, time.Now().Unix()) //nolint:mnd // TSIG fudge, the usual 5 minutes
	}

	reply, _, err := client.ExchangeContext(ctx, msg, p.server)
	if err != nil {
		return fmt.Errorf("dns update to %s: %w", p.server, err)
	}

	if reply.Rcode != dns.RcodeSuccess {
		return fmt.Errorf("%w: %s answered %s", ErrUpdateRefused, p.server, dns.RcodeToString[reply.Rcode])
	}

	return nil
}
