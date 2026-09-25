package shared

import "strings"

type EntryAccessChecker interface {
	CanAccessEntry(userID, workspaceID, entryID, entryType string, isAdmin bool) bool
}

type Person struct {
	UserID      string
	SystemAdmin bool
}

func (p Person) MayActOn(access EntryAccessChecker, workspaceID, entryID, entryType string) bool {
	if strings.TrimSpace(p.UserID) == "" || !EntryType(entryType).SupportsConversationView() {
		return false
	}
	return access != nil && access.CanAccessEntry(p.UserID, workspaceID, entryID, entryType, p.SystemAdmin)
}
