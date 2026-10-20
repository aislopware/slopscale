package dnsprovider

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/libdns/acmedns"
	"github.com/libdns/libdns"
)

var (
	errAcmeDNSAccount = errors.New("acme-dns needs username, password, subdomain and server_url")
	// The CA accepts a value found in a shared record for any of the
	// names, so a node could publish another's under its own name.
	errAcmeDNSOneAccount = errors.New("acme-dns with one account answers every name's challenge from one record; " +
		"node certificates need a registration per name under config")
	errAcmeDNSShared = errors.New("acme-dns registration mapped to more than one name")
)

// acmeDNSSpacing separates updates to a registration. acme-dns stamps an
// update with the second it lands in and overwrites the earlier stamp,
// either one on a tie, so which value is older holds only while updates
// land in distinct seconds. The margin absorbs clock slew.
const acmeDNSSpacing = time.Second + 100*time.Millisecond

// acmeDNS is the acme-dns provider. A registration serves the two values
// written to it last, and deleting is a no-op, so an update overwrites
// whichever is older even if a CA is still validating it. acmeDNS mirrors
// what each registration serves and refuses such an update.
type acmeDNS struct {
	acmedns.Provider

	mu sync.Mutex
	// served is what each registration serves, oldest first.
	served map[string][2]acmeDNSValue
}

func newAcmeDNS() *acmeDNS {
	return &acmeDNS{served: make(map[string][2]acmeDNSValue)}
}

// acmeDNSValue is a value acme-dns serves. It stays live, and must not
// be overwritten, until it is deleted here.
type acmeDNSValue struct {
	text string
	live bool
	// written is when its update returned; acme-dns stamped it no later.
	written time.Time
}

func (p *acmeDNS) AppendRecords(
	ctx context.Context,
	zone string,
	recs []libdns.Record,
) ([]libdns.Record, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	var appended []libdns.Record

	for _, rec := range recs {
		rr := rec.RR()
		reg := p.registration(zone, rr.Name)
		served := p.served[reg]

		if served[0].live {
			return appended, fmt.Errorf(
				"%w: acme-dns registration %s would overwrite a value still in use",
				ErrTooManyChallenges, reg,
			)
		}

		select {
		case <-ctx.Done():
			return appended, fmt.Errorf("waiting to update acme-dns: %w", ctx.Err())
		case <-time.After(time.Until(served[1].written.Add(acmeDNSSpacing))):
		}

		got, err := p.Provider.AppendRecords(ctx, zone, []libdns.Record{rec})
		appended = append(appended, got...)

		// A failed update may still have reached acme-dns, so the older
		// value counts as overwritten either way.
		p.served[reg] = [2]acmeDNSValue{
			served[1],
			{text: rr.Data, live: err == nil, written: time.Now()},
		}

		if err != nil {
			return appended, fmt.Errorf("updating acme-dns: %w", err)
		}
	}

	return appended, nil
}

func (p *acmeDNS) DeleteRecords(
	ctx context.Context,
	zone string,
	recs []libdns.Record,
) ([]libdns.Record, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, rec := range recs {
		rr := rec.RR()
		reg := p.registration(zone, rr.Name)
		served := p.served[reg]

		// The older copy of a value written twice is the one to free, so
		// the newer keeps serving it.
		for i, v := range served {
			if v.live && v.text == rr.Data {
				served[i].live = false

				break
			}
		}

		p.served[reg] = served
	}

	deleted, err := p.Provider.DeleteRecords(ctx, zone, recs)
	if err != nil {
		return deleted, fmt.Errorf("deleting from acme-dns: %w", err)
	}

	return deleted, nil
}

// Isolates reports whether name has a registration of its own;
// CheckIsolation makes sure no other name is mapped to it.
func (p *acmeDNS) Isolates(name string) bool {
	_, ok := p.Configs[name]

	return ok
}

// CheckIsolation requires a registration per name: every name sharing a
// registration has its challenge answered from the same record.
func (p *acmeDNS) CheckIsolation() error {
	if p.Configs == nil {
		return errAcmeDNSOneAccount
	}

	names := make(map[string]string, len(p.Configs))

	for _, name := range slices.Sorted(maps.Keys(p.Configs)) {
		sub := p.Configs[name].Subdomain

		if other, shared := names[sub]; shared {
			return fmt.Errorf("%w: %s and %s share registration %s", errAcmeDNSShared, other, name, sub)
		}

		names[sub] = name
	}

	return nil
}

// registration returns the subdomain of the account acmedns writes a
// record with, picked the way acmedns picks it.
func (p *acmeDNS) registration(zone, name string) string {
	if p.Configs == nil {
		return p.Subdomain
	}

	domain := strings.Trim(libdns.AbsoluteName(name, zone), ".")

	return p.Configs[strings.TrimPrefix(domain, "_acme-challenge.")].Subdomain
}

// prepare restores the names under config. The configuration store
// splits keys on dots, so "laptop.ts.example.com" in the file arrives as
// nested maps; a registration is the map holding a subdomain, and the
// path to it is its name.
func (p *acmeDNS) prepare(input map[string]any) {
	nested, ok := input["config"].(map[string]any)
	if !ok {
		return
	}

	flat := make(map[string]any, len(nested))
	flattenRegistrations(flat, "", nested)
	input["config"] = flat
}

func flattenRegistrations(out map[string]any, prefix string, m map[string]any) {
	for k, v := range m {
		name := k
		if prefix != "" {
			name = prefix + "." + k
		}

		sub, ok := v.(map[string]any)
		if !ok {
			continue
		}

		if _, isRegistration := sub["subdomain"]; isRegistration {
			// acme-dns-client stores what /register returned, which
			// includes the source ranges the account accepts updates
			// from; publishing has no use for them.
			delete(sub, "allowfrom")

			out[strings.ToLower(name)] = sub

			continue
		}

		flattenRegistrations(out, name, sub)
	}
}

// validate reports at startup what acmedns would only report at the first
// certificate. It builds requests without checking the URL, so a bad
// server_url would panic.
func (p *acmeDNS) validate() error {
	accounts := []acmedns.DomainConfig{{
		Username:  p.Username,
		Password:  p.Password,
		Subdomain: p.Subdomain,
		ServerURL: p.ServerURL,
	}}
	if p.Configs != nil {
		accounts = slices.Collect(maps.Values(p.Configs))
	}

	for _, a := range accounts {
		u, err := url.Parse(a.ServerURL)
		missing := a.Username == "" || a.Password == "" || a.Subdomain == ""

		if missing || err != nil || u.Host == "" {
			return errAcmeDNSAccount
		}
	}

	return nil
}
