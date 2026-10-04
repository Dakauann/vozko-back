package workspace_usecase

import "vozko/domain/workspace"

type listRolePresetsUseCase struct{}

func NewListRolePresetsUseCase() workspace.ListRolePresetsUseCase {
	return listRolePresetsUseCase{}
}

func (listRolePresetsUseCase) Execute() ([]workspace.ResolvedRolePreset, error) {
	out := make([]workspace.ResolvedRolePreset, 0, len(workspace.RolePresets))
	for _, preset := range workspace.RolePresets {
		permissions, err := preset.Permissions()
		if err != nil {
			return nil, err
		}
		out = append(out, workspace.ResolvedRolePreset{RolePreset: preset, Permissions: permissions})
	}
	return out, nil
}
