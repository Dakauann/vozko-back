package workspace_department

import "errors"

var (
	ErrDepartmentNotFound       = errors.New("department not found")
	ErrDepartmentNameRequired   = errors.New("department name is required")
	ErrDepartmentMemberExists   = errors.New("member is already in this department")
	ErrDepartmentMemberNotFound = errors.New("member is not in this department")
	ErrDepartmentRequired       = errors.New("you belong to multiple departments, select one")
	ErrDepartmentAccessDenied   = errors.New("you do not have access to this department")
)
