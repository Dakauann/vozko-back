package inbox_assignment

import "errors"

var (
	ErrDepartmentOutOfScope  = errors.New("inbox assignment: department is not in this workspace")
	ErrHandOffTargetNoAccess = errors.New("inbox assignment: hand-off target has no conversation access")
)

type RouletteHandOff struct {
	WorkspaceID  string
	EntryID      string
	EntryType    string
	DepartmentID string
	ByActorID    string
}
