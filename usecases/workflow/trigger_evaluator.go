package workflow_usecase

import (
	"fmt"
	"log/slog"
	"time"

	"vozko/domain/cache"
	"vozko/domain/workflow"
)

type triggerEvaluator struct {
	workflowRepo workflow.WorkflowRepository
	runRepo      workflow.WorkflowRunRepository
	engine       *RunEngine
	sharedState  cache.SharedState
	waiters      *ReplyWaiters
}

func NewTriggerEvaluator(
	workflowRepo workflow.WorkflowRepository,
	runRepo workflow.WorkflowRunRepository,
	engine *RunEngine,
	sharedState cache.SharedState,
) workflow.CheckedTriggerEvaluator {
	return &triggerEvaluator{
		workflowRepo: workflowRepo,
		runRepo:      runRepo,
		engine:       engine,
		sharedState:  sharedState,
		waiters:      NewReplyWaiters(workflowRepo, runRepo, engine),
	}
}

func (te *triggerEvaluator) WaitingForReply(event workflow.TriggerEvent) (*workflow.WorkflowRun, error) {
	return te.waiters.WaitingForReply(event)
}

func (te *triggerEvaluator) WakeForReply(run *workflow.WorkflowRun, event workflow.TriggerEvent) bool {
	return te.waiters.WakeForReply(run, event)
}

var _ workflow.ReplyWaiters = (*triggerEvaluator)(nil)

func eventLogAttrs(event workflow.TriggerEvent) []any {
	return []any{
		slog.String("workspace_id", event.WorkspaceID),
		slog.String("entry_id", event.EntryID),
		slog.String("trigger", string(event.TriggerType)),
	}
}

func (te *triggerEvaluator) Evaluate(event workflow.TriggerEvent) {
	_ = te.EvaluateChecked(event)
}

func (te *triggerEvaluator) EvaluateChecked(event workflow.TriggerEvent) error {
	slog.Debug("workflow.trigger.evaluate", eventLogAttrs(event)...)

	handledReplyRun := ""
	handledReplyWorkflow := ""
	if event.TriggerType == workflow.TriggerMessageReceived && event.EntryID != "" {
		routed, ok := te.routeReply(event)
		if !ok {
			return nil
		}
		handledReplyRun, handledReplyWorkflow = routed.RunID, routed.WorkflowID
	}

	workflows, err := te.workflowRepo.FindActiveByTrigger(event.WorkspaceID, event.TriggerType)
	if err != nil {
		slog.Error("workflow.trigger.find_workflows_failed", append(eventLogAttrs(event), slog.String("error", err.Error()))...)
		return fmt.Errorf("find workflows for %s: %w", event.TriggerType, err)
	}

	slog.Debug("workflow.trigger.candidates", append(eventLogAttrs(event), slog.Int("workflows", len(workflows)))...)

	for _, w := range workflows {

		if handledReplyWorkflow != "" && w.ID == handledReplyWorkflow {
			slog.Info("workflow.trigger.message_already_consumed", append(eventLogAttrs(event), slog.String("workflow_id", w.ID), slog.String("run_id", handledReplyRun))...)
			continue
		}

		if !event.MayStart(w.ID) {
			slog.Info("workflow.trigger.own_move_skipped", append(eventLogAttrs(event), slog.String("workflow_id", w.ID))...)
			continue
		}

		if !te.matchesTriggerConfig(w, event) {
			continue
		}

		trigger := w.Graph.TriggerNodeByType(event.TriggerType)
		if trigger == nil {
			continue
		}

		existing, err := te.blockingRun(w, trigger, event)
		if err != nil {
			slog.Error("workflow.trigger.existing_run_check_failed", append(eventLogAttrs(event), slog.String("workflow_id", w.ID), slog.String("error", err.Error()))...)
			continue
		}
		if existing != nil {

			if existing.ID == handledReplyRun {
				continue
			}
			wakesOnMessage := existing.Status == workflow.RunStatusWaiting && (existing.WaitReason == workflow.WaitReasonReply || existing.WaitReason == workflow.WaitReasonDuration)
			if !wakesOnMessage {
				slog.Info("workflow.trigger.existing_run_blocks", append(runLogAttrs(existing, existing.CurrentNodeID), slog.String("status", string(existing.Status)))...)
				continue
			}
			pinned, err := pinnedWorkflow(te.workflowRepo, existing, w)
			if err != nil || pinned == nil {
				slog.Error("workflow.trigger.pinned_version_unreadable", append(runLogAttrs(existing, existing.CurrentNodeID), slog.Any("error", err))...)
				continue
			}
			if existing.WaitReason == workflow.WaitReasonReply {
				te.waiters.wake(existing, pinned, event)
			} else if existing.WaitReason == workflow.WaitReasonDuration {

				te.wakeRunForMessage(existing, pinned, event)
			} else {
				slog.Info("workflow.trigger.existing_run_blocks", append(runLogAttrs(existing, existing.CurrentNodeID), slog.String("status", string(existing.Status)))...)
			}
			continue
		}

		if !tryAcquireWorkspaceSlot(te.sharedState, event.WorkspaceID) {
			slog.Warn("workflow.trigger.workspace_at_capacity", append(eventLogAttrs(event), slog.String("workflow_id", w.ID), slog.Int("limit", maxConcurrentRunsPerWorkspace))...)
			continue
		}

		run := newTriggeredRun(w, trigger, event)
		if err := te.runRepo.Create(run); err != nil {
			slog.Error("workflow.trigger.create_run_failed", append(eventLogAttrs(event), slog.String("workflow_id", w.ID), slog.String("error", err.Error()))...)
			releaseWorkspaceSlot(te.sharedState, event.WorkspaceID)
			continue
		}

		slog.Info("workflow.trigger.run_created", append(runLogAttrs(run, run.CurrentNodeID), slog.String("workflow_id", w.ID), slog.String("started_by", run.StartedBy))...)

		executeLocked(te.engine, run, w)
		releaseWorkspaceSlot(te.sharedState, event.WorkspaceID)
	}
	return nil
}

func (te *triggerEvaluator) blockingRun(w *workflow.Workflow, trigger *workflow.Node, event workflow.TriggerEvent) (*workflow.WorkflowRun, error) {
	if event.StartsRunPerEvent() {
		return nil, nil
	}
	return te.runRepo.FindActiveByEntryAndTrigger(w.ID, event.EntryID, trigger.ID)
}

func (te *triggerEvaluator) routeReply(event workflow.TriggerEvent) (workflow.ReplyRouting, bool) {
	if event.Reply != nil {
		return *event.Reply, true
	}
	waiters := te.waiters
	run, err := waiters.WaitingForReply(event)
	if err != nil {
		return workflow.ReplyRouting{}, false
	}
	if run == nil || !waiters.WakeForReply(run, event) {
		return workflow.ReplyRouting{}, true
	}
	return workflow.ReplyRouting{RunID: run.ID, WorkflowID: run.WorkflowID}, true
}

func (te *triggerEvaluator) wakeRunForMessage(run *workflow.WorkflowRun, w *workflow.Workflow, event workflow.TriggerEvent) {
	if !te.engine.TryLockRun(run.ID) {
		slog.Info("workflow.trigger.message_wake_locked", runLogAttrs(run, run.CurrentNodeID)...)
		return
	}
	defer te.engine.UnlockRun(run.ID)

	slog.Info("workflow.trigger.message_cancels_wait", runLogAttrs(run, run.CurrentNodeID)...)

	node := w.Graph.FindNode(run.CurrentNodeID)
	if node == nil || (node.Type != workflow.NodeTypeWaitDuration && node.Type != workflow.NodeTypeWaitSchedule) {
		return
	}
	edges := w.Graph.OutgoingEdges(run.CurrentNodeID)
	nextID := findExactEdgeByLabel(edges, "message_received")
	if nextID == "" {
		slog.Info("workflow.trigger.wait_has_no_message_edge", runLogAttrs(run, run.CurrentNodeID)...)
		return
	}

	run.State.Set("_wait_outcome", "message_received")
	workflow.ForgetTriggerMessage(&run.State)
	for k, v := range event.Data {
		run.State.Set(k, v)
	}
	run.SetRunning()
	run.State.Set("_prev_node_id", run.CurrentNodeID)
	run.CurrentNodeID = nextID
	run.UpdatedAt = time.Now().UTC()

	if err := te.runRepo.Update(run); err != nil {
		slog.Error("workflow.trigger.message_wake_save_failed", append(runLogAttrs(run, run.CurrentNodeID), slog.String("error", err.Error()))...)
		return
	}

	if err := te.engine.Execute(run, w); err != nil {
		slog.Error("workflow.trigger.engine_error", append(runLogAttrs(run, run.CurrentNodeID), slog.String("error", outbound.String(err.Error())))...)
	}
}

func (te *triggerEvaluator) matchesTriggerConfig(w *workflow.Workflow, event workflow.TriggerEvent) bool {

	if campaignWfID, ok := event.Data["campaign_workflow_id"].(string); ok && campaignWfID != "" {
		if w.ID != campaignWfID {
			slog.Debug("workflow.trigger.skip_campaign_linked_elsewhere", append(eventLogAttrs(event), slog.String("workflow_id", w.ID), slog.String("linked_workflow_id", campaignWfID))...)
			return false
		}
	}

	if accountWfID, ok := event.Data["account_workflow_id"].(string); ok && accountWfID != "" {
		if w.ID != accountWfID {
			slog.Debug("workflow.trigger.skip_account_linked_elsewhere", append(eventLogAttrs(event), slog.String("workflow_id", w.ID), slog.String("linked_workflow_id", accountWfID))...)
			return false
		}
	}

	triggerNode := w.Graph.TriggerNodeByType(event.TriggerType)
	cfg := map[string]interface{}{}
	if triggerNode != nil && triggerNode.Config != nil {
		cfg = triggerNode.Config
	}
	if len(cfg) == 0 && len(w.TriggerConfig) > 0 {
		cfg = w.TriggerConfig
	}
	if len(cfg) == 0 {
		return true
	}

	switch event.TriggerType {
	case workflow.TriggerStageAdded:
		StageID, _ := cfg[workflow.DataKeyStageID].(string)
		if StageID == "" {
			return true
		}
		eventTagID, _ := event.Data[workflow.DataKeyStageID].(string)
		return StageID == eventTagID

	case workflow.TriggerFormSubmitted:
		formID, _ := event.Data[workflow.DataKeyFormID].(string)
		return workflow.FormTriggerAccepts(cfg, formID)

	default:
		return true
	}
}
