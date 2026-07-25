package label_usecase

import "vozko/domain/label"

type getEntryLabelsUseCase struct {
	repo label.Repository
}

func NewGetEntryLabelsUseCase(repo label.Repository) label.GetEntryLabelsUseCase {
	return &getEntryLabelsUseCase{repo: repo}
}

func (uc *getEntryLabelsUseCase) Execute(workspaceID, entryID, entryType string) ([]*label.EntryLabel, error) {
	if err := label.ValidateEntryType(entryType); err != nil {
		return nil, err
	}
	return uc.repo.GetEntryLabels(entryID, entryType, workspaceID)
}
