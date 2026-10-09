package conversation

import "vozko/domain/shared"

type EntryAccess interface {
	CanAccess(entryID, entryType string) bool
}

type EntryPlacement struct {
	WorkspaceID  string
	DepartmentID string
}

type EntryVisibility interface {
	VisibleEntries(refs []shared.EntryRef) (map[shared.EntryRef]bool, error)
}
