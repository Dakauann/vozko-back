package commentautomationhttp

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	ca "vozko/domain/commentautomation"
	"vozko/domain/privatereply"
)

func TestSharedErrorsMapToStableCodes(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{privatereply.ErrUsed, http.StatusConflict, "private_reply_used"},
		{privatereply.ErrExpired, http.StatusConflict, "private_reply_expired"},
		{privatereply.ErrDeadlineUnknown, http.StatusConflict, "private_reply_deadline_unknown"},
		{ca.ErrRuleNotFound, http.StatusNotFound, "not_found"},
		{fmt.Errorf("%w: like on instagram", ca.ErrActionUnsupported), http.StatusBadRequest, "invalid_rule"},
		{ca.ErrReplyEmpty, http.StatusBadRequest, "invalid_rule"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		if !WriteError(rec, tc.err) {
			t.Fatalf("%v not handled", tc.err)
		}
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.code) {
			t.Errorf("%v -> %d %s", tc.err, rec.Code, rec.Body.String())
		}
	}
	if WriteError(httptest.NewRecorder(), errors.New("other")) {
		t.Fatal("unrelated errors are left to the caller")
	}
}
