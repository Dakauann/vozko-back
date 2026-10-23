package webhookanswer

import (
	"net/http"
	"testing"
)

func TestEveryRejectionReasonAnswersWithTheStatusTheCallerSees(t *testing.T) {
	cases := map[string]int{
		"webhook_inactive":      http.StatusNotFound,
		"webhook_not_found":     http.StatusNotFound,
		"method_not_allowed":    http.StatusMethodNotAllowed,
		"unauthorized":          http.StatusUnauthorized,
		"entry_required":        http.StatusBadRequest,
		"entry_not_found":       http.StatusNotFound,
		"entry_forbidden":       http.StatusForbidden,
		"lead_contact_required": http.StatusBadRequest,
		"lead_phone_invalid":    http.StatusUnprocessableEntity,
		"lead_not_found":        http.StatusNotFound,
		"trigger_invalid":       http.StatusUnprocessableEntity,
		"no_trigger_node":       http.StatusUnprocessableEntity,
		"workflow_not_active":   http.StatusConflict,
		"workspace_at_capacity": http.StatusTooManyRequests,
		"internal_error":        http.StatusInternalServerError,
		"anything_else":         http.StatusInternalServerError,
	}
	for reason, want := range cases {
		if got := RejectionStatus(reason); got != want {
			t.Errorf("RejectionStatus(%s) = %d, want %d", reason, got, want)
		}
	}
}

func TestAcceptedOutcomesAnswerWithTheirStatus(t *testing.T) {
	if AcceptStatus("accepted") != http.StatusAccepted || AcceptStatus("duplicate") != http.StatusOK || AcceptStatus("already_running") != http.StatusOK || AcceptStatus("ignored") != http.StatusAccepted {
		t.Fatal("accepted is 202, duplicate and already running are 200")
	}
}
