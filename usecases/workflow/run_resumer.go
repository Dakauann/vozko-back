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
