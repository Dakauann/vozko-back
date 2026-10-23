package user

import "testing"

func TestCanonicalEmailIgnoresCaseAndSurroundingSpaces(t *testing.T) {
	if got := CanonicalEmail("  Maria.Silva@Gmail.COM "); got != "maria.silva@gmail.com" {
		t.Fatalf("got %q", got)
	}
}
