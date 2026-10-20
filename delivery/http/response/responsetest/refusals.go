package responsetest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"vozko/delivery/http/response"
)

var snakeCase = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

func CodeOf(t testing.TB, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return body.Code
}

func RequireStableCodes(t testing.TB, refusals []response.Refusal) {
	t.Helper()
	for _, r := range refusals {
		if !snakeCase.MatchString(r.Code) {
			t.Errorf("refusal %q (%d) needs a stable snake_case code, got %q", r.Message, r.Status, r.Code)
		}
		if r.Message == "" || r.Status < http.StatusBadRequest {
			t.Errorf("refusal %q needs a fallback message and an error status, got %d %q", r.Code, r.Status, r.Message)
		}
	}
}

func RequireEveryErrorWritesItsCode(t testing.TB, table []response.ErrorRefusal, write func(http.ResponseWriter, error)) {
	t.Helper()
	RequireStableCodes(t, response.ErrorRefusalsOf(table))
	for _, entry := range table {
		rec := httptest.NewRecorder()
		write(rec, entry.Err)
		if got := CodeOf(t, rec); got == "" || rec.Code != entry.Refusal.Status {
			t.Errorf("%v wrote %d %q, want %d %q", entry.Err, rec.Code, got, entry.Refusal.Status, entry.Refusal.Code)
		}
	}
}
