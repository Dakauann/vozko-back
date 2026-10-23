package emailcampaignhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/emailcampaign"
	"vozko/infra/http/middleware"
	emailcampaign_usecase "vozko/usecases/emailcampaign"
)

type readyCampaigns struct {
	Campaigns
	readied string
	err     error
}

func (r *readyCampaigns) Ready(_ context.Context, a Actor, campaignID string) (*emailcampaign.Campaign, error) {
	r.readied = campaignID
	if r.err != nil {
		return nil, r.err
	}
	return &emailcampaign.Campaign{ID: campaignID, WorkspaceID: a.WorkspaceID, Status: emailcampaign.StatusReady}, nil
}

func (r *readyCampaigns) Present(_ context.Context, c emailcampaign.Campaign) (emailcampaign_usecase.CampaignView, error) {
	return emailcampaign_usecase.CampaignView{Campaign: c, Actions: emailcampaign.AllowedActions(c.Status)}, nil
}

func serveReady(t *testing.T, campaigns *readyCampaigns) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	router.HandleFunc("/email/campaigns/{campaignId}/ready", NewHandler(campaigns).Ready).Methods(http.MethodPost)
	req := httptest.NewRequest(http.MethodPost, "/email/campaigns/c-1/ready", nil)
	ctx := context.WithValue(req.Context(), middleware.WorkspaceIDContextKey, "ws-1")
	ctx = context.WithValue(ctx, middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req.WithContext(ctx))
	return rec
}

func TestReadyLeavesTheCampaignReadyAndOffersTheStart(t *testing.T) {
	campaigns := &readyCampaigns{}
	rec := serveReady(t, campaigns)
	if rec.Code != http.StatusOK || campaigns.readied != "c-1" || !strings.Contains(rec.Body.String(), `"status":"ready"`) || !strings.Contains(rec.Body.String(), `"start"`) {
		t.Fatalf("ready = %d %s", rec.Code, rec.Body)
	}
	blocked := &readyCampaigns{err: &emailcampaign_usecase.LaunchBlockedError{Report: emailcampaign.LaunchReport{Blocking: []emailcampaign.Issue{{Code: emailcampaign.IssueAudienceEmpty}}}}}
	if rec := serveReady(t, blocked); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "audience_empty") {
		t.Fatalf("blocked = %d %s", rec.Code, rec.Body)
	}
	if rec := serveReady(t, &readyCampaigns{err: emailcampaign.ErrNotDraft}); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "email_campaign_not_draft") {
		t.Fatalf("not a draft = %d %s", rec.Code, rec.Body)
	}
}
