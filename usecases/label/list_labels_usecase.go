package label_usecase

import "vozko/domain/label"

type listLabelsUseCase struct {
	repo label.Repository
}

func NewListLabelsUseCase(repo label.Repository) label.ListLabelsUseCase {
	return &listLabelsUseCase{repo: repo}
}

func (uc *listLabelsUseCase) Execute(workspaceID string) ([]*label.Label, error) {
	return uc.repo.ListByWorkspace(workspaceID)
}
