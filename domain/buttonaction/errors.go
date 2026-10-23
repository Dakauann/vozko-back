package buttonaction

import (
	"errors"

	"vozko/domain/shared"
	"vozko/domain/workflow"
)

var (
	ErrWorkspaceRequired       = errors.New("button action: workspace is required")
	ErrActorRequired           = errors.New("button action: the person acting is required")
	ErrForbidden               = errors.New("button action: you do not have the permissions this action needs")
	ErrTemplateNotFound        = errors.New("button action: template not found")
	ErrCampaignNotFound        = errors.New("button action: campaign not found")
	ErrCampaignNoTemplate      = errors.New("button action: the campaign has no template")
	ErrKindUnknown             = errors.New("button action: unknown action kind")
	ErrButtonNotQuickReply     = errors.New("button action: the button is not a quick reply of the template")
	ErrButtonActionStale       = errors.New("button action: the button text changed since it was shown, look at the button again and confirm its action")
	ErrParamsInvalid           = errors.New("button action: the parameters do not fit the action kind")
	ErrParamsNotInWorkspace    = errors.New("button action: a parameter does not belong to this workspace")
	ErrConfirmationTooLong     = errors.New("button action: the confirmation is too long")
	ErrDuplicateButton         = errors.New("button action: a button can carry only one action")
	ErrExitLocked              = errors.New("button action: exits are set on the template and a campaign cannot add, change or remove them")
	ErrExitWordLength          = errors.New("button action: an exit word needs 1 to 30 characters")
	ErrExitWordFormat          = errors.New("button action: an exit word is a single word without spaces, typed without the slash the system adds")
	ErrExitConfirmationLong    = errors.New("button action: the exit confirmation is too long")
	ErrVersionConflict         = shared.ErrVersionConflict
	ErrUnavailable             = errors.New("button action: button actions are not available on this server")
	ErrExitRiskNotAcknowledged = errors.New("button action: the template shows no way out, acknowledge the risk to go on")
	ErrMarketingNeedsExit      = errors.New("button action: a marketing template must show the lead a way out, an exit button or a /word in its text")
)

var errorCodes = []struct {
	err  error
	code string
}{
	{ErrWorkspaceRequired, "button_action_workspace_required"},
	{ErrActorRequired, "button_action_actor_required"},
	{ErrForbidden, "button_action_forbidden"},
	{ErrTemplateNotFound, "button_action_template_not_found"},
	{ErrCampaignNotFound, "button_action_campaign_not_found"},
	{ErrCampaignNoTemplate, "button_action_campaign_without_template"},
	{ErrKindUnknown, "button_action_kind_unknown"},
	{ErrButtonNotQuickReply, "button_action_not_quick_reply"},
	{ErrButtonActionStale, "button_action_stale"},
	{ErrParamsInvalid, "button_action_params_invalid"},
	{ErrParamsNotInWorkspace, "button_action_params_not_found"},
	{ErrConfirmationTooLong, "button_action_confirmation_too_long"},
	{ErrDuplicateButton, "button_action_duplicate_button"},
	{ErrExitLocked, "button_action_exit_locked"},
	{ErrExitWordLength, "exit_word_length"},
	{ErrExitWordFormat, "exit_word_format"},
	{ErrExitConfirmationLong, "exit_confirmation_too_long"},
	{ErrUnavailable, "button_action_unavailable"},
	{ErrExitRiskNotAcknowledged, "exit_risk_not_acknowledged"},
	{ErrMarketingNeedsExit, "marketing_template_needs_exit"},
	{workflow.ErrWorkflowNeedsMessageTrigger, workflow.NeedsMessageTriggerCode},
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
