package stage_usecase

import (
	"strings"

	"github.com/google/uuid"

	"vozko/domain/stage"
)

type CreateStageUseCase struct {
	repo stage.Repository
}

func NewCreateStageUseCase(repo stage.Repository) stage.CreateStageUseCase {
	return &CreateStageUseCase{repo: repo}
}

func (uc *CreateStageUseCase) Execute(workspaceID string, input stage.CreateStageInput) (*stage.Stage, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, stage.ErrTagNameRequired
	}

	description := strings.TrimSpace(input.Description)
	if description == "" {
		return nil, stage.ErrTagDescRequired
	}

	// The board is workspace-global now: a new stage becomes a canonical column on
	// the default conversation pipeline (the repository attaches it), not a
	// per-campaign clone. Uniqueness and position are computed over that same
	// canonical set, which ListByCampaign returns regardless of the campaign args.
	existing, err := uc.repo.ListByCampaign(workspaceID, input.CampaignID, input.CampaignType)
	if err != nil {
		return nil, err
	}
	lname := strings.ToLower(name)
	maxPosition := 0
	for _, t := range existing {
		if strings.ToLower(strings.TrimSpace(t.Name)) == lname {
			return nil, stage.ErrTagNameExists
		}
		if t.Position > maxPosition {
			maxPosition = t.Position
		}
	}

	t := &stage.Stage{
		ID:          uuid.New().String(),
		WorkspaceID: workspaceID,
		Name:        lname,
		Description: description,
		Color:       strings.TrimSpace(input.Color),
		Position:    maxPosition + 1,
	}

	if err := uc.repo.Create(t); err != nil {
		return nil, err
	}

	return uc.repo.FindByID(t.ID)
}
