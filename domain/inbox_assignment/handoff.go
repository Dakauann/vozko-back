package inbox_assignment

import "errors"

var (
	// ErrDepartmentOutOfScope: the department does not exist or belongs to
	// another workspace. A hand-off never reaches outside the conversation's
	// workspace, whatever id a workflow variable produced.
	ErrDepartmentOutOfScope = errors.New("inbox assignment: department is not in this workspace")
	// ErrHandOffTargetNoAccess: the person named cannot open conversations in
	// this workspace, so handing them one would leave it unanswered.
	ErrHandOffTargetNoAccess = errors.New("inbox assignment: hand-off target has no conversation access")
)

// RouletteHandOff asks the roulette, the same ring inbound conversations use,
// to deal a conversation to the next eligible person.
type RouletteHandOff struct {
	WorkspaceID string
	EntryID     string
	EntryType   string
	// DepartmentID draws from that department's ring. Empty means the
	// conversation's own department; a person already holding it then keeps it.
	DepartmentID string
	// ByActorID is recorded as who handed it off. Empty means the automation
	// holding the conversation, or the system.
	ByActorID string
}
