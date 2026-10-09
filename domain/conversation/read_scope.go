package conversation

import (
	"errors"
	"slices"
)

var ErrDepartmentOutsideScope = errors.New("conversation: the selected department is outside the actor's scope")

type ReadScope struct {
	DepartmentIDs          []string
	RestrictDepartments    bool
	AssigneeOverrideUserID string
}

func (s DepartmentAccessScope) ReadScope(actorID string, isAdmin bool, selectedDepartmentID string) (ReadScope, error) {
	read := ReadScope{DepartmentIDs: s.DepartmentIDs, RestrictDepartments: s.Restrict}
	if selectedDepartmentID != "" && (s.Restrict || s.WorkspaceHasDepartments) {
		if s.Restrict && !slices.Contains(s.DepartmentIDs, selectedDepartmentID) {
			return ReadScope{}, ErrDepartmentOutsideScope
		}
		read.DepartmentIDs = []string{selectedDepartmentID}
		read.RestrictDepartments = true
	}
	if !isAdmin && read.RestrictDepartments {
		read.AssigneeOverrideUserID = actorID
	}
	return read, nil
}
