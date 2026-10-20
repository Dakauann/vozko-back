package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"

	"vozko/domain/campaign"
	wc "vozko/domain/whatsapp_campaign"
)

func TestCampaignEditRefusalCodesAreStable(t *testing.T) {
	want := []string{
		"campaign_phone_unavailable",
		"campaign_receptive_managed_by_number",
		"campaign_running",
		"campaign_schedule_in_past",
		"campaign_schedule_out_of_window",
		"campaign_template_not_ready",
		"campaign_template_phone_mismatch",
		"campaign_template_unavailable",
		"campaign_template_variables_missing",
	}
	if got := CampaignEditRefusalCodes(); !reflect.DeepEqual(got, want) {
		t.Fatalf("codes = %v, want %v", got, want)
	}
}

func TestEveryEditRefusalIsWrittenWithItsCode(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{wc.ErrCampaignScheduledStartInvalid, http.StatusBadRequest, "campaign_schedule_in_past"},
		{wc.ErrCampaignScheduledStartTooSoon, http.StatusBadRequest, "campaign_schedule_out_of_window"},
		{wc.ErrCampaignRunning, http.StatusConflict, "campaign_running"},
		{wc.NewTemplateNotReadyError("boas_vindas", "paused"), http.StatusUnprocessableEntity, "campaign_template_not_ready"},
		{wc.ErrCampaignTemplateNotApproved, http.StatusUnprocessableEntity, "campaign_template_not_ready"},
		{wc.ErrCampaignTemplatePhoneMismatch, http.StatusUnprocessableEntity, "campaign_template_phone_mismatch"},
		{wc.ErrCampaignTemplateVariablesMismatch, http.StatusBadRequest, "campaign_template_variables_missing"},
		{wc.ErrCampaignBusinessPhoneNoAccess, http.StatusForbidden, "campaign_phone_unavailable"},
		{wc.ErrReceptiveManagedByNumber, http.StatusConflict, "campaign_receptive_managed_by_number"},
		{fmt.Errorf("%w: key %q missing for lead 1", campaign.ErrWorkflowVarsMissing, "escola"), http.StatusBadRequest, "campaign_workflow_vars_missing"},
		{campaign.ErrWorkflowForbidden, http.StatusForbidden, "campaign_workflow_forbidden"},
		{campaign.ErrAutomationUnavailable, http.StatusServiceUnavailable, "campaign_automation_unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			rec := httptest.NewRecorder()
			(&WhatsAppCampaignHandler{}).handleDomainError(rec, tc.err)
			var body struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("body %q: %v", rec.Body.String(), err)
			}
			if rec.Code != tc.status || body.Code != tc.code {
				t.Fatalf("%v wrote %d %q, want %d %q", tc.err, rec.Code, body.Code, tc.status, tc.code)
			}
		})
	}
}

func TestNoKnownEditRefusalFallsThroughToAServerError(t *testing.T) {
	for _, err := range []error{wc.ErrCampaignScheduledStartInvalid, wc.ErrCampaignRunning, wc.ErrCampaignTemplateNotFound, errors.Join(wc.ErrCampaignTemplateVariableEmpty)} {
		rec := httptest.NewRecorder()
		(&WhatsAppCampaignHandler{}).handleDomainError(rec, err)
		if rec.Code >= http.StatusInternalServerError {
			t.Fatalf("%v answered %d", err, rec.Code)
		}
	}
	codes := CampaignEditRefusalCodes()
	if !sort.StringsAreSorted(codes) {
		t.Fatalf("codes must be listed sorted for the translation drift guard: %v", codes)
	}
}
