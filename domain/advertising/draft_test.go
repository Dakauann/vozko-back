package advertising

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

var draftNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func imageCreative() CreativeDraft {
	return CreativeDraft{Format: FormatImage, PrimaryText: "Fale com a gente", Media: MediaRef{Kind: MediaImage, MediaID: "media-1"}}
}

func validDraft() AdDraft {
	d := AdDraft{
		AdAccountID: "acc-1",
		Identity:    Identity{PageID: "page-1"},
		Campaign:    CampaignDraft{Name: "Leads outubro", Objective: ObjectiveEngagement},
		AdSet: AdSetDraft{
			Destination: DestinationWhatsApp, Goal: GoalConversations, WhatsAppNumber: "+55 (11) 98888-7777",
			Budget:    &Budget{Kind: BudgetDaily, Amount: 2000},
			Targeting: Targeting{Locations: []GeoLocation{{Kind: LocationCountry, Key: "BR", Name: "Brasil"}}},
		},
		Ads: []AdItem{{Creative: imageCreative()}},
	}
	d.Normalize()
	return d
}

func issuesOf(t *testing.T, err error) []FieldIssue {
	t.Helper()
	var v *ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("got %v, want a ValidationError", err)
	}
	return v.Issues
}

func requireIssues(t *testing.T, err error, want ...FieldIssue) {
	t.Helper()
	got := issuesOf(t, err)
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Fatalf("missing %v in %v", w, got)
		}
	}
}

func TestValidWhatsAppDraftPasses(t *testing.T) {
	if err := validDraft().Validate(draftNow); err != nil {
		t.Fatalf("valid draft refused: %v", err)
	}
}

func TestNormalizeFillsNamesAgePlacementsAndDigits(t *testing.T) {
	d := validDraft()
	if d.AdSet.Name != "Leads outubro" || d.Ads[0].Name != "Leads outubro" {
		t.Fatalf("names %q %q", d.AdSet.Name, d.Ads[0].Name)
	}
	if d.AdSet.WhatsAppNumber != "5511988887777" || !d.AdSet.Placements.Automatic {
		t.Fatalf("number %q placements %+v", d.AdSet.WhatsAppNumber, d.AdSet.Placements)
	}
	if d.AdSet.Targeting.AgeMin != 18 || d.AdSet.Targeting.AgeMax != 65 || d.Campaign.SpecialCategory != CategoryNone {
		t.Fatalf("defaults %+v %s", d.AdSet.Targeting, d.Campaign.SpecialCategory)
	}
}

func TestSeveralAdsGetNumberedNames(t *testing.T) {
	d := validDraft()
	d.Ads = []AdItem{{Creative: imageCreative()}, {Creative: imageCreative()}}
	d.Normalize()
	if d.Ads[0].Name != "Leads outubro 1" || d.Ads[1].Name != "Leads outubro 2" {
		t.Fatalf("names %q %q", d.Ads[0].Name, d.Ads[1].Name)
	}
}

func TestGoalMustBelongToTheObjectiveAndDestination(t *testing.T) {
	d := validDraft()
	d.AdSet.Goal = GoalAppInstalls
	requireIssues(t, d.Validate(draftNow), FieldIssue{"adSet.goal", "not_for_objective"})
	traffic := validDraft()
	traffic.Campaign.Objective = ObjectiveTraffic
	traffic.AdSet.Destination, traffic.AdSet.Goal = DestinationWebsite, GoalLandingPageViews
	traffic.Ads[0].Creative.Link = "https://loja.example.com"
	if err := traffic.Validate(draftNow); err != nil {
		t.Fatalf("website traffic refused: %v", err)
	}
}

func TestPoliticalAdsAreRefused(t *testing.T) {
	d := validDraft()
	d.Campaign.SpecialCategory = CategoryPolitics
	requireIssues(t, d.Validate(draftNow), FieldIssue{"campaign.specialCategory", "political_not_supported"})
}

func TestRestrictedCategoriesEnforceMetaTargetingRules(t *testing.T) {
	d := validDraft()
	d.Campaign.SpecialCategory = CategoryHousing
	d.AdSet.Targeting.AgeMin = 25
	d.AdSet.Targeting.Genders = []int{GenderFemale}
	d.AdSet.Targeting.ExcludedCustomAudiences = []TargetRef{{ID: "ca-1"}}
	d.AdSet.Targeting.Locations = []GeoLocation{{Kind: LocationCity, Key: "123", RadiusKm: 17}}
	requireIssues(t, d.Validate(draftNow),
		FieldIssue{"adSet.targeting.age", "restricted_category"},
		FieldIssue{"adSet.targeting.genders", "restricted_category"},
		FieldIssue{"adSet.targeting.exclusions", "restricted_category"},
		FieldIssue{"adSet.targeting.locations", "restricted_category"},
	)
}

func TestBudgetLivesOnTheCampaignOrTheAdSetNeverBoth(t *testing.T) {
	both := validDraft()
	both.Campaign.Budget = &Budget{Kind: BudgetDaily, Amount: 5000}
	requireIssues(t, both.Validate(draftNow), FieldIssue{"adSet.budget", "campaign_has_budget"})
	none := validDraft()
	none.AdSet.Budget = nil
	requireIssues(t, none.Validate(draftNow), FieldIssue{"adSet.budget", "required"})
	cbo := validDraft()
	cbo.Campaign.Budget, cbo.AdSet.Budget = &Budget{Kind: BudgetDaily, Amount: 5000}, nil
	if err := cbo.Validate(draftNow); err != nil {
		t.Fatalf("campaign budget refused: %v", err)
	}
}

func TestLifetimeBudgetNeedsAnEndAndEnablesSchedules(t *testing.T) {
	d := validDraft()
	d.AdSet.Budget = &Budget{Kind: BudgetLifetime, Amount: 100_000}
	requireIssues(t, d.Validate(draftNow), FieldIssue{"adSet.endAt", "required_for_lifetime_budget"})
	end := draftNow.Add(7 * 24 * time.Hour)
	d.AdSet.EndAt = &end
	d.AdSet.Schedule = []DayPart{{Days: []int{1, 2, 3, 4, 5}, StartMinute: 9 * 60, EndMinute: 18 * 60}}
	if err := d.Validate(draftNow); err != nil {
		t.Fatalf("lifetime schedule refused: %v", err)
	}
	daily := validDraft()
	daily.AdSet.Schedule = d.AdSet.Schedule
	requireIssues(t, daily.Validate(draftNow), FieldIssue{"adSet.schedule", "needs_lifetime_budget"})
}

func TestBidStrategiesNeedTheirAmounts(t *testing.T) {
	d := validDraft()
	d.AdSet.Bid = Bid{Strategy: BidCostCap}
	requireIssues(t, d.Validate(draftNow), FieldIssue{"adSet.bid.amount", "must_be_positive"})
	d.AdSet.Bid = Bid{Strategy: BidMinROAS, ROASFloor: 2}
	requireIssues(t, d.Validate(draftNow), FieldIssue{"adSet.bid.strategy", "roas_needs_value_goal"})
}

func TestEachDestinationNeedsItsOwnIdentity(t *testing.T) {
	wa := validDraft()
	wa.AdSet.WhatsAppNumber = ""
	requireIssues(t, wa.Validate(draftNow), FieldIssue{"adSet.whatsAppNumber", "required"})
	ig := validDraft()
	ig.AdSet.Destination = DestinationInstagramDirect
	requireIssues(t, ig.Validate(draftNow), FieldIssue{"adSet.instagramUserId", "required"})
	sales := validDraft()
	sales.Campaign.Objective = ObjectiveSales
	sales.AdSet.Destination, sales.AdSet.Goal = DestinationWebsite, GoalOffsiteConversion
	requireIssues(t, sales.Validate(draftNow), FieldIssue{"adSet.pixelId", "required"}, FieldIssue{"adSet.pixelEvent", "invalid"})
	app := validDraft()
	app.Campaign.Objective = ObjectiveAppPromotion
	app.AdSet.Destination, app.AdSet.Goal = DestinationApp, GoalAppInstalls
	requireIssues(t, app.Validate(draftNow), FieldIssue{"adSet.appId", "required"}, FieldIssue{"adSet.appStoreUrl", "required"})
}

func TestInstantFormAdsNeedAForm(t *testing.T) {
	d := validDraft()
	d.Campaign.Objective = ObjectiveLeads
	d.AdSet.Destination, d.AdSet.Goal = DestinationInstantForm, GoalLeadGeneration
	requireIssues(t, d.Validate(draftNow), FieldIssue{"ads[0].creative.leadFormId", "required"})
	d.Ads[0].Creative.LeadFormID = "form-1"
	if err := d.Validate(draftNow); err != nil {
		t.Fatalf("instant form refused: %v", err)
	}
}

func TestCarouselNeedsTwoToTenCards(t *testing.T) {
	d := validDraft()
	d.Ads[0].Creative = CreativeDraft{Format: FormatCarousel, PrimaryText: "Veja", Cards: []CarouselCard{{Media: MediaRef{Kind: MediaImage, MediaID: "m1"}}}}
	requireIssues(t, d.Validate(draftNow), FieldIssue{"ads[0].creative.cards", "count"})
	d.Ads[0].Creative.Cards = append(d.Ads[0].Creative.Cards, CarouselCard{Media: MediaRef{Kind: MediaVideo, MediaID: "m2"}})
	if err := d.Validate(draftNow); err != nil {
		t.Fatalf("carousel refused: %v", err)
	}
}

func TestFlexibleCreativeNeedsTextsAndMedia(t *testing.T) {
	d := validDraft()
	d.Ads[0].Creative = CreativeDraft{Format: FormatFlexible}
	requireIssues(t, d.Validate(draftNow), FieldIssue{"ads[0].creative.texts", "count"}, FieldIssue{"ads[0].creative.medias", "count"})
}

func TestExistingPostOnlyForPostDestinationsAndNeedsOnePost(t *testing.T) {
	d := validDraft()
	d.Ads[0].Creative = CreativeDraft{Format: FormatExistingPost}
	requireIssues(t, d.Validate(draftNow), FieldIssue{"ads[0].creative.format", "not_for_destination"})
	d.AdSet.Destination, d.AdSet.Goal = DestinationOnPost, GoalPostEngagement
	requireIssues(t, d.Validate(draftNow), FieldIssue{"ads[0].creative.postId", "required"})
	d.Ads[0].Creative.PostID = "page-1_99"
	if err := d.Validate(draftNow); err != nil {
		t.Fatalf("boost refused: %v", err)
	}
}

func TestWebsiteAdsNeedAnHTTPSLink(t *testing.T) {
	d := validDraft()
	d.Campaign.Objective = ObjectiveTraffic
	d.AdSet.Destination, d.AdSet.Goal = DestinationWebsite, GoalLinkClicks
	d.Ads[0].Creative.Link = "http://loja.example.com"
	requireIssues(t, d.Validate(draftNow), FieldIssue{"ads[0].creative.link", "invalid_url"})
}

func TestManualPlacementsMustKeepTheDestinationsApp(t *testing.T) {
	d := validDraft()
	d.Identity.InstagramUserID = "ig-1"
	d.AdSet.Destination = DestinationInstagramDirect
	d.AdSet.Placements = Placements{Platforms: []string{PlatformFacebook}}
	requireIssues(t, d.Validate(draftNow), FieldIssue{"adSet.placements.platforms", "instagram_required"})
	d.AdSet.Placements = Placements{Platforms: []string{PlatformInstagram}, Positions: map[string][]string{PlatformInstagram: {"moon"}}}
	requireIssues(t, d.Validate(draftNow), FieldIssue{"adSet.placements.positions", "invalid"})
}

func TestAddingAdsToAnExistingAdSetSkipsItsFields(t *testing.T) {
	d := AdDraft{AdAccountID: "acc-1", Identity: Identity{PageID: "page-1"}, AdSet: AdSetDraft{ExistingID: "s-1"}, Ads: []AdItem{{Name: "Novo", Creative: imageCreative()}}}
	d.Normalize()
	d.Adopt(ExistingParents{
		AdSet:    &Object{MetaID: "s-1", CampaignMetaID: "c-1", DestinationType: "WHATSAPP", OptimizationGoal: "CONVERSATIONS"},
		Campaign: &Object{MetaID: "c-1", Objective: string(ObjectiveEngagement), Name: "Leads"},
	})
	if err := d.Validate(draftNow); err != nil {
		t.Fatalf("adding an ad refused: %v", err)
	}
	if d.NewCampaign() || d.NewAdSet() || d.Campaign.ExistingID != "c-1" {
		t.Fatalf("draft %+v", d.Campaign)
	}
}

func TestAddingAdsToAnAppAdSetReadsTheAppDestination(t *testing.T) {
	d := AdDraft{AdAccountID: "acc-1", Identity: Identity{PageID: "page-1"}, AdSet: AdSetDraft{ExistingID: "s-1"}}
	d.Adopt(ExistingParents{
		AdSet:    &Object{MetaID: "s-1", CampaignMetaID: "c-1", OptimizationGoal: string(GoalAppInstalls)},
		Campaign: &Object{MetaID: "c-1", Objective: string(ObjectiveAppPromotion), Name: "App"},
	})
	if d.AdSet.Destination != DestinationApp || d.AdSet.Goal != GoalAppInstalls {
		t.Fatalf("ad set %+v", d.AdSet)
	}
}

func TestTooManyAdsAndLongNamesAreRefused(t *testing.T) {
	d := validDraft()
	for len(d.Ads) <= maxAdsPerDraft {
		d.Ads = append(d.Ads, AdItem{Name: "x", Creative: imageCreative()})
	}
	requireIssues(t, d.Validate(draftNow), FieldIssue{"ads", "too_many"})
	long := validDraft()
	long.Campaign.Name = strings.Repeat("a", 401)
	requireIssues(t, long.Validate(draftNow), FieldIssue{"campaign.name", "too_long"})
}

func TestSameWhatsAppNumberIgnoresFormatting(t *testing.T) {
	if !SameWhatsAppNumber("+55 11 98888-7777", "5511988887777") || SameWhatsAppNumber("", "") {
		t.Fatal("number matching is wrong")
	}
}

func TestOnlyWorkspaceNumbersEqualToThePageLinkedNumberAreOffered(t *testing.T) {
	page := RemotePage{WhatsAppNumber: "+55 11 98888-7777"}
	numbers := []WorkspaceNumber{{Kind: NumberOfficial, Number: "5511988887777"}, {Kind: NumberUnofficial, Number: "5511900000000"}}
	got := NumbersLinkedTo(page, numbers)
	if len(got) != 1 || got[0].Kind != NumberOfficial {
		t.Fatalf("linked %v", got)
	}
	if NumbersLinkedTo(RemotePage{}, numbers) != nil {
		t.Fatal("page without a whatsapp number offered numbers")
	}
}
