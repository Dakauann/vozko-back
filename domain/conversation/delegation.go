package conversation

import (
	"context"
	"errors"
	"strings"
	"time"

	"vozko/domain/shared"
)

var (
	ErrAutomationInvalid  = errors.New("conversation: delegate to an agent or a workflow")
	ErrAutomationUnusable = errors.New("conversation: that agent or workflow is not active in this workspace")
)

func NewAutomation(kind, id string) (Automation, error) {
	a := Automation{Kind: AutomationKind(strings.TrimSpace(kind)), ID: strings.TrimSpace(id)}
	if !a.Valid() {
		return Automation{}, ErrAutomationInvalid
	}
	return a, nil
}

func (a Automation) Valid() bool {
	return (a.Kind == AutomationAgent || a.Kind == AutomationWorkflow) && strings.TrimSpace(a.ID) != ""
}

type Delegation struct {
	WorkspaceID string
	EntryID     string
	EntryType   shared.EntryType
	Automation  Automation
	DelegatedBy string
	DelegatedAt time.Time
}

type DelegationRepository interface {
	Find(ctx context.Context, entryID string, entryType shared.EntryType) (*Delegation, error)
	FindMany(ctx context.Context, entries []shared.EntryRef) (map[shared.EntryRef]Automation, error)
	Save(ctx context.Context, d Delegation) error
	Delete(ctx context.Context, entryID string, entryType shared.EntryType) error
}

func (c ChannelAutomation) DelegatedTo(a Automation) ChannelAutomation {
	out := c
	id := a.ID
	out.AgentID, out.WorkflowID = nil, nil
	out.EnableAgentResponses, out.EnableWorkflow = false, false
	switch a.Kind {
	case AutomationAgent:
		out.AgentID, out.EnableAgentResponses = &id, true
	case AutomationWorkflow:
		out.WorkflowID, out.EnableWorkflow = &id, true
	}
	return out
}
