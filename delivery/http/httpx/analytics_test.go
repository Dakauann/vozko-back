package httpx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/cache"
)

func TestAnalyticsLimitsAnswerWithTheirOwnStatus(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		status     int
		retryAfter string
	}{
		{"a full gate asks the client to retry", fmt.Errorf("summary: %w", cache.ErrGateBusy), http.StatusServiceUnavailable, "5"},
		{"a compute past its deadline is a gateway timeout", context.DeadlineExceeded, http.StatusGatewayTimeout, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()

			if !WriteAnalyticsLimit(rec, tc.err, "dispatch report") {
				t.Fatal("WriteAnalyticsLimit() = false, want the limit handled")
			}
			if rec.Code != tc.status || rec.Header().Get("Retry-After") != tc.retryAfter {
				t.Fatalf("status = %d, Retry-After = %q; want %d, %q", rec.Code, rec.Header().Get("Retry-After"), tc.status, tc.retryAfter)
			}
		})
	}
}

func TestAnyOtherErrorIsLeftToTheCaller(t *testing.T) {
	rec := httptest.NewRecorder()

	if WriteAnalyticsLimit(rec, errors.New("boom"), "dispatch report") {
		t.Fatal("WriteAnalyticsLimit() = true for an unrelated error")
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("wrote %q for an error it does not own", rec.Body.String())
	}
}
