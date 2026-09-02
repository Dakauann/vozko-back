package stage_usecase

import (
	"log"

	"github.com/google/uuid"

	ce "vozko/domain/conversation_event"
	"vozko/domain/shared"
	"vozko/domain/stage"
)

// enforcePipelineCoherence switches the funnel-coherence rule from reporting to
// rejecting.
//
// It is ON. The ordering it waited for is done: every stage list now asks for a
// funnel (ListStagesUseCase takes a pipelineID, the CRM passes the selected one,
// and the conversation dropdown reads the lead's own), so the only clicks this
// rejects are ones that were genuinely about to strand a lead. Flipping it before
// that would have failed legitimate clicks on lists that were showing the wrong
// funnel's stages, which is why it shipped off first.
const enforcePipelineCoherence = true

type AssignEntryStageUseCase struct {
	repo   stage.Repository
	events ce.Logger
}

// NewAssignEntryStageUseCase wires the stage move.
//
// events may be nil (unit tests): a missing timeline entry must never fail a
// move. It belongs HERE and not in the HTTP handler, where it used to live:
// three callers reach this use case — the stage endpoint, the CRM bulk action
// and the AI's manage_entry_stage tool — and only the first one logged, so a
// bulk move or an AI move changed the board and left the conversation's history
// blank.
func NewAssignEntryStageUseCase(repo stage.Repository, events ce.Logger) stage.AssignEntryStageUseCase {
	return &AssignEntryStageUseCase{repo: repo, events: events}
}

func (uc *AssignEntryStageUseCase) Execute(workspaceID string, input stage.AssignEntryStageInput) (*stage.EntryStage, error) {
	if err := stage.ValidateEntryType(input.EntryType); err != nil {
		return nil, err
	}

	t, err := uc.repo.FindByID(input.StageID)
	if err != nil {
		return nil, err
	}
	if t.WorkspaceID != workspaceID {
		return nil, stage.ErrUnauthorized
	}

	if err := uc.checkPipelineCoherence(workspaceID, input, t); err != nil {
		return nil, err
	}

	// Read the outgoing stage before the upsert replaces it, so the timeline can
	// say what the move was FROM and not just where it landed.
	from, _ := uc.repo.GetEntryStage(input.EntryID, input.EntryType, workspaceID)

	et := &stage.EntryStage{
		ID:          uuid.New().String(),
		StageID:     input.StageID,
		EntryID:     input.EntryID,
		EntryType:   input.EntryType,
		WorkspaceID: workspaceID,
	}

	if err := uc.repo.AssignStage(et); err != nil {
		return nil, err
	}

	uc.logMove(workspaceID, input, from, t)

	fetched, err := uc.repo.GetEntryStage(input.EntryID, input.EntryType, workspaceID)
	if err != nil {
		return nil, err
	}
	if fetched != nil {
		return fetched, nil
	}

	return et, nil
}

// logMove records the move on the conversation's activity timeline.
//
// It carries the stage NAMES, not just the ids: the timeline renders whatever
// the event stored, and stage_id/to_stage_id alone rendered as a bare "Stage
// changed" with nothing saying which stage. The names are already in hand here
// (the target was loaded for the workspace check, the previous one for
// coherence), so naming the move costs no extra query.
//
// tag_added is emitted alongside for backward compatibility: stages were once
// called tags and consumers still read that event. Both are best-effort — a
// conversation must not lose a stage move because telemetry is unreachable.
func (uc *AssignEntryStageUseCase) logMove(
	workspaceID string,
	input stage.AssignEntryStageInput,
	from *stage.EntryStage,
	to *stage.Stage,
) {
	if uc.events == nil {
		return
	}

	details := map[string]string{
		"stage_id":    input.StageID,
		"to_stage_id": input.StageID,
		"stage_name":  to.Name,
	}
	if from != nil && from.StageID != "" && from.StageID != input.StageID {
		details["from_stage_id"] = from.StageID
		if prev, err := uc.repo.FindByID(from.StageID); err == nil && prev != nil {
			details["from_stage_name"] = prev.Name
		}
	}

	channel := shared.EntryType(input.EntryType).EventChannel()
	uc.events.Log(ce.New(workspaceID, input.EntryID, input.EntryType, ce.EventStageChanged).
		WithActor(input.ActorID).
		WithChannel(channel).
		WithDetails(details).
		Build())
	uc.events.Log(ce.New(workspaceID, input.EntryID, input.EntryType, ce.EventTagAdded).
		WithActor(input.ActorID).
		WithChannel(channel).
		WithDetails(map[string]string{"stage_id": input.StageID, "stage_name": to.Name}).
		Build())
}

// checkPipelineCoherence holds the rule that keeps a lead on one funnel:
//
//	A lead is on exactly one funnel — the funnel of the stage it currently holds.
//	A stage may only be assigned to a lead already on that stage's funnel.
//
// A lead with no stage yet is unconstrained: first placement is what puts it on a
// funnel, so there is nothing to contradict. Changing funnel is meant to be an
// explicit move that sets stage and funnel together, not a side effect of picking
// from a list that was showing the wrong funnel's stages.
//
// Violating it strands the lead: the board renders one column per stage of ITS
// funnel and matches entries by stage id, so a lead holding a stage from another
// funnel lands in no column at all — it vanishes from its own board rather than
// showing up somewhere wrong, which is the harder failure to notice.
//
// The check is derived, never stored. stages.pipeline_id already answers "which
// funnel is this stage on", so the lead's funnel is a lookup away; copying a
// pipeline_id onto entry_stages would add a second source of truth to keep in
// sync, which is the bug class this exists to close.
func (uc *AssignEntryStageUseCase) checkPipelineCoherence(
	workspaceID string,
	input stage.AssignEntryStageInput,
	target *stage.Stage,
) error {
	current, err := uc.repo.GetEntryStage(input.EntryID, input.EntryType, workspaceID)
	if err != nil || current == nil || current.StageID == "" {
		// No current stage (or the lookup failed): nothing to contradict. A failed
		// lookup must not block a legitimate move — the rule is an integrity check,
		// not an authorization gate, and those are already applied above.
		return nil
	}
	if current.StageID == input.StageID {
		return nil
	}

	currentStage, err := uc.repo.FindByID(current.StageID)
	if err != nil || currentStage == nil {
		return nil
	}
	// A stage predating the pipeline migration may carry no pipeline; it cannot
	// contradict anything, and treating "" as a distinct funnel would flag every
	// legacy row.
	if currentStage.PipelineID == "" || target.PipelineID == "" {
		return nil
	}
	if currentStage.PipelineID == target.PipelineID {
		return nil
	}

	if enforcePipelineCoherence {
		return stage.ErrStagePipelineMismatch
	}
	log.Printf(
		"[stage-coherence] entry %s (%s) would move across funnels: stage %q pipeline %s -> stage %q pipeline %s (workspace %s)",
		input.EntryID, input.EntryType,
		currentStage.Name, currentStage.PipelineID,
		target.Name, target.PipelineID,
		workspaceID,
	)
	return nil
}
