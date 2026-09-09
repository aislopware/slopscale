package apiv1

import (
	"crypto/rand"
	"encoding/hex"
	"errors"

	"github.com/aislopware/slopscale/hscontrol/db"
	"github.com/aislopware/slopscale/hscontrol/egress"
	"github.com/aislopware/slopscale/hscontrol/posture"
	"github.com/aislopware/slopscale/hscontrol/posture/integration"
	"github.com/aislopware/slopscale/hscontrol/state"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/zerolog/log"
)

// mapError translates a state/db-layer error into a Huma HTTP error
// (NotFound→404, invalid input→400, conflict→409, everything else→500)
// from the lists below.

// badRequestErrors are the validation failures a caller can fix; mapError
// answers them with 400.
var badRequestErrors = []error{
	state.ErrGivenNameInvalid,
	state.ErrGivenNameTaken,
	state.ErrNodeNameNotUnique,
	state.ErrNodeMarkedTaggedButHasNoTags,
	state.ErrNodeHasNeitherUserNorTags,
	state.ErrRequestedTagsInvalidOrNotPermitted,
	db.ErrUserStillHasNodes,
	db.ErrCannotChangeOIDCUser,
	db.ErrPreAuthKeyNotTaggedOrOwned,
	db.ErrSingleUseAuthKeyHasBeenUsed,
	types.ErrInvalidRole,
	state.ErrUnknownSetting,
	state.ErrShareWithOwner,
	db.ErrNodeNotShared,
	types.ErrGroupNameEmpty,
	types.ErrGroupNameInvalid,
	types.ErrGroupNameTooLong,
	types.ErrGroupBuiltin,
	types.ErrGroupSyncedUsers,
	types.ErrGroupSyncedName,
	types.ErrGroupSelfMembers,
	types.ErrRuleBuiltin,
	types.ErrRuleSelfSource,
	types.ErrRuleSelfBoth,
	types.ErrRuleNameEmpty,
	types.ErrRuleNameTooLong,
	types.ErrRuleNoSources,
	types.ErrRuleNoDestinations,
	types.ErrRulePortsWithout,
	types.ErrRulePortsInvalid,
	types.ErrInvalidAccessProtocol,
	types.ErrDNSSettingsInvalid,
	types.ErrDERPSettingsInvalid,
	state.ErrDERPRelayUnavailable,
	types.ErrNetworkNameEmpty,
	types.ErrNetworkNameTooLong,
	types.ErrNetworkNoPrefixes,
	types.ErrNetworkNoGroups,
	types.ErrNetworkPrefixInvalid,
	types.ErrGroupDNSRuleNameEmpty,
	types.ErrGroupDNSRuleNameTooLong,
	types.ErrGroupDNSRuleNoDomains,
	types.ErrGroupDNSRuleNoNameservers,
	types.ErrGroupDNSRuleNoGroups,
	types.ErrGroupDNSRuleReservedZone,
	types.ErrKeyExpiryOutOfRange,
	egress.ErrBlocked,
	types.ErrWebhookURLInvalid,
	types.ErrWebhookNoSubscriptions,
	types.ErrWebhookEventUnknown,
	types.ErrWebhookProviderUnknown,
	types.ErrWebhookDescriptionLong,
	types.ErrWebhookMailtoInvalid,
	types.ErrWebhookTelegramNoChat,
	types.ErrWebhookMailUnavailable,
	types.ErrLogStreamNameInvalid,
	types.ErrLogStreamURLInvalid,
	types.ErrLogStreamDestinationUnknown,
	types.ErrLogStreamTokenRequired,
	types.ErrLogStreamTokenLong,
	types.ErrSSHRecorderInvalid,
	types.ErrPostureNameEmpty,
	types.ErrPostureNameTooLong,
	types.ErrPostureNameInvalid,
	types.ErrPostureEmpty,
	types.ErrPostureTooMany,
	types.ErrPostureCountryNoGeo,
	types.ErrRuleExpiryPast,
	types.ErrMemberExpiryPast,
	types.ErrAccessRequestNoUser,
	types.ErrAccessRequestNotRequestable,
	types.ErrAccessRequestDuration,
	types.ErrAccessRequestTextTooLong,
	types.ErrAccessRequestNodeOwner,
	posture.ErrEmpty,
	posture.ErrAttribute,
	posture.ErrOperator,
	posture.ErrValue,
	posture.ErrTrailing,
	posture.ErrListExpected,
	posture.ErrScalarWanted,
	posture.ErrOrderedValue,
	posture.ErrUnterminated,
	posture.ErrUnknownPrefix,
	posture.ErrScheduleDays,
	posture.ErrScheduleTime,
	posture.ErrScheduleTimezone,
	types.ErrVIPServiceName,
	types.ErrVIPServiceHostTagged,
	state.ErrVIPServiceUnknownName,
	types.ErrVIPServicePorts,
	types.ErrAppConnectorName,
	types.ErrAppConnectorDomain,
	types.ErrAppConnectorSelector,
	types.ErrAppConnectorEmpty,
	types.ErrAppConnectorRoute,
	types.ErrPostureIntegrationProvider,
	types.ErrPostureIntegrationName,
	types.ErrPostureIntegrationConfig,
	state.ErrUnknownDiagnostic,
	types.ErrPreferencesInvalid,
}

// notFoundErrors are the missing records; mapError answers them with 404.
var notFoundErrors = []error{
	db.ErrNotFound,
	state.ErrNodeNotFound,
	state.ErrNodeNotInNodeStore,
	db.ErrUserNotFound,
	db.ErrNodeNotFoundRegistrationCache,
	state.ErrRegistrationExpired,
	types.ErrGroupNotFound,
	types.ErrRuleNotFound,
	types.ErrGroupMemberMissing,
	types.ErrNetworkNotFound,
	types.ErrGroupDNSRuleNotFound,
	types.ErrPostureNotFound,
	types.ErrAccessRequestNotFound,
	types.ErrWebhookNotFound,
	types.ErrLogStreamNotFound,
	types.ErrSSHRecordingNotFound,
	types.ErrVIPServiceNotFound,
	types.ErrAppConnectorNotFound,
	types.ErrPostureIntegrationNotFound,
}

// conflictErrors are the clashes with existing state; mapError answers
// them with 409.
var conflictErrors = []error{
	state.ErrNodeKeyInUse,
	state.ErrAmbiguousNodeOwnership,
	state.ErrOwnerExists,
	db.ErrNodeAlreadyShared,
	types.ErrGroupNameTaken,
	types.ErrGroupInUse,
	types.ErrGroupMemberExists,
	types.ErrNetworkNameTaken,
	types.ErrGroupDNSRuleNameTaken,
	types.ErrPostureNameTaken,
	types.ErrPostureInUse,
	types.ErrAccessRequestDecided,
	types.ErrAccessRequestPendingExists,
	types.ErrVIPServiceNameTaken,
	types.ErrAppConnectorNameTaken,
	types.ErrPostureIntegrationNameTaken,
	state.ErrPostureProviderEnabled,
	types.ErrTailnetLockDisabled,
	types.ErrTailnetLockNoSupportSecret,
	state.ErrNodeNotConnected,
	state.ErrRemoteConfigOff,
}

// isAnyOf reports whether err is, or wraps, one of the targets.
func isAnyOf(err error, targets ...error) bool {
	for _, target := range targets {
		if errors.Is(err, target) {
			return true
		}
	}

	return false
}

// Handlers use this default mapping and may return a more specific huma.ErrorN
// directly. msg is a human context prefix, e.g. "getting node".
func mapError(msg string, err error) error {
	if err == nil {
		return nil
	}

	switch {
	case isAnyOf(err, notFoundErrors...):
		return huma.Error404NotFound(msg, err)

	case isAnyOf(err, badRequestErrors...):
		return huma.Error400BadRequest(msg, err)

	case isAnyOf(err, conflictErrors...):
		return huma.Error409Conflict(msg, err)

	case errors.Is(err, state.ErrCannotChangeOwnRole),
		errors.Is(err, state.ErrRoleChangeForbidden),
		errors.Is(err, state.ErrOnlyOwnerTransfers),
		errors.Is(err, state.ErrOwnerRoleImmutable),
		errors.Is(err, state.ErrUserNotApproved),
		errors.Is(err, types.ErrAccessRequestOwn),
		errors.Is(err, db.ErrCannotDeleteOwner):
		return huma.Error403Forbidden(msg, err)

	case errors.Is(err, state.ErrDERPSourceUnreachable),
		errors.Is(err, integration.ErrUpstream),
		errors.Is(err, integration.ErrAuth):
		return huma.Error502BadGateway(msg, err)

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
