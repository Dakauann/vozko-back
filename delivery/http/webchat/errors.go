package webchat

import (
	"errors"
	"log"
	"net/http"

	"vozko/delivery/http/response"
	wcdomain "vozko/domain/webchat"
)

type errorMapping struct {
	err    error
	status int
	code   string
}

var errorMappings = []errorMapping{
	{wcdomain.ErrWidgetNotFound, http.StatusNotFound, "widget_not_found"},
	{wcdomain.ErrVisitorNotFound, http.StatusNotFound, "visitor_not_found"},
	{wcdomain.ErrConversationNotFound, http.StatusNotFound, "conversation_not_found"},
	{wcdomain.ErrWidgetPaused, http.StatusNotFound, "widget_unavailable"},
	{wcdomain.ErrOriginNotAllowed, http.StatusForbidden, "origin_not_allowed"},
	{wcdomain.ErrVisitorBlocked, http.StatusForbidden, "visitor_blocked"},
	{wcdomain.ErrIdentityRequired, http.StatusForbidden, "identity_required"},
	{wcdomain.ErrVisitorTokenInvalid, http.StatusUnauthorized, "session_invalid"},
	{wcdomain.ErrVisitorTokenExpired, http.StatusUnauthorized, "session_expired"},
	{wcdomain.ErrIdentityTokenInvalid, http.StatusUnauthorized, "identity_invalid"},
	{wcdomain.ErrIdentityTokenExpired, http.StatusUnauthorized, "identity_expired"},
	{wcdomain.ErrChallengeInvalid, http.StatusBadRequest, "challenge_invalid"},
	{wcdomain.ErrChallengeExpired, http.StatusBadRequest, "challenge_expired"},
	{wcdomain.ErrChallengeUnsolved, http.StatusBadRequest, "challenge_unsolved"},
	{wcdomain.ErrChallengeReused, http.StatusBadRequest, "challenge_reused"},
	{wcdomain.ErrRateLimited, http.StatusTooManyRequests, "rate_limited"},
	{wcdomain.ErrIntakePending, http.StatusConflict, "intake_pending"},
	{wcdomain.ErrIntakeFieldRequired, http.StatusUnprocessableEntity, "field_required"},
	{wcdomain.ErrIntakeNameInvalid, http.StatusUnprocessableEntity, "name_invalid"},
	{wcdomain.ErrIntakeEmailInvalid, http.StatusUnprocessableEntity, "email_invalid"},
	{wcdomain.ErrIntakePhoneInvalid, http.StatusUnprocessableEntity, "phone_invalid"},
	{wcdomain.ErrIntakeConsentRequired, http.StatusUnprocessableEntity, "consent_required"},
	{wcdomain.ErrMessageEmpty, http.StatusUnprocessableEntity, "message_empty"},
	{wcdomain.ErrMessageTooLong, http.StatusUnprocessableEntity, "message_too_long"},
	{wcdomain.ErrClientMessageIDInvalid, http.StatusBadRequest, "client_message_id_invalid"},
	{wcdomain.ErrSelectionNotOffered, http.StatusUnprocessableEntity, "option_not_offered"},
	{wcdomain.ErrAttachmentsDisabled, http.StatusForbidden, "attachments_disabled"},
	{wcdomain.ErrAttachmentTypeRefused, http.StatusUnsupportedMediaType, "attachment_type_refused"},
	{wcdomain.ErrAttachmentTooLarge, http.StatusRequestEntityTooLarge, "attachment_too_large"},
	{wcdomain.ErrHumanRequestDisabled, http.StatusForbidden, "human_request_disabled"},
	{wcdomain.ErrReferenceNotInWorkspace, http.StatusUnprocessableEntity, "reference_not_in_workspace"},
	{wcdomain.ErrWorkspaceIDRequired, http.StatusForbidden, "workspace_required"},
	{wcdomain.ErrWidgetNameRequired, http.StatusUnprocessableEntity, "name_required"},
	{wcdomain.ErrWidgetNameTooLong, http.StatusUnprocessableEntity, "name_too_long"},
	{wcdomain.ErrWidgetOriginsRequired, http.StatusUnprocessableEntity, "origins_required"},
	{wcdomain.ErrWidgetTooManyOrigins, http.StatusUnprocessableEntity, "too_many_origins"},
	{wcdomain.ErrOriginInvalid, http.StatusUnprocessableEntity, "origin_invalid"},
	{wcdomain.ErrOriginInsecure, http.StatusUnprocessableEntity, "origin_insecure"},
	{wcdomain.ErrWidgetStatusInvalid, http.StatusUnprocessableEntity, "status_invalid"},
	{wcdomain.ErrWidgetPositionInvalid, http.StatusUnprocessableEntity, "position_invalid"},
	{wcdomain.ErrWidgetColorInvalid, http.StatusUnprocessableEntity, "color_invalid"},
	{wcdomain.ErrWidgetTextTooLong, http.StatusUnprocessableEntity, "text_too_long"},
	{wcdomain.ErrIntakeRuleInvalid, http.StatusUnprocessableEntity, "intake_rule_invalid"},
	{wcdomain.ErrIdentityModeInvalid, http.StatusUnprocessableEntity, "identity_mode_invalid"},
	{wcdomain.ErrPrivacyPolicyURLInvalid, http.StatusUnprocessableEntity, "privacy_policy_invalid"},
	{wcdomain.ErrCountryCodeInvalid, http.StatusUnprocessableEntity, "country_code_invalid"},
}

func writeError(w http.ResponseWriter, err error) {
	for _, m := range errorMappings {
		if errors.Is(err, m.err) {
			response.WriteCodedError(w, m.status, m.code, m.err.Error())
			return
		}
	}
	log.Printf("[webchat] unexpected error: %v", err)
	response.WriteCodedError(w, http.StatusInternalServerError, "internal_error", "something went wrong")
}
