package smartcampaignhttp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/delivery/http/response"
	sc "vozko/domain/whatsapp_campaign/smartcampaign"
)

func TestABlockedStartExplainsWhichSignalBlocksIt(t *testing.T) {
	rec := httptest.NewRecorder()
	blocked := &sc.BlockedError{Decision: sc.Decision{Action: sc.Stop, Findings: []sc.Finding{
		{Rule: sc.RuleTemplatePaused, Action: sc.Stop}, {Rule: sc.RuleTemplateFlagged, Action: sc.Pause},
	}}}

	if !WriteRefusal(rec, fmt.Errorf("start: %w", blocked)) {
		t.Fatal("a blocked start was not handled")
	}

	var body response.ErrorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusConflict || body.Code != "smart_campaign_blocked" || body.Expected["reasons"] != "template_paused,template_flagged" {
		t.Fatalf("status %d, body %+v", rec.Code, body)
	}
}

func TestABusyCampaignIsAskedToRetry(t *testing.T) {
	rec := httptest.NewRecorder()
	if !WriteRefusal(rec, sc.ErrCampaignBusy) || rec.Code != http.StatusConflict {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestOtherErrorsAreLeftToTheCaller(t *testing.T) {
	if WriteRefusal(httptest.NewRecorder(), errors.New("anything else")) {
		t.Fatal("an unrelated error was swallowed")
	}
}

func TestAnInvalidPolicyIsABadRequestWithItsProblems(t *testing.T) {
	rec := httptest.NewRecorder()
	invalid := fmt.Errorf("%w: send hours are required", sc.ErrInvalidPolicy)

	if !WriteRefusal(rec, invalid) {
		t.Fatal("an invalid policy was not handled")
	}

	var body response.ErrorResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusBadRequest || body.Code != "smart_campaign_invalid_policy" || body.Message != invalid.Error() {
		t.Fatalf("status %d, body %+v", rec.Code, body)
	}
}
