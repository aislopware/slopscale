package apiv1

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/aislopware/slopscale/hscontrol/scope"
	"github.com/aislopware/slopscale/hscontrol/state"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
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

	// FunnelIngress reports whether the server runs the embedded Funnel
	// ingress (funnel.enabled in the config file).
	FunnelIngress bool `json:"funnelIngress"`
	// FunnelListenAddrs are the public addresses the embedded ingress
	// listens on.
	FunnelListenAddrs []string `json:"funnelListenAddrs" nullable:"false"`
	// FunnelPorts are the ports Funnel may be turned on for.
	FunnelPorts []int `json:"funnelPorts" nullable:"false"`
	// FunnelIngressNodes counts the ingress nodes that have joined the
	// tailnet, the embedded one included; Funnel delivers nothing while
	// it is zero.
	FunnelIngressNodes int `json:"funnelIngressNodes"`
	// LatestClientVersion is the latest stable Tailscale client release
	// the server found at pkgs.tailscale.com; empty until the first
	// lookup or while client_updates.check is off.
	LatestClientVersion string `json:"latestClientVersion"`
	// ClientUpdatesCheck reports whether the server looks the latest
	// client release up (client_updates.check in the config file).
	ClientUpdatesCheck bool `json:"clientUpdatesCheck"`
	// ControlDialPlan lists the addresses clients are told to reach the
	// server at before resolving its name (control_dial_plan in the
	// config file).
	ControlDialPlan []string `json:"controlDialPlan" nullable:"false"`
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
		Tags:        []string{tagSettings},
		Security:    bearerAuth,
	}, scope.FeatureSettingsRead), func(_ context.Context, _ *struct{}) (*serverInfoOutput, error) {
		return &serverInfoOutput{Body: serverInfoFrom(
			b.Cfg, b.State.DERP(), len(b.State.FunnelIngressNodes()), b.State.LatestClientVersion(),
		)}, nil
	})
}

func serverInfoFrom(cfg *types.Config, derp state.DERPStatus, ingressNodes int, latestClient string) ServerInfo {
	version := types.GetVersionInfo()
	info := ServerInfo{
		Version:             version.Version,
		Commit:              version.Commit,
		BuildTime:           version.BuildTime,
		GoVersion:           version.Go.Version,
		StartedAt:           startedAt,
		OIDCScopes:          []string{},
		DERPRegions:         len(derp.Regions),
		DERPServer:          derp.RelayRunning,
		FunnelListenAddrs:   []string{},
		FunnelPorts:         []int{},
		FunnelIngressNodes:  ingressNodes,
		LatestClientVersion: latestClient,
		ControlDialPlan:     []string{},
	}

	if cfg == nil {
		return info
	}

	info.FunnelIngress = cfg.Funnel.Enabled
	info.FunnelListenAddrs = append(info.FunnelListenAddrs, cfg.Funnel.ListenAddrs...)

	for _, p := range cfg.Funnel.FunnelPorts() {
		info.FunnelPorts = append(info.FunnelPorts, int(p))
	}

	info.ClientUpdatesCheck = cfg.ClientUpdates.Check

	for _, addr := range cfg.ControlDialPlan {
		info.ControlDialPlan = append(info.ControlDialPlan, addr.String())
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
