package workspace_usecase

import (
	"strings"

	"vozko/domain/shared"
	"vozko/domain/workspace"
)

// listAssignableMembersUseCase returns the paginated, searchable set of members
// the caller is allowed to assign a conversation to. The visibility policy is
// delegated to MemberVisibilityUseCase so the repository only performs data
// access (it receives a resolved scope, never a policy decision).
type listAssignableMembersUseCase struct {
	repo       workspace.Repository
	visibility workspace.MemberVisibilityUseCase
}

func NewListAssignableMembersUseCase(repo workspace.Repository, visibility workspace.MemberVisibilityUseCase) workspace.ListAssignableMembersUseCase {
	return &listAssignableMembersUseCase{repo: repo, visibility: visibility}
}

func (uc *listAssignableMembersUseCase) Execute(userID, workspaceID string, isPlatformAdmin bool, search string, page, pageSize int) ([]*workspace.AssignableMember, int64, error) {
	scope, err := uc.visibility.Scope(userID, workspaceID, isPlatformAdmin)
	if err != nil {
		return nil, 0, err
	}

	if pageSize <= 0 {
		pageSize = shared.DefaultPageSize
	}
	if page <= 0 {
		page = 1
	}

	members, total, err := uc.repo.ListAssignableMembers(workspaceID, strings.TrimSpace(search), scope.Restrict, scope.DepartmentIDs, scope.IncludeAdmins, userID, page, pageSize)
	if err != nil {
		return nil, 0, err
	}

	memberIDs := make([]string, 0, len(members))
	for _, m := range members {
		memberIDs = append(memberIDs, m.ID)
	}

	// A scoped caller only ever sees department labels within their own access.
	var restrictDepts []string
	if scope.Restrict {
		restrictDepts = scope.DepartmentIDs
	}
	deptsByMember, err := uc.repo.ListMemberDepartments(workspaceID, memberIDs, restrictDepts)
	if err != nil {
		return nil, 0, err
	}

	result := make([]*workspace.AssignableMember, 0, len(members))
	for _, m := range members {
		result = append(result, &workspace.AssignableMember{
			Member:      m,
			Departments: deptsByMember[m.ID],
		})
	}
	return result, total, nil
}
