package workspace_usecase

import (
	"context"

	"vozko/domain/workspace"
	workspace_department "vozko/domain/workspace/workspace_department"
	wsc "vozko/domain/workspace_config"
)

// workspaceConfigReader is the narrow slice of the workspace-config repository
// the visibility policy needs (whether admins participate in roulette).
type workspaceConfigReader interface {
	GetByWorkspaceID(ctx context.Context, workspaceID string) (*wsc.WorkspaceConfig, error)
}

// memberVisibilityUseCase is the single source of truth for "which members can
// this caller see/assign to" — used by both the HTTP assignable-members
// endpoint and the realtime conversation-assign guard, so the read path and the
// write path can never drift apart.
type memberVisibilityUseCase struct {
	repo         workspace.Repository
	deptRepo     workspace_department.Repository
	configReader workspaceConfigReader
}

func NewMemberVisibilityUseCase(repo workspace.Repository, deptRepo workspace_department.Repository, configReader workspaceConfigReader) workspace.MemberVisibilityUseCase {
	return &memberVisibilityUseCase{repo: repo, deptRepo: deptRepo, configReader: configReader}
}

// adminParticipatesInRoulette reports whether owners/admins are reachable as
// assignment targets for a department-scoped member. Defaults to true (admins
// reachable) when the workspace has no config row.
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

	// Platform admin: full visibility.
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

	// Workspace owner/admin: full visibility.
	if member.Role == workspace.RoleOwner || member.Role == workspace.RoleAdmin {
		return workspace.MemberVisibilityScope{Restrict: false}, nil
	}

	// Regular member must hold members:read to list members at all.
	canRead, err := uc.repo.HasPermission(member.ID, workspace.ResourceMembers, workspace.ActionRead)
	if err != nil {
		return workspace.MemberVisibilityScope{}, err
	}
	if !canRead {
		return workspace.MemberVisibilityScope{}, workspace.ErrUnauthorized
	}

	// A workspace with no departments imposes no scope — everyone is visible.
	departments, err := uc.deptRepo.ListDepartments(workspaceID)
	if err != nil {
		return workspace.MemberVisibilityScope{}, err
	}
	if len(departments) == 0 {
		return workspace.MemberVisibilityScope{Restrict: false}, nil
	}

	// members:view_others widens a regular member to every department.
	canViewOthers, err := uc.repo.HasPermission(member.ID, workspace.ResourceMembers, workspace.ActionViewOthers)
	if err != nil {
		return workspace.MemberVisibilityScope{}, err
	}
	if canViewOthers {
		return workspace.MemberVisibilityScope{Restrict: false}, nil
	}

	// Otherwise: scoped to the caller's own department(s), plus owners/admins
	// when the workspace lets them participate in roulette. An empty department
	// list means the caller belongs to no department (sees only themselves, and
	// admins if reachable).
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

	// A caller can always reference themselves.
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

	// The target must be a member of THIS workspace, regardless of scope.
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

	// Department-scoped caller may also reach owners/admins when they participate
	// in roulette — even without a shared department (escalation path).
	if scope.IncludeAdmins && (target.Role == workspace.RoleOwner || target.Role == workspace.RoleAdmin) {
		return true, nil
	}

	if len(scope.DepartmentIDs) == 0 {
		// Caller sees only themselves (+ admins, handled above); target is someone else.
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
