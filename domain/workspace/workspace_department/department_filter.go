package workspace_department

type DepartmentFilter struct {
	IsOwnerOrAdmin bool

	DepartmentIDs []string

	SelectedDepartmentID *string

	WorkspaceHasDepartments bool
}

func (f *DepartmentFilter) ShouldFilter() bool {
	if f == nil || f.IsOwnerOrAdmin {
		return false
	}
	return f.hasWorkspaceDepartments()
}

func (f *DepartmentFilter) EffectiveDepartmentIDs() []string {
	if f == nil {
		return nil
	}
	if f.SelectedDepartmentID != nil && *f.SelectedDepartmentID != "" {
		return []string{*f.SelectedDepartmentID}
	}
	if f.IsOwnerOrAdmin || !f.hasWorkspaceDepartments() {
		return nil
	}
	return f.DepartmentIDs
}

func (f *DepartmentFilter) DepartmentIDForCreation() (string, error) {
	if f == nil || f.IsOwnerOrAdmin {

		if f != nil && f.SelectedDepartmentID != nil && *f.SelectedDepartmentID != "" {
			return *f.SelectedDepartmentID, nil
		}
		return "", nil
	}
	if !f.hasWorkspaceDepartments() {
		return "", nil
	}
	if f.SelectedDepartmentID != nil && *f.SelectedDepartmentID != "" {
		return *f.SelectedDepartmentID, nil
	}
	if len(f.DepartmentIDs) == 0 {
		return "", ErrDepartmentAccessDenied
	}
	if len(f.DepartmentIDs) == 1 {
		return f.DepartmentIDs[0], nil
	}
	return "", ErrDepartmentRequired
}

func (f *DepartmentFilter) hasWorkspaceDepartments() bool {
	if f == nil {
		return false
	}
	if f.WorkspaceHasDepartments {
		return true
	}
	if len(f.DepartmentIDs) > 0 {
		return true
	}
	return f.SelectedDepartmentID != nil && *f.SelectedDepartmentID != ""
}
