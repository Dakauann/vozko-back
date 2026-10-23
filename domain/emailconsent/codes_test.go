package emailconsent

import (
	"fmt"
	"testing"
)

func TestEveryKnownErrorHasAUniqueCode(t *testing.T) {
	seen := map[string]bool{}
	for _, err := range KnownErrors() {
		code := ErrorCode(fmt.Errorf("wrapped: %w", err))
		if code == "" {
			t.Errorf("%v has no code", err)
		}
		if seen[code] {
			t.Errorf("code %q is used twice", code)
		}
		seen[code] = true
	}
	if ErrorCode(fmt.Errorf("unrelated")) != "" {
		t.Fatal("unknown errors have no code")
	}
}
