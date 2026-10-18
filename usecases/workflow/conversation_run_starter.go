package workflow_usecase

import (
	"errors"

	"vozko/domain/cache"
	"vozko/domain/workflow"
)

var errConversationRunStarterIncomplete = errors.New("workflow: starting a workflow for a conversation needs the workflow and run repositories, the launcher and the shared state")

type StartedRun struct {
	RunID          string
	AlreadyRunning bool
}

type runStarter struct {
	runs        workflow.WorkflowRunRepository
	launcher    RunLauncher
	sharedState cache.SharedState
}

func (s runStarter) start(w *workflow.Workflow, trigger *workflow.Node, event workflow.TriggerEvent) (StartedRun, error) {
	existing, err := s.runs.FindActiveByEntryAndTrigger(w.ID, event.EntryID, trigger.ID)
	if err != nil {
		return StartedRun{}, err
	}
	if existing != nil {
		return StartedRun{RunID: existing.ID, AlreadyRunning: true}, nil
	}
	if !tryAcquireWorkspaceSlot(s.sharedState, event.WorkspaceID) {
		return StartedRun{}, workflow.ErrWorkspaceAtCapacity
	}
	run := newTriggeredRun(w, trigger, event)
	if err := s.runs.Create(run); err != nil {
		releaseWorkspaceSlot(s.sharedState, event.WorkspaceID)
		return StartedRun{}, err
	}
	s.launcher.Launch(run, w)
	return StartedRun{RunID: run.ID}, nil
}

type ConversationStart struct {
	WorkspaceID string
	WorkflowID  string
	EntryID     string
	EntryType   string
	Data        map[string]interface{}
}

type ConversationRunStarter struct {
	workflows workflow.WorkflowRepository
	starter   runStarter
}

func NewConversationRunStarter(
	workflows workflow.WorkflowRepository,
	runs workflow.WorkflowRunRepository,
	launcher RunLauncher,
	sharedState cache.SharedState,
) (*ConversationRunStarter, error) {
	if workflows == nil || runs == nil || launcher == nil || sharedState == nil {
		return nil, errConversationRunStarterIncomplete
	}
	return &ConversationRunStarter{
		workflows: workflows,
		starter:   runStarter{runs: runs, launcher: launcher, sharedState: sharedState},
	}, nil
}

func (s *ConversationRunStarter) StartForConversation(in ConversationStart) (StartedRun, error) {
	w, err := s.workflows.FindByID(in.WorkflowID)
	if err != nil {
		return StartedRun{}, err
	}
	if err := w.StartsConversationsIn(in.WorkspaceID); err != nil {
		return StartedRun{}, err
	}
	trigger := w.Graph.TriggerNode()
	if trigger == nil {
		return StartedRun{}, workflow.ErrGraphNoTrigger
	}
	return s.starter.start(w, trigger, workflow.TriggerEvent{
		WorkspaceID: in.WorkspaceID,
		EntryID:     in.EntryID,
		EntryType:   in.EntryType,
		TriggerType: workflow.TriggerType(trigger.Type),
		Data:        in.Data,
	})
}
