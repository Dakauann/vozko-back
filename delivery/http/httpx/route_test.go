package httpx

import (
	"regexp"
	"testing"
)

func TestTheUUIDRoutePatternMatchesOnlyAUUID(t *testing.T) {
	pattern := regexp.MustCompile("^" + UUIDPattern + "$")
	for value, want := range map[string]bool{
		"0b7c1f9e-5a2d-4c8e-9f31-6d2a7b8c9e01": true,
		"0B7C1F9E-5A2D-4C8E-9F31-6D2A7B8C9E01": true,
		"0b7c1f9e5a2d4c8e9f316d2a7b8c9e01":     false,
		"not-a-uuid":                           false,
	} {
		if got := pattern.MatchString(value); got != want {
			t.Fatalf("%q matched %v, want %v", value, got, want)
		}
	}
}
