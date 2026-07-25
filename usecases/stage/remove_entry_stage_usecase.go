package stage_usecase

import "vozko/domain/stage"

type RemoveEntryStageUseCase struct {
	repo stage.Repository
}

func NewRemoveEntryStageUseCase(repo stage.Repository) stage.RemoveEntryStageUseCase {
	return &RemoveEntryStageUseCase{repo: repo}
}

func (uc *RemoveEntryStageUseCase) Execute(workspaceID, StageID, entryID, entryType string) error {
	if err := stage.ValidateEntryType(entryType); err != nil {
		return err
	}

	t, err := uc.repo.FindByID(StageID)
	if err != nil {
		return err
	}
	if t.WorkspaceID != workspaceID {
		return stage.ErrUnauthorized
	}

	return uc.repo.RemoveStage(StageID, entryID, entryType, workspaceID)
}
