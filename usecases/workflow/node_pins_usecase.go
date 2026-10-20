package workflow_usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type PinFromRunInput struct {
	WorkflowID string
	NodeID     string
	RunID      string
	StepID     string
	Revision   int
}

type NodePinsUseCase struct {
	workflows workflow.ScopedWorkflowsUseCase
	runs      workflow.GetRunUseCase
	now       func() time.Time
}

func NewNodePinsUseCase(workflows workflow.ScopedWorkflowsUseCase, runs workflow.GetRunUseCase) *NodePinsUseCase {
	return &NodePinsUseCase{workflows: workflows, runs: runs, now: func() time.Time { return time.Now().UTC() }}
}

func (uc *NodePinsUseCase) PinFromRun(ctx context.Context, scope workflow.Scope, viewer shared.Person, in PinFromRunInput) (*workflow.Workflow, error) {
	stored, err := uc.workflows.Get(scope, in.WorkflowID)
	if err != nil {
		return nil, err
	}
	if uc.runs == nil || strings.TrimSpace(in.RunID) == "" {
		return nil, workflow.ErrRunNotFound
	}
	detail, err := uc.runs.PickSteps(viewer, scope.WorkspaceID, in.RunID, workflow.PinPick(in.NodeID, in.StepID))
	if err != nil {
		return nil, err
	}
	if detail == nil || detail.Run == nil || detail.Run.WorkflowID != stored.ID {
		return nil, workflow.ErrRunNotFound
	}
	step := recordedStepOf(detail.Logs, in.NodeID, in.StepID)
	pin, err := workflow.PinFromStep(step, detail.Run.ID, uc.now())
	if err != nil {
		return nil, fmt.Errorf("%w: node %s in run %s", err, in.NodeID, detail.Run.ID)
	}
	graph := stored.Graph.Clone()
	if err := workflow.SetPin(&graph, in.NodeID, pin); err != nil {
		return nil, err
	}
	return uc.workflows.Update(ctx, scope, stored.ID, &workflow.Workflow{
		Name:        stored.Name,
		Description: stored.Description,
		Type:        stored.Type,
		Graph:       graph,
	}, in.Revision)
}

func recordedStepOf(steps []*workflow.WorkflowRunLog, nodeID, stepID string) *workflow.WorkflowRunLog {
	var picked *workflow.WorkflowRunLog
	for _, step := range steps {
		if step == nil || step.NodeID != nodeID {
			continue
		}
		if stepID != "" {
			if step.ID == stepID {
				return step
			}
			continue
		}
		if step.Pinnable() {
			picked = step
		}
	}
	return picked
}
