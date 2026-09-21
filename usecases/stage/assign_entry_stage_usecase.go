package stage_usecase

import (
	"log"

	"github.com/google/uuid"

	ce "vozko/domain/conversation_event"
	"vozko/domain/shared"
	"vozko/domain/stage"
)

const enforcePipelineCoherence = true

type AssignEntryStageUseCase struct {
	repo   stage.Repository
	events ce.Logger
}

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

func (uc *AssignEntryStageUseCase) checkPipelineCoherence(
	workspaceID string,
	input stage.AssignEntryStageInput,
	target *stage.Stage,
) error {
	current, err := uc.repo.GetEntryStage(input.EntryID, input.EntryType, workspaceID)
	if err != nil || current == nil || current.StageID == "" {
		return nil
	}
	if current.StageID == input.StageID {
		return nil
	}

	currentStage, err := uc.repo.FindByID(current.StageID)
	if err != nil || currentStage == nil {
		return nil
	}
	if currentStage.PipelineID == "" || target.PipelineID == "" {
		return nil
	}
	if currentStage.PipelineID == target.PipelineID {
		return nil
	}

	if input.AllowCrossPipeline {
		log.Printf(
			"[stage-coherence] entry %s (%s) moved across funnels on purpose: stage %q pipeline %s -> stage %q pipeline %s (workspace %s)",
			input.EntryID, input.EntryType,
			currentStage.Name, currentStage.PipelineID,
			target.Name, target.PipelineID,
			workspaceID,
		)
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
