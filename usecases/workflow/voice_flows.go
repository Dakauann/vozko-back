package workflow_usecase

import (
	"time"

	"github.com/google/uuid"

	"vozko/domain/workflow"
)

type inboundVoiceFlows struct {
	workflows workflow.WorkflowRepository
	runs      workflow.WorkflowRunRepository
	engine    *RunEngine
}

var _ workflow.InboundVoiceFlows = (*inboundVoiceFlows)(nil)

func NewInboundVoiceFlows(workflows workflow.WorkflowRepository, runs workflow.WorkflowRunRepository, engine *RunEngine) workflow.InboundVoiceFlows {
	return &inboundVoiceFlows{workflows: workflows, runs: runs, engine: engine}
}

func (f *inboundVoiceFlows) FlowFor(workspaceID, trunkID string) (*workflow.Workflow, error) {
	active, err := f.workflows.FindActiveByTrigger(workspaceID, workflow.TriggerCallReceived)
	if err != nil {
		return nil, err
	}
	var match *workflow.Workflow
	for _, w := range active {
		if w.VoiceTrunkID() != trunkID {
			continue
		}
		if match != nil {
			return nil, workflow.ErrVoiceFlowAmbiguous
		}
		match = w
	}
	return match, nil
}

func (f *inboundVoiceFlows) Answer(flow *workflow.Workflow, call workflow.InboundVoiceCall) error {
	if flow == nil || flow.Type != workflow.WorkflowTypeVoice || flow.WorkspaceID != call.WorkspaceID {
		return workflow.ErrTriggerNotAllowedForType
	}
	trigger := flow.Graph.TriggerNodeByType(workflow.TriggerCallReceived)
	if trigger == nil {
		return workflow.ErrGraphTriggerTypeMismatch
	}
	run := newVoiceRun(flow, trigger, call)
	if err := f.runs.Create(run); err != nil {
		return err
	}
	return f.engine.ExecuteWithRuntime(run, flow, call.Call)
}

func newVoiceRun(flow *workflow.Workflow, trigger *workflow.Node, call workflow.InboundVoiceCall) *workflow.WorkflowRun {
	state := workflow.NewRunState()
	state.Set("call_id", call.CallID)
	state.Set("caller_number", call.CallerNumber)
	state.Set("called_number", call.CalledNumber)
	now := time.Now().UTC()
	return &workflow.WorkflowRun{
		ID:            uuid.NewString(),
		WorkflowID:    flow.ID,
		WorkspaceID:   flow.WorkspaceID,
		EntryID:       call.CallID,
		EntryType:     workflow.EntryTypeSIPCall,
		Status:        workflow.RunStatusRunning,
		TriggerNodeID: trigger.ID,
		CurrentNodeID: trigger.ID,
		State:         state,
		StartedAt:     now,
		UpdatedAt:     now,
	}
}
