package conversation

import (
	"errors"
	"fmt"
	"testing"
)

func TestSendFailureCode(t *testing.T) {
	cause := errors.New("wallet short")
	refusal := &SendRefusal{Code: "email_balance_insufficient", Err: cause}
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"plain error falls back", errors.New("boom"), "send_failed"},
		{"refusal carries its code", refusal, "email_balance_insufficient"},
		{"wrapped refusal keeps its code", fmt.Errorf("adapter: %w", refusal), "email_balance_insufficient"},
		{"refusal without code falls back", &SendRefusal{Err: cause}, "send_failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SendFailureCode(tc.err, "send_failed"); got != tc.want {
				t.Errorf("SendFailureCode = %q, want %q", got, tc.want)
			}
		})
	}
	if !errors.Is(refusal, cause) || refusal.Error() != cause.Error() {
		t.Error("a refusal must unwrap to its cause and read as it")
	}
}
