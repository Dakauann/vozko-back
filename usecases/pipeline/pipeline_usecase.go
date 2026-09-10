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
	// SeedConversationPipeline fills pipelineID with columns.
	//
	// `stages` wins when non-empty: those are the columns the operator drew, and
	// nothing should second-guess them. Otherwise copyFromPipelineID duplicates
	// that funnel's columns, and an empty one falls back to the product defaults
	// so a funnel created by an integration is still usable.
	SeedConversationPipeline(workspaceID, pipelineID, copyFromPipelineID string, stages []pipeline.StageSeed) error
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

	// Created as the default: demote whatever held the flag, in the same one
	// call the update path uses. Passing IsDefault straight through is what let
	// "create and make it default" leave two defaults behind, and a default
	// funnel cannot be deleted, so the extra one was permanent.
	if p.IsDefault {
		if err := uc.repo.PromoteDefault(workspaceID, string(p.ObjectType), p.ID); err != nil {
			return nil, err
		}
	}

	// Seed only conversation funnels: the opportunity board has its own seeding
	// path (EnsureDefaultOpportunityPipeline) and a different stage vocabulary.
	if uc.seeder != nil && p.ObjectType == pipeline.ObjectConversation {
		if err := uc.seeder.SeedConversationPipeline(workspaceID, p.ID, strings.TrimSpace(input.CopyStagesFromPipelineID), input.Stages); err != nil {
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

	// The default flag is NOT written through the ordinary Update.
	//
	// It used to be, and that is the whole bug: setting it on a second funnel
	// left the first one set too, so a workspace accumulated defaults, and a
	// default funnel cannot be deleted. Promotion is a separate, atomic write
	// that demotes the incumbent; demotion of the last default is refused.
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

// Execute removes a funnel, refusing every case where doing so would lose
// something the operator did not agree to lose.
//
// The order is the guarantee. Existence, then the default check, then the
// bindings, then the destination, and only then any write — so a refusal is
// always reached before anything has moved, and a funnel is never left half
// emptied by a check that could have run first. The move is the one write that
// precedes the delete, and its failure aborts: conversations stranded on stages
// that are about to disappear is the exact outcome this guard exists to prevent.
//
// occupancy may be nil in unit contexts that never exercise the guard; the
// funnel then deletes as it did before, which is why every caller in the
// composition root supplies one.
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

// Execute reports what the funnel holds, through the SAME port the guard reads.
// One source is the point: a dialog that promised an empty funnel and a delete
// that then refused would be worse than no dialog at all.
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
