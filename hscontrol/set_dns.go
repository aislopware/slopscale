package hscontrol

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/juanfont/headscale/hscontrol/audit"
	"github.com/juanfont/headscale/hscontrol/dnsprovider"
	"github.com/juanfont/headscale/hscontrol/types"
	"github.com/rs/zerolog/log"
	"tailscale.com/tailcfg"
)

var (
	// ErrSetDNSNodeMismatch is returned when the node key in a set-dns
	// request does not belong to the Noise session it came over.
	ErrSetDNSNodeMismatch = errors.New("set-dns node key does not match the session")
	// ErrSetDNSNotTXT is returned for a record type other than TXT.
	ErrSetDNSNotTXT = errors.New("set-dns only publishes TXT records")
	// ErrSetDNSNameNotAllowed is returned for a name that is not the
	// node's own challenge record.
	ErrSetDNSNameNotAllowed = errors.New("set-dns name is not the node's ACME challenge record")
	// ErrSetDNSDisabled is returned when the server has no DNS provider.
	ErrSetDNSDisabled = errors.New("certificate assistance is not enabled")
)

const maxSetDNSBody = 16 << 10

// newDNSProvider builds the provider certificate assistance publishes
// through; nil when the feature is off.
//
//nolint:ireturn // the app holds any provider
func newDNSProvider(cfg *types.Config) (dnsprovider.Provider, error) {
	if !cfg.HTTPSCerts.Enabled {
		return nil, nil //nolint:nilnil // off is not an error
	}

	provider, err := dnsprovider.New(cfg.HTTPSCerts, cfg.BaseDomain)
	if err != nil {
		return nil, fmt.Errorf("building DNS provider: %w", err)
	}

	return provider, nil
}

// SetDNSHandler takes a [tailcfg.SetDNSRequest], which a client sends to
// answer an ACME DNS-01 challenge for one of its cert domains, and
// publishes the record. A node may only write the challenge record of
// its own MagicDNS name, over its own Noise session.
func (ns *noiseServer) SetDNSHandler(writer http.ResponseWriter, req *http.Request) {
	if ns.headscale.dnsProvider == nil {
		httpError(writer, NewHTTPError(http.StatusNotImplemented, "certificate assistance is not enabled",
			ErrSetDNSDisabled))

		return
	}

	var request tailcfg.SetDNSRequest

	err := json.NewDecoder(http.MaxBytesReader(writer, req.Body, maxSetDNSBody)).Decode(&request)
	if err != nil {
		httpError(writer, NewHTTPError(http.StatusBadRequest, "invalid set-dns request", err))

		return
	}

	node, ok := ns.headscale.state.GetNodeByNodeKey(request.NodeKey)
	if !ok || node.MachineKey() != ns.machineKey {
		httpError(writer, NewHTTPError(http.StatusUnauthorized, "node key does not match the session",
			fmt.Errorf("%w: %s", ErrSetDNSNodeMismatch, request.NodeKey.ShortString())))

		return
	}

	if !strings.EqualFold(request.Type, "TXT") {
		httpError(writer, NewHTTPError(http.StatusBadRequest, "only TXT records are published",
			fmt.Errorf("%w: %q", ErrSetDNSNotTXT, request.Type)))

		return
	}

	name := strings.ToLower(strings.TrimSuffix(request.Name, "."))

	if !node.ACMEChallengeAllowed(ns.headscale.cfg, name) {
		httpError(writer, NewHTTPError(http.StatusForbidden, "not this node's challenge record",
			fmt.Errorf("%w: %q", ErrSetDNSNameNotAllowed, request.Name)))

		return
	}

	err = ns.headscale.dnsProvider.SetTXT(req.Context(), name, request.Value)
	if err != nil {
		log.Error().Err(err).Str("name", name).Uint64("node.id", uint64(node.ID())).
			Msg("publishing ACME challenge record")
		httpError(writer, NewHTTPError(http.StatusBadGateway, "publishing the record failed", err))

		return
	}

	audit.Record(ns.headscale.state, &types.AuditEvent{
		Action:     "node.cert_challenge",
		TargetKind: "node",
		TargetID:   node.ID().String(),
		TargetName: node.GivenName(),
		Detail:     map[string]any{"name": name},
	})

	log.Info().Str("name", name).Uint64("node.id", uint64(node.ID())).Msg("ACME challenge record published")

	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(http.StatusOK)

	err = json.NewEncoder(writer).Encode(tailcfg.SetDNSResponse{})
	if err != nil {
		log.Debug().Err(err).Msg("writing set-dns response")
	}
}
