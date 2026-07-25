package notification_service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/resend/resend-go/v3"
)

func TestEmailServiceFrom(t *testing.T) {
	cases := []struct {
		name      string
		fromEmail string
		fromName  string
		want      string
	}{
		{"name and email", "no-reply@vozkoglobal.com", "Vozko", "Vozko <no-reply@vozkoglobal.com>"},
		{"email only", "no-reply@vozkoglobal.com", "", "no-reply@vozkoglobal.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &EmailService{fromEmail: tc.fromEmail, fromName: tc.fromName}
			if got := svc.from(); got != tc.want {
				t.Fatalf("from() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseRecipients(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"single", "a@x.com", []string{"a@x.com"}},
		{"comma separated with spaces", "a@x.com, b@y.com ,c@z.com", []string{"a@x.com", "b@y.com", "c@z.com"}},
		{"empty entries dropped", "a@x.com,,  ,b@y.com", []string{"a@x.com", "b@y.com"}},
		{"blank", "   ", []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseRecipients(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("parseRecipients(%q) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}

// Without an API key the service must fail closed rather than panic.
func TestSendEmailWithoutClient(t *testing.T) {
	svc := NewEmailService(nil, "", "onboarding@resend.dev", "Vozko", 0)
	if err := svc.SendEmail("a@x.com", "subject", "<p>body</p>"); err == nil {
		t.Fatal("expected error when RESEND_API_KEY is unset, got nil")
	}
}

func TestIsRetryableSendError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"rate limit", &resend.RateLimitError{RetryAfter: "1"}, true},
		{"deadline", context.DeadlineExceeded, true},
		{"validation", errors.New("[ERROR]: invalid `to` field"), false},
		{"unknown api error", errors.New("[ERROR]: something"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isRetryableSendError(tc.err); got != tc.want {
				t.Fatalf("isRetryableSendError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestSendBackoff(t *testing.T) {
	// Honour Retry-After when present and within cap.
	if got := sendBackoff(1, &resend.RateLimitError{RetryAfter: "2"}); got != 2*time.Second {
		t.Fatalf("retry-after backoff = %v, want 2s", got)
	}
	// Cap an excessive Retry-After.
	if got := sendBackoff(1, &resend.RateLimitError{RetryAfter: "100"}); got != emailSendMaxBackoff {
		t.Fatalf("capped retry-after = %v, want %v", got, emailSendMaxBackoff)
	}
	// Exponential growth for generic transient errors.
	if got := sendBackoff(1, errors.New("boom")); got != emailSendBaseBackoff {
		t.Fatalf("attempt 1 backoff = %v, want %v", got, emailSendBaseBackoff)
	}
	if got := sendBackoff(3, errors.New("boom")); got != emailSendBaseBackoff<<2 {
		t.Fatalf("attempt 3 backoff = %v, want %v", got, emailSendBaseBackoff<<2)
	}
}
