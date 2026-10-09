package leadaction

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorCode(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, ""},
		{ErrUnknownAction, "lead_action_unknown"},
		{fmt.Errorf("wrapped: %w", ErrParamsAmbiguous), "lead_action_params_ambiguous"},
		{ErrIdempotencyKeyReused, "idempotency_key_reused"},
		{ErrForbidden, "forbidden"},
		{errors.New("other"), ""},
	}
	for _, tc := range cases {
		if got := ErrorCode(tc.err); got != tc.want {
			t.Errorf("ErrorCode(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestEveryRefusalHasItsOwnCode(t *testing.T) {
	seen := map[string]error{}
	for _, known := range errorCodes {
		if other, dup := seen[known.code]; dup {
			t.Fatalf("%v and %v share the code %q", other, known.err, known.code)
		}
		seen[known.code] = known.err
	}
}
