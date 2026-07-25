// Package pipeline_usecase implements the pipeline CRUD usecases. It depends
// only on the domain pipeline.Repository port, honoring the dependency
// direction delivery -> usecases -> domain <- infra.
package pipeline_usecase

import (
	"strings"

	"github.com/google/uuid"

	"vozko/domain/pipeline"
)

type CreatePipelineUseCase struct {
	repo pipeline.Repository
}

func NewCreatePipelineUseCase(repo pipeline.Repository) pipeline.CreatePipelineUseCase {
	return &CreatePipelineUseCase{repo: repo}
}

func (uc *CreatePipelineUseCase) Execute(workspaceID string, input pipeline.CreatePipelineInput) (*pipeline.Pipeline, error) {
	p := &pipeline.Pipeline{
		ID:           uuid.New().String(),
		WorkspaceID:  workspaceID,
		Name:         input.Name,
		ObjectType:   pipeline.ObjectType(strings.TrimSpace(input.ObjectType)),
		DepartmentID: strings.TrimSpace(input.DepartmentID),
		Position:     input.Position,
		IsDefault:    input.IsDefault,
	}
	p.Normalize()
	if err := p.Validate(); err != nil {
		return nil, err
	}

	if p.Position == 0 {
		existing, err := uc.repo.ListByWorkspace(workspaceID, string(p.ObjectType))
		if err != nil {
			return nil, err
		}
		maxPos := 0
		for _, e := range existing {
			if e.Position > maxPos {
				maxPos = e.Position
			}
		}
		p.Position = maxPos + 1
	}

	if err := uc.repo.Create(p); err != nil {
		return nil, err
	}
	return uc.repo.GetByID(workspaceID, p.ID)
}

type UpdatePipelineUseCase struct {
	repo pipeline.Repository
}

func NewUpdatePipelineUseCase(repo pipeline.Repository) pipeline.UpdatePipelineUseCase {
	return &UpdatePipelineUseCase{repo: repo}
}

func (uc *UpdatePipelineUseCase) Execute(workspaceID, id string, input pipeline.UpdatePipelineInput) (*pipeline.Pipeline, error) {
	existing, err := uc.repo.GetByID(workspaceID, id)
	if err != nil {
		return nil, err
	}

	if input.Name != nil {
		existing.Name = *input.Name
	}
	if input.DepartmentID != nil {
		existing.DepartmentID = strings.TrimSpace(*input.DepartmentID)
	}
	if input.Position != nil {
		existing.Position = *input.Position
	}
	if input.IsDefault != nil {
		existing.IsDefault = *input.IsDefault
	}

	existing.Normalize()
	if err := existing.Validate(); err != nil {
		return nil, err
	}
	if err := uc.repo.Update(existing); err != nil {
		return nil, err
	}
	return uc.repo.GetByID(workspaceID, id)
}

type DeletePipelineUseCase struct {
	repo pipeline.Repository
}

func NewDeletePipelineUseCase(repo pipeline.Repository) pipeline.DeletePipelineUseCase {
	return &DeletePipelineUseCase{repo: repo}
}

func (uc *DeletePipelineUseCase) Execute(workspaceID, id string) error {
	return uc.repo.Delete(workspaceID, id)
}

type ListPipelinesUseCase struct {
	repo pipeline.Repository
}

func NewListPipelinesUseCase(repo pipeline.Repository) pipeline.ListPipelinesUseCase {
	return &ListPipelinesUseCase{repo: repo}
}

func (uc *ListPipelinesUseCase) Execute(workspaceID, objectType string) ([]*pipeline.Pipeline, error) {
	return uc.repo.ListByWorkspace(workspaceID, strings.TrimSpace(objectType))
}

type GetPipelineUseCase struct {
	repo pipeline.Repository
}

func NewGetPipelineUseCase(repo pipeline.Repository) pipeline.GetPipelineUseCase {
	return &GetPipelineUseCase{repo: repo}
}

func (uc *GetPipelineUseCase) Execute(workspaceID, id string) (*pipeline.Pipeline, error) {
	return uc.repo.GetByID(workspaceID, id)
}
