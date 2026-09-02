// Package pipeline_usecase implements the pipeline CRUD usecases. It depends
// only on the domain pipeline.Repository port, honoring the dependency
// direction delivery -> usecases -> domain <- infra.
package pipeline_usecase

import (
	"log"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/pipeline"
)

// StageSeeder gives a brand-new funnel its first columns.
//
// A funnel with no stages is not a funnel: the board renders nothing, no
// conversation can be placed on it, and the operator has no way to add a column
// without one already existing to anchor position. Seeding is therefore part of
// creating one, not a follow-up the caller may forget.
//
// It is a port rather than a direct dependency because stages are another
// aggregate — this package depends only on pipeline.Repository, and the
// composition root supplies the adapter.
type StageSeeder interface {
	// SeedConversationPipeline fills pipelineID with stages. copyFromPipelineID
	// duplicates that funnel's stages when set; empty means the product defaults.
	SeedConversationPipeline(workspaceID, pipelineID, copyFromPipelineID string) error
}

type CreatePipelineUseCase struct {
	repo   pipeline.Repository
	seeder StageSeeder
}

func NewCreatePipelineUseCase(repo pipeline.Repository) *CreatePipelineUseCase {
	return &CreatePipelineUseCase{repo: repo}
}

// SetStageSeeder enables stage seeding on creation. Returned as the concrete type
// from the constructor so the composition root can call this without a type
// assertion; interface fields still accept it unchanged.
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

	// Seed only conversation funnels: the opportunity board has its own seeding
	// path (EnsureDefaultOpportunityPipeline) and a different stage vocabulary.
	if uc.seeder != nil && p.ObjectType == pipeline.ObjectConversation {
		if err := uc.seeder.SeedConversationPipeline(workspaceID, p.ID, strings.TrimSpace(input.CopyStagesFromPipelineID)); err != nil {
			// The funnel exists and is already listed; a seeding failure leaves it
			// empty rather than orphaning it, and the operator can add columns by
			// hand. Losing the funnel over it would be the worse outcome.
			log.Printf("[pipeline] funnel %s created but not seeded: %v", p.ID, err)
		}
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
