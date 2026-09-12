package dnsprovider

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"codeberg.org/miekg/dns"
	"codeberg.org/miekg/dns/dnsutil"
	"github.com/aislopware/slopscale/hscontrol/types"
)

const rfc2136Timeout = 30 * time.Second

var (
	// ErrTSIGAlgorithm is returned for an algorithm the resolver library
	// does not know.
	ErrTSIGAlgorithm = errors.New("unknown TSIG algorithm")
	// ErrTSIGSecret is returned when the secret is not base64, the form
	// BIND key files and every provider use.
	ErrTSIGSecret = errors.New("TSIG secret is not base64")
	// ErrUpdateRefused wraps the server's verdict on an update.
	ErrUpdateRefused = errors.New("dns update refused")
	// ErrReplyUnsigned is returned when a signed update gets a reply the
	// server did not sign, or signed with a different key.
	ErrReplyUnsigned = errors.New("dns update reply not signed with the key")
	errNoTSIG        = errors.New("no TSIG record")
)

// tsigAlgorithms maps the config's names to the wire names. hmac-md5 is
// gone: RFC 8945 retired it and the library no longer signs with it.
var tsigAlgorithms = map[string]string{
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
	secret    []byte
	algorithm string
}

// NewRFC2136 builds the provider; the zone is where the records go.
func NewRFC2136(cfg types.RFC2136Config, zone string, ttl time.Duration) (*RFC2136, error) {
	p := &RFC2136{
		server: cfg.Server,
		zone:   dnsutil.Fqdn(strings.ToLower(zone)),
		ttl:    uint32(ttl.Seconds()),
	}

	if cfg.TSIGKeyName != "" {
		algorithm, ok := tsigAlgorithms[strings.ToLower(strings.TrimSuffix(cfg.TSIGAlgorithm, "."))]
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrTSIGAlgorithm, cfg.TSIGAlgorithm)
		}

		secret, err := base64.StdEncoding.DecodeString(cfg.TSIGSecret)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrTSIGSecret, err)
		}

		p.keyName = dnsutil.Fqdn(cfg.TSIGKeyName)
		p.secret = secret
		p.algorithm = algorithm
	}

	return p, nil
}

// SetTXT sends one update inserting the record.
func (p *RFC2136) SetTXT(ctx context.Context, name, value string) error {
	fqdn := dnsutil.Fqdn(strings.ToLower(name))
	if !inZone(fqdn, p.zone) {
		return fmt.Errorf("%w: %s not under %s", ErrNameOutsideZone, name, p.zone)
	}

	// An UPDATE carries the zone as its question and the records to
	// insert in the authority section (RFC 2136 section 2).
	msg := dns.NewMsg(p.zone, dns.TypeSOA)
	msg.Opcode = dns.OpcodeUpdate
	msg.RecursionDesired = false
	msg.Ns = []dns.RR{&dns.TXT{
		Hdr: dns.Header{Name: fqdn, Class: dns.ClassINET, TTL: p.ttl},
		Txt: []string{value},
	}}

	var (
		signer dns.HmacTSIG
		tsig   dns.TSIGOption
	)

	if p.keyName != "" {
		signer = dns.HmacTSIG{Secret: p.secret}
		msg.Pseudo = append(msg.Pseudo, dns.NewTSIG(p.keyName, p.algorithm, 0))

		err := dns.TSIGSign(msg, signer, &tsig)
		if err != nil {
			return fmt.Errorf("signing dns update: %w", err)
		}
	}

	client := dns.NewClient()
	client.Dialer = &net.Dialer{Timeout: rfc2136Timeout}
	client.ReadTimeout = rfc2136Timeout
	client.WriteTimeout = rfc2136Timeout

	reply, _, err := client.Exchange(ctx, msg, "tcp", p.server)
	if err != nil {
		return fmt.Errorf("dns update to %s: %w", p.server, err)
	}

	if p.keyName != "" {
		// The client does not verify signatures itself; a server that
		// accepted a signed update signs its answer with the same key
		// (RFC 8945 section 5.3), so an unsigned or foreign answer is
		// treated like a refusal.
		err = verifyReplyTSIG(reply, p.keyName, signer, &tsig)
		if err != nil {
			return fmt.Errorf("%w: %s: %w", ErrReplyUnsigned, p.server, err)
		}
	}

	if reply.Rcode != dns.RcodeSuccess {
		return fmt.Errorf("%w: %s answered %s", ErrUpdateRefused, p.server, dns.RcodeToString[reply.Rcode])
	}

	return nil
}

// verifyReplyTSIG checks the reply is signed by keyName over the request's
// MAC, which tsig carries from signing.
func verifyReplyTSIG(reply *dns.Msg, keyName string, signer dns.HmacTSIG, tsig *dns.TSIGOption) error {
	if len(reply.Pseudo) == 0 {
		return errNoTSIG
	}

	rr, ok := reply.Pseudo[len(reply.Pseudo)-1].(*dns.TSIG)
	if !ok {
		return errNoTSIG
	}

	if !strings.EqualFold(rr.Hdr.Name, keyName) {
		return fmt.Errorf("%w: signed with %s", errNoTSIG, rr.Hdr.Name)
	}

	err := dns.TSIGVerify(reply, signer, tsig)
	if err != nil {
		return fmt.Errorf("verifying reply signature: %w", err)
	}

	return nil
}
