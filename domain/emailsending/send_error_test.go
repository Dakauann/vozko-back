package emailsending

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestSendRetryAfterReadsTheHintOfATransientSendError(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		want   time.Duration
		wantOK bool
	}{
		{name: "transient with a hint", err: &SendError{Kind: SendErrorTransient, RetryAfter: 2 * time.Second}, want: 2 * time.Second, wantOK: true},
		{name: "wrapped", err: fmt.Errorf("send: %w", &SendError{Kind: SendErrorTransient, RetryAfter: time.Minute}), want: time.Minute, wantOK: true},
		{name: "transient without a hint", err: &SendError{Kind: SendErrorTransient}},
		{name: "a paused account is not a throttle", err: &SendError{Kind: SendErrorPaused, RetryAfter: time.Minute}},
		{name: "not a send error", err: errors.New("boom")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := SendRetryAfter(tc.err)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("SendRetryAfter = %s %v, want %s %v", got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestSuppressedErrorKeepsItsReasonAndStillMatchesTheSentinel(t *testing.T) {
	err := fmt.Errorf("send: %w", SuppressedError{Reason: SuppressNoMailDomain})
	if !errors.Is(err, ErrRecipientSuppressed) {
		t.Fatal("every caller that refuses suppressed recipients keeps working")
	}
	if got := SuppressReasonOf(err); got != SuppressNoMailDomain {
		t.Fatalf("reason = %q", got)
	}
	if got := SuppressReasonOf(ErrRecipientSuppressed); got != "" {
		t.Fatalf("a bare sentinel has no reason, got %q", got)
	}
}
