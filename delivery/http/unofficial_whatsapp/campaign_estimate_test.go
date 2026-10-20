package unofficial_whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/campaign"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	uwg "vozko/domain/unofficial_whatsapp_group"
	"vozko/infra/http/middleware"
	uwcuc "vozko/usecases/unofficial_whatsapp_campaign"
)

type stubEstimate struct {
	drafts    []uwc.EstimateInput
	remaining int
}

func (s *stubEstimate) Draft(_ context.Context, in uwc.EstimateInput, _ uw.DepartmentScope) (*uwc.DraftEstimate, error) {
	s.drafts = append(s.drafts, in)
	return &uwc.DraftEstimate{
		Estimate: uwc.SendEstimate{Duration: 90 * time.Second, Pending: 2, Senders: 2, Unsendable: 1},
		Groups: []uwc.GroupSenders{
			{JID: "120363000000000001@g.us", Name: "Ofertas VIP #1", Senders: []uwc.SenderNumber{{InstanceID: "n1", Label: "Vendas 1"}}},
			{JID: "120363000000000003@g.us", Name: "Avisos", Senders: []uwc.SenderNumber{}, Block: uwg.SenderCannotPost},
		},
	}, nil
}

func (s *stubEstimate) Remaining(context.Context, *uwc.Campaign) (*uwc.SendEstimate, error) {
	s.remaining++
	return &uwc.SendEstimate{Duration: 5 * time.Minute, Pending: 30, Senders: 3}, nil
}

type runningCampaign struct{}

func (runningCampaign) Execute(_ context.Context, id string) (*uwc.Campaign, error) {
	return &uwc.Campaign{ID: id, WorkspaceID: "ws-1", Audience: uwc.AudienceGroups, Status: campaign.StatusRunning}, nil
}

func asMember(r *http.Request) *http.Request {
	ctx := context.WithValue(r.Context(), middleware.WorkspaceIDContextKey, "ws-1")
	ctx = context.WithValue(ctx, middleware.ClaimsContextKey, &auth.Claims{UserID: "u-1", Role: "user"})
	return r.WithContext(ctx)
}

func postEstimate(h *CampaignHandler, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/unofficial-whatsapp/campaigns/estimate", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	h.Estimate(w, asMember(r))
	return w
}

func TestTheGroupEstimateNeedsPermissionToSeeTheGroups(t *testing.T) {
	estimate := &stubEstimate{}
	h := NewCampaignHandler(CampaignHandlerDeps{Estimate: estimate, Departments: openScope{}, Permissions: groupsAccess{}})

	w := postEstimate(h, `{"audience":"groups","groupJids":["120363000000000001@g.us"]}`)

	if w.Code != http.StatusForbidden || len(estimate.drafts) != 0 {
		t.Fatalf("status %d drafts %d, want 403 and no estimate", w.Code, len(estimate.drafts))
	}
}

func TestTheGroupEstimateShowsWhoPostsInEachGroup(t *testing.T) {
	estimate := &stubEstimate{}
	h := NewCampaignHandler(CampaignHandlerDeps{Estimate: estimate, Departments: openScope{}, Permissions: groupsAccess{allowed: true}})

	w := postEstimate(h, `{"audience":"groups","groupJids":["120363000000000001@g.us","120363000000000003@g.us"],"sendDelayMinMs":4000,"sendDelayMaxMs":6000}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
	var body struct{ Data campaignEstimateDraftDTO }
	if err := json.Unmarshal(w.Body.Bytes(), &body.Data); err != nil {
		t.Fatal(err)
	}
	if body.Data.Estimate.Seconds != 90 || body.Data.Estimate.Senders != 2 || body.Data.Estimate.Unsendable != 1 {
		t.Fatalf("estimate = %+v", body.Data.Estimate)
	}
	if len(body.Data.Groups) != 2 || body.Data.Groups[0].Senders[0].Label != "Vendas 1" || body.Data.Groups[1].Block != "cannot_post" {
		t.Fatalf("groups = %+v", body.Data.Groups)
	}
	if in := estimate.drafts[0]; in.WorkspaceID != "ws-1" || len(in.GroupJIDs) != 2 || in.SendDelayMinMS != 4000 {
		t.Fatalf("draft input = %+v", in)
	}
}

func TestTheCampaignDetailCarriesWhatIsLeftToSend(t *testing.T) {
	estimate := &stubEstimate{}
	h := NewCampaignHandler(CampaignHandlerDeps{
		Estimate: estimate, Departments: openScope{},
		Access: uwcuc.NewCampaignAccessUseCase(runningCampaign{}),
	})
	router := mux.NewRouter()
	router.HandleFunc("/unofficial-whatsapp/campaigns/{id}", h.Get).Methods(http.MethodGet)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, asMember(httptest.NewRequest(http.MethodGet, "/unofficial-whatsapp/campaigns/c-1", nil)))

	var body struct{ Data campaignDTO }
	if err := json.Unmarshal(w.Body.Bytes(), &body.Data); err != nil || w.Code != http.StatusOK {
		t.Fatalf("status %d err %v body %s", w.Code, err, w.Body.String())
	}
	if body.Data.Estimate == nil || body.Data.Estimate.Seconds != 300 || body.Data.Estimate.Pending != 30 {
		t.Fatalf("estimate = %+v, want 5 minutes for 30 pending", body.Data.Estimate)
	}
}
