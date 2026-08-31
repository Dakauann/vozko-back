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

	// CompareAndSwapRoundRobinState advances the pointer only if it still holds
	// `expected`, reporting false when another writer moved it first.
	//
	// Reading the pointer, computing the next owner, and writing it back is
	// three statements, and two inbound messages arriving together used to run
	// all three interleaved: both read the same pointer and both drew the same
	// agent. That was invisible while the pool was whoever happened to be
	// connected — often one person — and plainly unfair once the last-seen ring
	// holds everyone who worked today. The swap lets the loser recompute
	// against the pointer that actually won.
	//
	// An empty `expected` means "there is no pointer yet": the implementation
	// inserts, and loses to whoever inserts first.
	CompareAndSwapRoundRobinState(state *RoundRobinState, expected string) (bool, error)
}
