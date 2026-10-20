package mercadolivre

import (
	"reflect"
	"testing"
)

func TestAPackRefreshJobIsKeyedByItsPackAndMarker(t *testing.T) {
	job, err := PackJob("378136913", "2000000089077943", "stale:202610191600")
	if err != nil || job.Kind != JobRefreshPack || job.SellerID != "378136913" || job.ResourceID != "2000000089077943" {
		t.Fatalf("PackJob = %+v, %v", job, err)
	}
	if got := job.IdempotencyKey(); got != "ml:pack:/packs/2000000089077943:m:stale:202610191600" {
		t.Fatalf("pack key = %q", got)
	}
	topic, err := TopicForJob(job.Kind)
	if err != nil || topic == "" {
		t.Fatalf("topic = %q, %v", topic, err)
	}
	refusals := map[string][3]string{
		"seller not numeric": {"x", "1", "m"},
		"pack not numeric":   {"1", "MLB1", "m"},
		"no marker":          {"1", "2", " "},
	}
	for name, in := range refusals {
		if _, err := PackJob(in[0], in[1], in[2]); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestPackItemThumbnailsLiveUnderTheirConversation(t *testing.T) {
	got := PackItemThumbnailKeyBase("acct-1", "conv-1", "MLB123")
	if got != "mercadolivre/accounts/acct-1/conversations/conv-1/items/MLB123/thumbnail" {
		t.Fatalf("key base = %q", got)
	}
}

func TestThePackSummaryNamesTheFilesItOwns(t *testing.T) {
	cases := map[string]struct {
		summary *PackSummary
		want    []string
	}{
		"no summary":               {nil, nil},
		"items without thumbnails": {&PackSummary{Items: []PackItem{{ItemID: "MLB1"}}}, nil},
		"stored thumbnails, each once": {&PackSummary{Items: []PackItem{
			{ItemID: "MLB1", ThumbnailKey: "a.jpg"}, {ItemID: "MLB2"}, {ItemID: "MLB1", ThumbnailKey: "a.jpg"}, {ItemID: "MLB3", ThumbnailKey: "b.png"},
		}}, []string{"a.jpg", "b.png"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := tc.summary.ThumbnailKeys(); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("keys = %v, want %v", got, tc.want)
			}
		})
	}
}
