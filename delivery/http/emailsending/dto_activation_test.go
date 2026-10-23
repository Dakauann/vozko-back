package emailsending

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	es "vozko/domain/emailsending"
	esuc "vozko/usecases/emailsending"
)

func TestSetupSaysWhetherActivationIsAllowed(t *testing.T) {
	cases := []struct {
		name    string
		refusal error
		want    ActivationResponse
	}{
		{"paid plan", nil, ActivationResponse{Allowed: true}},
		{"no paid plan", es.ErrPlanRequired, ActivationResponse{ErrorCode: "email_plan_required"}},
		{"plan unreadable", fmt.Errorf("%w: timeout", es.ErrPlanUnreadable), ActivationResponse{ErrorCode: "email_plan_unavailable"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := presentSetup(esuc.StatusView{Activation: tc.refusal}).Activation
			if got != tc.want {
				t.Fatalf("activation = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestPlanRefusalsUseTheHouseStatuses(t *testing.T) {
	cases := map[error]int{es.ErrPlanRequired: http.StatusPaymentRequired, es.ErrPlanUnreadable: http.StatusServiceUnavailable}
	for err, status := range cases {
		rec := httptest.NewRecorder()
		writeError(rec, fmt.Errorf("provision: %w", err), "test")
		if rec.Code != status {
			t.Fatalf("%v: status %d, want %d", err, rec.Code, status)
		}
	}
}
