package stage_usecase

import "vozko/domain/stage"

type DeleteStageUseCase struct {
	repo stage.Repository
}

func NewDeleteStageUseCase(repo stage.Repository) stage.DeleteStageUseCase {
	return &DeleteStageUseCase{repo: repo}
}

func (uc *DeleteStageUseCase) Execute(workspaceID, StageID string) error {
	existing, err := uc.repo.FindByID(StageID)
	if err != nil {
		return err
	}
	if existing.WorkspaceID != workspaceID {
		return stage.ErrUnauthorized
	}
	if existing.IsDefault {
		return stage.ErrTagDefaultDelete
	}

	return uc.repo.Delete(StageID)
}
