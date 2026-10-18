package buttonaction

import (
	"errors"
	"fmt"
	"testing"
)

func TestEveryRefusalHasAStableCode(t *testing.T) {
	seen := map[string]bool{}
	for _, known := range errorCodes {
		if known.code == "" || seen[known.code] {
			t.Fatalf("code %q is empty or repeated", known.code)
		}
		seen[known.code] = true
		if got := ErrorCode(fmt.Errorf("wrapped: %w", known.err)); got != known.code {
			t.Errorf("%v: code = %q, want %q", known.err, got, known.code)
		}
	}
	if ErrorCode(nil) != "" || ErrorCode(errors.New("other")) != "" {
		t.Fatal("unknown errors carry no code")
	}
}
