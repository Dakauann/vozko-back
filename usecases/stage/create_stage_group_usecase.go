package stage_usecase

import (
	"context"
	"log"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/stage"
	workspace_department "vozko/domain/workspace/workspace_department"
)

type CreateStageGroupUseCase struct {
	repo               stage.StageGroupRepository
	stageRepo          stage.Repository
	departmentResolver workspace_department.CreationDepartmentResolver
}

// NewCreateStageGroupUseCase returns the concrete type rather than the
// stage.CreateStageGroupUseCase interface it satisfies, so the composition root can
// reach SetStageRepository without a type assertion. Interface fields still accept
// it unchanged.
func NewCreateStageGroupUseCase(repo stage.StageGroupRepository, departmentResolver ...workspace_department.CreationDepartmentResolver) *CreateStageGroupUseCase {
	var resolver workspace_department.CreationDepartmentResolver
	if len(departmentResolver) > 0 {
		resolver = departmentResolver[0]
	}
	return &CreateStageGroupUseCase{repo: repo, departmentResolver: resolver}
}

// SetStageRepository enables funnel materialization: with it wired, creating a
// group also creates the conversation pipeline that group describes, so the funnel
// appears in the CRM's funnel selector immediately instead of only once some
// campaign happens to reference the group.
//
// A setter rather than a constructor argument because the constructor is variadic
// on its last parameter — adding a required one ahead of it would silently change
// what every existing call site is passing. Left unset, group creation behaves
// exactly as before.
func (uc *CreateStageGroupUseCase) SetStageRepository(stageRepo stage.Repository) {
	uc.stageRepo = stageRepo
}

func (uc *CreateStageGroupUseCase) Execute(ctx context.Context, workspaceID string, input stage.CreateStageGroupInput) (*stage.StageGroup, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, stage.ErrTagGroupNameRequired
	}

	group := &stage.StageGroup{
		ID:          uuid.New().String(),
		WorkspaceID: workspaceID,
		Name:        name,
		Items:       make([]stage.StageGroupItem, len(input.Items)),
	}

	if uc.departmentResolver != nil {
		departmentID, err := uc.departmentResolver.Resolve(ctx, workspaceID)
		if err != nil {
			return nil, err
		}
		group.DepartmentID = departmentID
	}

	for i, item := range input.Items {
		group.Items[i] = stage.StageGroupItem{
			ID:           uuid.New().String(),
			StageGroupID: group.ID,
			Name:         strings.TrimSpace(item.Name),
			Description:  strings.TrimSpace(item.Description),
			Color:        strings.TrimSpace(item.Color),
			Position:     item.Position,
		}
		if group.Items[i].Position == 0 {
			group.Items[i].Position = i + 1
		}
	}

	if err := uc.repo.Create(group); err != nil {
		return nil, err
	}

	// Materialize the funnel now. Before this, a group was a template nothing
	// pointed at: it produced no pipeline, so it was absent from the funnel
	// selector, from "Gerenciar Etapas" and from every stage filter until a
	// campaign happened to reference it. Creating a funnel should be the act that
	// creates the funnel.
	//
	// Failure here is logged, not returned: the group itself was created
	// successfully, and ensurePipelineForGroup is idempotent, so the campaign path
	// (or a later edit) still converges on the same pipeline.
	if uc.stageRepo != nil {
		if _, err := ensurePipelineForGroup(uc.repo, uc.stageRepo, workspaceID, group.ID); err != nil {
			log.Printf("[StageGroup] group %s created but its pipeline was not materialized: %v", group.ID, err)
		}
	}

	return uc.repo.FindByID(group.ID)
}
