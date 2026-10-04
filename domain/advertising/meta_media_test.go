package advertising

import "testing"

func TestMetaMediaRefRoundTrips(t *testing.T) {
	ref := MetaMediaRef(MediaImage, "abc123")
	if id, ok := ref.MetaID(); !ok || id != "abc123" {
		t.Fatalf("meta id %q %v", id, ok)
	}
	if _, ok := (MediaRef{Kind: MediaImage, MediaID: "media-1"}).MetaID(); ok {
		t.Fatal("a library media is not hosted on Meta")
	}
}

func TestMetaMediaRefsKeepsOnlyMediaHostedOnMeta(t *testing.T) {
	c := CreativeDraft{
		Format: FormatCarousel,
		Cards: []CarouselCard{
			{Media: MetaMediaRef(MediaImage, "h1")},
			{Media: MetaMediaRef(MediaVideo, "v1")},
			{Media: MediaRef{Kind: MediaImage, MediaID: "media-1"}},
		},
	}
	refs := c.MetaMediaRefs()
	if len(refs) != 2 || refs[0].MediaID != "meta:h1" || refs[1].MediaID != "meta:v1" {
		t.Fatalf("refs %+v", refs)
	}
}
