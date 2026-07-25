package label_usecase

import (
	"github.com/google/uuid"

	"vozko/domain/label"
)

type assignEntryLabelUseCase struct {
	repo label.Repository
}

func NewAssignEntryLabelUseCase(repo label.Repository) label.AssignEntryLabelUseCase {
	return &assignEntryLabelUseCase{repo: repo}
}

func (uc *assignEntryLabelUseCase) Execute(workspaceID string, input label.AssignEntryLabelInput) (*label.EntryLabel, error) {
	if err := label.ValidateEntryType(input.EntryType); err != nil {
		return nil, err
	}

	l, err := uc.repo.FindByID(input.LabelID)
	if err != nil {
		return nil, err
	}
	if l.WorkspaceID != workspaceID {
		return nil, label.ErrUnauthorized
	}

	el := &label.EntryLabel{
		ID:        uuid.New().String(),
		LabelID:   input.LabelID,
		EntryID:   input.EntryID,
		EntryType: input.EntryType,
		WorkspaceID:    workspaceID,
	}

	if err := uc.repo.AssignLabel(el); err != nil {
		return nil, err
	}

	labels, err := uc.repo.GetEntryLabels(input.EntryID, input.EntryType, workspaceID)
	if err != nil {
		return nil, err
	}
	for _, fetched := range labels {
		if fetched.LabelID == input.LabelID {
			return fetched, nil
		}
	}

	return el, nil
}
