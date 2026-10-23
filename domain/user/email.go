package user

import "strings"

func CanonicalEmail(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}
