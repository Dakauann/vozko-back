package shared

import "strings"

const (
	contactMaskShown = 4
	contactMask      = "••••"
)

func MaskContact(contact string) string {
	runes := []rune(strings.TrimSpace(contact))
	if len(runes) == 0 {
		return ""
	}
	if len(runes) <= contactMaskShown {
		return contactMask
	}
	return contactMask + string(runes[len(runes)-contactMaskShown:])
}
