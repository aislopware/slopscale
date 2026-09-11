package types

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/aislopware/slopscale/hscontrol/egress"
	"tailscale.com/tailcfg"
)

// SettingDERP is the settings row that holds a [DERPSettings] as JSON. When
// the row exists the tailnet uses it instead of the derp section of the
// config file; deleting it returns to the file.
const SettingDERP SettingKey = "derp"

// TailscaleDERPMapURL is the public map of Tailscale's own relays, the
// config file's default source.
const TailscaleDERPMapURL = "https://controlplane.tailscale.com/derpmap/default"

// DERPMinUpdateFrequency is the shortest interval the map is refetched at.
const DERPMinUpdateFrequency = time.Minute

// DERPSettings is the part of the DERP configuration an operator can change
// while the server runs: where the map comes from, extra relays, how often
// the map is refetched and the embedded relay. The map files in derp.paths,
// the embedded relay's key file and
// derp.server.automatically_add_embedded_derp_region stay in the config
// file.
type DERPSettings struct {
	// URLs are the DERP maps fetched and merged, in order; a later map's
	// region replaces an earlier one with the same ID.
	URLs []string `json:"urls"`
	// Regions are relays the operator runs, added after the fetched maps.
	Regions []DERPCustomRegion `json:"regions"`
	// AutoUpdate refetches URLs every UpdateFrequency.
	AutoUpdate      bool          `json:"autoUpdate"`
	UpdateFrequency time.Duration `json:"updateFrequency"`
	// Server is the relay slopscale runs itself.
	Server DERPServerSettings `json:"server"`
}

// DERPServerSettings configures the embedded relay. It is served on the
// server_url, so that must be https, and STUN on its own UDP port unless
// STUN is off.
type DERPServerSettings struct {
	Enabled bool `json:"enabled"`
	// RegionID is the region the relay is published as; it replaces a
	// fetched region with the same ID.
	RegionID   tailcfg.DERPRegionID `json:"regionID"`
	RegionCode string               `json:"regionCode"`
	RegionName string               `json:"regionName"`
	// VerifyClients admits only nodes of this tailnet.
	VerifyClients bool `json:"verifyClients"`
	// STUNEnabled answers STUN on STUNAddr and publishes the port in the
	// map. Off, the region is published without STUN and STUNAddr is
	// ignored: for a relay on the machines' own network, reached through
	// the router, whose STUN replies would name the router instead of the
	// public address and make every machine there report a hard NAT.
	STUNEnabled bool `json:"stunEnabled"`
	// STUNAddr is the UDP host:port STUN listens on.
	STUNAddr string `json:"stunAddr"`
	// IPv4 and IPv6 are the relay's public addresses, published so clients
	// reach it while DNS is down; empty leaves them to DNS.
	IPv4 string `json:"ipv4"`
	IPv6 string `json:"ipv6"`
}

// UnmarshalJSON reads the settings with STUN on unless the JSON turns it
// off, so a settings row stored before the switch existed keeps STUN.
func (s *DERPServerSettings) UnmarshalJSON(data []byte) error {
	type plain DERPServerSettings

	out := plain{STUNEnabled: true}

	err := json.Unmarshal(data, &out)
	if err != nil {
		return fmt.Errorf("decoding embedded relay settings: %w", err)
	}

	*s = DERPServerSettings(out)

	return nil
}

// DERPCustomRegion is a region of relays the operator runs.
type DERPCustomRegion struct {
	ID   tailcfg.DERPRegionID `json:"id"`
	Code string               `json:"code"`
	Name string               `json:"name"`
	// Nodes are the relays of the region, at least one.
	Nodes []DERPCustomNode `json:"nodes"`
}

// DERPCustomNode is one relay of a custom region, as the Tailscale derper
// serves it.
type DERPCustomNode struct {
	// Name is unique within the region; empty takes the host name.
	Name string `json:"name"`
	// HostName is the relay's DNS name, which its certificate must match.
	HostName string `json:"hostName"`
	// IPv4 and IPv6 are optional fixed addresses; "none" means the relay
	// has no address of that family.
	IPv4 string `json:"ipv4"`
	IPv6 string `json:"ipv6"`
	// DERPPort is the relay's HTTPS port; 0 means 443.
	DERPPort int `json:"derpPort"`
	// STUNPort is the relay's UDP STUN port; 0 means 3478.
	STUNPort int `json:"stunPort"`
	// STUNOnly marks a node that only answers STUN, never relays.
	STUNOnly bool `json:"stunOnly"`
	// CanPort80 marks a relay that also serves plain HTTP on port 80 for
	// clients behind TLS-hostile middleboxes.
	CanPort80 bool `json:"canPort80"`
}

// ErrDERPSettingsInvalid is wrapped by every error [DERPSettings.Validate]
// returns, so callers can map the family to one response.
var ErrDERPSettingsInvalid = errors.New("invalid derp settings")

var (
	ErrDERPURLInvalid = fmt.Errorf(
		"%w: a map URL must be an http or https URL", ErrDERPSettingsInvalid,
	)
	ErrDERPRegionIDInvalid = fmt.Errorf(
		"%w: a region ID must be a positive number", ErrDERPSettingsInvalid,
	)
	ErrDERPRegionIDTaken = fmt.Errorf(
		"%w: two regions share an ID", ErrDERPSettingsInvalid,
	)
	ErrDERPRegionCodeEmpty = fmt.Errorf(
		"%w: a region needs a code", ErrDERPSettingsInvalid,
	)
	ErrDERPRegionNoNodes = fmt.Errorf(
		"%w: a region needs at least one relay", ErrDERPSettingsInvalid,
	)
	ErrDERPNodeHostEmpty = fmt.Errorf(
		"%w: a relay needs a host name", ErrDERPSettingsInvalid,
	)
	ErrDERPNodeNameTaken = fmt.Errorf(
		"%w: two relays of a region share a name", ErrDERPSettingsInvalid,
	)
	ErrDERPNodeIPInvalid = fmt.Errorf(
		"%w: a relay address must be an IP of its family, or none", ErrDERPSettingsInvalid,
	)
	ErrDERPPortInvalid = fmt.Errorf(
		"%w: a port must be between 0 and 65535", ErrDERPSettingsInvalid,
	)
	ErrDERPUpdateFrequencyTooShort = fmt.Errorf(
		"%w: the map cannot be refetched more often than every minute", ErrDERPSettingsInvalid,
	)
	ErrDERPSTUNAddrInvalid = fmt.Errorf(
		"%w: the STUN address must be a host:port", ErrDERPSettingsInvalid,
	)
	ErrDERPServerIPInvalid = fmt.Errorf(
		"%w: the embedded relay's address must be an IP of its family", ErrDERPSettingsInvalid,
	)
	// ErrDERPMapEmpty is returned when the settings leave no relay at
	// all; clients need at least one region to find each other.
	ErrDERPMapEmpty = fmt.Errorf(
		"%w: the settings leave no relay; keep a map URL, a relay or the embedded relay",
		ErrDERPSettingsInvalid,
	)
)

// Settings returns the runtime-changeable part of the config file's derp
// section, the starting point for an override.
func (d *DERPConfig) Settings() DERPSettings {
	urls := make([]string, 0, len(d.URLs))
	for _, u := range d.URLs {
		urls = append(urls, u.String())
	}

	return DERPSettings{
		URLs:            urls,
		Regions:         []DERPCustomRegion{},
		AutoUpdate:      d.AutoUpdate,
		UpdateFrequency: d.UpdateFrequency,
		Server: DERPServerSettings{
			Enabled:       d.ServerEnabled,
			RegionID:      d.ServerRegionID,
			RegionCode:    d.ServerRegionCode,
			RegionName:    d.ServerRegionName,
			VerifyClients: d.ServerVerifyClients,
			STUNEnabled:   d.STUNEnabled,
			STUNAddr:      d.STUNAddr,
			IPv4:          d.IPv4,
			IPv6:          d.IPv6,
		},
	}
}

// Clone returns a deep copy.
func (s DERPSettings) Clone() DERPSettings {
	out := s
	out.URLs = slices.Clone(s.URLs)
	out.Regions = make([]DERPCustomRegion, 0, len(s.Regions))

	for _, r := range s.Regions {
		r.Nodes = slices.Clone(r.Nodes)
		out.Regions = append(out.Regions, r)
	}

	return out
}

// Normalize trims every string, drops empty URLs, fills the defaults an
// operator may leave out (a region's name from its code, a relay's name
// from its host) and returns the result.
func (s DERPSettings) Normalize() DERPSettings {
	out := s.Clone()

	for i, u := range out.URLs {
		out.URLs[i] = strings.TrimSpace(u)
	}

	out.URLs = slices.DeleteFunc(out.URLs, func(u string) bool { return u == "" })
	if out.URLs == nil {
		out.URLs = []string{}
	}

	for i := range out.Regions {
		r := &out.Regions[i]
		r.Code = strings.TrimSpace(r.Code)
		r.Name = strings.TrimSpace(r.Name)

		if r.Name == "" {
			r.Name = r.Code
		}

		for j := range r.Nodes {
			n := &r.Nodes[j]
			n.HostName = strings.TrimSpace(n.HostName)
			n.Name = strings.TrimSpace(n.Name)
			n.IPv4 = strings.TrimSpace(n.IPv4)
			n.IPv6 = strings.TrimSpace(n.IPv6)

			if n.Name == "" {
				n.Name = n.HostName
			}
		}
	}

	srv := &out.Server
	srv.RegionCode = strings.TrimSpace(srv.RegionCode)
	srv.RegionName = strings.TrimSpace(srv.RegionName)
	srv.STUNAddr = strings.TrimSpace(srv.STUNAddr)
	srv.IPv4 = strings.TrimSpace(srv.IPv4)
	srv.IPv6 = strings.TrimSpace(srv.IPv6)

	if srv.RegionName == "" {
		srv.RegionName = srv.RegionCode
	}

	return out
}

// Validate checks a normalized settings value. Whether the embedded relay
// can run on this server (https server_url, key file) is the state's
// call, as it depends on the rest of the config.
func (s DERPSettings) Validate() error {
	for _, u := range s.URLs {
		parsed, err := url.Parse(u)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return fmt.Errorf("%w: %q", ErrDERPURLInvalid, u)
		}

		// The map is fetched by the server, so the URL must not point at
		// what only the server can reach; see hscontrol/egress.
		err = egress.Default().CheckHost(parsed.Host)
		if err != nil {
			return fmt.Errorf("DERP map URL points at a %w", err)
		}
	}

	if s.AutoUpdate && s.UpdateFrequency < DERPMinUpdateFrequency {
		return fmt.Errorf("%w: %s", ErrDERPUpdateFrequencyTooShort, s.UpdateFrequency)
	}

	seen := make(map[tailcfg.DERPRegionID]struct{}, len(s.Regions)+1)

	for _, r := range s.Regions {
		err := r.validate()
		if err != nil {
			return err
		}

		if _, dup := seen[r.ID]; dup {
			return fmt.Errorf("%w: %d", ErrDERPRegionIDTaken, r.ID)
		}

		seen[r.ID] = struct{}{}
	}

	if !s.Server.Enabled {
		return nil
	}

	err := s.Server.validate()
	if err != nil {
		return err
	}

	if _, dup := seen[s.Server.RegionID]; dup {
		return fmt.Errorf("%w: %d is also the embedded relay's", ErrDERPRegionIDTaken, s.Server.RegionID)
	}

	return nil
}

func (s DERPServerSettings) validate() error {
	if s.RegionID <= 0 {
		return fmt.Errorf("%w: embedded relay region %d", ErrDERPRegionIDInvalid, s.RegionID)
	}

	if s.RegionCode == "" {
		return fmt.Errorf("%w: embedded relay", ErrDERPRegionCodeEmpty)
	}

	if s.STUNEnabled {
		err := s.validateSTUNAddr()
		if err != nil {
			return err
		}
	}

	if s.IPv4 != "" {
		addr, err := netip.ParseAddr(s.IPv4)
		if err != nil || !addr.Is4() {
			return fmt.Errorf("%w: %q", ErrDERPServerIPInvalid, s.IPv4)
		}
	}

	if s.IPv6 != "" {
		addr, err := netip.ParseAddr(s.IPv6)
		if err != nil || !addr.Is6() {
			return fmt.Errorf("%w: %q", ErrDERPServerIPInvalid, s.IPv6)
		}
	}

	return nil
}

func (s DERPServerSettings) validateSTUNAddr() error {
	_, port, err := net.SplitHostPort(s.STUNAddr)
	if err != nil {
		return fmt.Errorf("%w: %q", ErrDERPSTUNAddrInvalid, s.STUNAddr)
	}

	// Port 0 lets the kernel pick, which tests rely on.
	p, err := strconv.Atoi(port)
	if err != nil || p < 0 || p > 65535 {
		return fmt.Errorf("%w: %q", ErrDERPSTUNAddrInvalid, s.STUNAddr)
	}

	return nil
}

// validateDERPNodeIP accepts an empty value, the literal "none" the
// Tailscale map format uses for "no address of this family", or an
// address of the family.
func validateDERPNodeIP(value string, v4 bool) error {
	if value == "" || value == "none" {
		return nil
	}

	addr, err := netip.ParseAddr(value)
	if err != nil || addr.Is4() != v4 {
		return fmt.Errorf("%w: %q", ErrDERPNodeIPInvalid, value)
	}

	return nil
}

// Region returns the region in the form the DERP map carries.
func (r DERPCustomRegion) Region() *tailcfg.DERPRegion {
	region := &tailcfg.DERPRegion{
		RegionID:   r.ID,
		RegionCode: r.Code,
		RegionName: r.Name,
		Nodes:      make([]*tailcfg.DERPNode, 0, len(r.Nodes)),
	}

	for _, n := range r.Nodes {
		region.Nodes = append(region.Nodes, &tailcfg.DERPNode{
			Name:      n.Name,
			RegionID:  r.ID,
			HostName:  n.HostName,
			IPv4:      n.IPv4,
			IPv6:      n.IPv6,
			DERPPort:  n.DERPPort,
			STUNPort:  n.STUNPort,
			STUNOnly:  n.STUNOnly,
			CanPort80: n.CanPort80,
		})
	}

	return region
}

// RegionsMap returns the custom regions keyed by ID, the shape the map
// merge takes.
func (s DERPSettings) RegionsMap() *tailcfg.DERPMap {
	dm := &tailcfg.DERPMap{Regions: make(map[tailcfg.DERPRegionID]*tailcfg.DERPRegion, len(s.Regions))}
	for _, r := range s.Regions {
		dm.Regions[r.ID] = r.Region()
	}

	return dm
}

func (r DERPCustomRegion) validate() error {
	if r.ID <= 0 {
		return fmt.Errorf("%w: %d", ErrDERPRegionIDInvalid, r.ID)
	}

	if r.Code == "" {
		return fmt.Errorf("%w: region %d", ErrDERPRegionCodeEmpty, r.ID)
	}

	if len(r.Nodes) == 0 {
		return fmt.Errorf("%w: region %s", ErrDERPRegionNoNodes, r.Code)
	}

	names := make(map[string]struct{}, len(r.Nodes))

	for _, n := range r.Nodes {
		if n.HostName == "" {
			return fmt.Errorf("%w: region %s", ErrDERPNodeHostEmpty, r.Code)
		}

		if _, dup := names[n.Name]; dup {
			return fmt.Errorf("%w: %s in region %s", ErrDERPNodeNameTaken, n.Name, r.Code)
		}

		names[n.Name] = struct{}{}

		err := validateDERPNodeIP(n.IPv4, true)
		if err != nil {
			return fmt.Errorf("%w: %s", err, n.HostName)
		}

		err = validateDERPNodeIP(n.IPv6, false)
		if err != nil {
			return fmt.Errorf("%w: %s", err, n.HostName)
		}

		for _, port := range []int{n.DERPPort, n.STUNPort} {
			if port < 0 || port > 65535 {
				return fmt.Errorf("%w: %d on %s", ErrDERPPortInvalid, port, n.HostName)
			}
		}
	}

	return nil
}
