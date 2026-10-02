package advertising

import "testing"

func TestEveryObjectIsCreatedPausedSoAHalfBuiltAdNeverSpends(t *testing.T) {
	d := validDraft()
	if CampaignSpecOf(d).Status != StatusPaused || AdSetSpecOf(d, "c").Status != StatusPaused || AdSpecOf(d, 0, "s", "cr", UploadedMedia{}).Status != StatusPaused {
		t.Fatal("an object would be created active")
	}
}

func TestWhatsAppAdPromotesTheChosenNumberOnThePage(t *testing.T) {
	spec := AdSetSpecOf(validDraft(), "c-1")
	if spec.PromotedObject != (PromotedObject{PageID: "page-1", WhatsAppPhoneNumber: "5511988887777"}) {
		t.Fatalf("promoted object %+v", spec.PromotedObject)
	}
	if spec.Goal != GoalConversations || spec.BillingEvent != BillingImpressions || spec.Budget.Amount != 2000 {
		t.Fatalf("delivery %+v", spec)
	}
}

func TestWebsiteConversionsPromoteThePixelNotThePage(t *testing.T) {
	d := validDraft()
	d.Campaign.Objective = ObjectiveSales
	d.AdSet.Destination, d.AdSet.Goal, d.AdSet.PixelID, d.AdSet.PixelEvent = DestinationWebsite, GoalOffsiteConversion, "px-1", EventPurchase
	if got := AdSetSpecOf(d, "c").PromotedObject; got != (PromotedObject{PixelID: "px-1", PixelEvent: EventPurchase}) {
		t.Fatalf("promoted object %+v", got)
	}
}

func TestCampaignBudgetMovesBudgetAndBidOffTheAdSet(t *testing.T) {
	d := validDraft()
	d.Campaign.Budget, d.Campaign.Bid = &Budget{Kind: BudgetDaily, Amount: 9000}, Bid{Strategy: BidCostCap, Amount: 500}
	d.AdSet.Budget = nil
	if spec := AdSetSpecOf(d, "c"); spec.Budget != nil || spec.Bid.Strategy != "" {
		t.Fatalf("ad set kept budget %+v", spec)
	}
	if spec := CampaignSpecOf(d); spec.Budget.Amount != 9000 || spec.Bid.Strategy != BidCostCap {
		t.Fatalf("campaign spec %+v", spec)
	}
}

func TestSpecialCategoryIsDeclaredOnlyWhenChosen(t *testing.T) {
	d := validDraft()
	if len(CampaignSpecOf(d).SpecialCategories) != 0 {
		t.Fatal("NONE declared as a category")
	}
	d.Campaign.SpecialCategory = CategoryEmployment
	if got := CampaignSpecOf(d).SpecialCategories; len(got) != 1 || got[0] != CategoryEmployment {
		t.Fatalf("categories %v", got)
	}
}

func TestCallToActionFollowsTheDestination(t *testing.T) {
	cases := map[Destination]CallToAction{
		DestinationWhatsApp: CTAWhatsAppMessage, DestinationMessenger: CTAMessagePage, DestinationInstagramDirect: CTAInstagramDirect,
		DestinationApp: CTAInstallApp, DestinationInstantForm: CTASignUp, DestinationWebsite: CTALearnMore, DestinationCatalog: CTAShopNow,
	}
	for d, want := range cases {
		if got := (CreativeDraft{}).ResolvedCallToAction(d); got != want {
			t.Fatalf("%s: %s", d, got)
		}
	}
	if got := (CreativeDraft{CallToAction: CTABookNow}).ResolvedCallToAction(DestinationWebsite); got != CTABookNow {
		t.Fatalf("chosen cta ignored: %s", got)
	}
}

func TestFlexibleCreativeUsesDynamicCreativeOutsideSalesAndAppPromotion(t *testing.T) {
	d := validDraft()
	d.Ads[0].Creative = CreativeDraft{Format: FormatFlexible, Texts: []string{"a", "b"}, Medias: []MediaRef{{Kind: MediaImage, MediaID: "m1"}}}
	if !AdSetSpecOf(d, "c").DynamicCreative || CreativeSpecOf(d, 0, UploadedMedia{}).AssetMode != AssetsDynamic {
		t.Fatal("engagement flexible creative is not dynamic creative")
	}
	d.Ads = append(d.Ads, AdItem{Name: "x", Creative: imageCreative()})
	requireIssues(t, d.Validate(draftNow), FieldIssue{"ads", "dynamic_creative_needs_own_ad_set"})
	sales := validDraft()
	sales.Campaign.Objective = ObjectiveSales
	sales.Ads[0].Creative = CreativeDraft{Format: FormatFlexible, Texts: []string{"a"}, Medias: []MediaRef{{Kind: MediaImage, MediaID: "m1"}}}
	if AdSetSpecOf(sales, "c").DynamicCreative || CreativeSpecOf(sales, 0, UploadedMedia{}).AssetMode != AssetsGroups {
		t.Fatal("sales flexible creative should use asset groups")
	}
}

func TestCatalogSalesCarryTheCatalogOnTheCampaignAndTheSetOnTheAdSet(t *testing.T) {
	d := validDraft()
	d.Campaign.Objective = ObjectiveSales
	d.AdSet.Destination, d.AdSet.Goal, d.AdSet.CatalogID, d.AdSet.ProductSetID = DestinationCatalog, GoalOffsiteConversion, "cat-1", "set-1"
	if CampaignSpecOf(d).ProductCatalogID != "cat-1" {
		t.Fatal("catalog missing on campaign")
	}
	if got := AdSetSpecOf(d, "c").PromotedObject; got.ProductSetID != "set-1" || got.PixelEvent != EventPurchase {
		t.Fatalf("promoted %+v", got)
	}
}

func TestAssetGroupsTravelOnTheAdForSalesFlexibleFormat(t *testing.T) {
	d := validDraft()
	d.Campaign.Objective = ObjectiveSales
	d.Ads[0].Creative = CreativeDraft{Format: FormatFlexible, Texts: []string{"a"}, Medias: []MediaRef{{Kind: MediaImage, MediaID: "m1"}}}
	if spec := AdSpecOf(d, 0, "s", "cr", UploadedMedia{}); spec.AssetGroups == nil || spec.AssetGroups.AssetMode != AssetsGroups {
		t.Fatalf("ad spec %+v", spec)
	}
	if AdSpecOf(validDraft(), 0, "s", "cr", UploadedMedia{}).AssetGroups != nil {
		t.Fatal("single image ad carries asset groups")
	}
}
