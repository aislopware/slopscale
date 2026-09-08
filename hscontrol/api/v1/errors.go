package apiv1

import (
	"errors"

	"github.com/danielgtaylor/huma/v2"
	"github.com/juanfont/headscale/hscontrol/db"
	"github.com/juanfont/headscale/hscontrol/posture"
	"github.com/juanfont/headscale/hscontrol/state"
	"github.com/juanfont/headscale/hscontrol/types"
)

// mapError translates a state/db-layer error into a Huma HTTP error
// (NotFound→404, invalid input→400, conflict→409, everything else→500).
// Handlers use this default mapping and may return a more specific huma.ErrorN
// directly. msg is a human context prefix, e.g. "getting node".
func mapError(msg string, err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, db.ErrNotFound),
		errors.Is(err, state.ErrNodeNotFound),
		errors.Is(err, state.ErrNodeNotInNodeStore),
		errors.Is(err, db.ErrUserNotFound),
		errors.Is(err, db.ErrNodeNotFoundRegistrationCache),
		errors.Is(err, state.ErrRegistrationExpired),
		errors.Is(err, types.ErrGroupNotFound),
		errors.Is(err, types.ErrRuleNotFound),
		errors.Is(err, types.ErrGroupMemberMissing),
		errors.Is(err, types.ErrNetworkNotFound),
		errors.Is(err, types.ErrGroupDNSRuleNotFound),
		errors.Is(err, types.ErrPostureNotFound),
		errors.Is(err, types.ErrAccessRequestNotFound),
		errors.Is(err, types.ErrWebhookNotFound),
		errors.Is(err, types.ErrLogStreamNotFound),
		errors.Is(err, types.ErrSSHRecordingNotFound):
		return huma.Error404NotFound(msg, err)

	case errors.Is(err, state.ErrGivenNameInvalid),
		errors.Is(err, state.ErrGivenNameTaken),
		errors.Is(err, state.ErrNodeNameNotUnique),
		errors.Is(err, state.ErrNodeMarkedTaggedButHasNoTags),
		errors.Is(err, state.ErrNodeHasNeitherUserNorTags),
		errors.Is(err, state.ErrRequestedTagsInvalidOrNotPermitted),
		errors.Is(err, db.ErrUserStillHasNodes),
		errors.Is(err, db.ErrCannotChangeOIDCUser),
		errors.Is(err, db.ErrPreAuthKeyNotTaggedOrOwned),
		errors.Is(err, db.ErrSingleUseAuthKeyHasBeenUsed),
		errors.Is(err, types.ErrInvalidRole),
		errors.Is(err, state.ErrUnknownSetting),
		errors.Is(err, state.ErrShareWithOwner),
		errors.Is(err, db.ErrNodeNotShared),
		errors.Is(err, types.ErrGroupNameEmpty),
		errors.Is(err, types.ErrGroupNameInvalid),
		errors.Is(err, types.ErrGroupNameTooLong),
		errors.Is(err, types.ErrGroupBuiltin),
		errors.Is(err, types.ErrGroupSyncedUsers),
		errors.Is(err, types.ErrGroupSelfMembers),
		errors.Is(err, types.ErrRuleBuiltin),
		errors.Is(err, types.ErrRuleSelfSource),
		errors.Is(err, types.ErrRuleSelfBoth),
		errors.Is(err, types.ErrRuleNameEmpty),
		errors.Is(err, types.ErrRuleNameTooLong),
		errors.Is(err, types.ErrRuleNoSources),
		errors.Is(err, types.ErrRuleNoDestinations),
		errors.Is(err, types.ErrRulePortsWithout),
		errors.Is(err, types.ErrRulePortsInvalid),
		errors.Is(err, types.ErrInvalidAccessProtocol),
		errors.Is(err, types.ErrDNSSettingsInvalid),
		errors.Is(err, types.ErrNetworkNameEmpty),
		errors.Is(err, types.ErrNetworkNameTooLong),
		errors.Is(err, types.ErrNetworkNoPrefixes),
		errors.Is(err, types.ErrNetworkNoGroups),
		errors.Is(err, types.ErrNetworkPrefixInvalid),
		errors.Is(err, types.ErrGroupDNSRuleNameEmpty),
		errors.Is(err, types.ErrGroupDNSRuleNameTooLong),
		errors.Is(err, types.ErrGroupDNSRuleNoDomains),
		errors.Is(err, types.ErrGroupDNSRuleNoNameservers),
		errors.Is(err, types.ErrGroupDNSRuleNoGroups),
		errors.Is(err, types.ErrGroupDNSRuleReservedZone),
		errors.Is(err, types.ErrKeyExpiryOutOfRange),
		errors.Is(err, types.ErrWebhookURLInvalid),
		errors.Is(err, types.ErrWebhookNoSubscriptions),
		errors.Is(err, types.ErrWebhookEventUnknown),
		errors.Is(err, types.ErrWebhookProviderUnknown),
		errors.Is(err, types.ErrWebhookDescriptionLong),
		errors.Is(err, types.ErrWebhookMailtoInvalid),
		errors.Is(err, types.ErrWebhookTelegramNoChat),
		errors.Is(err, types.ErrWebhookMailUnavailable),
		errors.Is(err, types.ErrLogStreamNameInvalid),
		errors.Is(err, types.ErrLogStreamURLInvalid),
		errors.Is(err, types.ErrLogStreamDestinationUnknown),
		errors.Is(err, types.ErrLogStreamTokenRequired),
		errors.Is(err, types.ErrLogStreamTokenLong),
		errors.Is(err, types.ErrSSHRecorderInvalid),
		errors.Is(err, types.ErrPostureNameEmpty),
		errors.Is(err, types.ErrPostureNameTooLong),
		errors.Is(err, types.ErrPostureNameInvalid),
		errors.Is(err, types.ErrPostureEmpty),
		errors.Is(err, types.ErrPostureTooMany),
		errors.Is(err, types.ErrPostureCountryNoGeo),
		errors.Is(err, types.ErrRuleExpiryPast),
		errors.Is(err, types.ErrMemberExpiryPast),
		errors.Is(err, types.ErrAccessRequestNoUser),
		errors.Is(err, types.ErrAccessRequestNotRequestable),
		errors.Is(err, types.ErrAccessRequestDuration),
		errors.Is(err, types.ErrAccessRequestTextTooLong),
		errors.Is(err, types.ErrAccessRequestNodeOwner),
		errors.Is(err, posture.ErrEmpty),
		errors.Is(err, posture.ErrAttribute),
		errors.Is(err, posture.ErrOperator),
		errors.Is(err, posture.ErrValue),
		errors.Is(err, posture.ErrTrailing),
		errors.Is(err, posture.ErrListExpected),
		errors.Is(err, posture.ErrScalarWanted),
		errors.Is(err, posture.ErrOrderedValue),
		errors.Is(err, posture.ErrUnterminated),
		errors.Is(err, posture.ErrUnknownPrefix),
		errors.Is(err, posture.ErrScheduleDays),
		errors.Is(err, posture.ErrScheduleTime),
		errors.Is(err, posture.ErrScheduleTimezone):
		return huma.Error400BadRequest(msg, err)

	case errors.Is(err, state.ErrNodeKeyInUse),
		errors.Is(err, state.ErrAmbiguousNodeOwnership),
		errors.Is(err, state.ErrOwnerExists),
		errors.Is(err, db.ErrNodeAlreadyShared),
		errors.Is(err, types.ErrGroupNameTaken),
		errors.Is(err, types.ErrGroupInUse),
		errors.Is(err, types.ErrGroupMemberExists),
		errors.Is(err, types.ErrNetworkNameTaken),
		errors.Is(err, types.ErrGroupDNSRuleNameTaken),
		errors.Is(err, types.ErrPostureNameTaken),
		errors.Is(err, types.ErrPostureInUse),
		errors.Is(err, types.ErrAccessRequestDecided),
		errors.Is(err, types.ErrAccessRequestPendingExists):
		return huma.Error409Conflict(msg, err)

	case errors.Is(err, state.ErrCannotChangeOwnRole),
		errors.Is(err, state.ErrRoleChangeForbidden),
		errors.Is(err, state.ErrOnlyOwnerTransfers),
		errors.Is(err, state.ErrOwnerRoleImmutable),
		errors.Is(err, state.ErrUserNotApproved),
		errors.Is(err, types.ErrAccessRequestOwn),
		errors.Is(err, db.ErrCannotDeleteOwner):
		return huma.Error403Forbidden(msg, err)

	default:
		return huma.Error500InternalServerError(msg, err)
	}
}
