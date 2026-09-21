package conversation

import (
	"fmt"
	"strings"

	"vozko/domain/shared"
)

func SignOutbound(entryType shared.EntryType, username, text string) string {
	username = strings.TrimSpace(username)
	if username == "" {
		return text
	}
	if entryType == shared.EntryTypeInstagram {
		return fmt.Sprintf("%s:\n%s", username, text)
	}
	return fmt.Sprintf("*%s*:\n%s", username, text)
}
