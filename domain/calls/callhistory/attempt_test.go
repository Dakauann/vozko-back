package callhistory

import (
	"testing"

	"vozko/domain/calls/cdr"
)

func TestOnlyACallPlacedToTheLeadCountsAsAnAttempt(t *testing.T) {
	cases := []struct {
		name string
		call cdr.Call
		want bool
	}{
		{"an outbound call", cdr.Call{Direction: cdr.DirectionOutbound}, true},
		{"a call from the lead", cdr.Call{Direction: cdr.DirectionInbound}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsAttempt(tc.call); got != tc.want {
				t.Fatalf("IsAttempt = %v, want %v", got, tc.want)
			}
		})
	}
}
