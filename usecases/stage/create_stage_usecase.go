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

	// A stage belongs to ONE funnel, so uniqueness and position are computed within
	// that funnel — never across the workspace. pipelineID names it explicitly (the
	// CRM sends the funnel the operator is looking at); without one the repository
	// attaches the stage to the workspace default, which is the legacy behaviour and
	// the reason a custom funnel could not be given a new column from the CRM.
	pipelineID := strings.TrimSpace(input.PipelineID)
	existing, err := uc.listSiblings(workspaceID, pipelineID, input)
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
		PipelineID:  pipelineID,
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

// listSiblings returns the stages the new one must be unique and ordered against:
// the named funnel's, or — with no funnel named — whatever the legacy campaign
// resolution lands on. Scoping this per funnel is what lets two funnels each have
// their own "fechado" without colliding.
func (uc *CreateStageUseCase) listSiblings(
	workspaceID, pipelineID string,
	input stage.CreateStageInput,
) ([]*stage.Stage, error) {
	if pipelineID != "" {
		return uc.repo.ListByPipeline(workspaceID, pipelineID)
	}
	return uc.repo.ListByCampaign(workspaceID, input.CampaignID, input.CampaignType)
}
