package leadexit

import (
	"errors"

	"vozko/domain/buttonaction"
	"vozko/domain/calls/calllist"
	"vozko/domain/conversation"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/lead"
	wo "vozko/domain/whatsapp_outreach"
	"vozko/domain/workflow"
)

var (
	ErrWorkspaceRequired = errors.New("lead exit: workspace is required")
	ErrUnavailable       = errors.New("lead exit: the lead exit is not available on this server")
	ErrInboundIncomplete = errors.New("lead exit: the inbound message needs its workspace and id")
	ErrRunInProgress     = errors.New("lead exit: this inbound message is being handled elsewhere")
)

const reasonActionFailed = "lead_exit_action_failed"

var errorCodes = []struct {
	err  error
	code string
}{
	{ErrWorkspaceRequired, "lead_exit_workspace_required"},
	{ErrUnavailable, "lead_exit_unavailable"},
	{ErrInboundIncomplete, "lead_exit_inbound_incomplete"},
	{ia.ErrDepartmentOutOfScope, "hand_off_department_out_of_scope"},
	{ia.ErrHandOffTargetNoAccess, "hand_off_target_no_access"},
	{conversation.ErrAutomationUnusable, "automation_unusable"},
	{conversation.ErrAutomationInvalid, "automation_invalid"},
	{workflow.ErrWorkflowNotFound, "workflow_not_found"},
	{workflow.ErrWorkflowNotActive, "workflow_not_active"},
	{workflow.ErrInvalidWorkflowType, "workflow_type_invalid"},
	{workflow.ErrGraphNoTrigger, "workflow_no_trigger"},
	{workflow.ErrWorkflowNeedsMessageTrigger, workflow.NeedsMessageTriggerCode},
	{workflow.ErrWorkspaceAtCapacity, "workflow_workspace_at_capacity"},
}

var refusals = []error{
	lead.ErrLeadNotFound,
	conversation.ErrAutomationUnusable,
	conversation.ErrAutomationInvalid,
	ia.ErrDepartmentOutOfScope,
	ia.ErrHandOffTargetNoAccess,
	workflow.ErrWorkflowNotFound,
	workflow.ErrWorkflowNotActive,
	workflow.ErrInvalidWorkflowType,
	workflow.ErrGraphNoTrigger,
	workflow.ErrWorkflowNeedsMessageTrigger,
	calllist.ErrLeadNotAdmitted,
	calllist.ErrListNotFound,
	calllist.ErrListNotActive,
	calllist.ErrListFull,
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
	if code := buttonaction.ErrorCode(err); code != "" {
		return code
	}
	if code := lead.ErrorCode(err); code != "" {
		return code
	}
	if code := wo.ErrorCode(err); code != "" {
		return code
	}
	return calllist.ErrorCode(err)
}

func ReasonCode(err error) string {
	if err == nil {
		return ""
	}
	if code := ErrorCode(err); code != "" {
		return code
	}
	return reasonActionFailed
}

func IsRefusal(err error) bool {
	if err == nil {
		return false
	}
	for _, refusal := range refusals {
		if errors.Is(err, refusal) {
			return true
		}
	}
	return lead.IsInputRefusal(err) || buttonaction.ErrorCode(err) != ""
}
