package carriermessaging

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	cm "vozko/domain/carriermessaging"
	"vozko/domain/rcs"
	"vozko/domain/sms"
)

var codeShape = regexp.MustCompile(`^(carrier|rcs|sms)_[a-z_]+$`)

func TestEveryRefusalHasAStableCode(t *testing.T) {
	for _, code := range RefusalCodes() {
		if !codeShape.MatchString(code) {
			t.Fatalf("code %q is not a stable snake_case code with a channel prefix", code)
		}
	}
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body = %q", rec.Body.String())
	}
	return body
}

func TestWriteErrorMapsWrappedDomainErrors(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{fmt.Errorf("card 2: %w", rcs.ErrSuggestionInvalid), http.StatusUnprocessableEntity, "rcs_button_invalid"},
		{sms.ErrRouteCannotSend, http.StatusConflict, "sms_route_paused"},
		{&cm.ShortfallError{MissingMicros: 5}, http.StatusPaymentRequired, "carrier_insufficient_balance"},
		{&cm.QuietHoursError{NextOpen: time.Date(2026, 10, 22, 11, 0, 0, 0, time.UTC)}, http.StatusConflict, "carrier_quiet_hours"},
		{errors.New("boom"), http.StatusInternalServerError, "carrier_request_failed"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		writeError(rec, tc.err, "test")
		body := decodeError(t, rec)
		if rec.Code != tc.status || body["code"] != tc.code {
			t.Fatalf("%v = %d %v, want %d %s", tc.err, rec.Code, body, tc.status, tc.code)
		}
	}
}
