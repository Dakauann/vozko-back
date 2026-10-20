package workflow_usecase

import (
	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type PinSources interface {
	RunVisible(viewer shared.Person, workspaceID, runID string) bool
}

type runPinSources struct {
	runs       workflow.WorkflowRunRepository
	visibility conversation.Visibility
}

func NewRunPinSources(runs workflow.WorkflowRunRepository, visibility conversation.Visibility) PinSources {
	return &runPinSources{runs: runs, visibility: visibility}
}

func (s *runPinSources) RunVisible(viewer shared.Person, workspaceID, runID string) bool {
	if s == nil || s.runs == nil {
		return false
	}
	_, err := findVisibleRun(s.runs, s.visibility, viewer, workspaceID, runID)
	return err == nil
}

func withheldFor(sources PinSources, viewer shared.Person, wf *workflow.Workflow) *workflow.Workflow {
	if wf == nil {
		return wf
	}
	runIDs := workflow.RunPinIDs(&wf.Graph)
	if len(runIDs) == 0 {
		return wf
	}
	visible := map[string]bool{}
	if sources != nil {
		for _, runID := range runIDs {
			visible[runID] = sources.RunVisible(viewer, wf.WorkspaceID, runID)
		}
	}
	cp := *wf
	cp.Graph = workflow.WithholdPins(wf.Graph, func(runID string) bool { return visible[runID] })
	return &cp
}

func withoutProductionPins(wf *workflow.Workflow) *workflow.Workflow {
	return withheldFor(nil, shared.Person{}, wf)
}

func viewerOf(scope workflow.Scope) shared.Person {
	return shared.Person{UserID: scope.Requester.UserID, SystemAdmin: scope.Requester.SystemAdmin}
}
