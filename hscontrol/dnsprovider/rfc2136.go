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
	"github.com/libdns/libdns"
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
	errRFC2136Server = errors.New("server is required")
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
	// Server is the authoritative server, host:port.
	Server string `json:"server"`
	// Zone is read by the configuration as the default zone the records
	// go in; https_certificates.zone takes its place.
	Zone string `json:"zone,omitempty"`
	// TSIG signs the updates; empty sends them unsigned.
	TSIGKeyName   string `json:"tsig_key_name,omitempty"`
	TSIGSecret    string `json:"tsig_secret,omitempty"`
	TSIGAlgorithm string `json:"tsig_algorithm,omitempty"`

	keyName   string
	secret    []byte
	algorithm string
}

// NewRFC2136 builds the provider.
func NewRFC2136(server, keyName, secret, algorithm string) (*RFC2136, error) {
	p := &RFC2136{Server: server, TSIGKeyName: keyName, TSIGSecret: secret, TSIGAlgorithm: algorithm}

	err := p.validate()
	if err != nil {
		return nil, err
	}

	return p, nil
}

// AppendRecords sends one update inserting the records.
func (p *RFC2136) AppendRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error) {
	err := p.update(ctx, zone, recs, dns.ClassINET)
	if err != nil {
		return nil, err
	}

	return recs, nil
}

// DeleteRecords sends one update deleting exactly these records (RFC 2136
// section 2.5.4); other values at the names stay.
func (p *RFC2136) DeleteRecords(ctx context.Context, zone string, recs []libdns.Record) ([]libdns.Record, error) {
	for _, rec := range recs {
		// An empty value would be "every record at the name" to some
		// servers, including ones the operator keeps there.
		if rec.RR().Data == "" {
			return nil, fmt.Errorf("%w: refusing to delete a TXT record without a value", ErrProviderConfig)
		}
	}

	err := p.update(ctx, zone, recs, dns.ClassNONE)
	if err != nil {
		return nil, err
	}

	return recs, nil
}

func (p *RFC2136) validate() error {
	if p.Server == "" {
		return errRFC2136Server
	}

	if p.TSIGKeyName == "" {
		return nil
	}

	alg := p.TSIGAlgorithm
	if alg == "" {
		alg = "hmac-sha256"
	}

	algorithm, ok := tsigAlgorithms[strings.ToLower(strings.TrimSuffix(alg, "."))]
	if !ok {
		return fmt.Errorf("%w: %q", ErrTSIGAlgorithm, p.TSIGAlgorithm)
	}

	secret, err := base64.StdEncoding.DecodeString(p.TSIGSecret)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrTSIGSecret, err)
	}

	p.keyName = dnsutil.Fqdn(p.TSIGKeyName)
	p.secret = secret
	p.algorithm = algorithm

	return nil
}

// update sends one UPDATE adding (class IN) or deleting (class NONE) the
// records.
func (p *RFC2136) update(ctx context.Context, zone string, recs []libdns.Record, class uint16) error {
	zone = dnsutil.Fqdn(strings.ToLower(zone))

	txts, err := txtRecords(zone, recs)
	if err != nil {
		return err
	}

	// An UPDATE carries the zone as its question and the records to
	// change in the authority section (RFC 2136 section 2).
	msg := dns.NewMsg(zone, dns.TypeSOA)
	msg.Opcode = dns.OpcodeUpdate
	msg.RecursionDesired = false

	for i, txt := range txts {
		if !inZone(txt[0], zone) {
			return fmt.Errorf("%w: %s not under %s", ErrNameOutsideZone, txt[0], zone)
		}

		ttl := uint32(recs[i].RR().TTL.Seconds())
		if class == dns.ClassNONE {
			ttl = 0
		}

		msg.Ns = append(msg.Ns, &dns.TXT{
			Hdr: dns.Header{Name: dnsutil.Fqdn(txt[0]), Class: class, TTL: ttl},
			Txt: []string{txt[1]},
		})
	}

	var (
		signer dns.HmacTSIG
		tsig   dns.TSIGOption
	)

	if p.keyName != "" {
		signer = dns.HmacTSIG{Secret: p.secret}
		msg.Pseudo = append(msg.Pseudo, dns.NewTSIG(p.keyName, p.algorithm, 0))

		err = dns.TSIGSign(msg, signer, &tsig)
		if err != nil {
			return fmt.Errorf("signing dns update: %w", err)
		}
	}

	client := dns.NewClient()
	client.Dialer = &net.Dialer{Timeout: rfc2136Timeout}
	client.ReadTimeout = rfc2136Timeout
	client.WriteTimeout = rfc2136Timeout

	reply, _, err := client.Exchange(ctx, msg, "tcp", p.Server)
	if err != nil {
		return fmt.Errorf("dns update to %s: %w", p.Server, err)
	}

	if p.keyName != "" {
		// The client does not verify signatures itself; a server that
		// accepted a signed update signs its answer with the same key
		// (RFC 8945 section 5.3), so an unsigned or foreign answer is
		// treated like a refusal.
		err = verifyReplyTSIG(reply, p.keyName, signer, &tsig)
		if err != nil {
			return fmt.Errorf("%w: %s: %w", ErrReplyUnsigned, p.Server, err)
		}
	}

	if reply.Rcode != dns.RcodeSuccess {
		return fmt.Errorf("%w: %s answered %s", ErrUpdateRefused, p.Server, dns.RcodeToString[reply.Rcode])
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
