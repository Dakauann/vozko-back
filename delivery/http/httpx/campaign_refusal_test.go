package httpx

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/campaign"
	"vozko/usecases/campaignguard"
)

func TestWriteSelectionSendRefusal(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "a locked selection send", err: campaign.ErrSelectionSendLocked, status: http.StatusConflict, code: "send_selection_locked"},
		{name: "a selection send started outside its review", err: fmt.Errorf("dispatch: %w", campaign.ErrSelectionStartNeedsReview), status: http.StatusConflict, code: "send_start_from_leads"},
		{name: "no gate", err: campaignguard.ErrUnavailable, status: http.StatusServiceUnavailable, code: "campaign_guard_unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			if !WriteSelectionSendRefusal(rec, tc.err) {
				t.Fatal("the refusal was not written")
			}
			if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.code) {
				t.Fatalf("status %d body %s, want %d %s", rec.Code, rec.Body.String(), tc.status, tc.code)
			}
		})
	}
	if WriteSelectionSendRefusal(httptest.NewRecorder(), campaign.ErrAlreadyRunning) {
		t.Fatal("another error was written as a selection send refusal")
	}
}
