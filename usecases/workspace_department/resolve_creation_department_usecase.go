package workspace_department_usecase

import (
	"context"
	"strings"

	workspace_domain "vozko/domain/workspace"
	workspace_department "vozko/domain/workspace/workspace_department"
)

type resolveCreationDepartmentUseCase struct {
	workspaceRepo  workspace_domain.Repository
	departmentRepo workspace_department.Repository
}

func NewResolveCreationDepartmentUseCase(
	workspaceRepo workspace_domain.Repository,
	departmentRepo workspace_department.Repository,
) workspace_department.CreationDepartmentResolver {
	return &resolveCreationDepartmentUseCase{
		workspaceRepo:  workspaceRepo,
		departmentRepo: departmentRepo,
	}
}

func (uc *resolveCreationDepartmentUseCase) Resolve(ctx context.Context, workspaceID string) (string, error) {
	scope, ok := workspace_department.GetCreationScope(ctx)
	if !ok || strings.TrimSpace(scope.UserID) == "" {
		return "", nil
	}

	requestedDepartmentID := strings.TrimSpace(scope.RequestedDepartmentID)
	if scope.IsSystemAdmin {
		return uc.resolvePrivilegedSelection(workspaceID, requestedDepartmentID)
	}

	member, err := uc.workspaceRepo.GetMember(workspaceID, scope.UserID)
	if err != nil {
		return "", err
	}
	if member == nil {
		return "", workspace_domain.ErrUnauthorized
	}

	if member.Role == workspace_domain.RoleOwner || member.Role == workspace_domain.RoleAdmin {
		return uc.resolvePrivilegedSelection(workspaceID, requestedDepartmentID)
	}

	departmentIDs, err := uc.departmentRepo.GetMemberDepartmentIDs(workspaceID, scope.UserID)
	if err != nil {
		return "", err
	}
	workspaceHasDepartments := len(departmentIDs) > 0
	if !workspaceHasDepartments {
		departments, err := uc.departmentRepo.ListDepartments(workspaceID)
		if err != nil {
			return "", err
		}
		workspaceHasDepartments = len(departments) > 0
	}

	filter := &workspace_department.DepartmentFilter{
		DepartmentIDs:           departmentIDs,
		WorkspaceHasDepartments: workspaceHasDepartments,
	}
	if requestedDepartmentID != "" {
		if !containsDepartmentID(departmentIDs, requestedDepartmentID) {
			return "", workspace_department.ErrDepartmentAccessDenied
		}
		filter.SelectedDepartmentID = &requestedDepartmentID
	}

	return filter.DepartmentIDForCreation()
}

func (uc *resolveCreationDepartmentUseCase) resolvePrivilegedSelection(workspaceID, requestedDepartmentID string) (string, error) {
	if requestedDepartmentID == "" {
		return "", nil
	}

	dept, err := uc.departmentRepo.GetDepartmentByID(requestedDepartmentID)
	if err != nil {
		return "", err
	}
	if dept == nil || dept.WorkspaceID != workspaceID {
		return "", workspace_department.ErrDepartmentAccessDenied
	}

	return requestedDepartmentID, nil
}

func containsDepartmentID(departmentIDs []string, departmentID string) bool {
	for _, candidate := range departmentIDs {
		if strings.TrimSpace(candidate) == departmentID {
			return true
		}
	}
	return false
}
