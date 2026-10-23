package workflow

import (
	"time"

	"vozko/domain/shared"
)

func (r *WorkflowRun) OnConversation() bool {
	return r.EntryType != EntryTypeSIPCall && !r.OnLead()
}

func (r *WorkflowRun) FollowsConversationOwner() bool {
	return r.OnConversation() && !StartedByAnAgent(r.StartedBy) && (r.TriggerType == TriggerMessageReceived || r.TriggerType == TriggerFirstMessage)
}

type StepNamer interface {
	StepName(nodeType NodeType) string
}

type ConversationRun struct {
	ID              string      `json:"id"`
	WorkflowID      string      `json:"workflowId"`
	WorkflowName    string      `json:"workflowName"`
	WorkflowVersion *int        `json:"workflowVersion,omitempty"`
	EntryID         string      `json:"entryId"`
	EntryType       string      `json:"entryType"`
	Status          RunStatus   `json:"status"`
	TriggerType     TriggerType `json:"triggerType,omitempty"`
	StartedBy       string      `json:"startedBy,omitempty"`
	StartedAt       time.Time   `json:"startedAt"`
	CompletedAt     *time.Time  `json:"completedAt,omitempty"`
	DurationMs      *int64      `json:"durationMs,omitempty"`
	WaitReason      WaitReason  `json:"waitReason,omitempty"`
	CurrentNodeID   string      `json:"currentNodeId,omitempty"`
	CurrentNodeType NodeType    `json:"currentNodeType,omitempty"`
	CurrentNodeName string      `json:"currentNodeName,omitempty"`
}

func (r ConversationRun) Duration() *int64 {
	return RunDuration(r.Status, r.StartedAt, r.CompletedAt)
}

func (r ConversationRun) Described(steps StepNamer) ConversationRun {
	r.DurationMs = r.Duration()
	if r.Status.IsTerminal() {
		r.CurrentNodeID, r.CurrentNodeType, r.CurrentNodeName, r.WaitReason = "", "", "", ""
		return r
	}
	if r.CurrentNodeType != "" && steps != nil {
		r.CurrentNodeName = steps.StepName(r.CurrentNodeType)
	}
	return r
}

type ConversationRuns interface {
	ListByEntry(workspaceID, entryID, entryType string, pagination shared.Pagination) (*shared.PaginatedResult[*ConversationRun], error)
}
