package types

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"tailscale.com/types/appctype"
	"tailscale.com/util/dnsname"
)

// An app is a set of domains reached through app connectors, following
// Tailscale's app connectors: the connector nodes, picked by tag, resolve
// the domains, advertise a route for every address they learn and forward
// the traffic; every client routes those addresses through them. The
// server tells the connectors what to watch through the
// tailscale.com/app-connectors capability and approves the routes they
// learn. See docs/ref/apps.md.

// AppConnectorID identifies an app row.
type AppConnectorID uint64

// String renders the ID in base 10.
func (id AppConnectorID) String() string {
	return strconv.FormatUint(uint64(id), 10)
}

// Uint64 returns the ID as a plain integer.
func (id AppConnectorID) Uint64() uint64 { return uint64(id) }

// AppConnectorAny is the connector selector that matches every node
// advertising as an app connector.
const AppConnectorAny = "*"

// AppConnector is an app: a name, the domains behind it, the tags of the
// nodes that connect to it and the routes they always advertise.
type AppConnector struct {
	ID          AppConnectorID
	Name        string
	Description string
	// Domains are what the connectors resolve, example.com or
	// *.example.com.
	Domains []string
	// Connectors are the tags of the connector nodes, or "*" for every
	// node running the connector service.
	Connectors []string
	// Routes are advertised by the connectors as they are, next to the
	// addresses they learn from the domains.
	Routes []netip.Prefix

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Attr is the capability value the client reads.
func (a *AppConnector) Attr() appctype.AppConnectorAttr {
	return appctype.AppConnectorAttr{
		Name:       a.Name,
		Domains:    slices.Clone(a.Domains),
		Routes:     slices.Clone(a.Routes),
		Connectors: slices.Clone(a.Connectors),
	}
}

// Selects reports whether a node is one of the app's connectors: it
// carries one of the app's tags, or the app takes every connector (*)
// and the node is tagged and runs the connector service. A connector is
// infrastructure, so like Tailscale's it is a tagged machine; a user's
// own machine never serves an app.
func (a *AppConnector) Selects(tags []string, runsConnector bool) bool {
	for _, c := range a.Connectors {
		if c == AppConnectorAny && runsConnector && len(tags) > 0 {
			return true
		}

		if c != AppConnectorAny && slices.Contains(tags, c) {
			return true
		}
	}

	return false
}

// Errors an app definition can fail with.
var (
	ErrAppConnectorNotFound  = errors.New("app not found")
	ErrAppConnectorNameTaken = errors.New("an app with that name exists")
	ErrAppConnectorName      = errors.New("app name must be 1 to 63 characters")
	ErrAppConnectorDomain    = errors.New("invalid app domain")
	ErrAppConnectorSelector  = errors.New("app connectors must be tags or *")
	ErrAppConnectorEmpty     = errors.New("an app needs at least one domain or route")
	ErrAppConnectorRoute     = errors.New("invalid app route")
)

const maxAppConnectorNameLength = 63

// ParseAppConnectorRoutes parses the static routes of an app.
func ParseAppConnectorRoutes(routes []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(routes))

	for _, r := range routes {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}

		p, err := netip.ParsePrefix(r)
		if err != nil {
			return nil, fmt.Errorf("%w: %q", ErrAppConnectorRoute, r)
		}

		if p.Bits() == 0 {
			return nil, fmt.Errorf("%w: %q covers everything", ErrAppConnectorRoute, r)
		}

		out = append(out, p.Masked())
	}

	slices.SortFunc(out, netip.Prefix.Compare)

	return slices.Compact(out), nil
}

// Normalize trims and sorts the app's lists and checks them; it is run
// before the app is stored.
func (a *AppConnector) Normalize() error {
	a.Name = strings.TrimSpace(a.Name)
	if a.Name == "" || len(a.Name) > maxAppConnectorNameLength {
		return ErrAppConnectorName
	}

	a.Description = strings.TrimSpace(a.Description)

	domains := make([]string, 0, len(a.Domains))

	for _, d := range a.Domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" {
			continue
		}

		d = strings.TrimSuffix(d, ".")
		bare := strings.TrimPrefix(d, "*.")

		_, err := dnsname.ToFQDN(bare)
		if err != nil || bare == "" || strings.Contains(bare, "*") {
			return fmt.Errorf("%w: %q", ErrAppConnectorDomain, d)
		}

		domains = append(domains, d)
	}

	slices.Sort(domains)
	a.Domains = slices.Compact(domains)

	connectors := make([]string, 0, len(a.Connectors))

	for _, c := range a.Connectors {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}

		if c != AppConnectorAny && !strings.HasPrefix(c, "tag:") {
			return fmt.Errorf("%w: %q", ErrAppConnectorSelector, c)
		}

		connectors = append(connectors, c)
	}

	slices.Sort(connectors)
	a.Connectors = slices.Compact(connectors)

	if len(a.Connectors) == 0 {
		a.Connectors = []string{AppConnectorAny}
	}

	if len(a.Domains) == 0 && len(a.Routes) == 0 {
		return ErrAppConnectorEmpty
	}

	return nil
}
