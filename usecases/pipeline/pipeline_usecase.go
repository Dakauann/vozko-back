package pipeline_usecase

import (
	"fmt"
	"log"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/pipeline"
	wd "vozko/domain/workspace/workspace_department"
)

type StageSeeder interface {
	SeedConversationPipeline(workspaceID, pipelineID, copyFromPipelineID string, stages []pipeline.StageSeed) error
}

type DepartmentDirectory interface {
	Execute(workspaceID string) ([]wd.Department, error)
}

func departmentInWorkspace(departments DepartmentDirectory, workspaceID, departmentID string) error {
	if departmentID == "" {
		return nil
	}
	if departments == nil {
		return pipeline.ErrDepartmentUnknown
	}
	list, err := departments.Execute(workspaceID)
	if err != nil {
		return fmt.Errorf("departments of %s: %w", workspaceID, err)
	}
	for _, d := range list {
		if d.ID == departmentID {
			return nil
		}
	}
	return pipeline.ErrDepartmentUnknown
}

type CreatePipelineUseCase struct {
	repo        pipeline.Repository
	departments DepartmentDirectory
	seeder      StageSeeder
}

func NewCreatePipelineUseCase(repo pipeline.Repository, departments DepartmentDirectory) *CreatePipelineUseCase {
	return &CreatePipelineUseCase{repo: repo, departments: departments}
}

func (uc *CreatePipelineUseCase) SetStageSeeder(s StageSeeder) {
	uc.seeder = s
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
	if err := departmentInWorkspace(uc.departments, workspaceID, p.DepartmentID); err != nil {
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

	if p.IsDefault {
		if err := uc.repo.PromoteDefault(workspaceID, string(p.ObjectType), p.ID); err != nil {
			return nil, err
		}
	}

	if uc.seeder != nil && p.ObjectType == pipeline.ObjectConversation {
		if err := uc.seeder.SeedConversationPipeline(workspaceID, p.ID, strings.TrimSpace(input.CopyStagesFromPipelineID), input.Stages); err != nil {
			log.Printf("[pipeline] funnel %s created but not seeded: %v", p.ID, err)
		}
	}

	return uc.repo.GetByID(workspaceID, p.ID)
}

type UpdatePipelineUseCase struct {
	repo        pipeline.Repository
	departments DepartmentDirectory
}

func NewUpdatePipelineUseCase(repo pipeline.Repository, departments DepartmentDirectory) pipeline.UpdatePipelineUseCase {
	return &UpdatePipelineUseCase{repo: repo, departments: departments}
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

	promote := false
	if input.IsDefault != nil {
		switch {
		case *input.IsDefault && !existing.IsDefault:
			promote = true
		case !*input.IsDefault && existing.IsDefault:
			return nil, pipeline.ErrDefaultRequired
		}
	}

	existing.Normalize()
	if err := existing.Validate(); err != nil {
		return nil, err
	}
	if input.DepartmentID != nil {
		if err := departmentInWorkspace(uc.departments, workspaceID, existing.DepartmentID); err != nil {
			return nil, err
		}
	}
	if err := uc.repo.Update(existing); err != nil {
		return nil, err
	}
	if promote {
		if err := uc.repo.PromoteDefault(workspaceID, string(existing.ObjectType), existing.ID); err != nil {
			return nil, err
		}
	}
	return uc.repo.GetByID(workspaceID, id)
}

type DeletePipelineUseCase struct {
	repo      pipeline.Repository
	occupancy pipeline.Occupancy
}

func NewDeletePipelineUseCase(repo pipeline.Repository, occupancy pipeline.Occupancy) pipeline.DeletePipelineUseCase {
	return &DeletePipelineUseCase{repo: repo, occupancy: occupancy}
}

func (uc *DeletePipelineUseCase) Execute(workspaceID, id string, input pipeline.DeletePipelineInput) error {
	target, err := uc.repo.GetByID(workspaceID, id)
	if err != nil {
		return err
	}
	if target == nil {
		return pipeline.ErrNotFound
	}
	if target.IsDefault {
		return pipeline.ErrDeleteDefault
	}
	if uc.occupancy == nil {
		return uc.repo.Delete(workspaceID, id)
	}

	usage, err := uc.occupancy.Usage(workspaceID, id)
	if err != nil {
		return err
	}
	if !usage.Deletable() {
		return pipeline.ErrDeleteBound
	}

	destination := ""
	if usage.Entries > 0 {
		destination = strings.TrimSpace(input.MoveEntriesTo)
		if destination == "" {
			return pipeline.ErrDeleteNeedsDestination
		}
		if destination == id {
			return pipeline.ErrDeleteDestinationInvalid
		}
		into, err := uc.repo.GetByID(workspaceID, destination)
		if err != nil {
			return err
		}
		if into == nil {
			return pipeline.ErrNotFound
		}
		if into.ObjectType != target.ObjectType {
			return pipeline.ErrDeleteDestinationInvalid
		}
	}

	if _, err := uc.occupancy.Vacate(workspaceID, id, destination); err != nil {
		return err
	}
	return uc.repo.Delete(workspaceID, id)
}

type GetPipelineUsageUseCase struct {
	repo      pipeline.Repository
	occupancy pipeline.Occupancy
}

func NewGetPipelineUsageUseCase(repo pipeline.Repository, occupancy pipeline.Occupancy) pipeline.GetPipelineUsageUseCase {
	return &GetPipelineUsageUseCase{repo: repo, occupancy: occupancy}
}

func (uc *GetPipelineUsageUseCase) Execute(workspaceID, id string) (pipeline.Usage, error) {
	p, err := uc.repo.GetByID(workspaceID, id)
	if err != nil {
		return pipeline.Usage{}, err
	}
	if p == nil {
		return pipeline.Usage{}, pipeline.ErrNotFound
	}
	if uc.occupancy == nil {
		return pipeline.Usage{}, nil
	}
	return uc.occupancy.Usage(workspaceID, id)
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
