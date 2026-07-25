package inbox_assignment

type Repository interface {
	FindByEntry(workspaceID, entryID, entryType string) (*InboxAssignment, error)

	FindByEntries(workspaceID string, entryIDs []string) ([]*InboxAssignment, error)

	FindByEntryAndUser(workspaceID, entryID, entryType, userID string) (*InboxAssignment, error)

	Assign(assignment *InboxAssignment) error

	Unassign(workspaceID, entryID, entryType string) error

	ListByUser(workspaceID, userID, entryType string) ([]string, error)

	IsAssignedToUser(workspaceID, entryID, entryType, userID string) (bool, error)

	GetRoundRobinState(workspaceID, businessPhoneID, departmentID string) (*RoundRobinState, error)

	SaveRoundRobinState(state *RoundRobinState) error
}
