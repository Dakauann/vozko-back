package stage_usecase

import (
	"github.com/google/uuid"

	"vozko/domain/stage"
)

type AssignEntryStageUseCase struct {
	repo stage.Repository
}

func NewAssignEntryStageUseCase(repo stage.Repository) stage.AssignEntryStageUseCase {
	return &AssignEntryStageUseCase{repo: repo}
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

	et := &stage.EntryStage{
		ID:          uuid.New().String(),
		StageID:       input.StageID,
		EntryID:     input.EntryID,
		EntryType:   input.EntryType,
		WorkspaceID: workspaceID,
	}

	if err := uc.repo.AssignStage(et); err != nil {
		return nil, err
	}

	fetched, err := uc.repo.GetEntryStage(input.EntryID, input.EntryType, workspaceID)
	if err != nil {
		return nil, err
	}
	if fetched != nil {
		return fetched, nil
	}

	return et, nil
}
