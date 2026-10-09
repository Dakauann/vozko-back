package leadaction

import "errors"

var errorCodes = []struct {
	err  error
	code string
}{
	{ErrUnknownAction, "lead_action_unknown"},
	{ErrParamsAmbiguous, "lead_action_params_ambiguous"},
	{ErrKeyRequired, "lead_action_key_required"},
	{ErrValueRequired, "lead_action_value_required"},
	{ErrOwnerRequired, "lead_action_owner_required"},
	{ErrBlockedRequired, "lead_action_blocked_required"},
	{ErrFormatUnsupported, "lead_action_format_unsupported"},
	{ErrAdAccountRequired, "lead_action_ad_account_required"},
	{ErrNameRequired, "lead_action_name_required"},
	{ErrRequirementUnknown, "lead_action_requirement_unknown"},
	{ErrForbidden, "forbidden"},
	{ErrWorkspaceRequired, "lead_action_workspace_required"},
	{ErrActorRequired, "lead_action_actor_required"},
	{ErrIdempotencyKeyRequired, "idempotency_key_required"},
	{ErrIdempotencyKeyReused, "idempotency_key_reused"},
	{ErrNotARun, "lead_action_not_a_run"},
	{ErrRunNotFound, "lead_action_run_not_found"},
	{ErrPreviewNotFound, "lead_action_preview_not_found"},
	{ErrClaimLost, "lead_action_claim_lost"},
	{ErrUnavailable, "lead_actions_unavailable"},
	{ErrSelectionTooLarge, "lead_action_selection_too_large"},
	{ErrSelectionEmpty, "lead_action_selection_empty"},
	{ErrModeUnsupported, "lead_action_mode_unsupported"},
	{ErrInProgress, "lead_action_in_progress"},
	{ErrPhoneUnavailable, "lead_action_phone_unavailable"},
	{ErrSnapshotLost, "lead_action_snapshot_lost"},
	{ErrAudienceNotFound, "lead_action_audience_not_found"},
	{ErrSendParamsRequired, "lead_action_send_params_required"},
	{ErrSendPhoneRequired, "lead_action_send_phone_required"},
	{ErrSendTemplateRequired, "lead_action_send_template_required"},
	{ErrSendInstanceRequired, "lead_action_send_instance_required"},
	{ErrSendMessageRequired, "lead_action_send_message_required"},
	{ErrSendPacingInvalid, "lead_action_send_pacing_invalid"},
	{ErrCallListRequired, "lead_action_call_list_required"},
}

func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	for _, known := range errorCodes {
		if errors.Is(err, known.err) {
			return known.code
		}
	}
	return ""
}
