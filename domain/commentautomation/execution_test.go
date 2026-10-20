package commentautomation

import (
	"errors"
	"fmt"
	"testing"
)

func TestOnlyFailuresBeforeTheClaimAreWorthRetrying(t *testing.T) {
	cause := errors.New("connection reset")
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"no error", nil, false},
		{"rules unavailable", fmt.Errorf("%w: %w", ErrRulesUnavailable, cause), true},
		{"claim unavailable", fmt.Errorf("%w: %w", ErrExecutionUnclaimed, cause), true},
		{"action failed after the claim", fmt.Errorf("%w: %w", ErrActionFailed, cause), false},
		{"several actions failed", errors.Join(fmt.Errorf("%w: a", ErrActionFailed), fmt.Errorf("%w: b", ErrActionFailed)), false},
		{"comment without id", ErrCommentIDRequired, false},
		{"anything unclassified", cause, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Retryable(tc.err); got != tc.want {
				t.Fatalf("Retryable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
