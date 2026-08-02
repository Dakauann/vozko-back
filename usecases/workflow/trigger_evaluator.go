package workflow_usecase

import (
	"errors"
	"log"
	"time"

	"vozko/domain/cache"
	"vozko/domain/workflow"
)

type triggerEvaluator struct {
	workflowRepo workflow.WorkflowRepository
	runRepo      workflow.WorkflowRunRepository
	engine       *RunEngine
	sharedState  cache.SharedState
}

func NewTriggerEvaluator(
	workflowRepo workflow.WorkflowRepository,
	runRepo workflow.WorkflowRunRepository,
	engine *RunEngine,
	sharedState cache.SharedState,
) workflow.TriggerEvaluator {
	return &triggerEvaluator{
		workflowRepo: workflowRepo,
		runRepo:      runRepo,
		engine:       engine,
		sharedState:  sharedState,
	}
}

func (te *triggerEvaluator) Evaluate(event workflow.TriggerEvent) {
	log.Printf("[workflow] trigger: evaluating event type=%s workspace=%s entry=%s", event.TriggerType, event.WorkspaceID, event.EntryID)

	// An incoming message must resume ANY run parked at wait_for_reply for this
	// entry — regardless of which trigger started that workflow. Resuming only via
	// trigger-matched workflows (the loop below) silently fails for workflows
	// started by trigger_first_message, leaving them stuck until their timeout.
	// This is the same resume the simulator does; here we route the real message.
	handledReplyRun := ""
	if event.TriggerType == workflow.TriggerMessageReceived && event.EntryID != "" {
		if run, lerr := te.runRepo.FindWaitingReplyByEntry(event.EntryID); lerr != nil {
			log.Printf("[workflow] trigger: failed to look up waiting-reply run for entry=%s: %v", event.EntryID, lerr)
		} else if run != nil {
			if w, werr := te.workflowRepo.FindByID(run.WorkflowID); werr == nil && w != nil {
				handledReplyRun = run.ID
				te.wakeRunForReply(run, w, event)
			}
		}
	}

	workflows, err := te.workflowRepo.FindActiveByTrigger(event.WorkspaceID, event.TriggerType)
	if err != nil {
		log.Printf("[workflow] trigger: failed to find workflows for %s: %v", event.TriggerType, err)
		return
	}

	log.Printf("[workflow] trigger: found %d active workflows for trigger=%s", len(workflows), event.TriggerType)

	for _, w := range workflows {

		if !te.matchesTriggerConfig(w, event) {
			continue
		}

		trigger := w.Graph.TriggerNodeByType(event.TriggerType)
		if trigger == nil {
			continue
		}

		existing, err := te.runRepo.FindActiveByEntryAndTrigger(w.ID, event.EntryID, trigger.ID)
		if err != nil {
			log.Printf("[workflow] trigger: failed to check existing run: %v", err)
			continue
		}
		if existing != nil {

			if existing.ID == handledReplyRun {
				// Already resumed above by the entry-based reply path.
				continue
			}
			if existing.Status == workflow.RunStatusWaiting && existing.WaitReason == workflow.WaitReasonReply {
				te.wakeRunForReply(existing, w, event)
			} else if existing.Status == workflow.RunStatusWaiting && existing.WaitReason == workflow.WaitReasonDuration {

				te.wakeRunForMessage(existing, w, event)
			} else {
				log.Printf("[workflow] trigger: skipping workflow=%s — existing run=%s in status=%s", w.ID, existing.ID, existing.Status)
			}
			continue
		}

		if !tryAcquireWorkspaceSlot(te.sharedState, event.WorkspaceID) {
			log.Printf("[workflow] trigger: workspace %s at concurrency limit (%d), skipping workflow %s", event.WorkspaceID, maxConcurrentRunsPerWorkspace, w.ID)
			continue
		}

		run := newTriggeredRun(w, trigger, event)
		if err := te.runRepo.Create(run); err != nil {
			log.Printf("[workflow] trigger: failed to create run: %v", err)
			releaseWorkspaceSlot(te.sharedState, event.WorkspaceID)
			continue
		}

		log.Printf("[workflow] trigger: created run=%s for workflow=%s entry=%s — starting engine", run.ID, w.ID, event.EntryID)

		executeLocked(te.engine, run, w)
		releaseWorkspaceSlot(te.sharedState, event.WorkspaceID)
	}
}

func (te *triggerEvaluator) wakeRunForReply(run *workflow.WorkflowRun, w *workflow.Workflow, event workflow.TriggerEvent) {
	if !te.engine.TryLockRun(run.ID) {
		log.Printf("[workflow] trigger: skipping wake for run=%s — already locked", run.ID)
		return
	}
	defer te.engine.UnlockRun(run.ID)

	log.Printf("[workflow] trigger: waking run=%s for reply (entry=%s)", run.ID, event.EntryID)

	// Shared reply-resume logic (same code path the simulator uses).
	if err := AdvanceOnReply(run, w, event.Data); err != nil {
		if errors.Is(err, ErrInteractiveReplyUnhandled) {
			// Stray reply to an interactive prompt (not one of its options and no
			// no_match/default branch). Leave the run parked so a later valid
			// selection or the timeout can resume it; do not error or advance.
			log.Printf("[workflow] trigger: run=%s ignored unhandled interactive reply (entry=%s) — left parked", run.ID, event.EntryID)
			return
		}
		if errors.Is(err, ErrMissingRepliedEdge) {
			run.SetError(err.Error())
			if uerr := te.runRepo.Update(run); uerr != nil {
				log.Printf("[workflow] trigger: failed to mark run %s as error: %v", run.ID, uerr)
			}
		}
		return
	}

	if err := te.runRepo.Update(run); err != nil {
		log.Printf("[workflow] trigger: failed to wake run %s: %v", run.ID, err)
		return
	}

	if err := te.engine.Execute(run, w); err != nil {
		log.Printf("[workflow] trigger: engine error for woken run %s: %v", run.ID, err)
	}
}

func (te *triggerEvaluator) wakeRunForMessage(run *workflow.WorkflowRun, w *workflow.Workflow, event workflow.TriggerEvent) {
	if !te.engine.TryLockRun(run.ID) {
		log.Printf("[workflow] trigger: skipping message wake for run=%s — already locked", run.ID)
		return
	}
	defer te.engine.UnlockRun(run.ID)

	log.Printf("[workflow] trigger: cancelling duration wait run=%s — user sent message (entry=%s)", run.ID, event.EntryID)

	node := w.Graph.FindNode(run.CurrentNodeID)
	if node == nil || (node.Type != workflow.NodeTypeWaitDuration && node.Type != workflow.NodeTypeWaitSchedule) {
		return
	}
	edges := w.Graph.OutgoingEdges(run.CurrentNodeID)
	nextID := findExactEdgeByLabel(edges, "message_received")
	if nextID == "" {
		log.Printf("[workflow] trigger: ignoring message wake for run=%s — wait node=%s has no message_received edge", run.ID, run.CurrentNodeID)
		return
	}

	run.State.Set("_wait_outcome", "message_received")
	for k, v := range event.Data {
		run.State.Set(k, v)
	}
	run.SetRunning()
	run.State.Set("_prev_node_id", run.CurrentNodeID)
	run.CurrentNodeID = nextID
	run.UpdatedAt = time.Now().UTC()

	if err := te.runRepo.Update(run); err != nil {
		log.Printf("[workflow] trigger: failed to wake run %s: %v", run.ID, err)
		return
	}

	if err := te.engine.Execute(run, w); err != nil {
		log.Printf("[workflow] trigger: engine error for woken run %s: %v", run.ID, err)
	}
}

func (te *triggerEvaluator) matchesTriggerConfig(w *workflow.Workflow, event workflow.TriggerEvent) bool {

	if campaignWfID, ok := event.Data["campaign_workflow_id"].(string); ok && campaignWfID != "" {
		if w.ID != campaignWfID {
			log.Printf("[workflow] trigger: skipping workflow=%s (campaign linked to workflow=%s)", w.ID, campaignWfID)
			return false
		}
	}

	// The channel account's own workflow link, set by Instagram and Telegram.
	//
	// Without this the link is decorative: every active workflow in the
	// workspace whose trigger matches runs on every conversation, so connecting
	// a second bot — or simply having a second workflow — makes both fire on the
	// same contact and the customer receives two greetings. Selecting a workflow
	// on the account has to mean only that workflow attends it.
	if accountWfID, ok := event.Data["account_workflow_id"].(string); ok && accountWfID != "" {
		if w.ID != accountWfID {
			log.Printf("[workflow] trigger: skipping workflow=%s (channel account linked to workflow=%s)", w.ID, accountWfID)
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
		StageID, _ := cfg["stage_id"].(string)
		if StageID == "" {
			return true
		}
		eventTagID, _ := event.Data["stage_id"].(string)
		return StageID == eventTagID

	case workflow.TriggerCampaignSent:
		campaignID, _ := cfg["campaign_id"].(string)
		if campaignID == "" {
			return true
		}
		eventCampaignID, _ := event.Data["campaign_id"].(string)
		return campaignID == eventCampaignID

	case workflow.TriggerNoReply:

		return true

	default:
		return true
	}
}
