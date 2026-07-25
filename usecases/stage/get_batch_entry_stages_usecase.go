package stage_usecase

import "vozko/domain/stage"

type GetBatchEntryStagesUseCase struct {
	repo stage.Repository
}

func NewGetBatchEntryStagesUseCase(repo stage.Repository) stage.GetBatchEntryStagesUseCase {
	return &GetBatchEntryStagesUseCase{repo: repo}
}

func (uc *GetBatchEntryStagesUseCase) Execute(workspaceID string, entryIDs []string, entryType string) (map[string]*stage.EntryStage, error) {
	if err := stage.ValidateEntryType(entryType); err != nil {
		return nil, err
	}
	if len(entryIDs) == 0 {
		return make(map[string]*stage.EntryStage), nil
	}
	return uc.repo.GetBatchEntryStages(entryIDs, entryType, workspaceID)
}
