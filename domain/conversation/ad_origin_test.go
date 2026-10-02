package conversation

import "testing"

func TestTheAdPlatformComesFromTheAdLink(t *testing.T) {
	for url, want := range map[string]AdPlatform{
		"https://www.instagram.com/p/abc":   AdPlatformInstagram,
		"https://fb.me/2xyz":                AdPlatformFacebook,
		"https://www.facebook.com/ads/123":  AdPlatformFacebook,
		"https://m.facebook.com/story.php": AdPlatformFacebook,
		"https://example.com/promo":         AdPlatformUnknown,
		"":                                  AdPlatformUnknown,
	} {
		if got := AdPlatformFromURL(url); got != want {
			t.Errorf("%q: platform = %q, want %q", url, got, want)
		}
	}
}

func TestAWhatsAppReferralBecomesAnAdWithItsPicture(t *testing.T) {
	image := (&WhatsAppReferral{SourceID: "ad-1", SourceURL: "https://fb.me/x", Headline: "Promoção", ImageURL: "https://scontent/i.jpg", ThumbnailURL: "https://scontent/t.jpg"}).AdReferral()
	if image == nil || image.AdID != "ad-1" || image.Title != "Promoção" || image.ImageURL != "https://scontent/i.jpg" || image.Platform != AdPlatformFacebook {
		t.Fatalf("image ad = %+v", image)
	}
	video := (&WhatsAppReferral{SourceID: "ad-2", Body: "Assista", MediaType: "video", ThumbnailURL: "https://scontent/t.jpg"}).AdReferral()
	if video == nil || video.ImageURL != "https://scontent/t.jpg" || video.Title != "Assista" {
		t.Fatalf("video ad = %+v", video)
	}
	if (*WhatsAppReferral)(nil).AdReferral() != nil || (&WhatsAppReferral{}).AdReferral() != nil {
		t.Fatal("no ad id and no title is not an ad")
	}
}

func TestAWhatsAppReferralKeepsTheClickIDForTheConversionsAPI(t *testing.T) {
	ad := (&WhatsAppReferral{SourceID: "ad-1", SourceType: "ad", CTWAClid: "ARAkLkA8rmlFeiCktEJQ"}).AdReferral()
	if ad == nil || ad.ClickID != "ARAkLkA8rmlFeiCktEJQ" || ad.SourceType != "ad" {
		t.Fatalf("ad = %+v", ad)
	}
}
