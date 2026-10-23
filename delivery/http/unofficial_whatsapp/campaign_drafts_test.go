package unofficial_whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/delivery/http/campaigndrafthttp"
	"vozko/domain/auth"
	"vozko/domain/campaign"
	"vozko/domain/campaignaudience"
	"vozko/domain/dealautomation"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/infra/http/middleware"
	uwuc "vozko/usecases/unofficial_whatsapp"
	uwcuc "vozko/usecases/unofficial_whatsapp_campaign"
)

type draftGateway struct{ status uw.Status }

func (g draftGateway) Instance(_ context.Context, id string) (*uw.Instance, error) {
	return &uw.Instance{ID: id, WorkspaceID: "ws-1", Status: g.status}, nil
}

func (draftGateway) Ref(context.Context, *uw.Instance) (uw.InstanceRef, error) {
	return uw.InstanceRef{}, nil
}

func (draftGateway) CheckNumbers(context.Context, uw.InstanceRef, []string) ([]uw.NumberCheck, error) {
	return nil, nil
}

func (draftGateway) MessagingLimits(context.Context, uw.InstanceRef) (*uw.Restriction, error) {
	return nil, nil
}

func (draftGateway) CacheRestriction(context.Context, string, uw.Restriction) error { return nil }

func (draftGateway) Resolve(context.Context, *uw.Instance, uwuc.ResolveInput) (*uwuc.Resolved, error) {
	return nil, nil
}

type draftActions struct{ acted []string }

func (d *draftActions) Act(_ context.Context, _ string, _ uw.DepartmentScope, id string, action campaign.Action) (*uwc.Campaign, error) {
	d.acted = append(d.acted, id+"|"+string(action))
	return nil, nil
}

type draftEstimate struct{}

func (draftEstimate) Draft(context.Context, uwc.EstimateInput, uw.DepartmentScope) (*uwc.DraftEstimate, error) {
	return &uwc.DraftEstimate{Groups: []uwc.GroupSenders{}}, nil
}

func (draftEstimate) Remaining(context.Context, *uwc.Campaign) (*uwc.SendEstimate, error) {
	return nil, nil
}

type draftAudience struct{}

func (draftAudience) Count(context.Context, *uwc.Campaign) (campaignaudience.Count, error) {
	n := 12
	return campaignaudience.Count{Sendable: &n}, nil
}

type draftDeals struct{ set []string }

func (d *draftDeals) Set(_ shared.Person, _ string, ch dealautomation.Channel, id, pipeline string) (*dealautomation.Setting, error) {
	d.set = append(d.set, string(ch.EntryType)+"|"+id+"|"+pipeline)
	return &dealautomation.Setting{}, nil
}

type unofficialFinisherFixture struct {
	create   *capturingCreate
	actions  *draftActions
	deals    *draftDeals
	finisher *DraftFinisher
}

func newUnofficialFinisher(status uw.Status) unofficialFinisherFixture {
	f := unofficialFinisherFixture{create: &capturingCreate{}, actions: &draftActions{}, deals: &draftDeals{}}
	h := NewCampaignHandler(CampaignHandlerDeps{Create: f.create, Departments: openScope{}})
	launch := uwcuc.NewDraftLaunch(uwcuc.DraftLaunchDeps{
		Create: f.create, Actions: f.actions, Estimate: draftEstimate{}, Audience: draftAudience{}, Preflight: uwcuc.NewStartPreflight(draftGateway{status: status}),
	})
	f.finisher = NewDraftFinisher(h, launch, f.deals)
	return f
}

func unofficialDraftRequest(role string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/campaign-drafts/unofficial_whatsapp/d1/launch", nil)
	ctx := context.WithValue(r.Context(), middleware.WorkspaceIDContextKey, "ws-1")
	ctx = context.WithValue(ctx, middleware.ClaimsContextKey, &auth.Claims{UserID: "u-1", Role: role})
	return r.WithContext(ctx)
}

const unofficialDraftBody = `{"name":"demo","instanceId":"inst-1","message":{"kind":"text","bodies":["oi /sair"]},"sendDelayMinMs":9000,"sendDelayMaxMs":15000,"scheduledStart":"2030-01-01T10:00:00Z","dealPipelineId":"pipe-9","seedOutcome":{"sentPercent":40,"failedPercent":10}}`

func TestUnofficialDraftReadyMapsTheRequestLikeCreate(t *testing.T) {
	f := newUnofficialFinisher(uw.StatusConnected)
	rec := httptest.NewRecorder()
	if !f.finisher.Launch(rec, unofficialDraftRequest("user"), json.RawMessage(unofficialDraftBody), campaign.Launch{Mode: campaign.LaunchReady}, "") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	got := f.create.got
	if got.Name != "demo" || got.InstanceID != "inst-1" || got.SendDelayMinMS != 9000 || !got.ScheduledStart.IsZero() {
		t.Fatalf("created %+v", got)
	}
	if got.SeedOutcome != nil {
		t.Fatal("a person who is not a system admin kept the seed outcome")
	}
	if len(f.deals.set) != 1 || f.deals.set[0] != "unofficial_whatsapp|c-1|pipe-9" || len(f.actions.acted) != 0 {
		t.Fatalf("deals %v acted %v", f.deals.set, f.actions.acted)
	}
}

func TestUnofficialDraftNowStartsTheCreatedCampaign(t *testing.T) {
	f := newUnofficialFinisher(uw.StatusConnected)
	rec := httptest.NewRecorder()
	if !f.finisher.Launch(rec, unofficialDraftRequest("user"), json.RawMessage(unofficialDraftBody), campaign.Launch{Mode: campaign.LaunchNow}, "") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var body campaigndrafthttp.LaunchResponse
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &body) != nil || !body.Started || len(f.actions.acted) != 1 || f.actions.acted[0] != "c-1|START" {
		t.Fatalf("%d %s %v", rec.Code, rec.Body.String(), f.actions.acted)
	}
}

func TestUnofficialDraftNowWithABannedNumberCreatesNothing(t *testing.T) {
	f := newUnofficialFinisher(uw.StatusBanned)
	rec := httptest.NewRecorder()
	if f.finisher.Launch(rec, unofficialDraftRequest("user"), json.RawMessage(unofficialDraftBody), campaign.Launch{Mode: campaign.LaunchNow}, "") {
		t.Fatal("a blocked launch reported a campaign")
	}
	if rec.Code != http.StatusConflict || !bytes.Contains(rec.Body.Bytes(), []byte("number_unusable")) || f.create.got != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestUnofficialDraftReviewAnswersCountEstimateAndCheck(t *testing.T) {
	f := newUnofficialFinisher(uw.StatusConnected)
	rec := httptest.NewRecorder()
	f.finisher.Review(rec, unofficialDraftRequest("user"), json.RawMessage(unofficialDraftBody))
	var body struct {
		Audience campaignaudience.Count        `json:"audience"`
		Estimate map[string]any                `json:"estimate"`
		Check    campaigndrafthttp.CheckReport `json:"check"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil || *body.Audience.Sendable != 12 || body.Estimate == nil || body.Check.Blocks {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if f.create.got != nil {
		t.Fatal("a review created a campaign")
	}
}
