package label_usecase

import (
	"strings"

	"vozko/domain/label"
)

type updateLabelUseCase struct {
	repo label.Repository
}

func NewUpdateLabelUseCase(repo label.Repository) label.UpdateLabelUseCase {
	return &updateLabelUseCase{repo: repo}
}

func (uc *updateLabelUseCase) Execute(workspaceID, labelID string, input label.UpdateLabelInput) (*label.Label, error) {
	existing, err := uc.repo.FindByID(labelID)
	if err != nil {
		return nil, err
	}
	if existing.WorkspaceID != workspaceID {
		return nil, label.ErrUnauthorized
	}

	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, label.ErrLabelNameRequired
		}
		nameExists, err := uc.repo.NameExistsInWorkspace(workspaceID, name, &labelID)
		if err != nil {
			return nil, err
		}
		if nameExists {
			return nil, label.ErrLabelNameExists
		}
		existing.Name = strings.ToLower(name)
	}

	if input.Color != nil {
		existing.Color = strings.TrimSpace(*input.Color)
	}

	if err := uc.repo.Update(existing); err != nil {
		return nil, err
	}

	return uc.repo.FindByID(labelID)
}
