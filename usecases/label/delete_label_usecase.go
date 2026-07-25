package label_usecase

import "vozko/domain/label"

type deleteLabelUseCase struct {
	repo label.Repository
}

func NewDeleteLabelUseCase(repo label.Repository) label.DeleteLabelUseCase {
	return &deleteLabelUseCase{repo: repo}
}

func (uc *deleteLabelUseCase) Execute(workspaceID, labelID string) error {
	existing, err := uc.repo.FindByID(labelID)
	if err != nil {
		return err
	}
	if existing.WorkspaceID != workspaceID {
		return label.ErrUnauthorized
	}

	return uc.repo.Delete(labelID)
}
