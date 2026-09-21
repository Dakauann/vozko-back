package stage_usecase

import (
	ce "vozko/domain/conversation_event"
	"vozko/domain/shared"
	"vozko/domain/stage"
)

type RemoveEntryStageUseCase struct {
	repo   stage.Repository
	events ce.Logger
}

func NewRemoveEntryStageUseCase(repo stage.Repository, events ce.Logger) stage.RemoveEntryStageUseCase {
	return &RemoveEntryStageUseCase{repo: repo, events: events}
}

func (uc *RemoveEntryStageUseCase) Execute(workspaceID string, input stage.RemoveEntryStageInput) error {
	if err := stage.ValidateEntryType(input.EntryType); err != nil {
		return err
	}

	t, err := uc.repo.FindByID(input.StageID)
	if err != nil {
		return err
	}
	if t.WorkspaceID != workspaceID {
		return stage.ErrUnauthorized
	}

	if err := uc.repo.RemoveStage(input.StageID, input.EntryID, input.EntryType, workspaceID); err != nil {
		return err
	}

	if uc.events != nil {
		uc.events.Log(ce.New(workspaceID, input.EntryID, input.EntryType, ce.EventTagRemoved).
			WithActor(input.ActorID).
			WithChannel(shared.EntryType(input.EntryType).EventChannel()).
			WithDetails(map[string]string{"stage_id": input.StageID, "stage_name": t.Name}).
			Build())
	}
	return nil
}
