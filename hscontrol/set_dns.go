package hscontrol

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/aislopware/slopscale/hscontrol/audit"
	"github.com/aislopware/slopscale/hscontrol/dnsprovider"
	"github.com/aislopware/slopscale/hscontrol/types"
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
	// ErrSetDNSNodeExpired is returned for a node whose key has expired.
	ErrSetDNSNodeExpired = errors.New("set-dns from an expired node")
	// ErrSetDNSBadValue is returned for a value that is not a DNS-01
	// challenge, so a node cannot store arbitrary text in the zone.
	ErrSetDNSBadValue = errors.New("set-dns value is not an ACME DNS-01 challenge")
)

const maxSetDNSBody = 16 << 10

// challengeValueRE matches an ACME DNS-01 TXT value: the unpadded
// base64url SHA-256 of the key authorization (RFC 8555 section 8.4).
var challengeValueRE = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// SetDNSHandler takes a [tailcfg.SetDNSRequest], which a client sends to
// answer an ACME DNS-01 challenge for one of its cert domains, and
// publishes the record. A node may only write the challenge record of
// one of its own cert domains, over its own Noise session. The client
// asks the CA to validate as soon as this returns, so it answers 200
// only once the zone's nameservers serve the record. The client never
// asks for removal; [dnsprovider.Challenges] removes the record later.
func (ns *noiseServer) SetDNSHandler(writer http.ResponseWriter, req *http.Request) {
	challenges := ns.slopscale.challenges
	if challenges == nil {
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

	node, ok := ns.slopscale.state.GetNodeByNodeKey(request.NodeKey)
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

	if node.IsExpired() {
		httpError(writer, NewHTTPError(http.StatusForbidden, "node is expired", ErrSetDNSNodeExpired))

		return
	}

	// The client builds the name from CertDomains, so it matches exactly;
	// folding case would let lookalike Unicode letters match.
	name := strings.TrimSuffix(request.Name, ".")

	if !node.ACMEChallengeAllowed(ns.slopscale.cfg, name) {
		httpError(writer, NewHTTPError(http.StatusForbidden, "not this node's challenge record",
			fmt.Errorf("%w: %q", ErrSetDNSNameNotAllowed, request.Name)))

		return
	}

	if !challengeValueRE.MatchString(request.Value) {
		httpError(writer, NewHTTPError(http.StatusBadRequest, "value is not an ACME DNS-01 challenge",
			ErrSetDNSBadValue))

		return
	}

	err = challenges.Present(req.Context(), node.ID().Uint64(), name, request.Value)
	if err != nil {
		log.Error().Err(err).Str("name", name).Uint64("node.id", node.ID().Uint64()).
			Msg("publishing ACME challenge record")
		httpError(writer, setDNSError(err))

		return
	}

	audit.Record(ns.slopscale.state, &types.AuditEvent{
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

// setDNSError tells the client whether retrying can help.
func setDNSError(err error) HTTPError {
	switch {
	case errors.Is(err, dnsprovider.ErrTooManyChallenges):
		return NewHTTPError(http.StatusTooManyRequests,
			"too many pending certificate challenges, try again later", err)
	case errors.Is(err, dnsprovider.ErrChallengesClosed):
		return NewHTTPError(http.StatusServiceUnavailable, "the server is shutting down, try again later", err)
	case errors.Is(err, dnsprovider.ErrChallengeProvider):
		return NewHTTPError(http.StatusBadGateway,
			"the DNS provider could not create the record, see the server log", err)
	default:
		return NewHTTPError(http.StatusGatewayTimeout,
			"the record is not served by every nameserver yet, try again", err)
	}
}
