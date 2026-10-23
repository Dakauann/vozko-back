package workflow_usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"vozko/domain/workflow"
)

var ErrRunBusy = errors.New("workflow: the run is being processed right now, try again")

func resumeRunFromCurrent(
	workflowRepo workflow.WorkflowRepository,
	runRepo workflow.WorkflowRunRepository,
	engine *RunEngine,
	run *workflow.WorkflowRun,
) error {
	if cancelled, err := cancelWhenAutomationOff(runRepo, engine, run); cancelled || err != nil {
		if errors.Is(err, workflow.ErrAutomationStateUnreadable) {
			return retryAfterUnreadableGate(runRepo, engine, run, err)
		}
		return err
	}
	waitedFor := run.WaitReason

	w, err := loadRunWorkflow(workflowRepo, runRepo, engine, run)
	if w == nil {
		return err
	}

	run.SetRunning()
	if err := runRepo.Update(run); err != nil {
		return err
	}

	node := w.Graph.FindNode(run.CurrentNodeID)
	if node == nil {
		cause := errors.New("current node not found during resume")
		engine.recordGuard(run, cause)
		run.SetError(cause.Error())
		_ = engine.saveWith(runRepo, run)
		return cause
	}

	if node.Type.AwaitsContact() {
		nextID := ""
		switch {
		case expiresUnanswered(node.Type, waitedFor):
			nextID = unansweredEdge(run, w, node)
			if nextID == "" {
				run.SetCompleted()
				return engine.saveWith(runRepo, run)
			}
		case node.Type == workflow.NodeTypeWaitDuration || node.Type == workflow.NodeTypeWaitSchedule:
			var err error
			nextID, err = timerElapsedEdge(run, w, node)
			if err != nil {
				run.SetError(err.Error())
				if updateErr := engine.saveWith(runRepo, run); updateErr != nil {
					return errors.Join(err, updateErr)
				}
				return err
			}
		}
		if nextID != "" {
			run.State.Set("_prev_node_id", run.CurrentNodeID)
			run.CurrentNodeID = nextID
			run.UpdatedAt = time.Now().UTC()
			if err := runRepo.Update(run); err != nil {
				return err
			}
		}
	}

	return engine.Execute(run, w)
}

var errRunWorkflowGone = errors.New("the workflow or the version this run started on no longer exists")

func loadRunWorkflow(workflows workflow.PublishedReader, runs workflow.WorkflowRunRepository, engine *RunEngine, run *workflow.WorkflowRun) (*workflow.Workflow, error) {
	w, err := workflow.LoadForRun(workflows, run)
	if err != nil && !errors.Is(err, workflow.ErrVersionNotFound) {
		slog.Warn("workflow.run.workflow_unreadable", append(runLogAttrs(run, run.CurrentNodeID), slog.String("error", err.Error()))...)
		return nil, err
	}
	if w != nil {
		return w, nil
	}
	cause := errRunWorkflowGone
	if err != nil {
		cause = fmt.Errorf("%w: %w", errRunWorkflowGone, err)
	}
	slog.Warn("workflow.run.workflow_gone", append(runLogAttrs(run, run.CurrentNodeID), slog.String("error", cause.Error()))...)
	engine.recordGuard(run, cause)
	run.SetError(cause.Error())
	return nil, engine.saveWith(runs, run)
}

func retryAfterUnreadableGate(runRepo workflow.WorkflowRunRepository, engine *RunEngine, run *workflow.WorkflowRun, cause error) error {
	attrs := append(runLogAttrs(run, run.CurrentNodeID), slog.String("error", cause.Error()))
	attempt := run.State.GetInt(workflow.StateKeyGateAttempts)
	if attempt < workflow.MaxRetriesPerNode {
		attempt++
		run.State.Set(workflow.StateKeyGateAttempts, attempt)
		delay := waitForRetry(run, attempt, run.WaitReason)
		slog.Warn("workflow.gate.unreadable_retry", append(attrs, slog.Int("attempt", attempt), slog.Duration("delay", delay))...)
		engine.recordGate(run, workflow.LogStatusGate, attempt, cause)
		return runRepo.Update(run)
	}
	slog.Error("workflow.gate.unreadable_failed", attrs...)
	run.State.Delete(workflow.StateKeyGateAttempts)
	engine.recordGate(run, workflow.LogStatusFailed, attempt+1, cause)
	run.SetError(cause.Error())
	return engine.saveWith(runRepo, run)
}

var errAutomationOff = errors.New("automation is off for this conversation, the run was cancelled before this node")

func cancelWhenAutomationOff(runRepo workflow.WorkflowRunRepository, engine *RunEngine, run *workflow.WorkflowRun) (bool, error) {
	if run.OnLead() {
		return false, nil
	}
	off, err := engine.automationOff(run.EntryID, run.EntryType)
	if err != nil {
		return true, err
	}
	run.State.Delete(workflow.StateKeyGateAttempts)
	if !off {
		return false, nil
	}
	slog.Info("workflow.gate.automation_off_cancel", append(runLogAttrs(run, run.CurrentNodeID), slog.String("entry_id", run.EntryID))...)
	engine.recordGate(run, workflow.LogStatusGate, 1, errAutomationOff)
	run.SetCancelled()
	return true, engine.saveWith(runRepo, run)
}

type FormRunResumer struct {
	workflows workflow.WorkflowRepository
	runs      workflow.WorkflowRunRepository
	engine    *RunEngine
}

func NewFormRunResumer(workflows workflow.WorkflowRepository, runs workflow.WorkflowRunRepository, engine *RunEngine) *FormRunResumer {
	return &FormRunResumer{workflows: workflows, runs: runs, engine: engine}
}

func (r *FormRunResumer) ResumeForm(_ context.Context, runID string, data map[string]any) error {
	run, err := r.waitingOnForm(runID)
	if err != nil || run == nil {
		return err
	}
	if !r.engine.TryLockRun(run.ID) {
		return fmt.Errorf("%w: run %s", ErrRunBusy, run.ID)
	}
	defer r.engine.UnlockRun(run.ID)

	run, err = r.waitingOnForm(runID)
	if err != nil || run == nil {
		return err
	}
	w, err := loadRunWorkflow(r.workflows, r.runs, r.engine, run)
	if w == nil {
		return err
	}
	node := w.Graph.FindNode(run.CurrentNodeID)
	if node == nil || !node.Type.IsFormPrompt() {
		slog.Info("workflow.form.not_parked_on_form", runLogAttrs(run, run.CurrentNodeID)...)
		return nil
	}
	if !answersNodeForm(node, data) {
		slog.Info("workflow.form.answer_for_another_form", append(runLogAttrs(run, run.CurrentNodeID),
			slog.Any("expected_form_id", node.Config[workflow.FormNodeFormIDKey]), slog.Any("answered_form_id", data[workflow.DataKeyFormID]))...)
		return nil
	}
	if cancelled, err := cancelWhenAutomationOff(r.runs, r.engine, run); cancelled || err != nil {
		return err
	}
	if err := AdvanceOnForm(run, w, data); err != nil {
		if !errors.Is(err, ErrMissingCompletedEdge) {
			return err
		}
		slog.Warn("workflow.form.cannot_continue", append(runLogAttrs(run, run.CurrentNodeID), slog.String("error", err.Error()))...)
		r.engine.recordGuard(run, err)
		run.SetError(err.Error())
		return r.engine.saveWith(r.runs, run)
	}
	if err := r.runs.Update(run); err != nil {
		return err
	}
	return r.engine.Execute(run, w)
}

func (r *FormRunResumer) waitingOnForm(runID string) (*workflow.WorkflowRun, error) {
	run, err := r.runs.FindByID(runID)
	if err != nil {
		return nil, err
	}
	if run == nil {
		slog.Info("workflow.form.run_gone", slog.String("run_id", runID))
		return nil, nil
	}
	if run.Status != workflow.RunStatusWaiting || run.WaitReason != workflow.WaitReasonForm {
		slog.Info("workflow.form.run_not_waiting_on_form", append(runLogAttrs(run, run.CurrentNodeID),
			slog.String("status", string(run.Status)), slog.String("wait_reason", string(run.WaitReason)))...)
		return nil, nil
	}
	return run, nil
}

func answersNodeForm(node *workflow.Node, data map[string]any) bool {
	expected, _ := node.Config[workflow.FormNodeFormIDKey].(string)
	got, _ := data[workflow.DataKeyFormID].(string)
	return strings.TrimSpace(expected) != "" && strings.TrimSpace(expected) == strings.TrimSpace(got)
}
