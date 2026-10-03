package advertising

import (
	"slices"
	"testing"
)

func TestSpendCapLimitIsAbsentWhenMetaHasNoCap(t *testing.T) {
	if (&AdAccount{SpendCap: 0}).SpendCapLimit() != nil {
		t.Fatal("zero cap must mean no limit")
	}
	if got := (&AdAccount{SpendCap: 50000}).SpendCapLimit(); got == nil || *got != 50000 {
		t.Fatalf("cap %v", got)
	}
}

func TestEmptyLevelMeansCampaignAndOthersAreKept(t *testing.T) {
	if Level("").OrCampaign() != LevelCampaign || LevelAd.OrCampaign() != LevelAd || Level("x").OrCampaign() != "x" {
		t.Fatal("unexpected level default")
	}
}

func TestCallsToActionDependOnTheDestination(t *testing.T) {
	if got := CallsToActionFor(DestinationWhatsApp); !slices.Equal(got, []CallToAction{CTAWhatsAppMessage}) {
		t.Fatalf("whatsapp %v", got)
	}
	if !slices.Contains(CallsToActionFor(DestinationApp), CTAInstallApp) {
		t.Fatal("app promotion needs the install button")
	}
	if slices.Contains(CallsToActionFor(DestinationWebsite), CTAInstallApp) {
		t.Fatal("a website ad cannot use the install button")
	}
}

func TestWebsiteCreativeRefusesTheInstallButton(t *testing.T) {
	v := newIssues()
	CreativeDraft{CallToAction: CTAInstallApp}.checkLinkCTA(v, DestinationWebsite)
	if len(v.out.Issues) != 1 || v.out.Issues[0].Code != "invalid" {
		t.Fatalf("issues %+v", v.out.Issues)
	}
}

func TestOnlyFacebookAndInstagramPostsExist(t *testing.T) {
	if !ValidPostPlatform(PlatformFacebook) || !ValidPostPlatform(PlatformInstagram) || ValidPostPlatform("threads") || ValidPostPlatform("") {
		t.Fatal("unexpected post platform rule")
	}
}

func TestAdsToPublishCountsTheDraftAds(t *testing.T) {
	d := AdDraft{Ads: []AdItem{{}, {}, {}}}
	if d.AdsToPublish() != 3 || (&PublishJob{Draft: d}).AdsToPublish() != 3 {
		t.Fatal("unexpected ad count")
	}
}

func TestOnlyImageAndVideoFormatsCarryOneMedia(t *testing.T) {
	if kind, ok := FormatImage.SingleMediaKind(); !ok || kind != MediaImage {
		t.Fatal("image")
	}
	if kind, ok := FormatVideo.SingleMediaKind(); !ok || kind != MediaVideo {
		t.Fatal("video")
	}
	if _, ok := FormatCarousel.SingleMediaKind(); ok {
		t.Fatal("carousel has cards, not one media")
	}
}

func TestAPostBelongsOnlyToItsOwnPage(t *testing.T) {
	page := RemotePage{PageID: "P1", InstagramUserID: "IG1"}
	cases := []struct {
		post RemotePost
		want bool
	}{
		{RemotePost{ID: "P1_9", Platform: PlatformFacebook}, true},
		{RemotePost{ID: "P2_9", Platform: PlatformFacebook}, false},
		{RemotePost{ID: "P19_9", Platform: PlatformFacebook}, false},
		{RemotePost{ID: "M1", Platform: PlatformInstagram, OwnerID: "IG1"}, true},
		{RemotePost{ID: "M1", Platform: PlatformInstagram, OwnerID: "IG2"}, false},
		{RemotePost{ID: "M1", Platform: PlatformInstagram}, false},
		{RemotePost{ID: "P1_9", Platform: "threads"}, false},
	}
	for _, c := range cases {
		if got := c.post.BelongsTo(page); got != c.want {
			t.Errorf("%+v: got %v", c.post, got)
		}
	}
	if (RemotePost{ID: "M1", Platform: PlatformInstagram, OwnerID: ""}).BelongsTo(RemotePage{PageID: "P1"}) {
		t.Fatal("a page without Instagram owns no media")
	}
}
