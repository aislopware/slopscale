package apiv2

import (
	"crypto/rand"
	"encoding/hex"
	"errors"

	"github.com/aislopware/slopscale/hscontrol/db"
	"github.com/aislopware/slopscale/hscontrol/egress"
	"github.com/aislopware/slopscale/hscontrol/state"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/zerolog/log"
)

// apiError is the Tailscale API error body. The official Tailscale Go client
// (and therefore the Terraform provider and tscli built on it) decodes 4xx/5xx
// responses into this shape. Huma's default RFC 9457 problem+json would reach
// them with an empty message, so tailscaleErrorTransformer rewrites every v2
// error into this shape. The HTTP status is read from the response code, not
// from the body's status field.
type apiError struct {
	Message string         `json:"message"`
	Data    []apiErrorData `json:"data,omitempty"`
	Status  int            `json:"status"`
}

type apiErrorData struct {
	User   string   `json:"user,omitempty"`
	Errors []string `json:"errors,omitempty"`
}

// tailscaleErrorTransformer rewrites Huma's RFC 9457 error model into the
// Tailscale error shape. It is registered on the v2 API config only, so the
// slopscale-native v1 API keeps emitting problem+json. Non-error bodies pass
// through untouched.
func tailscaleErrorTransformer(_ huma.Context, _ string, v any) (any, error) {
	em, ok := v.(*huma.ErrorModel)
	if !ok {
		return v, nil
	}

	message := em.Detail
	if message == "" {
		message = em.Title
	}

	out := apiError{Message: message, Status: em.Status}

	if len(em.Errors) > 0 {
		details := make([]string, 0, len(em.Errors))
		for _, d := range em.Errors {
			details = append(details, d.Error())
		}

		out.Data = []apiErrorData{{Errors: details}}
	}

	return out, nil
}

// mapError translates a state/db-layer error into a Huma HTTP error
// (not-found→404, invalid input→400, everything else→500). The transformer
// then reshapes it into the Tailscale body.
func mapError(msg string, err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, db.ErrNotFound),
		errors.Is(err, db.ErrPreAuthKeyNotFound),
		errors.Is(err, db.ErrUserNotFound),
		errors.Is(err, state.ErrNodeNotFound),
		errors.Is(err, types.ErrWebhookNotFound),
		errors.Is(err, types.ErrVIPServiceNotFound):
		return huma.Error404NotFound(msg, err)

	case errors.Is(err, db.ErrPreAuthKeyNotTaggedOrOwned),
		errors.Is(err, db.ErrPreAuthKeyACLTagInvalid),
		errors.Is(err, state.ErrGivenNameInvalid),
		errors.Is(err, state.ErrGivenNameTaken),
		errors.Is(err, state.ErrNodeNameNotUnique),
		errors.Is(err, state.ErrRequestedTagsInvalidOrNotPermitted),
		errors.Is(err, state.ErrUnknownSetting),
		errors.Is(err, types.ErrDNSSettingsInvalid),
		errors.Is(err, types.ErrKeyExpiryOutOfRange),
		errors.Is(err, types.ErrWebhookURLInvalid),
		errors.Is(err, types.ErrWebhookNoSubscriptions),
		errors.Is(err, types.ErrWebhookEventUnknown),
		errors.Is(err, egress.ErrBlocked),
		errors.Is(err, types.ErrWebhookProviderUnknown),
		errors.Is(err, types.ErrWebhookDescriptionLong),
		errors.Is(err, types.ErrVIPServiceName),
		errors.Is(err, types.ErrVIPServicePorts),
		errors.Is(err, types.ErrVIPServiceNameTaken):
		return huma.Error400BadRequest(msg, err)

	default:
		return internalError(msg, err)
	}
}

// internalErrorID is a short id shared by the response and the log line, so
// an operator can find the error the response does not carry.
func internalErrorID() string {
	var raw [4]byte

	_, _ = rand.Read(raw[:])

	return hex.EncodeToString(raw[:])
}

// internalError answers an unmapped error without echoing it: the text can
// carry a query, a file path or an internal host, none of which the caller
// asked about. The whole error goes to the log under the id.
func internalError(msg string, err error) error {
	id := internalErrorID()

	log.Error().Err(err).Str("errorId", id).Msg(msg)

	return huma.Error500InternalServerError("internal error, see the server log for id " + id)
}
