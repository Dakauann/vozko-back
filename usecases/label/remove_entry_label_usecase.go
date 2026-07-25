package label_usecase

import "vozko/domain/label"

type removeEntryLabelUseCase struct {
	repo label.Repository
}

func NewRemoveEntryLabelUseCase(repo label.Repository) label.RemoveEntryLabelUseCase {
	return &removeEntryLabelUseCase{repo: repo}
}

func (uc *removeEntryLabelUseCase) Execute(workspaceID, labelID, entryID, entryType string) error {
	if err := label.ValidateEntryType(entryType); err != nil {
		return err
	}

	l, err := uc.repo.FindByID(labelID)
	if err != nil {
		return err
	}
	if l.WorkspaceID != workspaceID {
		return label.ErrUnauthorized
	}

	return uc.repo.RemoveLabel(labelID, entryID, entryType, workspaceID)
}
