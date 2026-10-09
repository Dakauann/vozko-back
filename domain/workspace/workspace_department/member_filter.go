package workspace_department

import (
	"errors"

	"vozko/domain/workspace"
)

type MemberDepartments interface {
	GetMemberDepartmentIDs(workspaceID, userID string) ([]string, error)
	ListDepartments(workspaceID string) ([]Department, error)
}

func MemberFilter(source MemberDepartments, workspaceID, userID string) (*DepartmentFilter, error) {
	ids, err := source.GetMemberDepartmentIDs(workspaceID, userID)
	if err != nil {
		return nil, err
	}
	if len(ids) > 0 {
		return &DepartmentFilter{DepartmentIDs: ids, WorkspaceHasDepartments: true}, nil
	}
	departments, err := source.ListDepartments(workspaceID)
	if err != nil {
		return nil, err
	}
	return &DepartmentFilter{WorkspaceHasDepartments: len(departments) > 0}, nil
}

func BlockedFilter() *DepartmentFilter {
	return &DepartmentFilter{WorkspaceHasDepartments: true}
}

func RequesterFilter(ownerOrAdmin bool, source MemberDepartments, workspaceID, userID string) (*DepartmentFilter, error) {
	if ownerOrAdmin {
		return &DepartmentFilter{IsOwnerOrAdmin: true}, nil
	}
	return MemberFilter(source, workspaceID, userID)
}

func (f *DepartmentFilter) Narrow(requested []string) ([]string, bool) {
	if f == nil {
		return nil, true
	}
	if !f.ShouldFilter() {
		return requested, false
	}
	allowed := make(map[string]bool, len(f.DepartmentIDs))
	for _, id := range f.DepartmentIDs {
		allowed[id] = true
	}
	if len(requested) == 0 {
		return f.DepartmentIDs, len(f.DepartmentIDs) == 0
	}
	var kept []string
	for _, id := range requested {
		if allowed[id] {
			kept = append(kept, id)
		}
	}
	return kept, len(kept) == 0
}

var ErrMembersUnavailable = errors.New("workspace department: the member directory is not available")

type Members interface {
	GetMember(workspaceID, userID string) (*workspace.Member, error)
}

func SeesEveryDepartment(members Members, workspaceID, userID string, platformAdmin bool) (bool, error) {
	if platformAdmin {
		return true, nil
	}
	if members == nil {
		return false, ErrMembersUnavailable
	}
	member, err := members.GetMember(workspaceID, userID)
	if err != nil {
		return false, err
	}
	return member != nil && member.Role.CanManageMembers(), nil
}

func RequesterScope(members Members, source MemberDepartments, workspaceID, userID string, platformAdmin bool) (*DepartmentFilter, error) {
	every, err := SeesEveryDepartment(members, workspaceID, userID, platformAdmin)
	if err != nil {
		return nil, err
	}
	return RequesterFilter(every, source, workspaceID, userID)
}
