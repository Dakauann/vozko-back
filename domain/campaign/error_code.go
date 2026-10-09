package campaign

import (
	"errors"

	"vozko/domain/whatsapp/template"
)

var errorCodes = []struct {
	err  error
	code string
}{
	{ErrSelectionEmpty, "send_selection_empty"},
	{ErrSelectionOverCampaignCap, "send_selection_over_campaign_cap"},
	{ErrSelectionTooLarge, "send_selection_too_large"},
	{ErrHeaderVariableUnsupported, "send_header_variable_unsupported"},
	{ErrNamedParametersUnsupported, "send_named_parameters_unsupported"},
	{ErrUnaffordable, "unaffordable"},
	{ErrOverMonthlyCap, "over_cap"},
	{ErrFirstNInvalid, "send_first_n_invalid"},
	{ErrNothingEligible, "send_nothing_eligible"},
	{ErrCreationScopeMissing, "send_creation_scope_missing"},
	{ErrDepartmentRequired, "send_department_required"},
	{ErrNotFromSelection, "send_not_from_selection"},
	{ErrAlreadyStarted, "send_already_started"},
	{ErrSendIncomplete, "send_incomplete"},
	{ErrSelectionSendLocked, "send_selection_locked"},
	{ErrSendPreparing, "send_preparing"},
	{ErrSelectionStartNeedsReview, "send_start_from_leads"},
	{template.ErrPricingUnavailable, "send_pricing_unavailable"},
	{ErrBindingsMismatch, "send_bindings_mismatch"},
	{ErrBindingUnknown, "send_binding_unknown"},
	{ErrBindingLiteralEmpty, "send_binding_literal_empty"},
	{ErrBindingFieldUnknown, "send_binding_field_unknown"},
	{ErrBindingSensitive, "send_binding_sensitive"},
	{ErrWorkflowNotFound, "campaign_workflow_not_found"},
	{ErrWorkflowForbidden, "campaign_workflow_forbidden"},
	{ErrWorkflowVarsMissing, "campaign_workflow_vars_missing"},
	{ErrAgentNotFound, "campaign_agent_not_found"},
	{ErrAgentVarsMissing, "AGENT_REQUIRED_VARIABLE_MISSING"},
	{ErrAutomationUnavailable, "campaign_automation_unavailable"},
	{ErrIdempotencyUnavailable, "campaign_idempotency_unavailable"},
	{ErrLeadTargetsUnavailable, "campaign_lead_targets_unavailable"},
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
