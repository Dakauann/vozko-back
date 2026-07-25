package label_usecase

import (
	"strings"

	"github.com/google/uuid"

	"vozko/domain/label"
)

type createLabelUseCase struct {
	repo label.Repository
}

func NewCreateLabelUseCase(repo label.Repository) label.CreateLabelUseCase {
	return &createLabelUseCase{repo: repo}
}

func (uc *createLabelUseCase) Execute(workspaceID string, input label.CreateLabelInput) (*label.Label, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, label.ErrLabelNameRequired
	}

	exists, err := uc.repo.NameExistsInWorkspace(workspaceID, name, nil)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, label.ErrLabelNameExists
	}

	existing, err := uc.repo.ListByWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	maxPosition := 0
	for _, l := range existing {
		if l.Position > maxPosition {
			maxPosition = l.Position
		}
	}

	l := &label.Label{
		ID:          uuid.New().String(),
		WorkspaceID: workspaceID,
		Name:        strings.ToLower(name),
		Color:       strings.TrimSpace(input.Color),
		Position:    maxPosition + 1,
	}

	if err := uc.repo.Create(l); err != nil {
		return nil, err
	}

	return uc.repo.FindByID(l.ID)
}
