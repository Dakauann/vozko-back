package shared

import "strings"

func OptionalID(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func OptionalIDOf(value string) *string {
	return OptionalID(&value)
}
