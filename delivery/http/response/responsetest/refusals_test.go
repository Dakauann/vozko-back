package responsetest

import (
	"errors"
	"net/http"
	"testing"

	"vozko/delivery/http/response"
)

type recorder struct {
	testing.TB
	failures int
}

func (r *recorder) Errorf(string, ...any) { r.failures++ }

func (r *recorder) Helper() {}

var errGone = errors.New("gone")

func TestRequireEveryErrorWritesItsCode(t *testing.T) {
	table := []response.ErrorRefusal{{Err: errGone, Refusal: response.Refusal{Status: http.StatusNotFound, Code: "thing_gone", Message: "Sumiu"}}}
	tests := []struct {
		name     string
		write    func(http.ResponseWriter, error)
		failures int
	}{
		{"a writer that uses the table passes", func(w http.ResponseWriter, err error) { response.WriteErrorRefusal(w, err, table) }, 0},
		{"a writer that drops the code fails", func(w http.ResponseWriter, err error) {
			response.WriteError(w, http.StatusNotFound, "Sumiu", nil)
		}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &recorder{TB: t}
			RequireEveryErrorWritesItsCode(r, table, tc.write)
			if r.failures != tc.failures {
				t.Fatalf("failures = %d, want %d", r.failures, tc.failures)
			}
		})
	}
}

func TestRequireStableCodesRejectsCodesPeopleCannotTranslate(t *testing.T) {
	tests := []struct {
		name     string
		refusal  response.Refusal
		failures int
	}{
		{"snake case passes", response.Refusal{Status: http.StatusConflict, Code: "run_terminal", Message: "Finalizada"}, 0},
		{"an empty code fails", response.Refusal{Status: http.StatusConflict, Message: "Finalizada"}, 1},
		{"a sentence as code fails", response.Refusal{Status: http.StatusConflict, Code: "Run Terminal", Message: "Finalizada"}, 1},
		{"a refusal without fallback copy fails", response.Refusal{Status: http.StatusConflict, Code: "run_terminal"}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &recorder{TB: t}
			RequireStableCodes(r, []response.Refusal{tc.refusal})
			if r.failures != tc.failures {
				t.Fatalf("failures = %d, want %d", r.failures, tc.failures)
			}
		})
	}
}
