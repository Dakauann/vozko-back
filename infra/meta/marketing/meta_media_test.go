package marketing

import (
	"context"
	"testing"

	"vozko/domain/advertising"
)

func TestMetaMediaURLsResolvesImageHashesAndVideoPictures(t *testing.T) {
	g, calls := gatewayWith(t, routes(t, map[string]string{
		"GET /v26.0/act_123/adimages": `{"data":[{"hash":"h1","url":"https://cdn.meta/h1.png"},{"hash":"h2","url":"https://cdn.meta/h2.png"}]}`,
		"GET /v26.0/v1":               `{"id":"v1","picture":"https://cdn.meta/v1.jpg"}`,
	}))
	urls, err := g.MetaMediaURLs(context.Background(), "tok", "act_123", []advertising.MediaRef{
		advertising.MetaMediaRef(advertising.MediaImage, "h1"),
		advertising.MetaMediaRef(advertising.MediaImage, "h2"),
		advertising.MetaMediaRef(advertising.MediaVideo, "v1"),
		{Kind: advertising.MediaImage, MediaID: "media-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if urls["meta:h1"] != "https://cdn.meta/h1.png" || urls["meta:h2"] != "https://cdn.meta/h2.png" || urls["meta:v1"] != "https://cdn.meta/v1.jpg" || len(urls) != 3 {
		t.Fatalf("urls %+v", urls)
	}
	for _, call := range *calls {
		if call.path == "/v26.0/act_123/adimages" && (call.query.Get("hashes") != `["h1","h2"]` || call.query.Get("fields") != "hash,url") {
			t.Fatalf("query %v", call.query)
		}
	}
	if len(*calls) != 2 {
		t.Fatalf("calls %d, want one for the images and one for the video", len(*calls))
	}
}

func TestMetaMediaURLsWithoutMetaMediaMakesNoCall(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{}`))
	urls, err := g.MetaMediaURLs(context.Background(), "tok", "act_123", []advertising.MediaRef{{Kind: advertising.MediaImage, MediaID: "media-1"}})
	if err != nil || len(urls) != 0 || len(*calls) != 0 {
		t.Fatalf("urls %v err %v calls %d", urls, err, len(*calls))
	}
}
