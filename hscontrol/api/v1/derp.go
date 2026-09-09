package apiv1

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/state"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/types/change"
	"github.com/danielgtaylor/huma/v2"
	"tailscale.com/tailcfg"
)

func init() {
	registrations = append(registrations, registerDERP)
}

const tagDERP = "DERP"

// DERPRelay is one relay of a region the operator runs. Every field but
// the host name may be left out.
type DERPRelay struct {
	// Name is unique within the region; empty takes the host name.
	Name string `json:"name,omitempty" required:"false"`
	// HostName is the DNS name the relay's certificate matches.
	HostName string `json:"hostName"`
	// IPv4 and IPv6 are fixed addresses, or "none".
	IPv4 string `json:"ipv4,omitempty" required:"false"`
	IPv6 string `json:"ipv6,omitempty" required:"false"`
	// DERPPort is the HTTPS port; 0 means 443.
	DERPPort int `json:"derpPort,omitempty" required:"false"`
	// STUNPort is the UDP STUN port; 0 means 3478.
	STUNPort int `json:"stunPort,omitempty" required:"false"`
	// STUNOnly marks a relay that answers STUN but never relays.
	STUNOnly bool `json:"stunOnly,omitempty" required:"false"`
	// CanPort80 marks a relay that also serves plain HTTP on port 80.
	CanPort80 bool `json:"canPort80,omitempty" required:"false"`
}

// DERPCustomRegion is a region of relays the operator runs.
type DERPCustomRegion struct {
	// ID replaces a fetched region with the same ID.
	ID int `json:"id"`
	// Code is the short code clients show.
	Code string `json:"code"`
	// Name is empty to take the code.
	Name  string      `json:"name,omitempty"  required:"false"`
	Nodes []DERPRelay `json:"nodes,omitempty" nullable:"false" required:"false"`
}

// DERPServerSettings configures the relay slopscale runs itself. Only
// enabled is needed; the other fields matter while it is on.
type DERPServerSettings struct {
	Enabled bool `json:"enabled"`
	// RegionID replaces a fetched region with the same ID.
	RegionID   int    `json:"regionId,omitempty"   required:"false"`
	RegionCode string `json:"regionCode,omitempty" required:"false"`
	// RegionName is empty to take the code.
	RegionName string `json:"regionName,omitempty" required:"false"`
	// VerifyClients admits only this tailnet's machines. Left out of a
	// request it is on; a response always carries it.
	VerifyClients *bool `json:"verifyClients,omitempty" required:"false"`
	// STUNAddr is the UDP host:port STUN listens on.
	STUNAddr string `json:"stunAddr,omitempty" required:"false"`
	// IPv4 and IPv6 are public addresses published next to the host name.
	IPv4 string `json:"ipv4,omitempty" required:"false"`
	IPv6 string `json:"ipv6,omitempty" required:"false"`
}

// DERPSettings is the part of the DERP configuration that can change while
// the server runs.
type DERPSettings struct {
	// URLs are maps fetched and merged in order.
	URLs []string `json:"urls" nullable:"false"`
	// Regions are relays the operator runs.
	Regions []DERPCustomRegion `json:"regions" nullable:"false"`
	// AutoUpdate refetches the maps every UpdateFrequency.
	AutoUpdate bool `json:"autoUpdate"`
	// UpdateFrequency is a Go duration, at least 1m.
	UpdateFrequency string             `json:"updateFrequency"`
	Server          DERPServerSettings `json:"server"`
}

// SetDERPRequestBody is the body of setDERP. The request replaces every
// setting, so send the whole configuration.
type SetDERPRequestBody struct {
	URLs            []string           `json:"urls,omitempty"            required:"false"`
	Regions         []DERPCustomRegion `json:"regions,omitempty"         required:"false"`
	AutoUpdate      bool               `json:"autoUpdate,omitempty"      required:"false"`
	UpdateFrequency string             `json:"updateFrequency,omitempty" required:"false"`
	Server          DERPServerSettings `json:"server"`
}

// DERPMapRegion is one region of the map clients receive.
type DERPMapRegion struct {
	ID    int    `json:"id"`
	Code  string `json:"code"`
	Name  string `json:"name"`
	Nodes int    `json:"nodes"`
	// Source says where the region came from.
	Source string `enum:"tailscale,url,file,custom,embedded,config" json:"source"`
}

// DERP is the DERP configuration in force, where it comes from and the
// map clients receive.
type DERP struct {
	Effective  DERPSettings `doc:"What the server runs with."               json:"effective"`
	FromFile   DERPSettings `doc:"The config file's values."                json:"fromFile"`
	Overridden bool         `doc:"Settings set through the API are in use." json:"overridden"`
	// Paths are the config file's map files, merged after the URLs.
	Paths []string `json:"paths" nullable:"false"`
	// AutoAddEmbedded is off when a map file describes the embedded relay.
	AutoAddEmbedded bool `json:"autoAddEmbedded"`
	// RelayAvailable reports whether the server has a relay key, so the
	// embedded relay can be turned on.
	RelayAvailable bool `json:"relayAvailable"`
	// RelayRunning reports whether the embedded relay is serving.
	RelayRunning bool `json:"relayRunning"`
	// STUNAddr is the address STUN is bound to while the relay runs.
	STUNAddr  string `json:"stunAddr"`
	ServerURL string `doc:"Where the embedded relay is reached." json:"serverUrl"`
	// Regions are the map's regions in ID order.
	Regions []DERPMapRegion `json:"regions" nullable:"false"`
	// FetchedAt is when the map sources were last fetched.
	FetchedAt time.Time `json:"fetchedAt"`
	// FetchError is the last failed refresh, empty after a good one.
	FetchError string `json:"fetchError"`
}

type (
	derpOutput struct {
		// ETag identifies the effective settings the response reports, for
		// a later If-Match on setDERP.
		ETag string `header:"ETag"`
		Body DERP
	}

	setDERPInput struct {
		// IfMatch is an ETag an earlier read returned. Left out, the request
		// writes whatever it finds; set, it is refused with 412 when the
		// settings changed since that read. "*" always matches.
		IfMatch string `header:"If-Match"`
		Body    SetDERPRequestBody
	}
)

func derpRelaysFrom(nodes []types.DERPCustomNode) []DERPRelay {
	out := make([]DERPRelay, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, DERPRelay{
			Name:      n.Name,
			HostName:  n.HostName,
			IPv4:      n.IPv4,
			IPv6:      n.IPv6,
			DERPPort:  n.DERPPort,
			STUNPort:  n.STUNPort,
			STUNOnly:  n.STUNOnly,
			CanPort80: n.CanPort80,
		})
	}

	return out
}

func derpRelaysTo(nodes []DERPRelay) []types.DERPCustomNode {
	out := make([]types.DERPCustomNode, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, types.DERPCustomNode{
			Name:      n.Name,
			HostName:  n.HostName,
			IPv4:      n.IPv4,
			IPv6:      n.IPv6,
			DERPPort:  n.DERPPort,
			STUNPort:  n.STUNPort,
			STUNOnly:  n.STUNOnly,
			CanPort80: n.CanPort80,
		})
	}

	return out
}

func derpRegionsFrom(regions []types.DERPCustomRegion) []DERPCustomRegion {
	out := make([]DERPCustomRegion, 0, len(regions))
	for _, r := range regions {
		out = append(out, DERPCustomRegion{
			ID:    int(r.ID),
			Code:  r.Code,
			Name:  r.Name,
			Nodes: derpRelaysFrom(r.Nodes),
		})
	}

	return out
}

func derpRegionsTo(regions []DERPCustomRegion) []types.DERPCustomRegion {
	out := make([]types.DERPCustomRegion, 0, len(regions))
	for _, r := range regions {
		out = append(out, types.DERPCustomRegion{
			ID:    tailcfg.DERPRegionID(r.ID),
			Code:  r.Code,
			Name:  r.Name,
			Nodes: derpRelaysTo(r.Nodes),
		})
	}

	return out
}

func derpServerFrom(s types.DERPServerSettings) DERPServerSettings {
	return DERPServerSettings{
		Enabled:       s.Enabled,
		RegionID:      int(s.RegionID),
		RegionCode:    s.RegionCode,
		RegionName:    s.RegionName,
		VerifyClients: &s.VerifyClients,
		STUNAddr:      s.STUNAddr,
		IPv4:          s.IPv4,
		IPv6:          s.IPv6,
	}
}

func derpServerTo(s DERPServerSettings) types.DERPServerSettings {
	// Verification is the safe side, so a request that says nothing
	// about it gets it rather than an open relay.
	verify := true
	if s.VerifyClients != nil {
		verify = *s.VerifyClients
	}

	return types.DERPServerSettings{
		Enabled:       s.Enabled,
		RegionID:      tailcfg.DERPRegionID(s.RegionID),
		RegionCode:    s.RegionCode,
		RegionName:    s.RegionName,
		VerifyClients: verify,
		STUNAddr:      s.STUNAddr,
		IPv4:          s.IPv4,
		IPv6:          s.IPv6,
	}
}

func derpSettingsFrom(s types.DERPSettings) DERPSettings {
	out := DERPSettings{
		URLs:            s.URLs,
		Regions:         derpRegionsFrom(s.Regions),
		AutoUpdate:      s.AutoUpdate,
		UpdateFrequency: s.UpdateFrequency.String(),
		Server:          derpServerFrom(s.Server),
	}

	if out.URLs == nil {
		out.URLs = []string{}
	}

	return out
}

func derpSettingsTo(s SetDERPRequestBody) (types.DERPSettings, error) {
	var frequency time.Duration

	if s.UpdateFrequency != "" {
		d, err := time.ParseDuration(s.UpdateFrequency)
		if err != nil {
			return types.DERPSettings{}, huma.Error400BadRequest("parsing updateFrequency", err)
		}

		frequency = d
	}

	return types.DERPSettings{
		URLs:            s.URLs,
		Regions:         derpRegionsTo(s.Regions),
		AutoUpdate:      s.AutoUpdate,
		UpdateFrequency: frequency,
		Server:          derpServerTo(s.Server),
	}, nil
}

func derpFrom(st state.DERPStatus) DERP {
	regions := make([]DERPMapRegion, 0, len(st.Regions))
	for _, r := range st.Regions {
		regions = append(regions, DERPMapRegion{
			ID:     int(r.ID),
			Code:   r.Code,
			Name:   r.Name,
			Nodes:  r.Nodes,
			Source: string(r.Source),
		})
	}

	return DERP{
		Effective:       derpSettingsFrom(st.Effective),
		FromFile:        derpSettingsFrom(st.FromFile),
		Overridden:      st.Overridden,
		Paths:           st.Paths,
		AutoAddEmbedded: st.AutoAddEmbedded,
		RelayAvailable:  st.RelayAvailable,
		RelayRunning:    st.RelayRunning,
		STUNAddr:        st.STUNAddr,
		ServerURL:       st.ServerURL,
		Regions:         regions,
		FetchedAt:       st.FetchedAt,
		FetchError:      st.FetchError,
	}
}

// derpResponse is the status as a response body plus the ETag of the
// effective settings it reports, which every DERP operation returns.
func derpResponse(st state.DERPStatus) *derpOutput {
	body := derpFrom(st)

	return &derpOutput{ETag: derpETag(body.Effective), Body: body}
}

// derpETag is the quoted hex SHA-256 of the effective settings' JSON, whose
// field order is fixed by the struct: stable across reads, changes iff the
// settings change.
func derpETag(settings DERPSettings) string {
	data, err := json.Marshal(settings)
	if err != nil {
		return ""
	}

	sum := sha256.Sum256(data)

	return `"` + hex.EncodeToString(sum[:]) + `"`
}

// derpETagMatches reports whether an If-Match header satisfies the settings
// in force. "*" matches anything that exists, which the settings always do.
func derpETagMatches(ifMatch string, current types.DERPSettings) bool {
	ifMatch = strings.TrimSpace(ifMatch)
	if ifMatch == "*" {
		return true
	}

	return ifMatch == derpETag(derpSettingsFrom(current))
}

func registerDERP(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "getDERP",
		Method:      http.MethodGet,
		Path:        "/api/v1/derp",
		Summary:     "Get DERP settings",
		Description: "Returns the relay configuration the server runs with, the config file's values, " +
			"whether settings set through the API replace them and the map clients receive.",
		Tags:     []string{tagDERP},
		Security: bearerAuth,
	}, scope.FeatureSettingsRead), func(_ context.Context, _ *struct{}) (*derpOutput, error) {
		return derpResponse(b.State.DERP()), nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "setDERP",
		Method:      http.MethodPut,
		Path:        "/api/v1/derp",
		Summary:     "Set DERP settings",
		Description: "Replaces the runtime relay settings: the maps are fetched, the embedded relay is " +
			"started or stopped, and the new map is pushed to every client. A map that cannot be " +
			"fetched or that leaves no relay is refused and nothing changes. The map files in " +
			"derp.paths and the relay's key stay in the config file. Send the ETag a read returned " +
			"as If-Match to have the request refused with 412 when the settings changed since that read.",
		Tags:     []string{tagDERP},
		Security: bearerAuth,
	}, scope.FeatureSettings), "derp.set", "", ""), func(ctx context.Context, in *setDERPInput) (*derpOutput, error) {
		if in.IfMatch != "" && !derpETagMatches(in.IfMatch, b.State.EffectiveDERP()) {
			return nil, huma.Error412PreconditionFailed(
				"the DERP settings changed since they were read; read them again and retry with the new ETag",
			)
		}

		settings, err := derpSettingsTo(in.Body)
		if err != nil {
			return nil, err
		}

		st, c, err := b.State.SetDERP(ctx, settings)
		if err != nil {
			return nil, mapError("setting derp", err)
		}

		audit.Detail(ctx, "urls", auditURLs(st.Effective.URLs))
		audit.Detail(ctx, "regions", len(st.Effective.Regions))
		audit.Detail(ctx, "embedded", st.Effective.Server.Enabled)

		b.Change(c)

		return derpResponse(st), nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "resetDERP",
		Method:      http.MethodDelete,
		Path:        "/api/v1/derp",
		Summary:     "Reset DERP settings",
		Description: "Drops the runtime relay settings so the config file is in force again.",
		Tags:        []string{tagDERP},
		Security:    bearerAuth,
	}, scope.FeatureSettings), "derp.reset", "", ""), func(ctx context.Context, _ *struct{}) (*derpOutput, error) {
		st, c, err := b.State.ResetDERP(ctx)
		if err != nil {
			return nil, mapError("resetting derp", err)
		}

		b.Change(c)

		return derpResponse(st), nil
	})

	huma.Register(api, audited(withScope(huma.Operation{
		OperationID: "refreshDERP",
		Method:      http.MethodPost,
		Path:        "/api/v1/derp/refresh",
		Summary:     "Refetch the DERP maps",
		Description: "Fetches the map URLs and files again and pushes the map to every client when it changed.",
		Tags:        []string{tagDERP},
		Security:    bearerAuth,
	}, scope.FeatureSettings), "derp.refresh", "", ""), func(ctx context.Context, _ *struct{}) (*derpOutput, error) {
		changed, err := b.State.RefreshDERPMap(ctx)
		if err != nil {
			return nil, mapError("refreshing derp map", err)
		}

		if changed {
			b.Change(change.DERPMap())
		}

		return derpResponse(b.State.DERP()), nil
	})
}

// auditURLs is the map URLs as the audit log records them: scheme, host
// and path only, since a private map's URL may carry credentials in its
// user info or query.
func auditURLs(urls []string) []string {
	out := make([]string, 0, len(urls))

	for _, raw := range urls {
		u, err := url.Parse(raw)
		if err != nil {
			out = append(out, "(unparseable URL)")

			continue
		}

		u.User = nil
		u.RawQuery = ""
		u.Fragment = ""
		out = append(out, u.String())
	}

	return out
}
