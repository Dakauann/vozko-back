package label_usecase

import (
	"vozko/domain/label"
)

type reorderLabelsUseCase struct {
	repo label.Repository
}

func NewReorderLabelsUseCase(repo label.Repository) label.ReorderLabelsUseCase {
	return &reorderLabelsUseCase{repo: repo}
}

func (uc *reorderLabelsUseCase) Execute(workspaceID string, input label.ReorderLabelsInput) ([]*label.Label, error) {
	if len(input.LabelIDs) == 0 {
		return uc.repo.ListByWorkspace(workspaceID)
	}

	if err := uc.repo.ReorderLabels(workspaceID, input.LabelIDs); err != nil {
		return nil, err
	}

	return uc.repo.ListByWorkspace(workspaceID)
}
