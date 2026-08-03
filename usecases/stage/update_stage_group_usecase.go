package stage_usecase

import (
	"strings"

	"github.com/google/uuid"

	"vozko/domain/stage"
)

type UpdateStageGroupUseCase struct {
	repo      stage.StageGroupRepository
	stageRepo stage.Repository
}

func NewUpdateStageGroupUseCase(repo stage.StageGroupRepository, stageRepo stage.Repository) stage.UpdateStageGroupUseCase {
	return &UpdateStageGroupUseCase{repo: repo, stageRepo: stageRepo}
}

func (uc *UpdateStageGroupUseCase) Execute(workspaceID, groupID string, input stage.UpdateStageGroupInput) (*stage.StageGroup, error) {
	existing, err := uc.repo.FindByID(groupID)
	if err != nil {
		return nil, stage.ErrTagGroupNotFound
	}
	if existing.WorkspaceID != workspaceID {
		return nil, stage.ErrUnauthorized
	}

	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, stage.ErrTagGroupNameRequired
		}
		existing.Name = name
	}

	if err := uc.repo.Update(existing); err != nil {
		return nil, err
	}

	if input.Items != nil {

		for _, item := range existing.Items {
			if err := uc.repo.RemoveItem(item.ID); err != nil {
				return nil, err
			}
		}

		for i, itemInput := range input.Items {
			item := &stage.StageGroupItem{
				StageGroupID: groupID,
				Name:         strings.TrimSpace(itemInput.Name),
				Description:  strings.TrimSpace(itemInput.Description),
				Color:        strings.TrimSpace(itemInput.Color),
				Position:     itemInput.Position,
			}
			if item.Position == 0 {
				item.Position = i + 1
			}
			if err := uc.repo.AddItem(item); err != nil {
				return nil, err
			}
		}

		// The group's items are also materialized once into a shared conversation
		// pipeline ("same group → same board" in CloneStagesFromGroupUseCase):
		// campaigns created from this group reuse that pipeline and never re-clone.
		// So an edit here must be pushed onto that pipeline too, otherwise a removed
		// item lingers as a live stage and keeps showing up on the board and on every
		// newly created campaign.
		if err := uc.syncPipelineToGroup(workspaceID, groupID, input.Items); err != nil {
			return nil, err
		}
	}

	return uc.repo.FindByID(groupID)
}

// syncPipelineToGroup reconciles the conversation pipeline stamped from this group so
// its stages mirror the group's items: stages whose (normalized) name is no longer in
// the group are deleted, dropping their entry assignments, exactly as a manual stage
// delete does, new item names are cloned in, and surviving stages take the item's
// position/description/color. Matching is by normalized name, mirroring how the clone
// stores stage names. No-op when the group was never materialized (no campaign yet).
func (uc *UpdateStageGroupUseCase) syncPipelineToGroup(workspaceID, groupID string, items []stage.StageGroupItemInput) error {
	pipelineID, err := uc.stageRepo.FindConversationPipelineByGroup(workspaceID, groupID)
	if err != nil {
		return err
	}
	if pipelineID == "" {
		return nil
	}

	current, err := uc.stageRepo.ListByPipeline(workspaceID, pipelineID)
	if err != nil {
		return err
	}

	norm := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

	type desiredMeta struct {
		description string
		color       string
		position    int
		index       int
	}
	// Desired stage names in item order, deduped by normalized name.
	desired := make(map[string]desiredMeta, len(items))
	order := make([]string, 0, len(items))
	for i, it := range items {
		name := norm(it.Name)
		if name == "" {
			continue
		}
		pos := it.Position
		if pos == 0 {
			pos = i + 1
		}
		if _, seen := desired[name]; !seen {
			desired[name] = desiredMeta{
				description: strings.TrimSpace(it.Description),
				color:       strings.TrimSpace(it.Color),
				position:    pos,
				index:       i,
			}
			order = append(order, name)
		}
	}

	// Drop stages the group no longer has; refresh the ones it keeps.
	hasInitial := false
	present := make(map[string]bool, len(current))
	for _, s := range current {
		name := norm(s.Name)
		meta, keep := desired[name]
		if !keep {
			if err := uc.stageRepo.Delete(s.ID); err != nil {
				return err
			}
			continue
		}
		if s.IsInitial {
			hasInitial = true
		}
		if present[name] { // a duplicate row for a name we already reconciled, leave it
			continue
		}
		present[name] = true
		s.Position = meta.position
		s.Description = meta.description
		s.Color = meta.color
		if err := uc.stageRepo.Update(s); err != nil {
			return err
		}
	}

	// Clone in item names that don't exist on the pipeline yet.
	for _, name := range order {
		if present[name] {
			continue
		}
		meta := desired[name]
		s := &stage.Stage{
			ID:          uuid.New().String(),
			WorkspaceID: workspaceID,
			PipelineID:  pipelineID,
			Name:        name,
			Description: meta.description,
			Color:       meta.color,
			Position:    meta.position,
			IsInitial:   meta.index == 0 && !hasInitial,
		}
		if s.IsInitial {
			hasInitial = true
		}
		if err := uc.stageRepo.Create(s); err != nil {
			return err
		}
	}

	return nil
}
