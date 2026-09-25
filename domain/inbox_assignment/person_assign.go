package inbox_assignment

import (
	"errors"

	"vozko/domain/shared"
)

var (
	ErrAssignEntryAccess      = errors.New("inbox assignment: no access to this conversation")
	ErrAssignTargetIneligible = errors.New("inbox assignment: the member has no conversation access in this workspace")
	ErrAssignTargetOutOfReach = errors.New("inbox assignment: the member is outside the caller's departments")
)

type PersonAssignUseCase interface {
	Assign(by shared.Person, workspaceID, entryID, entryType, toUserID string) error
	HandOff(by shared.Person, workspaceID, entryID, entryType, departmentID string) (string, error)
}
