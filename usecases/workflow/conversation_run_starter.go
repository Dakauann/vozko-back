package workflow_usecase

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"vozko/domain/cache"
	"vozko/domain/workflow"
)

var errConversationRunStarterIncomplete = errors.New("workflow: starting a workflow for a conversation needs the workflow and run repositories, the launcher and the shared state")

type StartedRun struct {
	RunID           string
	AlreadyRunning  bool
	StartedAt       time.Time
	CurrentNodeID   string
	CurrentNodeType workflow.NodeType
}

type runStarter struct {
	runs        workflow.WorkflowRunRepository
	launcher    RunLauncher
	sharedState cache.SharedState
	workflows   workflow.PublishedReader
}

func (s runStarter) pinned(run *workflow.WorkflowRun, published *workflow.Workflow) *workflow.Workflow {
	if s.workflows == nil {
		return published
	}
	w, err := pinnedWorkflow(s.workflows, run, published)
	if err != nil {
		slog.Warn("workflow.run_start.pinned_version_unreadable", append(runLogAttrs(run, run.CurrentNodeID), slog.String("error", err.Error()))...)
		return nil
	}
	return w
}

func (s runStarter) start(w *workflow.Workflow, trigger *workflow.Node, event workflow.TriggerEvent) (StartedRun, error) {
	existing, err := s.runs.FindActiveByEntryAndTrigger(w.ID, event.EntryID, trigger.ID)
	if err != nil {
		return StartedRun{}, err
	}
	if existing != nil {
		started := StartedRun{RunID: existing.ID, AlreadyRunning: true, StartedAt: existing.StartedAt, CurrentNodeID: existing.CurrentNodeID}
		if pinned := s.pinned(existing, w); pinned != nil {
			if node := pinned.Graph.FindNode(existing.CurrentNodeID); node != nil {
				started.CurrentNodeType = node.Type
			}
		}
		return started, nil
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
	return StartedRun{RunID: run.ID, StartedAt: run.StartedAt, CurrentNodeID: run.CurrentNodeID, CurrentNodeType: trigger.Type}, nil
}

type ConversationStart struct {
	WorkspaceID string
	WorkflowID  string
	EntryID     string
	EntryType   string
	TriggerType workflow.TriggerType
	StartedBy   string
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
		starter:   runStarter{runs: runs, launcher: launcher, sharedState: sharedState, workflows: workflows},
	}, nil
}

func (s *ConversationRunStarter) StartForConversation(in ConversationStart) (StartedRun, error) {
	w, err := workflow.LoadPublished(s.workflows, in.WorkflowID)
	if err != nil {
		return StartedRun{}, err
	}
	if err := w.StartsConversationsIn(in.WorkspaceID); err != nil {
		return StartedRun{}, err
	}
	trigger, err := conversationTrigger(w, in.TriggerType)
	if err != nil {
		return StartedRun{}, err
	}
	return s.starter.start(w, trigger, workflow.TriggerEvent{
		WorkspaceID: in.WorkspaceID,
		EntryID:     in.EntryID,
		EntryType:   in.EntryType,
		TriggerType: workflow.TriggerType(trigger.Type),
		StartedBy:   in.StartedBy,
		Data:        in.Data,
	})
}

func conversationTrigger(w *workflow.Workflow, triggerType workflow.TriggerType) (*workflow.Node, error) {
	if triggerType == "" {
		return workflow.MessageStartTrigger(w.Graph.Nodes)
	}
	if trigger := w.Graph.TriggerNodeByType(triggerType); trigger != nil {
		return trigger, nil
	}
	return nil, fmt.Errorf("%w: %s", workflow.ErrTriggerNotInWorkflow, triggerType)
}
