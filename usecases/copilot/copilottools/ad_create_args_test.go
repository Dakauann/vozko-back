package copilottools

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
	adsuc "vozko/usecases/advertising"
)

const (
	cardImageID  = "0b7c6a1e-1d2f-4c3b-9a8e-7f6d5c4b3a31"
	cardVideoID  = "0b7c6a1e-1d2f-4c3b-9a8e-7f6d5c4b3a32"
	savedDraftID = "0b7c6a1e-1d2f-4c3b-9a8e-7f6d5c4b3a99"
)

func savedFrom(t *testing.T, args map[string]interface{}) advertising.AdDraft {
	t.Helper()
	var a adDraftArgs
	if err := decodeArgs(args, &a); err != nil {
		t.Fatal(err)
	}
	d, err := a.draft(&advertising.AdAccount{ID: adAccountUUID, Currency: "BRL", Timezone: "America/Sao_Paulo"})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func savedDraft(t *testing.T, drafts *stubAdDrafts, args map[string]interface{}) {
	t.Helper()
	drafts.stored = &adsuc.DraftView{Draft: &advertising.SavedDraft{ID: savedDraftID, AdAccountID: adAccountUUID, Version: 7, Content: savedFrom(t, args)}, State: advertising.DraftEditing}
}

func saveDraft(t *testing.T, deps AdsDeps, args map[string]interface{}) copilot.Result {
	t.Helper()
	tool := NewSaveAdDraftTool(deps)
	if err := tool.(copilot.Validator).Validate(context.Background(), adContext, args); err != nil {
		t.Fatalf("validate: %v", err)
	}
	return tool.Execute(context.Background(), adContext, args)
}

func refused(t *testing.T, tool copilot.Tool, args map[string]interface{}) {
	t.Helper()
	if err := tool.(copilot.Validator).Validate(context.Background(), adContext, args); !errors.Is(err, errInvalidArgs) {
		t.Fatalf("expected a refusal, got %v", err)
	}
}

func carouselArgs() map[string]interface{} {
	args := websiteArgs()
	delete(args, "media_id")
	args["format"] = "CAROUSEL"
	args["cards"] = []interface{}{
		map[string]interface{}{"media_id": cardImageID, "headline": "Plano Start", "link": "https://vozkoia.com/start"},
		map[string]interface{}{"media_id": cardVideoID, "media_kind": "video", "headline": "Plano Pro"},
	}
	return args
}

func TestACarouselKeepsEachCardWithItsMediaKindAndLink(t *testing.T) {
	deps, _, drafts, _ := fullAdDeps()
	saveDraft(t, deps, carouselArgs())
	c := drafts.created.Ads[0].Creative
	if c.Format != advertising.FormatCarousel || len(c.Cards) != 2 || c.Media.MediaID != "" ||
		c.Cards[0].Media != (advertising.MediaRef{Kind: advertising.MediaImage, MediaID: cardImageID}) || c.Cards[0].Link != "https://vozkoia.com/start" ||
		c.Cards[1].Media.Kind != advertising.MediaVideo {
		t.Fatalf("creative %+v", c)
	}
	fields := fieldsOf(NewSaveAdDraftTool(deps).(copilot.Describer).Describe(context.Background(), adContext, carouselArgs()))
	if fields["format"] != "Carrossel com 2 cartões" {
		t.Fatalf("fields %+v", fields)
	}
	preview := NewSaveAdDraftTool(deps).(copilot.Previewer).Preview(context.Background(), adContext, carouselArgs())
	if data := preview.Data.(AdCreativePreview); len(data.Cards) != 2 || data.Cards[1].Kind != advertising.MediaVideo {
		t.Fatalf("preview %+v", data)
	}
}

func TestACarouselCardWithAnInventedMediaIsRefused(t *testing.T) {
	deps, _, _, _ := fullAdDeps()
	args := carouselArgs()
	args["cards"] = []interface{}{
		map[string]interface{}{"media_id": "foto do produto"},
		map[string]interface{}{"media_id": cardVideoID, "media_kind": "video"},
	}
	refused(t, NewSaveAdDraftTool(deps), args)
	args["cards"] = []interface{}{
		map[string]interface{}{"media_id": cardImageID, "media_kind": "gif"},
		map[string]interface{}{"media_id": cardVideoID},
	}
	refused(t, NewSaveAdDraftTool(deps), args)
}

func TestAFlexibleAdCarriesEveryMediaAndTextVariation(t *testing.T) {
	deps, _, drafts, _ := fullAdDeps()
	args := websiteArgs()
	delete(args, "media_id")
	delete(args, "primary_text")
	args["format"] = "FLEXIBLE"
	args["media_ids"] = []interface{}{cardImageID}
	args["video_ids"] = []interface{}{cardVideoID}
	args["texts"] = []interface{}{"Atenda mais rápido", "Venda pelo WhatsApp"}
	args["headlines"] = []interface{}{"Vozko"}
	saveDraft(t, deps, args)
	c := drafts.created.Ads[0].Creative
	want := []advertising.MediaRef{{Kind: advertising.MediaImage, MediaID: cardImageID}, {Kind: advertising.MediaVideo, MediaID: cardVideoID}}
	if c.Format != advertising.FormatFlexible || !reflect.DeepEqual(c.Medias, want) || len(c.Texts) != 2 || c.Headlines[0] != "Vozko" {
		t.Fatalf("creative %+v", c)
	}
	args["media_ids"] = []interface{}{"imagem-1"}
	refused(t, NewSaveAdDraftTool(deps), args)
}

func TestAnExistingInstagramPostPromotesTheMediaForEngagement(t *testing.T) {
	deps, _, drafts, _ := fullAdDeps()
	args := createAdArgsMap()
	delete(args, "media_id")
	delete(args, "primary_text")
	delete(args, "whatsapp_number")
	args["destination"] = "ON_POST"
	args["format"] = "EXISTING_POST"
	args["post_id"], args["post_platform"], args["instagram_user_id"] = "17900000000000001", "instagram", "1784"
	saveDraft(t, deps, args)
	d := drafts.created
	if d.AdSet.Destination != advertising.DestinationOnPost || d.AdSet.Goal != advertising.GoalPostEngagement ||
		d.Ads[0].Creative.InstagramMediaID != "17900000000000001" || d.Ads[0].Creative.PostID != "" {
		t.Fatalf("draft %+v", d)
	}
	fields := fieldsOf(NewSaveAdDraftTool(deps).(copilot.Describer).Describe(context.Background(), adContext, args))
	if fields["format"] != "Publicação existente do Instagram" || fields["destination"] != "Engajamento com a publicação" {
		t.Fatalf("fields %+v", fields)
	}
	args["post_platform"] = "facebook"
	args["post_id"] = "1001_2002"
	saveDraft(t, deps, args)
	if c := drafts.created.Ads[0].Creative; c.PostID != "1001_2002" || c.InstagramMediaID != "" {
		t.Fatalf("creative %+v", c)
	}
}

func TestAnAppAdGoesToTheStoreLinkOfTheApp(t *testing.T) {
	deps, _, drafts, _ := fullAdDeps()
	args := createAdArgsMap()
	delete(args, "whatsapp_number")
	args["objective"], args["destination"] = "OUTCOME_APP_PROMOTION", "APP"
	args["app_id"], args["app_store_url"] = "555", "https://play.google.com/store/apps/details?id=com.vozko"
	saveDraft(t, deps, args)
	s := drafts.created.AdSet
	if s.Goal != advertising.GoalAppInstalls || s.AppID != "555" || s.AppStoreURL != "https://play.google.com/store/apps/details?id=com.vozko" {
		t.Fatalf("ad set %+v", s)
	}
	fields := fieldsOf(NewSaveAdDraftTool(deps).(copilot.Describer).Describe(context.Background(), adContext, args))
	if fields["objective"] != "Promoção do app" || !strings.HasPrefix(fields["destination"], "App na loja https://play.google.com") {
		t.Fatalf("fields %+v", fields)
	}
	delete(args, "app_id")
	refused(t, NewSaveAdDraftTool(deps), args)
}

func TestACatalogAdSellsAProductSet(t *testing.T) {
	deps, _, drafts, _ := fullAdDeps()
	args := websiteArgs()
	delete(args, "media_id")
	args["objective"], args["destination"], args["format"] = "OUTCOME_SALES", "CATALOG", "CATALOG"
	args["catalog_id"], args["product_set_id"] = "888", "999"
	saveDraft(t, deps, args)
	d := drafts.created
	if d.AdSet.CatalogID != "888" || d.AdSet.ProductSetID != "999" || d.Ads[0].Creative.Format != advertising.FormatCatalog {
		t.Fatalf("draft %+v", d)
	}
	delete(args, "product_set_id")
	refused(t, NewSaveAdDraftTool(deps), args)
}

func TestACampaignLifetimeBudgetLivesOnTheCampaign(t *testing.T) {
	deps, _, drafts, _ := fullAdDeps()
	args := websiteArgs()
	delete(args, "daily_budget")
	args["budget"], args["budget_kind"], args["budget_level"], args["end_date"] = 500.0, "lifetime", "campaign", "2026-10-31"
	saveDraft(t, deps, args)
	d := drafts.created
	if d.AdSet.Budget != nil || d.Campaign.Budget == nil || *d.Campaign.Budget != (advertising.Budget{Kind: advertising.BudgetLifetime, Amount: 50000}) {
		t.Fatalf("campaign %+v ad set budget %+v", d.Campaign, d.AdSet.Budget)
	}
	fields := fieldsOf(NewSaveAdDraftTool(deps).(copilot.Describer).Describe(context.Background(), adContext, args))
	if fields["budget"] != "BRL 500,00 no total" || !strings.HasPrefix(fields["budget_level"], "na campanha") || fields["ends"] != "até 31/10/2026" {
		t.Fatalf("fields %+v", fields)
	}
	delete(args, "end_date")
	refused(t, NewSaveAdDraftTool(deps), args)
}

func TestTheDailyBudgetNameOnlyMeansADailyBudget(t *testing.T) {
	deps, _, _, _ := fullAdDeps()
	args := websiteArgs()
	args["budget_kind"], args["end_date"] = "lifetime", "2026-10-31"
	refused(t, NewSaveAdDraftTool(deps), args)
	args = websiteArgs()
	delete(args, "daily_budget")
	refused(t, NewSaveAdDraftTool(deps), args)
}

func TestChosenPlacementsAndACostCapReachTheAdSet(t *testing.T) {
	deps, _, drafts, _ := fullAdDeps()
	args := websiteArgs()
	args["placements"] = []interface{}{"facebook", "instagram"}
	args["bid_strategy"], args["bid_amount"] = "COST_CAP", 12.5
	saveDraft(t, deps, args)
	s := drafts.created.AdSet
	if s.Placements.Automatic || !reflect.DeepEqual(s.Placements.Platforms, []string{"facebook", "instagram"}) ||
		s.Bid != (advertising.Bid{Strategy: advertising.BidCostCap, Amount: 1250}) {
		t.Fatalf("ad set %+v", s)
	}
	fields := fieldsOf(NewSaveAdDraftTool(deps).(copilot.Describer).Describe(context.Background(), adContext, args))
	if fields["placements"] == "" || strings.Contains(fields["placements"], "automáticos") || fields["bid"] == "" {
		t.Fatalf("fields %+v", fields)
	}
	args["placements"] = []interface{}{"automatic", "facebook"}
	refused(t, NewSaveAdDraftTool(deps), args)
}

func TestPlacementsAreAutomaticByDefault(t *testing.T) {
	deps, _, drafts, _ := fullAdDeps()
	saveDraft(t, deps, websiteArgs())
	if p := drafts.created.AdSet.Placements; !p.Automatic {
		t.Fatalf("placements %+v", p)
	}
}

func TestCustomAudiencesTakeOnlyMetaIds(t *testing.T) {
	deps, _, drafts, _ := fullAdDeps()
	args := websiteArgs()
	args["custom_audiences"] = []interface{}{"23850001:Clientes"}
	args["excluded_custom_audiences"] = []interface{}{"23850002"}
	saveDraft(t, deps, args)
	tg := drafts.created.AdSet.Targeting
	if len(tg.CustomAudiences) != 1 || tg.CustomAudiences[0] != (advertising.TargetRef{ID: "23850001", Name: "Clientes"}) || tg.ExcludedCustomAudiences[0].ID != "23850002" {
		t.Fatalf("targeting %+v", tg)
	}
	args["custom_audiences"] = []interface{}{"clientes vip"}
	refused(t, NewSaveAdDraftTool(deps), args)
}

func TestTheStartDateBeginsAtMidnightInTheAccountTimezone(t *testing.T) {
	deps, _, drafts, _ := fullAdDeps()
	args := websiteArgs()
	args["start_date"] = "2026-10-10"
	saveDraft(t, deps, args)
	want := time.Date(2026, 10, 10, 3, 0, 0, 0, time.UTC)
	if start := drafts.created.AdSet.StartAt; start == nil || !start.Equal(want) {
		t.Fatalf("start %v", start)
	}
	if fields := fieldsOf(NewSaveAdDraftTool(deps).(copilot.Describer).Describe(context.Background(), adContext, args)); fields["starts"] != "a partir de 10/10/2026" {
		t.Fatalf("fields %+v", fields)
	}
	args["start_date"] = "10/10/2026"
	refused(t, NewSaveAdDraftTool(deps), args)
}

func fullArgs() map[string]interface{} {
	args := carouselArgs()
	delete(args, "daily_budget")
	args["budget"], args["budget_kind"], args["budget_level"] = 700.0, "lifetime", "campaign"
	args["bid_strategy"], args["bid_amount"] = "LOWEST_COST_WITH_BID_CAP", 3.0
	args["start_date"], args["end_date"] = "2026-10-05", "2026-10-31"
	args["placements"] = []interface{}{"facebook", "instagram"}
	args["custom_audiences"] = []interface{}{"23850001:Clientes"}
	args["interests"] = []interface{}{"6003:Marketing digital"}
	args["genders"] = []interface{}{"female"}
	args["age_min"], args["age_max"] = 25, 45
	args["keep_paused"] = true
	return args
}

func TestASavedDraftReadsBackInTheArgumentsThatBuiltIt(t *testing.T) {
	account := &advertising.AdAccount{ID: adAccountUUID, Currency: "BRL", Timezone: "America/Sao_Paulo"}
	original := savedFrom(t, fullArgs())
	rebuilt, err := draftArgs(original, account).draft(account)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rebuilt, original) {
		t.Fatalf("rebuilt %+v\noriginal %+v", rebuilt, original)
	}
}

func TestGetAdDraftShowsTheSettingsWithEloArgumentNames(t *testing.T) {
	deps, _, drafts, _ := fullAdDeps()
	savedDraft(t, drafts, fullArgs())
	result := NewGetAdDraftTool(deps).Execute(context.Background(), adContext, map[string]interface{}{"draft_id": savedDraftID})
	settings := result.Data.(map[string]interface{})["settings"].(map[string]interface{})
	if settings["budget"] != 700.0 || settings["budget_level"] != "campaign" || settings["format"] != "CAROUSEL" || settings["end_date"] != "2026-10-31" ||
		settings["start_date"] != "2026-10-05" || settings["media_id"] != nil {
		t.Fatalf("settings %+v", settings)
	}
}

func TestUpdateAdDraftChangesOnlyTheGivenFields(t *testing.T) {
	deps, _, drafts, _ := fullAdDeps()
	savedDraft(t, drafts, websiteArgs())
	stored := drafts.stored.Draft.Content
	stored.AdSet.Targeting.Locations = []advertising.GeoLocation{{Kind: advertising.LocationCity, Key: "2430536", Name: "São Paulo", RadiusKm: 25}}
	stored.AdSet.Schedule = nil
	drafts.stored.Draft.Content = stored
	tool := NewUpdateAdDraftTool(deps)
	args := map[string]interface{}{"draft_id": savedDraftID, "version": 7, "daily_budget": 50.0, "headline": "Novo título"}
	if err := tool.(copilot.Validator).Validate(context.Background(), adContext, args); err != nil {
		t.Fatal(err)
	}
	fields := fieldsOf(tool.(copilot.Describer).Describe(context.Background(), adContext, args))
	if fields["changes"] != "daily_budget, headline" || fields["budget"] != "BRL 50,00 por dia" {
		t.Fatalf("fields %+v", fields)
	}
	result := tool.Execute(context.Background(), adContext, args)
	if result.Status != copilot.StatusOK || drafts.updatedVersion != 7 {
		t.Fatalf("result %+v version %d", result, drafts.updatedVersion)
	}
	got := drafts.updated
	if got.AdSet.Budget.Amount != 5000 || got.Ads[0].Creative.Headline != "Novo título" || got.Ads[0].Creative.PrimaryText != "Fale com a gente" ||
		!reflect.DeepEqual(got.AdSet.Targeting.Locations, stored.AdSet.Targeting.Locations) || got.AdSet.Goal != advertising.GoalLandingPageViews {
		t.Fatalf("updated %+v", got)
	}
}

func TestUpdateAdDraftRecommendsAGoalAgainWhenTheRouteChanges(t *testing.T) {
	deps, _, drafts, _ := fullAdDeps()
	savedDraft(t, drafts, websiteArgs())
	args := map[string]interface{}{"draft_id": savedDraftID, "version": 7, "objective": "OUTCOME_ENGAGEMENT", "destination": "WHATSAPP", "whatsapp_number": "5511988887777"}
	if result := NewUpdateAdDraftTool(deps).Execute(context.Background(), adContext, args); result.Status != copilot.StatusOK {
		t.Fatalf("result %+v", result)
	}
	if drafts.updated.Campaign.Objective != advertising.ObjectiveEngagement || drafts.updated.AdSet.Goal != advertising.GoalConversations || drafts.updated.AdSet.WhatsAppNumber != "5511988887777" {
		t.Fatalf("draft %+v", drafts.updated)
	}
}

func TestUpdateAdDraftRenamesTheLevelsThatFollowedTheCampaign(t *testing.T) {
	deps, _, drafts, _ := fullAdDeps()
	savedDraft(t, drafts, websiteArgs())
	NewUpdateAdDraftTool(deps).Execute(context.Background(), adContext, map[string]interface{}{"draft_id": savedDraftID, "version": 7, "campaign_name": "Black Friday"})
	if d := drafts.updated; d.Campaign.Name != "Black Friday" || d.AdSet.Name != "Black Friday" || d.Ads[0].Name != "Black Friday" {
		t.Fatalf("draft %+v", d)
	}
}

func TestUpdateAdDraftRefusesWhatItCannotApply(t *testing.T) {
	deps, _, drafts, _ := fullAdDeps()
	savedDraft(t, drafts, websiteArgs())
	tool := NewUpdateAdDraftTool(deps)
	refused(t, tool, map[string]interface{}{"draft_id": savedDraftID, "version": 7})
	refused(t, tool, map[string]interface{}{"draft_id": savedDraftID, "version": 7, "orcamento": 10.0})
	refused(t, tool, map[string]interface{}{"draft_id": savedDraftID, "version": 7, "ad_account_id": adAccountUUID})
	refused(t, tool, map[string]interface{}{"draft_id": "rascunho 1", "version": 7, "daily_budget": 10.0})
	refused(t, tool, map[string]interface{}{"draft_id": savedDraftID, "version": 7, "objective": "OUTCOME_AWARENESS", "destination": "WHATSAPP"})
	refused(t, tool, map[string]interface{}{"draft_id": savedDraftID, "daily_budget": 10.0})
	refused(t, tool, map[string]interface{}{"draft_id": savedDraftID, "version": 6, "daily_budget": 10.0})
	drafts.stored.Draft.JobID = "job-1"
	drafts.stored.Draft.UpdatedAt = time.Now()
	refused(t, tool, map[string]interface{}{"draft_id": savedDraftID, "version": 7, "daily_budget": 10.0})
	if drafts.updated != nil {
		t.Fatalf("nothing may be saved, got %+v", drafts.updated)
	}
}

func TestUpdateAdDraftLeavesTheCreativesOfAMultiAdDraftToTheEditor(t *testing.T) {
	deps, _, drafts, _ := fullAdDeps()
	savedDraft(t, drafts, websiteArgs())
	content := drafts.stored.Draft.Content
	content.Ads = append(content.Ads, content.Ads[0])
	drafts.stored.Draft.Content = content
	tool := NewUpdateAdDraftTool(deps)
	refused(t, tool, map[string]interface{}{"draft_id": savedDraftID, "version": 7, "headline": "Outro"})
	if result := tool.Execute(context.Background(), adContext, map[string]interface{}{"draft_id": savedDraftID, "version": 7, "daily_budget": 40.0}); result.Status != copilot.StatusOK || len(drafts.updated.Ads) != 2 {
		t.Fatalf("result %+v", result)
	}
}

type stubCreativeSources struct{}

func (stubCreativeSources) Posts(_ context.Context, _, _, pageID, platform string) ([]advertising.RemotePost, error) {
	return []advertising.RemotePost{{ID: pageID + "_1", Platform: platform, Message: strings.Repeat("a", 300)}}, nil
}
func (stubCreativeSources) Apps(context.Context, string, string) ([]advertising.RemoteApp, error) {
	return []advertising.RemoteApp{{ID: "555", Name: "Vozko", StoreURLs: []string{"https://play.google.com/store/apps/details?id=com.vozko"}}}, nil
}
func (stubCreativeSources) Catalogs(context.Context, string, string) ([]advertising.RemoteCatalog, error) {
	return []advertising.RemoteCatalog{{ID: "888", Name: "Loja", ProductSets: []advertising.RemoteProductSet{{ID: "999", Name: "Todos", ProductCount: 12}}}}, nil
}

func TestTheSourceListsGiveTheIdsTheAdArgumentsTake(t *testing.T) {
	deps, _, _, _ := fullAdDeps()
	deps.Sources = stubCreativeSources{}
	account := map[string]interface{}{"ad_account_id": adAccountUUID}
	posts := NewListPagePostsTool(deps).Execute(context.Background(), adContext, map[string]interface{}{"ad_account_id": adAccountUUID, "page_id": "1001", "platform": "facebook"})
	post := posts.Data.(map[string]interface{})["posts"].([]map[string]interface{})[0]
	if post["post_id"] != "1001_1" || post["post_platform"] != "facebook" || len([]rune(post["text"].(string))) > maxPostTextRunes+1 {
		t.Fatalf("posts %+v", posts)
	}
	apps := NewListAdAppsTool(deps).Execute(context.Background(), adContext, account)
	if app := apps.Data.(map[string]interface{})["apps"].([]map[string]interface{})[0]; app["app_id"] != "555" {
		t.Fatalf("apps %+v", apps)
	}
	catalogs := NewListAdCatalogsTool(deps).Execute(context.Background(), adContext, account)
	catalog := catalogs.Data.(map[string]interface{})["catalogs"].([]map[string]interface{})[0]
	if catalog["catalog_id"] != "888" || catalog["product_sets"].([]map[string]interface{})[0]["product_set_id"] != "999" {
		t.Fatalf("catalogs %+v", catalogs)
	}
	if result := NewListAdAppsTool(deps).Execute(context.Background(), adContext, map[string]interface{}{"ad_account_id": "act_1"}); result.Status != copilot.StatusError {
		t.Fatalf("an invented account must be refused, got %+v", result)
	}
}

func TestTheCardsArgumentDescribesEachCard(t *testing.T) {
	for _, tool := range []copilot.Tool{NewCreateAdTool(AdsDeps{}), NewSaveAdDraftTool(AdsDeps{}), NewUpdateAdDraftTool(AdsDeps{})} {
		cards := tool.Definition().Parameters["cards"]
		if cards.Items == nil || cards.Items.Type != "object" || cards.Items.Properties["media_id"].Type != "string" {
			t.Fatalf("%s cards %+v", tool.Definition().Name, cards)
		}
	}
	update := NewUpdateAdDraftTool(AdsDeps{}).Definition()
	if _, ok := update.Parameters["ad_account_id"]; ok || !reflect.DeepEqual(update.Required, []string{"draft_id", "version"}) || update.Parameters["version"].Type != "integer" {
		t.Fatalf("update definition %+v", update)
	}
}
