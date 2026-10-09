package whatsapp_campaign_entry

import (
	"errors"
	"fmt"
	"testing"

	"vozko/domain/balance"
)

func TestFailureCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"monthly send cap", balance.ErrMonthlySendCapReached, ErrorCodeMonthlySendCapReached},
		{"wrapped monthly send cap", fmt.Errorf("send: %w", balance.ErrMonthlySendCapReached), ErrorCodeMonthlySendCapReached},
		{"send window closed", fmt.Errorf("send: %w", balance.ErrSendWindowClosed), ErrorCodeSendWindowClosed},
		{"insufficient balance keeps no code", balance.ErrInsufficientBalance, 0},
		{"unknown error keeps no code", errors.New("boom"), 0},
		{"nil keeps no code", nil, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := FailureCode(tc.err); got != tc.want {
				t.Errorf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestErrorCodeMonthlySendCapReached_IsStable(t *testing.T) {
	if ErrorCodeMonthlySendCapReached != 900009 {
		t.Fatalf("the frontend maps 900009; got %d", ErrorCodeMonthlySendCapReached)
	}
	if ErrorCodeSendWindowClosed != 900010 {
		t.Fatalf("the frontend maps 900010; got %d", ErrorCodeSendWindowClosed)
	}
}
