package call_roulette

import "vozko/domain/shared"

type EntryType string

const (
	EntryTypeVoice EntryType = "voice"
)

type RouletteState struct {
	ID                 string
	WorkspaceID        string
	SIPTrunkID         string
	DepartmentID       string
	LastAssignedUserID string
}

type Repository interface {
	GetState(workspaceID, sipTrunkID, departmentID string) (*RouletteState, error)
	SaveState(state *RouletteState) error
	Assign(entryID string, entryType EntryType, workspaceID, sipTrunkID, departmentID, assignedUserID string) error
	FindByEntry(workspaceID, entryID string, entryType EntryType) (*CallAssignment, error)
	Unassign(workspaceID, entryID string, entryType EntryType) error
	ListByUser(workspaceID, userID string) ([]*CallAssignment, error)
}

type CallAssignment struct {
	ID             string    `json:"id"`
	WorkspaceID    string    `json:"workspaceId"`
	SIPTrunkID     string    `json:"sipTrunkId"`
	EntryID        string    `json:"entryId"`
	EntryType      EntryType `json:"entryType"`
	AssignedUserID string    `json:"assignedUserId"`
	CreatedAt      string    `json:"createdAt"`
	UpdatedAt      string    `json:"updatedAt"`
}

type EligibleUserProvider interface {
	GetEligibleUsersForWorkspace(workspaceID string, skipAdmins bool) []string
	GetEligibleUsersForWorkspaceDepartment(workspaceID, departmentID string, skipAdmins bool) []string
}

type WorkspaceConfigProvider interface {
	GetByWorkspaceID(workspaceID string) (SkipAdminAssignment bool, err error)
}

type CampaignWorkspaceResolver interface {
	GetEntryWorkspaceID(entryID string, entryType string) (string, error)
	GetEntryDepartmentID(entryID string, entryType string) (string, error)
	GetEntrySIPTrunkID(entryID string) (string, error)
}

func init() {
	var _ shared.PaginatedResult[*CallAssignment]
}
