package workflow

import (
	"encoding/json"

	"vozko/domain/conversation"
)

const (
	StepOutputSentMessages = "sent_messages"
	StepOutputWouldDo      = "would_do"
)

type SimulatedChangeKind string

const (
	SimulatedLabel      SimulatedChangeKind = "label"
	SimulatedStage      SimulatedChangeKind = "stage"
	SimulatedMember     SimulatedChangeKind = "member"
	SimulatedDepartment SimulatedChangeKind = "department"
	SimulatedDeal       SimulatedChangeKind = "deal"
	SimulatedLeadField  SimulatedChangeKind = "lead_field"
)

type SimulatedChange struct {
	Kind        SimulatedChangeKind `json:"kind"`
	Action      string              `json:"action"`
	TargetID    string              `json:"target_id,omitempty"`
	TargetName  string              `json:"target_name,omitempty"`
	Field       string              `json:"field,omitempty"`
	Value       interface{}         `json:"value,omitempty"`
	Description string              `json:"description"`
}

type SandboxEffects struct {
	Messages []conversation.Message
	Changes  []SimulatedChange
}

func (e SandboxEffects) Empty() bool {
	return len(e.Messages) == 0 && len(e.Changes) == 0
}

func WithSandboxEffects(step *WorkflowRunLog, effects SandboxEffects) *WorkflowRunLog {
	if step == nil || effects.Empty() {
		return step
	}
	cp := *step
	cp.Output = make(map[string]interface{}, len(step.Output)+2)
	for k, v := range step.Output {
		cp.Output[k] = v
	}
	if len(effects.Messages) > 0 {
		cp.Output[StepOutputSentMessages] = genericJSON(effects.Messages)
	}
	if len(effects.Changes) > 0 {
		cp.Output[StepOutputWouldDo] = genericJSON(effects.Changes)
		cp.Simulated = true
	}
	return &cp
}

func genericJSON(value interface{}) interface{} {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}
