package workflowalerthttp

import (
	"time"

	"vozko/domain/workflow"
)

type AlertResponse struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind" enums:"workflow_called_inactive,node_failing,webhook_rejecting,run_stuck,agent_tool_failing,inbound_lookup_failed"`
	Severity   string     `json:"severity" enums:"high,medium"`
	FixAction  string     `json:"fixAction" enums:"publish,open_run,open_node,view_deliveries,open_agent,open_conversation"`
	Status     string     `json:"status" enums:"open,resolved"`
	WorkflowID string     `json:"workflowId,omitempty"`
	NodeID     string     `json:"nodeId,omitempty"`
	AgentID    string     `json:"agentId,omitempty"`
	Subject    string     `json:"subject,omitempty"`
	RunID      string     `json:"runId,omitempty"`
	EntryID    string     `json:"entryId,omitempty"`
	EntryType  string     `json:"entryType,omitempty"`
	Sample     string     `json:"sample,omitempty"`
	Count      int        `json:"count"`
	FirstSeen  time.Time  `json:"firstSeen"`
	LastSeen   time.Time  `json:"lastSeen"`
	ResolvedAt *time.Time `json:"resolvedAt,omitempty"`
}

type WorkflowAlertsResponse struct {
	Items []AlertResponse `json:"items"`
}

func toResponse(a workflow.RuntimeAlert) AlertResponse {
	out := AlertResponse{
		ID:         a.ID,
		Kind:       string(a.Key.Kind),
		Severity:   string(a.Severity),
		FixAction:  string(a.FixAction),
		Status:     string(a.Status),
		WorkflowID: a.Key.WorkflowID,
		NodeID:     a.Key.NodeID,
		AgentID:    a.Key.AgentID,
		Subject:    a.Key.Subject,
		RunID:      a.RunID(),
		Sample:     a.Sample,
		Count:      a.Count,
		FirstSeen:  a.FirstSeen,
		LastSeen:   a.LastSeen,
		ResolvedAt: a.ResolvedAt,
	}
	if ref, ok := a.Conversation(); ok {
		out.EntryID, out.EntryType = ref.EntryID, string(ref.EntryType)
	}
	return out
}
