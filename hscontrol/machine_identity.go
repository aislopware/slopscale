package hscontrol

import (
	"cmp"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/aislopware/slopscale/hscontrol/idtoken"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/wire"
	"github.com/rs/zerolog/log"
	"tailscale.com/tailcfg"
)

// ErrIdentityRefused is returned for an identity token asked by a node
// that is not (or no longer) a member of the tailnet.
var ErrIdentityRefused = errors.New("node may not get an identity token")

// ErrNoAudience is returned for an identity token request naming no
// audience.
var ErrNoAudience = errors.New("identity token request names no audience")

// IDTokenHandler signs an identity token for the node, as
// `tailscale id-token <aud>` asks over [tailcfg.TokenRequest]; see
// docs/ref/identity-tokens.md. The token says who the node is, so only
// a node that is in the tailnet right now gets one.
func (ns *noiseServer) IDTokenHandler(writer http.ResponseWriter, req *http.Request) {
	var request tailcfg.TokenRequest

	err := wire.UnmarshalRead(req.Body, &request)
	if err != nil {
		httpError(writer, NewHTTPError(http.StatusBadRequest, "invalid token request", err))

		return
	}

	node, err := ns.sessionNode(request.NodeKey)
	if err != nil {
		httpError(writer, err)

		return
	}

	audience := strings.TrimSpace(request.Audience)
	if audience == "" {
		httpError(writer, NewHTTPError(http.StatusBadRequest, "no audience requested", ErrNoAudience))

		return
	}

	if reason := identityRefusal(node); reason != "" {
		httpError(writer, NewHTTPError(http.StatusForbidden, reason, ErrIdentityRefused))

		return
	}

	signer, err := ns.slopscale.state.IDTokenSigner()
	if err != nil {
		httpError(writer, NewHTTPError(http.StatusInternalServerError, "identity token signing is unavailable", err))

		return
	}

	token, err := signer.Sign(ns.identityClaims(node, audience, time.Now()))
	if err != nil {
		httpError(writer, NewHTTPError(http.StatusInternalServerError, "signing the identity token", err))

		return
	}

	log.Info().Caller().
		Uint64("node.id", node.ID().Uint64()).
		Str("node.name", node.GivenName()).
		Str("audience", audience).
		Msg("issued an identity token")

	writer.Header().Set("Content-Type", "application/json; charset=utf-8")

	err = wire.MarshalWrite(writer, &tailcfg.TokenResponse{IDToken: token})
	if err != nil {
		log.Error().Caller().Err(err).Msg("encoding the identity token response")
	}
}

// identityRefusal says why a node gets no token, or nothing when it may.
func identityRefusal(node types.NodeView) string {
	switch {
	case !node.IsApproved():
		return "the device is waiting for approval"
	case node.IsSuspended():
		return "the device is suspended"
	case node.IsExpired():
		return "the device's key has expired"
	default:
		return ""
	}
}

// identityClaims fills the token with what the node is, in the shape
// [tailcfg.TokenResponse] documents.
func (ns *noiseServer) identityClaims(node types.NodeView, audience string, now time.Time) idtoken.Claims {
	cfg := ns.slopscale.cfg
	issuer := strings.TrimSuffix(cfg.ServerURL, "/")
	domain := cfg.Domain()

	claims := idtoken.NewClaims(issuer, audience, now)
	claims.Subject = magicDNSName(node, cfg.BaseDomain)
	claims.Key = node.NodeKey().String()
	claims.Addresses = node.IPsAsString()
	claims.NodeID = node.ID().Uint64()
	claims.Node = node.GivenName()
	claims.Domain = domain

	if node.IsTagged() {
		for _, tag := range node.Tags().All() {
			claims.Tags = append(claims.Tags, domain+":"+tag)
		}

		return claims
	}

	if user := node.User(); user.Valid() {
		claims.User = domain + ":" + user.Username()
		claims.UserID = uint64(user.ID())
	}

	return claims
}

// magicDNSName is the node's MagicDNS name without the trailing dot,
// falling back to the given name when the server has no base domain.
func magicDNSName(node types.NodeView, baseDomain string) string {
	fqdn, err := node.GetFQDN(baseDomain)
	if err != nil || fqdn == "" {
		return node.GivenName()
	}

	return strings.TrimSuffix(fqdn, ".")
}

// OIDCDiscoveryHandler serves the OpenID discovery document a verifier
// reads to find the identity token key set.
func (h *Slopscale) OIDCDiscoveryHandler(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, idtoken.Discovery(strings.TrimSuffix(h.cfg.ServerURL, "/")))
}

// JWKSHandler serves the identity token signing key's public half.
func (h *Slopscale) JWKSHandler(writer http.ResponseWriter, _ *http.Request) {
	signer, err := h.state.IDTokenSigner()
	if err != nil {
		httpError(writer, NewHTTPError(http.StatusInternalServerError, "identity token signing is unavailable", err))

		return
	}

	writeJSON(writer, signer.JWKS())
}

// whoAmIResponse answers `tailscale debug ts2021`'s GET /machine/whoami,
// which only checks that the connection works and logs the body; it
// tells the caller which nodes the connection's machine key belongs to.
type whoAmIResponse struct {
	ServerVersion string       `json:"serverVersion"`
	MachineKey    string       `json:"machineKey"`
	Nodes         []whoAmINode `json:"nodes"`
}

type whoAmINode struct {
	ID        uint64   `json:"id"`
	Name      string   `json:"name"`
	NodeKey   string   `json:"nodeKey"`
	User      string   `json:"user,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Addresses []string `json:"addresses"`
}

// WhoAmIHandler tells the connection's machine which nodes it is.
func (ns *noiseServer) WhoAmIHandler(writer http.ResponseWriter, _ *http.Request) {
	response := whoAmIResponse{
		ServerVersion: types.GetVersionInfo().Version,
		MachineKey:    ns.machineKey.String(),
		Nodes:         []whoAmINode{},
	}

	for _, node := range ns.slopscale.state.GetNodesByMachineKeyAllUsers(ns.machineKey) {
		entry := whoAmINode{
			ID:        node.ID().Uint64(),
			Name:      node.GivenName(),
			NodeKey:   node.NodeKey().String(),
			Tags:      node.Tags().AsSlice(),
			Addresses: node.IPsAsString(),
		}

		if !node.IsTagged() && node.User().Valid() {
			entry.User = node.User().Username()
		}

		response.Nodes = append(response.Nodes, entry)
	}

	slices.SortFunc(response.Nodes, func(a, b whoAmINode) int { return cmp.Compare(a.ID, b.ID) })

	writeJSON(writer, response)
}
