package inbox_assignment

import "time"

type RosterProvider interface {
	ListRouletteMembers(workspaceID, departmentID string, skipAdmins bool) ([]string, error)
}

type LastSeenReader interface {
	LastSeen(workspaceID string, userIDs []string) (map[string]time.Time, error)
}

type EntryAttentionReader interface {
	AttendedSince(entryID, entryType, assignedUserID string, since time.Time) (bool, error)
}
