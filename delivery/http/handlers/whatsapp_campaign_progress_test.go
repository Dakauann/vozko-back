package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/campaign"
	wc "vozko/domain/whatsapp_campaign"
	wd "vozko/domain/workspace/workspace_department"
)

type ownedOnly struct{ campaign *wc.Campaign }

func (o ownedOnly) Owned(_ string, _ *wd.DepartmentFilter, id string) (*wc.Campaign, error) {
	if o.campaign == nil || o.campaign.ID != id {
		return nil, wc.ErrCampaignNotFound
	}
	return o.campaign, nil
}

type fixedProgress struct{ seen *campaign.LiveCampaign }

func (f *fixedProgress) Execute(_ context.Context, live campaign.LiveCampaign) (*campaign.Progress, error) {
	f.seen = &live
	progress := campaign.NewProgress(live.Status, &campaign.Counts{Total: 10, Pending: 4, Sent: 6}, campaign.ProgressSample{}.At, nil)
	return &progress, nil
}

func progressRequest(h *WhatsAppCampaignHandler, id string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/whatsapp/campaigns/"+id+"/progress", nil)
	req = mux.SetURLVars(req, map[string]string{"id": id})
	rec := httptest.NewRecorder()
	h.Progress(rec, req)
	return rec
}

func TestProgressAnswersForTheCampaignTheCallerOwns(t *testing.T) {
	owned := &wc.Campaign{ID: "camp-1", Status: wc.CampaignStatusRunning}
	progress := &fixedProgress{}
	h := &WhatsAppCampaignHandler{}
	h.SetCampaignAccess(ownedOnly{campaign: owned})
	h.SetLive(progress, nil)

	rec := progressRequest(h, "camp-1")
	if rec.Code != http.StatusOK || progress.seen == nil || progress.seen.ID != owned.ID || progress.seen.Status != owned.Status {
		t.Fatalf("status %d, use case saw %+v", rec.Code, progress.seen)
	}
	var body campaign.Progress
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	if body.Status != campaign.StatusRunning || body.RefreshAfterSeconds != int(campaign.ProgressRefresh(campaign.StatusRunning)/time.Second) || body.Metrics.Sent != 6 {
		t.Fatalf("progress = %+v", body)
	}
}

func TestProgressOfACampaignTheCallerCannotSeeIsNotRead(t *testing.T) {
	progress := &fixedProgress{}
	h := &WhatsAppCampaignHandler{}
	h.SetCampaignAccess(ownedOnly{})
	h.SetLive(progress, nil)

	if rec := progressRequest(h, "camp-x"); rec.Code != http.StatusNotFound || progress.seen != nil {
		t.Fatalf("status %d, use case saw %+v", rec.Code, progress.seen)
	}
}

func TestProgressWithoutItsUseCaseRefusesWithACode(t *testing.T) {
	h := &WhatsAppCampaignHandler{}
	h.SetCampaignAccess(ownedOnly{campaign: &wc.Campaign{ID: "camp-1"}})

	rec := progressRequest(h, "camp-1")
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if rec.Code != http.StatusServiceUnavailable || body.Code != "campaign_live_unavailable" {
		t.Fatalf("status %d code %q", rec.Code, body.Code)
	}
}
