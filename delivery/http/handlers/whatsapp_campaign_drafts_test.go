package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vozko/delivery/http/campaigndrafthttp"
	"vozko/domain/auth"
	"vozko/domain/balance"
	"vozko/domain/buttonaction"
	"vozko/domain/campaign"
	"vozko/domain/campaignaudience"
	"vozko/domain/dealautomation"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/whatsapp/quality"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	wd "vozko/domain/workspace/workspace_department"
	"vozko/infra/http/middleware"
	buttonaction_usecase "vozko/usecases/buttonaction"
	whatsapp_campaign_usecase "vozko/usecases/whatsapp_campaign"
)

type draftTemplates struct{}

func (draftTemplates) FindByID(id string) (*template.Template, error) {
	return &template.Template{ID: id, ExternalID: "ext", Status: template.TemplateStatusApproved, Category: template.TemplateCategoryUtility}, nil
}

type draftQuality struct{}

func (draftQuality) LatestBySignal(quality.Subject, string) (map[quality.Signal]quality.Change, error) {
	return map[quality.Signal]quality.Change{}, nil
}

type draftPhones struct{ status businessphone.Status }

func (p draftPhones) FindByID(id string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	return &businessphone.WhatsAppBusinessPhoneNumber{ID: id, OwnerWorkspaceID: "ws", Status: p.status}, nil
}

type draftGrants struct{}

func (draftGrants) HasAccess(string, string) (bool, error) { return true, nil }

type draftExits struct{}

func (draftExits) ExitConfigured(context.Context, string, string, string) (bool, error) {
	return true, nil
}

type draftCosts struct{}

func (draftCosts) GetTemplateCostMicros(string, string) (int64, error) { return 1000, nil }

type draftBalances struct{}

func (draftBalances) GetBalance(string) (int64, error) { return 1_000_000_000, nil }

type draftCaps struct{}

func (draftCaps) MonthlySendCapUsage(string, time.Time) (*balance.SendCapUsage, error) {
	return nil, nil
}

type draftCount struct{}

func (draftCount) Count(context.Context, *wc.Campaign) (campaignaudience.Count, error) {
	n := 25
	return campaignaudience.Count{Sendable: &n}, nil
}

type draftCreate struct{ got []*wc.Campaign }

func (d *draftCreate) Execute(_ context.Context, c *wc.Campaign) (*wc.Campaign, error) {
	stored := *c
	stored.ID = "camp-new"
	d.got = append(d.got, &stored)
	return &stored, nil
}

type draftStart struct{ started []string }

func (d *draftStart) StartConfirmed(_ string, _ *wd.DepartmentFilter, id, _ string) (*wc.Campaign, error) {
	d.started = append(d.started, id)
	return nil, nil
}

type draftDeals struct{ set []string }

func (d *draftDeals) Set(_ shared.Person, _ string, ch dealautomation.Channel, containerID, pipelineID string) (*dealautomation.Setting, error) {
	d.set = append(d.set, string(ch.EntryType)+"|"+containerID+"|"+pipelineID)
	return &dealautomation.Setting{}, nil
}

type draftRoutes struct {
	saved    []string
	versions []int64
	inputs   [][]buttonaction.Input
	order    *[]string
}

func (d *draftRoutes) CampaignRoutes(context.Context, buttonaction_usecase.Actor, string) (*buttonaction_usecase.CampaignView, error) {
	return &buttonaction_usecase.CampaignView{Routes: buttonaction.CampaignRoutes{Version: 3}}, nil
}

func (d *draftRoutes) SaveCampaignRoutes(_ context.Context, _ buttonaction_usecase.Actor, id string, version int64, inputs []buttonaction.Input) (*buttonaction_usecase.CampaignView, error) {
	d.saved = append(d.saved, id)
	d.versions = append(d.versions, version)
	d.inputs = append(d.inputs, inputs)
	*d.order = append(*d.order, "routes")
	return &buttonaction_usecase.CampaignView{}, nil
}

type orderedStart struct {
	draftStart
	order *[]string
}

func (o *orderedStart) StartConfirmed(ws string, d *wd.DepartmentFilter, id, fp string) (*wc.Campaign, error) {
	*o.order = append(*o.order, "start")
	return o.draftStart.StartConfirmed(ws, d, id, fp)
}

type finisherFixture struct {
	create   *draftCreate
	start    *orderedStart
	deals    *draftDeals
	routes   *draftRoutes
	finisher *WhatsAppDraftFinisher
	order    []string
}

func newFinisherFixture(phone businessphone.Status) *finisherFixture {
	f := &finisherFixture{create: &draftCreate{}, deals: &draftDeals{}}
	f.start = &orderedStart{order: &f.order}
	f.routes = &draftRoutes{order: &f.order}
	check := whatsapp_campaign_usecase.NewLaunchCheck(whatsapp_campaign_usecase.LaunchCheckDeps{
		Templates: draftTemplates{}, Quality: draftQuality{}, Phones: draftPhones{status: phone}, TemplateGrants: draftGrants{},
		Exits: draftExits{}, Costs: draftCosts{}, Balances: draftBalances{}, Caps: draftCaps{}, Now: time.Now,
	})
	launch := whatsapp_campaign_usecase.NewDraftLaunch(whatsapp_campaign_usecase.DraftLaunchDeps{Create: f.create, Start: f.start, Check: check, Audience: draftCount{}})
	f.finisher = NewWhatsAppDraftFinisher(&WhatsAppCampaignHandler{}, launch, f.deals, f.routes)
	return f
}

func draftHTTPRequest() *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/campaign-drafts/whatsapp/d1/launch", nil)
	ctx := context.WithValue(req.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "u1", Role: "user"})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws")
	return req.WithContext(ctx)
}

const draftBody = `{"name":"Natal","templateId":"tpl-1","businessPhoneId":"phone-1","enableAnalysis":true,"aiModel":"openai/gpt-4o-mini","scheduledStart":"2030-01-01T10:00:00Z","dealPipelineId":"pipe-1","buttonRoutes":[{"buttonIndex":1,"buttonText":"Quero","kind":"tag","params":{}}]}`

func TestDraftReadyMapsTheRequestAndAppliesTheSettings(t *testing.T) {
	f := newFinisherFixture(businessphone.StatusConnected)
	rec := httptest.NewRecorder()
	if !f.finisher.Launch(rec, draftHTTPRequest(), json.RawMessage(draftBody), campaign.Launch{Mode: campaign.LaunchReady}, "") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	got := f.create.got[0]
	if got.Name != "Natal" || got.TemplateID != "tpl-1" || got.BusinessPhoneID != "phone-1" || !got.EnableAnalysis || got.AiModel != "openai/gpt-4o-mini" {
		t.Fatalf("created %+v", got)
	}
	if !got.ScheduledStart.IsZero() {
		t.Fatal("ready kept the draft's schedule")
	}
	if f.routes.versions[0] != 3 || len(f.routes.inputs[0]) != 1 || f.routes.inputs[0][0].ButtonIndex != 1 || f.routes.inputs[0][0].ButtonText != "Quero" {
		t.Fatalf("routes saved with version %v inputs %+v", f.routes.versions, f.routes.inputs)
	}
	if len(f.deals.set) != 1 || f.deals.set[0] != "whatsapp|camp-new|pipe-1" || len(f.routes.saved) != 1 || len(f.start.started) != 0 {
		t.Fatalf("deals %v routes %v started %v", f.deals.set, f.routes.saved, f.start.started)
	}
	var body campaigndrafthttp.LaunchResponse
	if rec.Code != http.StatusCreated || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Started || body.Mode != "ready" || len(body.Warnings) != 0 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestDraftNowAppliesTheSettingsBeforeStarting(t *testing.T) {
	f := newFinisherFixture(businessphone.StatusConnected)
	rec := httptest.NewRecorder()
	if !f.finisher.Launch(rec, draftHTTPRequest(), json.RawMessage(draftBody), campaign.Launch{Mode: campaign.LaunchNow}, "") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if len(f.order) != 2 || f.order[0] != "routes" || f.order[1] != "start" {
		t.Fatalf("order %v", f.order)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"started":true`)) {
		t.Fatalf("%s", rec.Body.String())
	}
}

func TestDraftNowBlockedCreatesNothingAndAnswersTheIssues(t *testing.T) {
	f := newFinisherFixture(businessphone.StatusRestricted)
	rec := httptest.NewRecorder()
	if f.finisher.Launch(rec, draftHTTPRequest(), json.RawMessage(draftBody), campaign.Launch{Mode: campaign.LaunchNow}, "") {
		t.Fatal("a blocked launch reported a campaign")
	}
	if rec.Code != http.StatusConflict || !bytes.Contains(rec.Body.Bytes(), []byte("campaign_launch_blocked")) || !bytes.Contains(rec.Body.Bytes(), []byte("phone_unavailable")) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if len(f.create.got) != 0 || len(f.start.started) != 0 || len(f.deals.set) != 0 {
		t.Fatal("a blocked launch created, settled or started")
	}
}

func TestDraftReviewAnswersCountQuoteAndCheck(t *testing.T) {
	f := newFinisherFixture(businessphone.StatusConnected)
	rec := httptest.NewRecorder()
	f.finisher.Review(rec, draftHTTPRequest(), json.RawMessage(draftBody))
	var body struct {
		Audience campaignaudience.Count        `json:"audience"`
		Quote    *SendQuoteResponse            `json:"quote"`
		Check    campaigndrafthttp.CheckReport `json:"check"`
	}
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if *body.Audience.Sendable != 25 || body.Quote == nil || body.Quote.CostMicros != 25000 || body.Check.Blocks || body.Check.Findings == nil {
		t.Fatalf("body %+v", body)
	}
	if len(f.create.got) != 0 {
		t.Fatal("a review created a campaign")
	}
}

func TestDraftWithAnInvalidRequestIsCoded(t *testing.T) {
	f := newFinisherFixture(businessphone.StatusConnected)
	rec := httptest.NewRecorder()
	f.finisher.Review(rec, draftHTTPRequest(), json.RawMessage(`{"name":12}`))
	if rec.Code != http.StatusUnprocessableEntity || !bytes.Contains(rec.Body.Bytes(), []byte("campaign_draft_invalid")) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
}
