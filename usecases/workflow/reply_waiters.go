package workflow_usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"vozko/domain/choicestats"
	"vozko/domain/workflow"
)

type ReplyWaiters struct {
	workflows workflow.WorkflowRepository
	runs      workflow.WorkflowRunRepository
	engine    *RunEngine
}

func NewReplyWaiters(workflows workflow.WorkflowRepository, runs workflow.WorkflowRunRepository, engine *RunEngine) *ReplyWaiters {
	return &ReplyWaiters{workflows: workflows, runs: runs, engine: engine}
}

func (w *ReplyWaiters) WaitingForReply(event workflow.TriggerEvent) (*workflow.WorkflowRun, error) {
	run, err := w.runs.FindWaitingReplyByEntry(event.EntryID)
	if err == nil && run != nil && run.FollowsConversationOwner() {
		var published *workflow.Workflow
		published, err = w.workflows.FindByID(run.WorkflowID)
		if err == nil {
			run, err = conversationRunCurrent(w.runs, w.engine, run, published, event)
		}
	}
	if err == nil {
		return run, nil
	}
	slog.Error("workflow.reply.waiting_lookup_failed", append(eventLogAttrs(event), slog.String("error", err.Error()))...)
	if w.engine != nil && w.engine.metrics != nil {
		w.engine.metrics.IncWorkflowReplyLookupFailed()
	}
	w.engine.raiseAlert(workflow.InboundLookupFailedSignal(event, time.Now().UTC()))
	return nil, fmt.Errorf("find the run waiting for a reply on entry %s: %w", event.EntryID, err)
}

func (w *ReplyWaiters) WakeForReply(run *workflow.WorkflowRun, event workflow.TriggerEvent) bool {
	if !answers(run, event) {
		return false
	}
	wf, err := loadRunWorkflow(w.workflows, w.runs, w.engine, run)
	if wf == nil {
		if err != nil {
			slog.Error("workflow.reply.workflow_unreadable", append(runLogAttrs(run, run.CurrentNodeID), slog.String("error", err.Error()))...)
		}
		return false
	}
	w.wake(run, wf, event)
	return true
}

func answers(run *workflow.WorkflowRun, event workflow.TriggerEvent) bool {
	if run.AnswersWait(event.SentAt) {
		return true
	}
	slog.Info("workflow.trigger.reply_sent_before_wait", append(runLogAttrs(run, run.CurrentNodeID),
		slog.Time("sent_at", event.SentAt), slog.Time("waiting_since", *run.WaitingSince))...)
	return false
}

func (w *ReplyWaiters) wake(run *workflow.WorkflowRun, wf *workflow.Workflow, event workflow.TriggerEvent) {
	if !answers(run, event) {
		return
	}
	if !w.engine.TryLockRun(run.ID) {
		slog.Info("workflow.trigger.reply_wake_locked", runLogAttrs(run, run.CurrentNodeID)...)
		return
	}
	defer w.engine.UnlockRun(run.ID)

	slog.Info("workflow.trigger.reply_wake", runLogAttrs(run, run.CurrentNodeID)...)

	parked := wf.Graph.FindNode(run.CurrentNodeID)
	promptKey := run.State.GetString(workflow.StateKeyChoicePrompt)
	if err := AdvanceOnReply(run, wf, event.Data); err != nil {
		if errors.Is(err, ErrInteractiveReplyUnhandled) {
			slog.Info("workflow.trigger.interactive_reply_unhandled", runLogAttrs(run, run.CurrentNodeID)...)
			return
		}
		if errors.Is(err, ErrMissingRepliedEdge) {
			w.engine.recordGuard(run, err)
			run.SetError(err.Error())
			if uerr := w.engine.saveWith(w.runs, run); uerr != nil {
				slog.Error("workflow.trigger.mark_error_failed", append(runLogAttrs(run, run.CurrentNodeID), slog.String("error", uerr.Error()))...)
			}
		}
		return
	}

	if err := w.runs.Update(run); err != nil {
		slog.Error("workflow.trigger.reply_wake_save_failed", append(runLogAttrs(run, run.CurrentNodeID), slog.String("error", err.Error()))...)
		return
	}
	w.engine.recordChoiceClick(run, parked, promptKey, event.Data)

	if err := w.engine.Execute(run, wf); err != nil {
		slog.Error("workflow.trigger.engine_error", append(runLogAttrs(run, run.CurrentNodeID), slog.String("error", outbound.String(err.Error())))...)
	}
}

func (e *RunEngine) recordChoiceClick(run *workflow.WorkflowRun, parked *workflow.Node, promptKey string, data map[string]interface{}) {
	if e.choices == nil || parked == nil || !parked.Type.IsInteractivePrompt() || promptKey == "" {
		return
	}
	selected, _ := data[workflow.DataKeySelectedOptionID].(string)
	e.choices.RecordClick(context.Background(), choicestats.Click{
		WorkspaceID: run.WorkspaceID,
		PromptKey:   promptKey,
		Option:      choicestats.ResponseOption(workflow.InteractiveOptionIDs(parked.Config), selected),
		At:          time.Now().UTC(),
	})
}
