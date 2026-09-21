package workspace_usecase

import (
	"context"

	"vozko/domain/workspace"
	workspace_department "vozko/domain/workspace/workspace_department"
	wsc "vozko/domain/workspace_config"
)

type workspaceConfigReader interface {
	GetByWorkspaceID(ctx context.Context, workspaceID string) (*wsc.WorkspaceConfig, error)
}

type memberVisibilityUseCase struct {
	repo         workspace.Repository
	deptRepo     workspace_department.Repository
	configReader workspaceConfigReader
}

func NewMemberVisibilityUseCase(repo workspace.Repository, deptRepo workspace_department.Repository, configReader workspaceConfigReader) workspace.MemberVisibilityUseCase {
	return &memberVisibilityUseCase{repo: repo, deptRepo: deptRepo, configReader: configReader}
}

func (uc *memberVisibilityUseCase) adminParticipatesInRoulette(workspaceID string) bool {
	if uc.configReader == nil {
		return true
	}
	cfg, err := uc.configReader.GetByWorkspaceID(context.Background(), workspaceID)
	if err != nil || cfg == nil {
		return true
	}
	return !cfg.SkipAdminAssignment
}

func (uc *memberVisibilityUseCase) Scope(userID, workspaceID string, isPlatformAdmin bool) (workspace.MemberVisibilityScope, error) {
	if userID == "" || workspaceID == "" {
		return workspace.MemberVisibilityScope{}, workspace.ErrUnauthorized
	}

	if isPlatformAdmin {
		return workspace.MemberVisibilityScope{Restrict: false}, nil
	}

	member, err := uc.repo.GetMember(workspaceID, userID)
	if err != nil {
		return workspace.MemberVisibilityScope{}, err
	}
	if member == nil {
		return workspace.MemberVisibilityScope{}, workspace.ErrUnauthorized
	}

	if member.Role == workspace.RoleOwner || member.Role == workspace.RoleAdmin {
		return workspace.MemberVisibilityScope{Restrict: false}, nil
	}

	canRead, err := uc.repo.HasPermission(member.ID, workspace.ResourceMembers, workspace.ActionRead)
	if err != nil {
		return workspace.MemberVisibilityScope{}, err
	}
	if !canRead {
		return workspace.MemberVisibilityScope{}, workspace.ErrUnauthorized
	}

	departments, err := uc.deptRepo.ListDepartments(workspaceID)
	if err != nil {
		return workspace.MemberVisibilityScope{}, err
	}
	if len(departments) == 0 {
		return workspace.MemberVisibilityScope{Restrict: false}, nil
	}

	canViewOthers, err := uc.repo.HasPermission(member.ID, workspace.ResourceMembers, workspace.ActionViewOthers)
	if err != nil {
		return workspace.MemberVisibilityScope{}, err
	}
	if canViewOthers {
		return workspace.MemberVisibilityScope{Restrict: false}, nil
	}

	departmentIDs, err := uc.deptRepo.GetMemberDepartmentIDs(workspaceID, userID)
	if err != nil {
		return workspace.MemberVisibilityScope{}, err
	}
	return workspace.MemberVisibilityScope{
		Restrict:      true,
		DepartmentIDs: departmentIDs,
		IncludeAdmins: uc.adminParticipatesInRoulette(workspaceID),
	}, nil
}

func (uc *memberVisibilityUseCase) CanView(callerUserID, targetUserID, workspaceID string, isPlatformAdmin bool) (bool, error) {
	if callerUserID == "" || targetUserID == "" || workspaceID == "" {
		return false, nil
	}

	if callerUserID == targetUserID {
		return true, nil
	}

	scope, err := uc.Scope(callerUserID, workspaceID, isPlatformAdmin)
	if err != nil {
		if err == workspace.ErrUnauthorized {
			return false, nil
		}
		return false, err
	}

	target, err := uc.repo.GetMember(workspaceID, targetUserID)
	if err != nil {
		return false, err
	}
	if target == nil {
		return false, nil
	}

	if !scope.Restrict {
		return true, nil
	}

	if scope.IncludeAdmins && (target.Role == workspace.RoleOwner || target.Role == workspace.RoleAdmin) {
		return true, nil
	}

	if len(scope.DepartmentIDs) == 0 {
		return false, nil
	}

	targetDepartmentIDs, err := uc.deptRepo.GetMemberDepartmentIDs(workspaceID, targetUserID)
	if err != nil {
		return false, err
	}
	return departmentsIntersect(scope.DepartmentIDs, targetDepartmentIDs), nil
}

func departmentsIntersect(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	set := make(map[string]struct{}, len(a))
	for _, v := range a {
		set[v] = struct{}{}
	}
	for _, v := range b {
		if _, ok := set[v]; ok {
			return true
		}
	}
	return false
}
