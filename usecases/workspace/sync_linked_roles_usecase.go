package workspace_usecase

import (
	"errors"
	"fmt"
	"log"

	"vozko/domain/workspace"
)

type LinkedRolesReport struct {
	Updated  int
	Detached int
}

type SyncLinkedRolesUseCase struct {
	repo     workspace.Repository
	roleRepo workspace.CustomRoleRepository
}

func NewSyncLinkedRolesUseCase(repo workspace.Repository, roleRepo workspace.CustomRoleRepository) *SyncLinkedRolesUseCase {
	return &SyncLinkedRolesUseCase{repo: repo, roleRepo: roleRepo}
}

func (uc *SyncLinkedRolesUseCase) Execute() (LinkedRolesReport, error) {
	var report LinkedRolesReport
	roles, err := uc.roleRepo.ListLinkedRoles()
	if err != nil {
		return report, err
	}
	var failures []error
	for _, role := range roles {
		changed, err := role.SyncWithPreset()
		if errors.Is(err, workspace.ErrUnknownRolePreset) {
			role.Linked = false
			if err := uc.roleRepo.UpdateRole(role); err != nil {
				failures = append(failures, fmt.Errorf("detach role %s: %w", role.ID, err))
				continue
			}
			log.Printf("[workspace] role %s was linked to preset %q, which no longer exists; it keeps its permissions as a copy", role.ID, role.PresetKey)
			report.Detached++
			continue
		}
		if err != nil {
			failures = append(failures, fmt.Errorf("sync role %s: %w", role.ID, err))
			continue
		}
		if !changed {
			continue
		}
		if err := uc.roleRepo.UpdateRole(role); err != nil {
			failures = append(failures, fmt.Errorf("sync role %s: %w", role.ID, err))
			continue
		}
		if err := propagateRolePermissions(uc.repo, uc.roleRepo, role); err != nil {
			failures = append(failures, err)
			continue
		}
		report.Updated++
	}
	return report, errors.Join(failures...)
}
