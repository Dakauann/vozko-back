package copilottools

import (
	"context"
	"testing"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
)

const otherAdAccount = "9e1f2a3b-4c5d-4e6f-8a7b-0c1d2e3f4a5b"

func publishedCarousel(accountID string) *advertising.ObjectDetail {
	detail := carouselDetail()
	detail.Object.AdAccountID = accountID
	return detail
}

func leadArgsFrom(source string) map[string]interface{} {
	args := websiteArgs()
	args["objective"], args["destination"] = "OUTCOME_LEADS", "ON_AD"
	args["lead_form_id"] = "555001"
	delete(args, "link")
	delete(args, "display_link")
	args["format"] = "CAROUSEL"
	delete(args, "media_id")
	args["cards"] = []interface{}{
		map[string]interface{}{"media_id": "meta:h1", "headline": "Todos os canais num só lugar"},
		map[string]interface{}{"media_id": "meta:h2", "headline": "Todos os canais num só lugar"},
	}
	if source != "" {
		args["source_ad_id"] = source
	}
	return args
}

func TestANewDraftReusesThePublishedAdsImages(t *testing.T) {
	deps, _, drafts, editor := fullAdDeps()
	editor.details = map[string]*advertising.ObjectDetail{"120303": publishedCarousel(adAccountUUID)}
	if result := saveDraft(t, deps, leadArgsFrom("120303")); result.Status != copilot.StatusOK {
		t.Fatalf("result %+v", result)
	}
	cards := drafts.created.Ads[0].Creative.Cards
	if len(cards) != 2 || cards[0].Media.MediaID != "meta:h1" || cards[1].Media.MediaID != "meta:h2" {
		t.Fatalf("cards %+v", cards)
	}
}

func TestPublishedImagesNeedTheirAdInTheSameAccount(t *testing.T) {
	deps, _, _, editor := fullAdDeps()
	editor.details = map[string]*advertising.ObjectDetail{
		"120303": publishedCarousel(otherAdAccount),
		"120200": {Object: &advertising.Object{MetaID: "120200", AdAccountID: adAccountUUID, Level: advertising.LevelCampaign}},
	}
	for _, source := range []string{"", "120303", "120200"} {
		refused(t, NewSaveAdDraftTool(deps), leadArgsFrom(source))
	}
	refused(t, NewCreateAdTool(deps), leadArgsFrom("120303"))
}

func TestGetAdCreativeShowsWhatTheAdIsMadeOf(t *testing.T) {
	deps, _, _, editor := fullAdDeps()
	editor.details = map[string]*advertising.ObjectDetail{"120303": publishedCarousel(adAccountUUID)}
	result := NewGetAdCreativeTool(deps).Execute(context.Background(), adContext, map[string]interface{}{"meta_id": "120303"})
	if result.Status != copilot.StatusOK {
		t.Fatalf("result %+v", result)
	}
	data := result.Data.(map[string]interface{})
	creative := data["creative"].(*advertising.CreativeDraft)
	if data["ad_account_id"] != adAccountUUID || creative.Cards[1].Media.MediaID != "meta:h2" || creative.Greeting != "Olá!" {
		t.Fatalf("data %+v", data)
	}
}
