package stage_usecase

import "vozko/domain/stage"

type GetEntryStageUseCase struct {
	repo stage.Repository
}

func NewGetEntryStageUseCase(repo stage.Repository) stage.GetEntryStageUseCase {
	return &GetEntryStageUseCase{repo: repo}
}

func (uc *GetEntryStageUseCase) Execute(workspaceID, entryID, entryType string) (*stage.EntryStage, error) {
	if err := stage.ValidateEntryType(entryType); err != nil {
		return nil, err
	}
	return uc.repo.GetEntryStage(entryID, entryType, workspaceID)
}
