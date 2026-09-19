package workflow_usecase

import (
	"errors"
	"fmt"
	"log"
	"time"

	"vozko/domain/workflow"
)

func resumeRunFromCurrent(
	workflowRepo workflow.WorkflowRepository,
	runRepo workflow.WorkflowRunRepository,
	engine *RunEngine,
	run *workflow.WorkflowRun,
) error {
	// Automation is re-checked HERE, on resume, not only where the run started.
	//
	// A parked run outlives the decision that created it. The inbound path
	// refuses to start one while automation is off, but a run already asleep on
	// a timer or waiting for a reply had nothing re-ask on the way back, so it
	// woke up and messaged the contact anyway. Live case: automation switched
	// off at 11:13 and the run still sent at 15:21, cutting across an attendant
	// who was talking to the patient.
	//
	// This function is the single choke point for BOTH resume paths — the wake
	// consumer and the manager — which is why the check belongs here rather
	// than in either caller.
	//
	// Cancelled, not errored: the operator asked for silence and got it. An
	// error would retry, alert, and eventually deliver the very message the
	// operator was trying to stop.
	if engine.automationOff(run.EntryID, run.EntryType) {
		log.Printf("[workflow][resume] automation disabled for entry=%s, cancelling run=%s at node=%s",
			run.EntryID, run.ID, run.CurrentNodeID)
		run.SetCancelled()
		return runRepo.Update(run)
	}

	w, err := workflowRepo.FindByID(run.WorkflowID)
	if err != nil || w == nil {
		run.SetError("workflow not found during resume")
		_ = runRepo.Update(run)
		if err != nil {
			return err
		}
		return fmt.Errorf("workflow not found during resume")
	}

	run.SetRunning()
	if err := runRepo.Update(run); err != nil {
		return err
	}

	node := w.Graph.FindNode(run.CurrentNodeID)
	if node == nil {
		run.SetError("current node not found during resume")
		_ = runRepo.Update(run)
		return fmt.Errorf("current node not found during resume")
	}

	if node.Type.ParksForReply() {
		edges := w.Graph.OutgoingEdges(run.CurrentNodeID)
		nextID := ""
		switch {
		case node.Type == workflow.NodeTypeWaitForReply:
			run.State.Set("_wait_outcome", "timeout")
			nextID = findExactEdgeByLabel(edges, "timeout")
			if nextID == "" {
				run.SetCompleted()
				return runRepo.Update(run)
			}
		case node.Type.IsInteractivePrompt():
			// The contact never chose an option before the timeout.
			run.State.Set("_wait_outcome", "no_reply")
			nextID = findExactEdgeByLabel(edges, "no_reply")
			if nextID == "" {
				run.SetCompleted()
				return runRepo.Update(run)
			}
		case node.Type == workflow.NodeTypeWaitDuration || node.Type == workflow.NodeTypeWaitSchedule:
			run.State.Set("_wait_outcome", "completed")
			nextID = resolveRequiredWaitEdge(edges, "completed")
			if nextID == "" {
				err := fmt.Errorf("%w: node %q output %q", workflow.ErrNodeMissingRequiredOutput, node.ID, "completed")
				run.SetError(err.Error())
				if updateErr := runRepo.Update(run); updateErr != nil {
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
