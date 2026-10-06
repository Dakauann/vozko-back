package advertising

import (
	"context"
	"testing"

	ads "vozko/domain/advertising"
)

func metaImageDraft() ads.AdDraft {
	draft := publishableDraft()
	creative := imageAd()
	creative.Media = ads.MetaMediaRef(ads.MediaImage, "h1")
	draft.Ads = []ads.AdItem{{Creative: creative}}
	return draft
}

func TestPreflightShowsImagesAlreadyOnMeta(t *testing.T) {
	w := newWorld()
	w.gateway.metaMedia = map[string]string{"meta:h1": "https://cdn.meta/h1.png"}
	pre, err := w.publisher().Preflight(context.Background(), "ws-1", metaImageDraft())
	if err != nil {
		t.Fatal(err)
	}
	if pre.MediaURLs["meta:h1"] != "https://cdn.meta/h1.png" {
		t.Fatalf("media urls %+v", pre.MediaURLs)
	}
}

func TestPreflightRefusesAnImageMetaDoesNotHaveInTheAccount(t *testing.T) {
	w := newWorld()
	_, err := w.publisher().Preflight(context.Background(), "ws-1", metaImageDraft())
	requireIssue(t, err, "ads[0].creative.media", "not_found")
}
