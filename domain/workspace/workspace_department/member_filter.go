package workspace_department

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
