package user

import "strings"

var superAdminEmails = []string{
	"dakauannc@gmail.com",
	"dakauannc@vozkoia.com",
}

func IsSuperAdmin(email string) bool {
	normalized := strings.ToLower(strings.TrimSpace(email))
	if normalized == "" {
		return false
	}
	for _, allowed := range superAdminEmails {
		if normalized == allowed {
			return true
		}
	}
	return false
}
