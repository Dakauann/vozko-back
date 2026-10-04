package copilottools

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
	adsuc "vozko/usecases/advertising"
)

const adAccountUUID = "7d3c1a52-6c55-4a39-9a7e-2b1f0a5c9d10"

var adTestClock = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

type stubAdAccounts struct{}

func (stubAdAccounts) List(context.Context, string) ([]*advertising.AdAccount, error) {
	return []*advertising.AdAccount{{ID: adAccountUUID, MetaAccountID: "1762972444913096", Name: "Loja", Currency: "BRL", Timezone: "America/Sao_Paulo"}}, nil
}

type stubAdAssets struct{}

func (stubAdAssets) Pages(context.Context, string, string) ([]adsuc.PromotablePage, error) {
	return nil, nil
}
func (stubAdAssets) Locations(context.Context, string, string, string) ([]advertising.RemoteLocation, error) {
	return nil, nil
}
func (stubAdAssets) NameLocations(_ context.Context, _, _ string, locations []advertising.GeoLocation) ([]advertising.GeoLocation, error) {
	out := make([]advertising.GeoLocation, 0, len(locations))
	for _, l := range locations {
		if strings.Contains(l.Key, "-") {
			return nil, advertising.FieldError("locations", advertising.CodeUnknownLocation)
		}
		l.Name = "Meta " + l.Key
		out = append(out, l)
	}
	return out, nil
}

type stubAdManager struct {
	statusCalls   int
	budgetChecked int64
	budgetSet     int64
	copied        []advertising.CopyRequest
	lifecycles    []advertising.Lifecycle
}

func (m *stubAdManager) CheckStatus(_ context.Context, _, id string, _ bool) (*advertising.Object, error) {
	if id != "120200" {
		return nil, advertising.ErrObjectNotFound
	}
	return &advertising.Object{MetaID: id, Name: "Leads outubro", Level: advertising.LevelCampaign}, nil
}
func (m *stubAdManager) SetStatus(ctx context.Context, ws, id string, on bool) (*advertising.Object, error) {
	m.statusCalls++
	return m.CheckStatus(ctx, ws, id, on)
}
func (m *stubAdManager) CheckBudget(_ context.Context, _, id string, amount int64) (*advertising.Object, *advertising.AdAccount, error) {
	m.budgetChecked = amount
	if amount < 519 {
		return nil, nil, advertising.FieldError("budget.amount", advertising.CodeBelowMinimum)
	}
	return &advertising.Object{MetaID: id, Name: "Conjunto", Level: advertising.LevelAdSet, LifetimeBudget: 2000}, &advertising.AdAccount{Currency: "BRL"}, nil
}
func (m *stubAdManager) SetBudget(_ context.Context, _, id string, amount int64) (*advertising.Object, error) {
	m.budgetSet = amount
	return &advertising.Object{MetaID: id, Level: advertising.LevelAdSet, LifetimeBudget: amount}, nil
}

type stubAdPublisher struct {
	published *adsuc.PublishInput
	unfunded  bool
}

func (p *stubAdPublisher) Preflight(ctx context.Context, ws string, d advertising.AdDraft) (*adsuc.Preflight, error) {
	if p.unfunded {
		return nil, advertising.ErrNoFundingSource
	}
	return p.Check(ctx, ws, d)
}
func (p *stubAdPublisher) Check(_ context.Context, _ string, d advertising.AdDraft) (*adsuc.Preflight, error) {
	if err := d.Validate(adTestClock); err != nil {
		return nil, err
	}
	return &adsuc.Preflight{
		Draft: d, Account: &advertising.AdAccount{Name: "Loja", Currency: "BRL", Timezone: "America/Sao_Paulo"},
		Page: advertising.RemotePage{Name: "Loja Centro"}, MediaURLs: map[string]string{d.Ads[0].Creative.Media.MediaID: "https://cdn/x.jpg"},
		Fee: adsuc.Fee{PriceMicros: 1_000_000, Currency: "USD"},
	}, nil
}
func (p *stubAdPublisher) Publish(_ context.Context, in adsuc.PublishInput) (*advertising.PublishJob, error) {
	p.published = &in
	return &advertising.PublishJob{Status: advertising.JobPublished, Progress: advertising.Progress{CampaignID: "c-1", Ads: map[int]string{0: "a-1"}}}, nil
}

func adDeps() (AdsDeps, *stubAdManager, *stubAdPublisher) {
	manager, publisher := &stubAdManager{}, &stubAdPublisher{}
	return AdsDeps{Accounts: stubAdAccounts{}, Assets: stubAdAssets{}, Manage: manager, Publish: publisher, Editor: &stubAdEditor{}}, manager, publisher
}

var adContext = copilot.Context{WorkspaceID: "ws-1", UserID: "u-1"}

func createAdArgsMap() map[string]interface{} {
	return map[string]interface{}{
		"ad_account_id": adAccountUUID, "campaign_name": "Leads outubro", "objective": "OUTCOME_ENGAGEMENT", "destination": "WHATSAPP",
		"page_id": "1001", "whatsapp_number": "5511988887777", "daily_budget": 30.0,
		"locations": []interface{}{"country:BR"}, "primary_text": "Fale com a gente",
		"format": "IMAGE", "media_id": "0b7c6a1e-1d2f-4c3b-9a8e-7f6d5c4b3a21",
	}
}

func TestCreateAdTurnsTheBudgetIntoMinorUnitsOfTheAccountCurrency(t *testing.T) {
	deps, _, publisher := adDeps()
	tool := NewCreateAdTool(deps)
	if err := tool.(copilot.Validator).Validate(context.Background(), adContext, createAdArgsMap()); err != nil {
		t.Fatal(err)
	}
	result := tool.Execute(context.Background(), adContext, createAdArgsMap())
	if result.Status != copilot.StatusOK || publisher.published.Draft.AdSet.Budget.Amount != 3000 || publisher.published.Actor != advertising.ActorAssistant || result.Data.(map[string]interface{})["ad_meta_id"] != "a-1" {
		t.Fatalf("result %+v published %+v", result, publisher.published)
	}
}

func TestCreateAdCardNamesAccountPageObjectiveAndBudget(t *testing.T) {
	deps, _, _ := adDeps()
	fields := NewCreateAdTool(deps).(copilot.Describer).Describe(context.Background(), adContext, createAdArgsMap())
	want := map[string]string{"account": "Loja", "page": "Loja Centro", "budget": "BRL 30,00 por dia", "objective": "Engajamento", "destination": "WhatsApp 5511988887777"}
	got := map[string]string{}
	for _, f := range fields {
		got[f.Key] = f.Value
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s = %q, want %q (fields %+v)", k, got[k], v, fields)
		}
	}
	if _, ok := got["fee"]; ok {
		t.Fatal("the fee is shown in reais by the preview, never as raw dollars in the card")
	}
	preview := NewCreateAdTool(deps).(copilot.Previewer).Preview(context.Background(), adContext, createAdArgsMap())
	if preview == nil || preview.Kind != PreviewAdCreative {
		t.Fatalf("preview %+v", preview)
	}
	if data := preview.Data.(AdCreativePreview); data.MediaURL != "https://cdn/x.jpg" || data.MediaKind != advertising.MediaImage ||
		data.Format != advertising.FormatImage || data.CallToAction != advertising.CTAWhatsAppMessage || data.DailyBudget != 3000 {
		t.Fatalf("preview data %+v", data)
	}
}

func TestCreateAdRefusesAnInventedAccount(t *testing.T) {
	deps, _, _ := adDeps()
	args := createAdArgsMap()
	args["ad_account_id"] = "act_123"
	if err := NewCreateAdTool(deps).(copilot.Validator).Validate(context.Background(), adContext, args); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("err %v", err)
	}
}

func TestCreateAdRefusesABadLocation(t *testing.T) {
	deps, _, _ := adDeps()
	args := createAdArgsMap()
	args["locations"] = []interface{}{"Brasil"}
	if err := NewCreateAdTool(deps).(copilot.Validator).Validate(context.Background(), adContext, args); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("err %v", err)
	}
}

func TestStatusToolsRefuseInventedIdsBeforeAnyChange(t *testing.T) {
	deps, manager, _ := adDeps()
	off := NewTurnOffAdTool(deps)
	if err := off.(copilot.Validator).Validate(context.Background(), adContext, map[string]interface{}{"meta_id": "campanha leads"}); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("err %v", err)
	}
	if err := off.(copilot.Validator).Validate(context.Background(), adContext, map[string]interface{}{"meta_id": "999"}); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("unknown id err %v", err)
	}
	if result := off.Execute(context.Background(), adContext, map[string]interface{}{"meta_id": "120200"}); result.Status != copilot.StatusOK || manager.statusCalls != 1 {
		t.Fatalf("result %+v calls %d", result, manager.statusCalls)
	}
}

func TestBudgetToolKeepsTheBudgetKindAndSendsMinorUnits(t *testing.T) {
	deps, manager, _ := adDeps()
	tool := NewUpdateAdBudgetTool(deps)
	args := map[string]interface{}{"meta_id": "120201", "amount": 55.5}
	fields := tool.(copilot.Describer).Describe(context.Background(), adContext, args)
	if fields[1].Value != "BRL 20,00 no total" || fields[2].Value != "BRL 55,50 no total" {
		t.Fatalf("fields %+v", fields)
	}
	if result := tool.Execute(context.Background(), adContext, args); result.Status != copilot.StatusOK || manager.budgetSet != 5550 || manager.budgetChecked != 5550 {
		t.Fatalf("result %+v budget %d checked %d", result, manager.budgetSet, manager.budgetChecked)
	}
}

func TestBudgetToolChecksTheRealAmountAgainstTheMinimumBeforeApproval(t *testing.T) {
	deps, manager, _ := adDeps()
	err := NewUpdateAdBudgetTool(deps).(copilot.Validator).Validate(context.Background(), adContext, map[string]interface{}{"meta_id": "120201", "amount": 3.0})
	if !errors.Is(err, errInvalidArgs) || manager.budgetChecked != 300 || manager.budgetSet != 0 {
		t.Fatalf("err %v checked %d set %d", err, manager.budgetChecked, manager.budgetSet)
	}
}

func TestCreateAdPublishesAVideoAsAVideoCreative(t *testing.T) {
	deps, _, publisher := adDeps()
	args := createAdArgsMap()
	args["format"] = "VIDEO"
	if result := NewCreateAdTool(deps).Execute(context.Background(), adContext, args); result.Status != copilot.StatusOK {
		t.Fatalf("result %+v", result)
	}
	c := publisher.published.Draft.Ads[0].Creative
	if c.Format != advertising.FormatVideo || c.Media.Kind != advertising.MediaVideo {
		t.Fatalf("creative %+v", c)
	}
}

func TestCreateAdRefusesACarouselWithoutCards(t *testing.T) {
	deps, _, _ := adDeps()
	args := createAdArgsMap()
	args["format"] = "CAROUSEL"
	if err := NewCreateAdTool(deps).(copilot.Validator).Validate(context.Background(), adContext, args); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("err %v", err)
	}
}

func TestPreviewCarriesCarouselCardsAndFlexibleMedias(t *testing.T) {
	pre := &adsuc.Preflight{
		Draft:     advertising.AdDraft{AdSet: advertising.AdSetDraft{Destination: advertising.DestinationWebsite}},
		Account:   &advertising.AdAccount{Name: "Loja", Currency: "BRL"},
		MediaURLs: map[string]string{"m-1": "https://cdn/1.jpg", "m-2": "https://cdn/2.mp4"},
	}
	data := creativePreview(pre, advertising.CreativeDraft{
		Format: advertising.FormatCarousel,
		Cards: []advertising.CarouselCard{
			{Media: advertising.MediaRef{Kind: advertising.MediaImage, MediaID: "m-1"}, Headline: "Um"},
			{Media: advertising.MediaRef{Kind: advertising.MediaVideo, MediaID: "m-2"}, Headline: "Dois"},
		},
	})
	if len(data.Cards) != 2 || data.Cards[1].URL != "https://cdn/2.mp4" || data.Cards[1].Kind != advertising.MediaVideo || data.CallToAction != advertising.CTALearnMore || data.MediaURL != "" {
		t.Fatalf("preview %+v", data)
	}
}

func TestToolsFindTheAccountByMetasIdOrNameAndListTheChoicesOtherwise(t *testing.T) {
	deps, _, _ := adDeps()
	for _, ref := range []string{adAccountUUID, "act_1762972444913096", "loja"} {
		account, err := deps.account(context.Background(), adContext, ref)
		if err != nil || account.ID != adAccountUUID {
			t.Fatalf("%q: %+v %v", ref, account, err)
		}
	}
	_, err := deps.account(context.Background(), adContext, "Vozko CRM BRL")
	if !errors.Is(err, errInvalidArgs) || !strings.Contains(err.Error(), "Loja (ad_account_id "+adAccountUUID+", BRL)") {
		t.Fatalf("err %v", err)
	}
}

func TestTheLocationCardShowsMetasNamesAndRefusesInventedKeys(t *testing.T) {
	f := newManageFixture()
	tool := f.tool(t, "edit_ad_set")
	fields := fieldMap(tool.(copilot.Describer).Describe(context.Background(), adContext, map[string]interface{}{"meta_id": "120201", "locations": []interface{}{"region:455"}}))
	if !strings.HasSuffix(fields["locations"], "→ Meta 455") {
		t.Fatalf("fields %+v", fields)
	}
	err := tool.(copilot.Validator).Validate(context.Background(), adContext, map[string]interface{}{"meta_id": "120201", "locations": []interface{}{"region:BR-RN"}})
	if !errors.Is(err, errInvalidArgs) || !strings.Contains(err.Error(), "search_ad_locations") {
		t.Fatalf("an invented location reached the card: %v", err)
	}
}
