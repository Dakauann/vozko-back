package workspace_department

func (f *DepartmentFilter) Allows(departmentID string) bool {
	if f == nil {
		return false
	}
	if !f.ShouldFilter() {
		return true
	}
	return f.isOwn(departmentID)
}

func (f *DepartmentFilter) ListScope() ([]string, bool) {
	if f == nil || f.BlockedByMissingDepartment() {
		return nil, true
	}
	if !f.ShouldFilter() {
		return nil, false
	}
	return f.DepartmentIDs, false
}

func (f *DepartmentFilter) ReadScope(requested string) (string, error) {
	if f == nil || f.BlockedByMissingDepartment() {
		return "", ErrDepartmentAccessDenied
	}
	if !f.ShouldFilter() {
		return requested, nil
	}
	if requested != "" {
		if !f.isOwn(requested) {
			return "", ErrDepartmentAccessDenied
		}
		return requested, nil
	}
	if len(f.DepartmentIDs) == 1 {
		return f.DepartmentIDs[0], nil
	}
	return "", ErrDepartmentRequired
}

func (f *DepartmentFilter) SeesWholeWorkspace() bool {
	return f != nil && !f.ShouldFilter()
}

func (f *DepartmentFilter) isOwn(departmentID string) bool {
	if departmentID == "" {
		return false
	}
	for _, id := range f.DepartmentIDs {
		if id == departmentID {
			return true
		}
	}
	return false
}
