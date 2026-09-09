// Package dnsprovider publishes DNS records in the zone that holds the
// tailnet's MagicDNS names, so that machines can answer ACME DNS-01
// challenges for their own names; see docs/ref/https-certificates.md.
package dnsprovider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
)

// Provider publishes TXT records.
type Provider interface {
	// SetTXT adds value as a TXT record at name (a fully qualified name
	// without a trailing dot). Other values at the name stay, because a
	// certificate for a name and its wildcard needs two challenges at
	// once.
	SetTXT(ctx context.Context, name, value string) error
}

// ErrNameOutsideZone is returned for a record that does not belong to
// the provider's zone.
var ErrNameOutsideZone = errors.New("record name is outside the DNS zone")

// defaultTTL applies when the config sets none.
const defaultTTL = time.Minute

// New builds the provider the config names.
//
//nolint:ireturn // the caller wants any provider
func New(cfg types.HTTPSCertsConfig, baseDomain string) (Provider, error) {
	ttl := cfg.TTL
	if ttl <= 0 {
		ttl = defaultTTL
	}

	switch cfg.Provider {
	case types.DNSProviderCloudflare:
		return NewCloudflare(cfg.Cloudflare, ttl), nil
	case types.DNSProviderRFC2136:
		zone := cfg.RFC2136.Zone
		if zone == "" {
			zone = baseDomain
		}

		return NewRFC2136(cfg.RFC2136, zone, ttl)
	case types.DNSProviderCommand:
		return NewCommand(cfg.Command), nil
	default:
		return nil, fmt.Errorf("%w: %q", types.ErrDNSProviderUnknown, cfg.Provider)
	}
}

// inZone reports whether name is the zone or a name under it.
func inZone(name, zone string) bool {
	name = strings.TrimSuffix(strings.ToLower(name), ".")
	zone = strings.TrimSuffix(strings.ToLower(zone), ".")

	return name == zone || strings.HasSuffix(name, "."+zone)
}
