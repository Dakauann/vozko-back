package marketing

import (
	"context"
	"reflect"
	"testing"
	"time"

	"vozko/domain/advertising"
)

func TestSearchTargetingInterests(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"data":[{"id":"600","name":"Futebol","path":["Interesses","Esportes"],
		"audience_size_lower_bound":1000,"audience_size_upper_bound":"2000"}]}`))
	options, err := g.SearchTargeting(context.Background(), "tok", "9", advertising.SearchInterests, "fut")
	if err != nil {
		t.Fatal(err)
	}
	q := (*calls)[0].query
	if (*calls)[0].path != "/v26.0/search" || q.Get("type") != "adinterest" || q.Get("q") != "fut" || q.Get("locale") != "pt_BR" {
		t.Fatalf("query = %v", q)
	}
	want := []advertising.TargetingOption{{ID: "600", Name: "Futebol", Path: []string{"Interesses", "Esportes"}, AudienceMin: 1000, AudienceMax: 2000}}
	if !reflect.DeepEqual(options, want) {
		t.Fatalf("options = %+v", options)
	}
}

func TestSearchTargetingBehaviorsFiltersByName(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"data":[{"id":"1","name":"Viajantes frequentes"},{"id":"2","name":"Pequenos empresários"},{"id":"3","name":"Viajantes internacionais"}]}`))
	options, err := g.SearchTargeting(context.Background(), "tok", "9", advertising.SearchBehaviors, "VIAJ")
	if err != nil {
		t.Fatal(err)
	}
	q := (*calls)[0].query
	if q.Get("type") != "adTargetingCategory" || q.Get("class") != "behaviors" || q.Has("q") {
		t.Fatalf("query = %v", q)
	}
	if len(options) != 2 || options[0].ID != "1" || options[1].ID != "3" {
		t.Fatalf("options = %+v", options)
	}
}

func TestSearchTargetingLanguagesUseKey(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"data":[{"key":23,"name":"Português (Brasil)"}]}`))
	options, err := g.SearchTargeting(context.Background(), "tok", "9", advertising.SearchLanguages, "portug")
	if err != nil {
		t.Fatal(err)
	}
	if (*calls)[0].query.Get("type") != "adlocale" || len(options) != 1 || options[0].ID != "23" {
		t.Fatalf("options = %+v", options)
	}
}

func TestSearchTargetingFailures(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"data":[]}`))
	if _, err := g.SearchTargeting(context.Background(), "tok", "9", "schools", "x"); err == nil || len(*calls) != 0 {
		t.Fatalf("err %v calls %d", err, len(*calls))
	}
	g, _ = gatewayWith(t, ok(`{"data":[{"name":"Sem id"}]}`))
	if _, err := g.SearchTargeting(context.Background(), "tok", "9", advertising.SearchInterests, "x"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestEstimateReach(t *testing.T) {
	target := advertising.Targeting{Locations: []advertising.GeoLocation{{Kind: advertising.LocationCountry, Key: "BR"}}, AgeMin: 18, AgeMax: 65}
	tests := []struct {
		name    string
		body    string
		want    advertising.ReachEstimate
		wantErr bool
	}{
		{name: "ready", body: `{"data":{"users_lower_bound":1000,"users_upper_bound":1500,"estimate_ready":true}}`, want: advertising.ReachEstimate{Lower: 1000, Upper: 1500, Ready: true}},
		{name: "unavailable", body: `{"data":{"users_lower_bound":-1,"users_upper_bound":-1,"estimate_ready":true}}`},
		{name: "not ready", body: `{"data":{"users_lower_bound":10,"users_upper_bound":20,"estimate_ready":false}}`},
		{name: "missing bounds", body: `{"data":{"estimate_ready":true}}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, calls := gatewayWith(t, ok(tt.body))
			estimate, err := g.EstimateReach(context.Background(), "tok", "9", target, advertising.Placements{Automatic: true}, advertising.GoalReach)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil || *estimate != tt.want {
				t.Fatalf("estimate %+v err %v", estimate, err)
			}
			q := (*calls)[0].query
			if (*calls)[0].path != "/v26.0/act_9/reachestimate" || q.Get("optimization_goal") != "REACH" {
				t.Fatalf("call = %+v", (*calls)[0])
			}
			jsonEqual(t, "targeting_spec", q.Get("targeting_spec"), `{"geo_locations":{"countries":["BR"]},"age_min":18,"age_max":65,"targeting_automation":{"advantage_audience":0}}`)
		})
	}
}

func TestListCatalogsMergesOwnedAndClient(t *testing.T) {
	g, calls := gatewayWith(t, routes(t, map[string]string{
		"GET /v26.0/B1/owned_product_catalogs":  `{"data":[{"id":"CAT1","name":"Loja","product_sets":{"data":[{"id":"PS1","name":"Todos","product_count":40}]}}]}`,
		"GET /v26.0/B1/client_product_catalogs": `{"data":[{"id":"CAT1","name":"Loja"},{"id":"CAT2","name":"Cliente"}]}`,
	}))
	catalogs, err := g.ListCatalogs(context.Background(), "tok", "B1")
	if err != nil {
		t.Fatal(err)
	}
	want := []advertising.RemoteCatalog{
		{ID: "CAT1", Name: "Loja", ProductSets: []advertising.RemoteProductSet{{ID: "PS1", Name: "Todos", ProductCount: 40}}},
		{ID: "CAT2", Name: "Cliente"},
	}
	if !reflect.DeepEqual(catalogs, want) {
		t.Fatalf("catalogs = %+v", catalogs)
	}
	if (*calls)[0].query.Get("fields") != "id,name,product_sets.limit(100){id,name,product_count}" {
		t.Fatalf("fields = %s", (*calls)[0].query.Get("fields"))
	}
}

func TestListApps(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"data":[{"id":"APP1","name":"Loja","icon_url":"https://icon",
		"object_store_urls":{"itunes":"https://apps.apple.com/app/id1","google_play":"https://play.google.com/x","fb_canvas":null}}]}`))
	apps, err := g.ListApps(context.Background(), "tok", "9")
	if err != nil {
		t.Fatal(err)
	}
	want := []advertising.RemoteApp{{ID: "APP1", Name: "Loja", IconURL: "https://icon", StoreURLs: []string{"https://apps.apple.com/app/id1", "https://play.google.com/x"}}}
	if (*calls)[0].path != "/v26.0/act_9/advertisable_applications" || !reflect.DeepEqual(apps, want) {
		t.Fatalf("apps = %+v", apps)
	}
}

func TestListPagePostsUsesCachedPageToken(t *testing.T) {
	g, calls := gatewayWith(t, routes(t, map[string]string{
		"GET /v26.0/P1": `{"access_token":"page-tok","id":"P1"}`,
		"GET /v26.0/P1/published_posts": `{"data":[
			{"id":"P1_1","message":"Promo","created_time":"2026-09-01T10:00:00+0000","full_picture":"https://pic","permalink_url":"https://fb/1","is_eligible_for_promotion":true},
			{"id":"P1_2","message":"Velho","is_eligible_for_promotion":false},
			{"id":"P1_3","message":"Sem info"}]}`,
		"GET /v26.0/P1/canvases": `{"data":[]}`,
	}))
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	g.now = func() time.Time { return now }

	posts, err := g.ListPagePosts(context.Background(), "sys", "P1")
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	want := []advertising.RemotePost{{ID: "P1_1", Platform: "facebook", Message: "Promo", PictureURL: "https://pic", Permalink: "https://fb/1", CreatedTime: &created}}
	if !reflect.DeepEqual(posts, want) {
		t.Fatalf("posts = %+v", posts)
	}
	if (*calls)[0].query.Get("fields") != "access_token" || (*calls)[1].query.Get("appsecret_proof") == "" {
		t.Fatalf("calls = %+v", *calls)
	}

	if _, err := g.ListInstantExperiences(context.Background(), "sys", "P1"); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 3 {
		t.Fatalf("page token was fetched again: %d calls", len(*calls))
	}

	now = now.Add(11 * time.Minute)
	if _, err := g.ListPagePosts(context.Background(), "sys", "P1"); err != nil {
		t.Fatal(err)
	}
	if len(*calls) != 5 || (*calls)[3].path != "/v26.0/P1" {
		t.Fatalf("expired page token was not refreshed: %+v", *calls)
	}

	if _, err := g.ListPagePosts(context.Background(), "other-sys", "P1"); err != nil {
		t.Fatal(err)
	}
	if (*calls)[5].path != "/v26.0/P1" {
		t.Fatalf("page token leaked across system tokens: %+v", (*calls)[5])
	}
}

func TestListInstagramMediaKeepsOnlyBoostable(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"data":[
		{"id":"M1","caption":"Reel","media_type":"VIDEO","media_url":"https://v","thumbnail_url":"https://th","permalink":"https://ig/1","timestamp":"2026-09-02T10:00:00+0000","boost_eligibility_info":{"eligible_to_boost":true}},
		{"id":"M2","caption":"Música","boost_eligibility_info":{"eligible_to_boost":false}},
		{"id":"M3","caption":"Sem info"}]}`))
	posts, err := g.ListInstagramMedia(context.Background(), "tok", "IG1")
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	want := []advertising.RemotePost{{ID: "M1", Platform: "instagram", Message: "Reel", PictureURL: "https://th", Permalink: "https://ig/1", CreatedTime: &created}}
	if (*calls)[0].path != "/v26.0/IG1/media" || !reflect.DeepEqual(posts, want) {
		t.Fatalf("posts = %+v", posts)
	}
}

func TestListInstantExperiencesSkipsUnpublished(t *testing.T) {
	g, calls := gatewayWith(t, routes(t, map[string]string{
		"GET /v26.0/P1":          `{"access_token":"page-tok"}`,
		"GET /v26.0/P1/canvases": `{"data":[{"id":"CV1","name":"Vitrine","is_published":true},{"id":"CV2","name":"Rascunho","is_published":false}]}`,
	}))
	experiences, err := g.ListInstantExperiences(context.Background(), "sys", "P1")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(experiences, []advertising.RemoteInstantExperience{{ID: "CV1", Name: "Vitrine"}}) || (*calls)[1].query.Get("fields") != "id,name,is_published" {
		t.Fatalf("experiences = %+v", experiences)
	}
}

func TestPageWithoutTokenFails(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"id":"P1"}`))
	if _, err := g.ListPagePosts(context.Background(), "sys", "P1"); err == nil || len(*calls) != 1 {
		t.Fatalf("err %v calls %d", err, len(*calls))
	}
}

func TestGetPagePostUsesThePageToken(t *testing.T) {
	g, calls := gatewayWith(t, routes(t, map[string]string{
		"GET /v26.0/P1":   `{"access_token":"page-tok","id":"P1"}`,
		"GET /v26.0/P1_7": `{"id":"P1_7","message":"Promo","created_time":"2026-09-01T10:00:00+0000","full_picture":"https://pic","permalink_url":"https://fb/7"}`,
	}))
	post, err := g.GetPagePost(context.Background(), "sys", "P1", "P1_7")
	if err != nil {
		t.Fatal(err)
	}
	if post.ID != "P1_7" || post.Platform != "facebook" || post.PictureURL != "https://pic" || (*calls)[1].query.Get("fields") != pagePostFields {
		t.Fatalf("post = %+v calls = %+v", post, *calls)
	}
}

func TestGetInstagramMediaReadsTheOwner(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"id":"M1","caption":"Reel","media_url":"https://v","thumbnail_url":"https://th","permalink":"https://ig/1","timestamp":"2026-09-02T10:00:00+0000","owner":{"id":"IG1"}}`))
	post, err := g.GetInstagramMedia(context.Background(), "tok", "M1")
	if err != nil {
		t.Fatal(err)
	}
	if post.OwnerID != "IG1" || post.PictureURL != "https://th" || (*calls)[0].path != "/v26.0/M1" {
		t.Fatalf("post = %+v", post)
	}
}
