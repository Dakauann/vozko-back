package audiencex

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"vozko/domain/cache"
	"vozko/domain/campaign"
	audience "vozko/domain/campaignaudience"
	"vozko/domain/lead"
	"vozko/domain/selection"
	uw "vozko/domain/unofficial_whatsapp"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
)

type body struct {
	Code    string         `json:"code"`
	Details map[string]int `json:"details"`
}

func refused(t *testing.T, err error) (int, body) {
	t.Helper()
	rec := httptest.NewRecorder()
	if !WriteRefusal(rec, err) {
		t.Fatalf("%v was not refused", err)
	}
	var b body
	if e := json.Unmarshal(rec.Body.Bytes(), &b); e != nil {
		t.Fatal(e)
	}
	return rec.Code, b
}

func TestEveryAudienceErrorIsACodedRefusalNeverA500(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{audience.ErrSourceAmbiguous, http.StatusBadRequest, "audience_source_ambiguous"},
		{fmt.Errorf("x: %w", audience.ErrVariableUnknown), http.StatusBadRequest, "audience_variable_unknown"},
		{audience.ErrListNotFound, http.StatusNotFound, "audience_list_not_found"},
		{audience.ErrRowDuplicate, http.StatusConflict, "audience_row_duplicate"},
		{audience.ErrListExpired, http.StatusGone, "audience_list_expired"},
		{audience.ErrFileTooLarge, http.StatusRequestEntityTooLarge, "audience_file_too_large"},
		{campaign.ErrListTooLarge, http.StatusRequestEntityTooLarge, "audience_list_too_large"},
		{campaign.ErrListContactColumn, http.StatusUnprocessableEntity, "audience_contact_column_missing"},
		{audience.ErrChoiceRequired, http.StatusUnprocessableEntity, "audience_variable_choice_required"},
		{audience.ErrUnavailable, http.StatusServiceUnavailable, "audience_unavailable"},
		{selection.ErrFilterRequired, http.StatusBadRequest, "selection_filter_required"},
		{selection.ErrScopeDenied, http.StatusForbidden, "selection_scope_denied"},
		{lead.ErrLeadForbidden, http.StatusForbidden, "forbidden"},
		{fmt.Errorf("variable {{1}}: %w", campaign.ErrBindingSensitive), http.StatusBadRequest, "send_binding_sensitive"},
		{campaign.ErrCreationScopeMissing, http.StatusForbidden, "send_creation_scope_missing"},
		{fmt.Errorf("%w: %w", wc.ErrCampaignTemplateNotFound, template.ErrTemplateAccessDenied), http.StatusUnprocessableEntity, "send_template_unavailable"},
		{template.ErrTemplateNotSendable, http.StatusUnprocessableEntity, "send_template_unavailable"},
		{uw.ErrInstanceOutsideDepartment, http.StatusUnprocessableEntity, "send_instance_unavailable"},
		{cache.ErrGateBusy, http.StatusServiceUnavailable, "audience_busy"},
	}
	for _, tc := range cases {
		status, b := refused(t, tc.err)
		if status != tc.status || b.Code != tc.code {
			t.Fatalf("%v = %d %q, want %d %q", tc.err, status, b.Code, tc.status, tc.code)
		}
		if !slices.Contains(Codes(), tc.code) {
			t.Fatalf("%q is written but missing from Codes()", tc.code)
		}
	}
}

func TestAChangedSelectionCarriesTheCounts(t *testing.T) {
	status, b := refused(t, &selection.CountChangedError{Expected: 10, Matched: 12})
	if status != http.StatusConflict || b.Code != "selection_changed" || b.Details["expected"] != 10 || b.Details["matched"] != 12 {
		t.Fatalf("refusal = %d %+v", status, b)
	}
}

func TestAnUnknownErrorIsLeftToTheCaller(t *testing.T) {
	if WriteRefusal(httptest.NewRecorder(), errors.New("boom")) {
		t.Fatal("an unknown error was refused")
	}
}

func TestCodesCoverEveryAudienceDomainCode(t *testing.T) {
	for _, code := range audience.ErrorCodes() {
		if !slices.Contains(Codes(), code) {
			t.Fatalf("%q has no refusal", code)
		}
	}
}

func TestTheAudienceRequestBecomesTheDomainSpec(t *testing.T) {
	var r AudienceRequest
	if err := json.Unmarshal([]byte(`{"source":"leads","selection":{"mode":"ids","ids":["a"]},"variables":[{"key":"1","source":"lead.first_name","missing":"text","text":"cliente"}]}`), &r); err != nil {
		t.Fatal(err)
	}
	spec := r.Spec()
	if spec.Source != audience.SourceLeads || spec.Selection == nil || spec.Selection.Mode != selection.ModeIDs || len(spec.Variables) != 1 || spec.Variables[0].Text != "cliente" || spec.Variables[0].Source != campaign.BindFirstName {
		t.Fatalf("spec = %+v", spec)
	}
	req := r.Request("user-1", true)
	if req == nil || req.UserID != "user-1" || !req.IsAdmin || req.Spec.Source != audience.SourceLeads {
		t.Fatalf("request = %+v", req)
	}
	var none *AudienceRequest
	if none.Request("user-1", false) != nil {
		t.Fatal("no recipients make no request")
	}
}
