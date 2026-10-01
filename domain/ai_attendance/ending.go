package ai_attendance

const (
	EndReasonHumanReply           = "human_reply"
	EndReasonConversationFinished = "conversation_finished"
	EndReasonAutomationHandoff    = "automation_handoff"
	EndReasonAutomationPaused     = "automation_paused"
	EndReasonManualAssignment     = "manual_assignment"
)

type EndRequest struct {
	WorkspaceID string
	EntryID     string
	EntryType   string
	CallID      string
	Outcome     Outcome
	Reason      string
	HandoffTo   string
	EndedBy     string
}

type SessionEnder interface {
	End(request EndRequest)
}
