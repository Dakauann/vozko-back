package tiktok

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestErrorClassification(t *testing.T) {
	cases := []struct {
		name                                                     string
		err                                                      error
		auth, perm, rate, budget, notFound, invalid, retry, sure bool
	}{
		{"invalid token", &Error{Code: CodeInvalidToken}, true, false, false, false, false, false, false, true},
		{"http 401", &Error{HTTPStatus: 401}, true, false, false, false, false, false, false, true},
		{"no permission", &Error{Code: CodeNoPermission}, false, true, false, false, false, false, false, true},
		{"throttled", &Error{Code: CodeRateLimited}, false, false, true, false, false, false, true, true},
		{"local limit", &Error{Code: CodeLocalRateLimited}, false, false, true, false, false, false, true, true},
		{"limiter down", &Error{Code: CodeLimiterUnavailable}, false, false, false, false, false, false, true, true},
		{"budget", &Error{Code: CodeBudgetBlocked}, false, false, false, true, false, false, false, true},
		{"not found", &Error{Code: CodeNotFound}, false, false, false, false, true, false, false, true},
		{"bad parameter", &Error{Code: CodeInvalidParameter}, false, false, false, false, false, true, false, true},
		{"system error on a read", &Error{Code: CodeSystemError}, false, false, false, false, false, false, true, false},
		{"system error on a send", &Error{Code: CodeSystemError, Ambiguous: true}, false, false, false, false, false, false, false, false},
		{"transport on a send", &Error{Code: CodeTransport, Ambiguous: true}, false, false, false, false, false, false, false, false},
		{"http 502 on a read", &Error{HTTPStatus: 502}, false, false, false, false, false, false, true, false},
		{"plain error", errors.New("boom"), false, false, false, false, false, false, false, false},
		{"wrapped budget", fmt.Errorf("send: %w", &Error{Code: CodeBudgetBlocked}), false, false, false, true, false, false, false, true},
	}
	for _, tc := range cases {
		got := []bool{IsAuth(tc.err), IsPermission(tc.err), IsRateLimited(tc.err), IsBudgetBlocked(tc.err), IsNotFound(tc.err), IsInvalidRequest(tc.err), IsRetryable(tc.err), IsDefiniteRefusal(tc.err)}
		want := []bool{tc.auth, tc.perm, tc.rate, tc.budget, tc.notFound, tc.invalid, tc.retry, tc.sure}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%s: predicate %d = %v, want %v", tc.name, i, got[i], want[i])
			}
		}
	}
}

func TestErrorMessageCarriesTheRequestIDButNeverATokenField(t *testing.T) {
	e := &Error{Code: CodeInvalidToken, HTTPStatus: 200, Message: "Invalid access token", RequestID: "202310190000"}
	msg := e.Error()
	if !strings.Contains(msg, "40105") || !strings.Contains(msg, "202310190000") {
		t.Fatalf("message = %q", msg)
	}
	if strings.Contains(strings.ToLower(msg), "access-token:") {
		t.Fatalf("message leaks a header: %q", msg)
	}
}
