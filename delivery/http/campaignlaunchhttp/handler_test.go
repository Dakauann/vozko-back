package campaignlaunchhttp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/campaign/launchcheck"
	wc "vozko/domain/whatsapp_campaign"
	wd "vozko/domain/workspace/workspace_department"
)

type ownedCampaigns map[string]*wc.Campaign

func (o ownedCampaigns) Owned(workspaceID string, _ *wd.DepartmentFilter, id string) (*wc.Campaign, error) {
	c, ok := o[id]
	if !ok || c.WorkspaceID != workspaceID {
		return nil, wc.ErrCampaignNotFound
	}
	return c, nil
}

type checkStub struct{ report launchcheck.Report }

func (c checkStub) Run(context.Context, string) (launchcheck.Report, error) { return c.report, nil }

func serve(report launchcheck.Report, path string) *httptest.ResponseRecorder {
	router := mux.NewRouter()
	h := NewHandler(ownedCampaigns{"c1": {ID: "c1", WorkspaceID: "ws"}}, checkStub{report: report})
	router.HandleFunc("/whatsapp/campaigns/{id}/launch-check", h.Get).Methods(http.MethodGet)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Workspace-ID", "ws")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestTheLaunchCheckReturnsTheFindingsAndTheirFingerprint(t *testing.T) {
	report := launchcheck.Report{Findings: []launchcheck.Finding{{Code: launchcheck.CodeTemplateRed, Level: launchcheck.Confirm}}}

	rec := serve(report, "/whatsapp/campaigns/c1/launch-check")

	var body Response
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if len(body.Findings) != 1 || body.Fingerprint != report.Fingerprint() || body.Blocks || !body.NeedsConfirmation {
		t.Fatalf("body %+v", body)
	}
}

func TestAnotherWorkspacesCampaignIsNotFound(t *testing.T) {
	if rec := serve(launchcheck.Report{}, "/whatsapp/campaigns/c2/launch-check"); rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
}

func TestARefusalBecomesAConflictWithTheFindings(t *testing.T) {
	report := launchcheck.Report{Findings: []launchcheck.Finding{{Code: launchcheck.CodePhoneUnavailable, Level: launchcheck.Block}, {Code: launchcheck.CodeNoExit, Level: launchcheck.Confirm}}}
	rec := httptest.NewRecorder()

	if !WriteRefusal(rec, report.Admit("")) {
		t.Fatal("a launch refusal must be written")
	}
	var body struct {
		Code     string            `json:"code"`
		Expected map[string]string `json:"expected"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusConflict || body.Code != "campaign_launch_blocked" || body.Expected["blocks"] != "phone_unavailable" || body.Expected["confirms"] != "no_exit" {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if WriteRefusal(httptest.NewRecorder(), context.Canceled) {
		t.Fatal("other errors are not launch refusals")
	}
}
