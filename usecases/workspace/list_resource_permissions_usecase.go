package workspace_usecase

import (
	"sort"

	"vozko/domain/workspace"
)

type listResourcePermissionsUseCase struct{}

func NewListResourcePermissionsUseCase() workspace.ListResourcePermissionsUseCase {
	return &listResourcePermissionsUseCase{}
}

func (uc *listResourcePermissionsUseCase) Execute() []workspace.ResourcePermissionInfo {
	// Iterate the permission catalogue (ResourceActions) with sorted keys so the
	// listing is deterministic regardless of Go's map iteration order.
	resources := make([]workspace.Resource, 0, len(workspace.ResourceActions))
	for r := range workspace.ResourceActions {
		resources = append(resources, r)
	}
	sort.Slice(resources, func(i, j int) bool { return resources[i] < resources[j] })

	result := make([]workspace.ResourcePermissionInfo, 0, len(resources))
	for _, r := range resources {
		defs := workspace.ResourceActions[r]
		actions := make([]workspace.Action, len(defs))
		descriptions := make(map[string]string, len(defs))
		var deps map[string][]workspace.PermissionEntry
		for j, d := range defs {
			actions[j] = d.ActionName
			descriptions[string(d.ActionName)] = d.Description
			if len(d.Requires) > 0 {
				if deps == nil {
					deps = make(map[string][]workspace.PermissionEntry)
				}
				deps[string(d.ActionName)] = d.Requires
			}
		}

		result = append(result, workspace.ResourcePermissionInfo{
			Resource:           r,
			Actions:            actions,
			ActionDescriptions: descriptions,
			Dependencies:       deps,
		})
	}
	return result
}
