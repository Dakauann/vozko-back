package workspace

import "strings"

func OwnerOrPlatformAdmin(userID, ownerID string, platformAdmin bool) bool {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false
	}
	return platformAdmin || userID == strings.TrimSpace(ownerID)
}
