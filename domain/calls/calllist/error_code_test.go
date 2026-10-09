package calllist

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestErrorCodeNamesEachRefusal(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, ""},
		{fmt.Errorf("wrapped: %w", ErrItemTaken), "call_list_item_taken"},
		{ErrCallNotTheItems, "call_list_call_not_the_items"},
		{ErrForbidden, "forbidden"},
		{errors.New("other"), ""},
	}
	for _, tc := range cases {
		if got := ErrorCode(tc.err); got != tc.want {
			t.Errorf("ErrorCode(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

func TestEveryRefusalHasItsOwnCallListCode(t *testing.T) {
	seen := map[string]error{}
	for _, known := range errorCodes {
		if other, dup := seen[known.code]; dup {
			t.Fatalf("%v and %v share the code %q", other, known.err, known.code)
		}
		seen[known.code] = known.err
		if known.code != "forbidden" && !strings.HasPrefix(known.code, "call_list") {
			t.Errorf("code %q must start with call_list", known.code)
		}
	}
}
