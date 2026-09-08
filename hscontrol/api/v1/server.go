package apiv1

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/scope"
	"github.com/juanfont/headscale/hscontrol/state"
	"github.com/juanfont/headscale/hscontrol/types"
)

func init() {
	registrations = append(registrations, registerServer)
}

// ServerInfo is what an operator needs to know about the running server
// that is not a setting: where it is, how it was built and what the
// config file gave it.
type ServerInfo struct {
	Version   string    `json:"version"`
	Commit    string    `json:"commit"`
	BuildTime string    `json:"buildTime"`
	GoVersion string    `json:"goVersion"`
	StartedAt time.Time `json:"startedAt"`

	ServerURL  string `json:"serverUrl"`
	ListenAddr string `json:"listenAddr"`
	IPv4Prefix string `json:"ipv4Prefix"`
	IPv6Prefix string `json:"ipv6Prefix"`
	BaseDomain string `json:"baseDomain"`
	MagicDNS   bool   `json:"magicDns"`
	// Database is sqlite or postgres.
	Database string `json:"database"`
	// PolicyMode is file or database.
	PolicyMode string `json:"policyMode"`
	PolicyPath string `json:"policyPath"`
	// TLS is how TLS is provided: letsencrypt, files or none (a proxy
	// terminates it).
	TLS string `json:"tls"`
	// OIDCIssuer is empty without an identity provider.
	OIDCIssuer string   `json:"oidcIssuer"`
	OIDCScopes []string `json:"oidcScopes" nullable:"false"`
	// DERPRegions counts the regions of the map clients receive; the
	// relay settings themselves are at /api/v1/derp.
	DERPRegions int `json:"derpRegions"`
	// DERPServer reports whether the embedded DERP relay is serving.
	DERPServer bool `json:"derpServer"`
	// EphemeralInactivityTimeout is how long an ephemeral node may stay
	// offline before it is deleted.
	EphemeralInactivityTimeout string `json:"ephemeralInactivityTimeout"`
	// NodeExpiry is the config file's default key expiry; empty means never.
	NodeExpiry string `json:"nodeExpiry"`
}

type serverInfoOutput struct {
	Body ServerInfo
}

var startedAt = time.Now()

func registerServer(api huma.API, b Backend) {
	huma.Register(api, withScope(huma.Operation{
		OperationID: "getServerInfo",
		Method:      http.MethodGet,
		Path:        "/api/v1/server",
		Summary:     "Get server info",
		Description: "The build, addresses and config file values of the running server.",
		Tags:        []string{"Settings"},
		Security:    bearerAuth,
	}, scope.FeatureSettingsRead), func(_ context.Context, _ *struct{}) (*serverInfoOutput, error) {
		return &serverInfoOutput{Body: serverInfoFrom(b.Cfg, b.State.DERP())}, nil
	})
}

func serverInfoFrom(cfg *types.Config, derp state.DERPStatus) ServerInfo {
	version := types.GetVersionInfo()
	info := ServerInfo{
		Version:     version.Version,
		Commit:      version.Commit,
		BuildTime:   version.BuildTime,
		GoVersion:   version.Go.Version,
		StartedAt:   startedAt,
		OIDCScopes:  []string{},
		DERPRegions: len(derp.Regions),
		DERPServer:  derp.RelayRunning,
	}

	if cfg == nil {
		return info
	}

	info.ServerURL = cfg.ServerURL
	info.ListenAddr = cfg.Addr
	info.BaseDomain = cfg.BaseDomain
	info.MagicDNS = cfg.DNSConfig.MagicDNS
	info.Database = strings.TrimSuffix(cfg.Database.Type, "3")
	info.PolicyMode = string(cfg.Policy.Mode)
	info.PolicyPath = cfg.Policy.Path
	info.TLS = tlsMode(cfg)
	info.OIDCIssuer = cfg.OIDC.Issuer
	info.OIDCScopes = slices.Clone(cfg.OIDC.Scope)
	info.EphemeralInactivityTimeout = cfg.Node.Ephemeral.InactivityTimeout.String()

	if cfg.PrefixV4 != nil {
		info.IPv4Prefix = cfg.PrefixV4.String()
	}

	if cfg.PrefixV6 != nil {
		info.IPv6Prefix = cfg.PrefixV6.String()
	}

	if cfg.Node.Expiry > 0 {
		info.NodeExpiry = cfg.Node.Expiry.String()
	}

	if info.OIDCScopes == nil {
		info.OIDCScopes = []string{}
	}

	return info
}

func tlsMode(cfg *types.Config) string {
	switch {
	case cfg.TLS.LetsEncrypt.Hostname != "":
		return "letsencrypt"
	case cfg.TLS.CertPath != "":
		return "files"
	default:
		return "none"
	}
}
