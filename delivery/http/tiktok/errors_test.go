package tiktok

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	ttdomain "vozko/domain/tiktok"
	"vozko/usecases/shared/automationrefs"
	ttuc "vozko/usecases/tiktok"
)

var errThrottled = errors.New("throttled")

func TestDomainErrorsMapToHTTPStatuses(t *testing.T) {
	limited := func(err error) bool { return errors.Is(err, errThrottled) }
	cases := []struct {
		err  error
		want int
	}{
		{ttdomain.ErrAccountNotFound, http.StatusNotFound},
		{fmt.Errorf("x: %w", ttdomain.ErrCommentNotOwned), http.StatusNotFound},
		{ttdomain.ErrAccountAlreadyLinked, http.StatusConflict},
		{ttuc.ErrAccountUnavailable, http.StatusConflict},
		{ttdomain.ErrNeedsReconnect, http.StatusConflict},
		{ttdomain.ErrCommentTextTooLong, http.StatusUnprocessableEntity},
		{automationrefs.ErrNotInWorkspace, http.StatusUnprocessableEntity},
		{errThrottled, http.StatusTooManyRequests},
		{errors.New("tiktok exploded"), http.StatusBadGateway},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		writeDomainError(rec, tc.err, "falhou", limited)
		if rec.Code != tc.want {
			t.Errorf("%v: status %d, want %d", tc.err, rec.Code, tc.want)
		}
	}
}

func TestConnectErrorCodesNameTheReason(t *testing.T) {
	cases := map[error]string{
		ttuc.ErrAuthorizationDeclined:    "declined",
		ttuc.ErrAuthorizationCode:        "code_missing",
		ttdomain.ErrScopeMissing:         "scope_missing",
		ttdomain.ErrNotEligibleAccount:   "not_eligible_account",
		ttdomain.ErrAccountAlreadyLinked: "already_linked",
		ttdomain.ErrIdentityMismatch:     "identity_mismatch",
		ttuc.ErrProbeInconclusive:        "probe_inconclusive",
		errors.New("other"):              "connect_failed",
	}
	for err, want := range cases {
		if got := connectErrorCode(fmt.Errorf("wrapped: %w", err)); got != want {
			t.Errorf("%v: code %q, want %q", err, got, want)
		}
	}
}
