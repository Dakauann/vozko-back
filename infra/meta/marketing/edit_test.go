package marketing

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"vozko/domain/advertising"
)

func TestGetAdSetDetail(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"id":"S1","name":"Público","campaign_id":"C1","status":"ACTIVE","effective_status":"ACTIVE",
		"lifetime_budget":"100000","optimization_goal":"VALUE","destination_type":"WEBSITE",
		"bid_strategy":"LOWEST_COST_WITH_MIN_ROAS","bid_constraints":{"roas_average_floor":15000},
		"promoted_object":{"page_id":"P1","pixel_id":"PX1","custom_event_type":"PURCHASE"},
		"adset_schedule":[{"start_minute":540,"end_minute":720,"days":[1,2],"timezone_type":"USER"}],
		"targeting":{"geo_locations":{"countries":["BR"]},"age_min":25,"age_max":40,"publisher_platforms":["instagram"],"instagram_positions":["stream","story"]}}`))

	detail, err := g.GetObjectDetail(context.Background(), "tok", "S1", advertising.LevelAdSet)
	if err != nil {
		t.Fatal(err)
	}
	fields := (*calls)[0].query.Get("fields")
	for _, f := range []string{"targeting", "adset_schedule", "bid_constraints", "bid_amount", "promoted_object"} {
		if !strings.Contains(fields, f) {
			t.Fatalf("fields %s miss %s", fields, f)
		}
	}
	if detail.Object.MetaID != "S1" || *detail.Budget != (advertising.Budget{Kind: advertising.BudgetLifetime, Amount: 100000}) {
		t.Fatalf("detail = %+v", detail)
	}
	if detail.Bid != (advertising.Bid{Strategy: advertising.BidMinROAS, ROASFloor: 1.5}) || detail.Identity.PageID != "P1" {
		t.Fatalf("bid %+v identity %+v", detail.Bid, detail.Identity)
	}
	wantTargeting := advertising.Targeting{Locations: []advertising.GeoLocation{{Kind: advertising.LocationCountry, Key: "BR"}}, AgeMin: 25, AgeMax: 40}
	wantPlacements := advertising.Placements{Platforms: []string{"instagram"}, Positions: map[string][]string{"instagram": {"stream", "story"}}}
	if !reflect.DeepEqual(*detail.Targeting, wantTargeting) || !reflect.DeepEqual(*detail.Placements, wantPlacements) {
		t.Fatalf("targeting %+v placements %+v", *detail.Targeting, *detail.Placements)
	}
	if !reflect.DeepEqual(detail.Schedule, []advertising.DayPart{{Days: []int{1, 2}, StartMinute: 540, EndMinute: 720}}) || detail.Creative != nil {
		t.Fatalf("schedule %+v creative %+v", detail.Schedule, detail.Creative)
	}
}

func TestGetAdSetDetailRejectsAdvertiserTimeSchedule(t *testing.T) {
	g, _ := gatewayWith(t, ok(`{"id":"S1","targeting":{"geo_locations":{"countries":["BR"]}},
		"adset_schedule":[{"start_minute":0,"end_minute":60,"days":[1],"timezone_type":"ADVERTISER"}]}`))
	if _, err := g.GetObjectDetail(context.Background(), "tok", "S1", advertising.LevelAdSet); err == nil {
		t.Fatal("expected an error")
	}
}

func TestGetCampaignDetail(t *testing.T) {
	g, _ := gatewayWith(t, ok(`{"id":"C1","daily_budget":"9000","bid_strategy":"COST_CAP","special_ad_categories":["NONE"]}`))
	detail, err := g.GetObjectDetail(context.Background(), "tok", "C1", advertising.LevelCampaign)
	if err != nil {
		t.Fatal(err)
	}
	if *detail.Budget != (advertising.Budget{Kind: advertising.BudgetDaily, Amount: 9000}) || detail.Bid.Strategy != advertising.BidCostCap || detail.Targeting != nil {
		t.Fatalf("detail = %+v", detail)
	}
}

func adDetail(t *testing.T, creative string, extra string) *advertising.ObjectDetail {
	t.Helper()
	g, calls := gatewayWith(t, ok(`{"id":"A1","adset_id":"S1","campaign_id":"C1","status":"PAUSED","adset":{"destination_type":"WEBSITE"},`+extra+`"creative":`+creative+`}`))
	detail, err := g.GetObjectDetail(context.Background(), "tok", "A1", advertising.LevelAd)
	if err != nil {
		t.Fatal(err)
	}
	fields := (*calls)[0].query.Get("fields")
	if !strings.Contains(fields, "object_story_spec") || !strings.Contains(fields, "creative_asset_groups_spec") || !strings.Contains(fields, "adset{destination_type}") {
		t.Fatalf("fields = %s", fields)
	}
	if detail.Object.DestinationType != "WEBSITE" || detail.Budget != nil {
		t.Fatalf("object = %+v", detail.Object)
	}
	return detail
}

func TestGetAdDetailMapsCreativeFormats(t *testing.T) {
	tests := []struct {
		name     string
		creative string
		extra    string
		want     advertising.CreativeDraft
		identity advertising.Identity
	}{
		{
			name: "image",
			creative: `{"id":"CR1","object_story_spec":{"page_id":"P1","instagram_user_id":"IG1","link_data":{"link":"https://loja.com","message":"Oferta",
				"name":"Fale","caption":"loja.com","image_hash":"h1","call_to_action":{"type":"SHOP_NOW","value":{"link":"https://loja.com"}}}},
				"degrees_of_freedom_spec":{"creative_features_spec":{"image_touchups":{"enroll_status":"OPT_IN"},"inline_comment":{"enroll_status":"OPT_OUT"}}}}`,
			want: advertising.CreativeDraft{
				Format: advertising.FormatImage, PrimaryText: "Oferta", Headline: "Fale", DisplayLink: "loja.com", Link: "https://loja.com",
				Media: advertising.MediaRef{Kind: advertising.MediaImage, MediaID: "meta:h1"}, CallToAction: advertising.CTAShopNow, Enhancements: true,
			},
			identity: advertising.Identity{PageID: "P1", InstagramUserID: "IG1"},
		},
		{
			name: "whatsapp video",
			creative: `{"id":"CR1","object_story_spec":{"page_id":"P1","video_data":{"video_id":"V2","image_url":"https://t","message":"Oferta","title":"Fale",
				"call_to_action":{"type":"WHATSAPP_MESSAGE","value":{"app_destination":"WHATSAPP"}},
				"page_welcome_message":{"text_format":{"customer_action_type":"ice_breakers","message":{"text":"Oi","ice_breakers":[{"title":"Preço?"}]}}}}}}`,
			want: advertising.CreativeDraft{
				Format: advertising.FormatVideo, PrimaryText: "Oferta", Headline: "Fale", CallToAction: advertising.CTAWhatsAppMessage,
				Media: advertising.MediaRef{Kind: advertising.MediaVideo, MediaID: "meta:V2"}, Greeting: "Oi", IceBreakers: []string{"Preço?"},
			},
			identity: advertising.Identity{PageID: "P1"},
		},
		{
			name: "carousel",
			creative: `{"id":"CR1","object_story_spec":{"page_id":"P1","link_data":{"link":"https://api.whatsapp.com/send","message":"Oferta",
				"child_attachments":[{"link":"https://loja.com/a","name":"A","image_hash":"h1"},{"link":"https://loja.com/b","name":"B","video_id":"V2","picture":"https://t"}]}}}`,
			want: advertising.CreativeDraft{
				Format: advertising.FormatCarousel, PrimaryText: "Oferta",
				Cards: []advertising.CarouselCard{
					{Media: advertising.MediaRef{Kind: advertising.MediaImage, MediaID: "meta:h1"}, Headline: "A", Link: "https://loja.com/a"},
					{Media: advertising.MediaRef{Kind: advertising.MediaVideo, MediaID: "meta:V2"}, Headline: "B", Link: "https://loja.com/b"},
				},
			},
			identity: advertising.Identity{PageID: "P1"},
		},
		{
			name: "collection",
			creative: `{"id":"CR1","object_story_spec":{"page_id":"P1","link_data":{"link":"https://fb.com/canvas_doc/CV1","message":"Oferta","image_hash":"h1",
				"collection_thumbnails":[{"element_id":"E1","element_crops":{"100x100":[[0,0],[100,100]]}}]}}}`,
			want: advertising.CreativeDraft{
				Format: advertising.FormatCollection, PrimaryText: "Oferta", InstantExperience: "CV1",
				Media: advertising.MediaRef{Kind: advertising.MediaImage, MediaID: "meta:h1"},
			},
			identity: advertising.Identity{PageID: "P1"},
		},
		{
			name: "instant form",
			creative: `{"id":"CR1","object_story_spec":{"page_id":"P1","link_data":{"link":"http://fb.me/","message":"Oferta","image_hash":"h1",
				"call_to_action":{"type":"SIGN_UP","value":{"link":"http://fb.me/","lead_gen_form_id":"F1"}}}}}`,
			want: advertising.CreativeDraft{
				Format: advertising.FormatImage, PrimaryText: "Oferta", CallToAction: advertising.CTASignUp, LeadFormID: "F1",
				Media: advertising.MediaRef{Kind: advertising.MediaImage, MediaID: "meta:h1"},
			},
			identity: advertising.Identity{PageID: "P1"},
		},
		{
			name: "catalog",
			creative: `{"id":"CR1","product_set_id":"PS1","object_story_spec":{"page_id":"P1","template_data":{"link":"https://loja.com","message":"{{product.name}}",
				"call_to_action":{"type":"SHOP_NOW"}}}}`,
			want:     advertising.CreativeDraft{Format: advertising.FormatCatalog, PrimaryText: "{{product.name}}", Link: "https://loja.com", CallToAction: advertising.CTAShopNow},
			identity: advertising.Identity{PageID: "P1"},
		},
		{
			name:     "facebook post",
			creative: `{"id":"CR1","object_story_id":"P1_55"}`,
			want:     advertising.CreativeDraft{Format: advertising.FormatExistingPost, PostID: "P1_55"},
			identity: advertising.Identity{PageID: "P1"},
		},
		{
			name:     "instagram post",
			creative: `{"id":"CR1","object_id":"P1","instagram_user_id":"IG1","source_instagram_media_id":"IGM1"}`,
			want:     advertising.CreativeDraft{Format: advertising.FormatExistingPost, InstagramMediaID: "IGM1"},
			identity: advertising.Identity{PageID: "P1", InstagramUserID: "IG1"},
		},
		{
			name: "dynamic",
			creative: `{"id":"CR1","object_story_spec":{"page_id":"P1"},"asset_feed_spec":{"images":[{"hash":"h1"}],"videos":[{"video_id":"V2"}],
				"bodies":[{"text":"A"},{"text":"B"}],"titles":[{"text":"T"}],"link_urls":[{"website_url":"https://loja.com","display_url":"loja.com"}],
				"call_to_action_types":["SHOP_NOW"],"ad_formats":["AUTOMATIC_FORMAT"]}}`,
			want: advertising.CreativeDraft{
				Format: advertising.FormatFlexible, Texts: []string{"A", "B"}, Headlines: []string{"T"}, Descriptions: []string{},
				Medias: []advertising.MediaRef{{Kind: advertising.MediaImage, MediaID: "meta:h1"}, {Kind: advertising.MediaVideo, MediaID: "meta:V2"}},
				Link:   "https://loja.com", DisplayLink: "loja.com", CallToAction: advertising.CTAShopNow,
			},
			identity: advertising.Identity{PageID: "P1"},
		},
		{
			name:     "asset groups",
			creative: `{"id":"CR1","object_story_spec":{"page_id":"P1"}}`,
			extra: `"creative_asset_groups_spec":{"groups":[{"group_uuid":"g1","images":[{"hash":"h1"}],
				"texts":[{"text":"Promo","text_type":"primary_text"},{"text":"50%","text_type":"headline"}],
				"call_to_action":{"type":"SHOP_NOW","value":{"link":"https://loja.com"}}}]},`,
			want: advertising.CreativeDraft{
				Format: advertising.FormatFlexible, Texts: []string{"Promo"}, Headlines: []string{"50%"},
				Medias: []advertising.MediaRef{{Kind: advertising.MediaImage, MediaID: "meta:h1"}}, Link: "https://loja.com", CallToAction: advertising.CTAShopNow,
			},
			identity: advertising.Identity{PageID: "P1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detail := adDetail(t, tt.creative, tt.extra)
			if !reflect.DeepEqual(*detail.Creative, tt.want) {
				t.Fatalf("creative = %+v\nwant     %+v", *detail.Creative, tt.want)
			}
			if detail.Identity != tt.identity {
				t.Fatalf("identity = %+v", detail.Identity)
			}
		})
	}
}

func TestGetAdDetailRejectsUnreadableCreatives(t *testing.T) {
	tests := map[string]string{
		"unknown shape":        `{"id":"A1","creative":{"id":"CR1"}}`,
		"image without hash":   `{"id":"A1","creative":{"id":"CR1","object_story_spec":{"page_id":"P1","link_data":{"link":"https://x.com","picture":"https://p"}}}}`,
		"no creative":          `{"id":"A1"}`,
		"unknown group text":   `{"id":"A1","creative":{"id":"CR1"},"creative_asset_groups_spec":{"groups":[{"images":[{"hash":"h"}],"texts":[{"text":"x","text_type":"caption"}]}]}}`,
		"video without its id": `{"id":"A1","creative":{"id":"CR1","object_story_spec":{"page_id":"P1","video_data":{"message":"x"}}}}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			g, _ := gatewayWith(t, ok(body))
			if _, err := g.GetObjectDetail(context.Background(), "tok", "A1", advertising.LevelAd); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func strPtr(s string) *string { return &s }

func TestUpdateObjectSendsOnlyChangedFields(t *testing.T) {
	end := time.Date(2026, 12, 1, 3, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		level  advertising.Level
		spec   advertising.EditSpec
		want   map[string]string
		absent []string
	}{
		{
			name:   "rename",
			level:  advertising.LevelAd,
			spec:   advertising.EditSpec{Name: strPtr("Novo")},
			want:   map[string]string{"name": "Novo"},
			absent: []string{"daily_budget", "creative", "targeting", "end_time"},
		},
		{
			name:  "ad set budget and cap bid",
			level: advertising.LevelAdSet,
			spec: advertising.EditSpec{
				Budget: &advertising.Budget{Kind: advertising.BudgetDaily, Amount: 4200},
				Bid:    &advertising.Bid{Strategy: advertising.BidCap, Amount: 300},
				EndAt:  &end,
			},
			want:   map[string]string{"daily_budget": "4200", "bid_strategy": "LOWEST_COST_WITH_BID_CAP", "bid_amount": "300", "end_time": "2026-12-01T03:00:00Z"},
			absent: []string{"name", "stop_time"},
		},
		{
			name:   "campaign end uses stop time",
			level:  advertising.LevelCampaign,
			spec:   advertising.EditSpec{EndAt: &end, Budget: &advertising.Budget{Kind: advertising.BudgetLifetime, Amount: 90000}},
			want:   map[string]string{"stop_time": "2026-12-01T03:00:00Z", "lifetime_budget": "90000"},
			absent: []string{"end_time"},
		},
		{
			name:  "schedule",
			level: advertising.LevelAdSet,
			spec:  advertising.EditSpec{Schedule: []advertising.DayPart{{Days: []int{0}, StartMinute: 60, EndMinute: 120}}},
			want:  map[string]string{"adset_schedule": `[{"start_minute":60,"end_minute":120,"days":[0],"timezone_type":"USER"}]`, "pacing_type": `["day_parting"]`},
		},
		{
			name:  "schedule cleared",
			level: advertising.LevelAdSet,
			spec:  advertising.EditSpec{Schedule: []advertising.DayPart{}},
			want:  map[string]string{"adset_schedule": `[]`, "pacing_type": `["standard"]`},
		},
		{
			name:  "creative swap",
			level: advertising.LevelAd,
			spec:  advertising.EditSpec{CreativeID: "CR2"},
			want:  map[string]string{"creative": `{"creative_id":"CR2"}`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, calls := gatewayWith(t, ok(`{"success":true}`))
			if err := g.UpdateObject(context.Background(), "tok", "X1", tt.level, tt.spec); err != nil {
				t.Fatal(err)
			}
			call := (*calls)[0]
			if len(*calls) != 1 || call.method != http.MethodPost || call.path != "/v26.0/X1" {
				t.Fatalf("calls = %+v", *calls)
			}
			expectForm(t, call, tt.want, tt.absent...)
		})
	}
}

const currentTargeting = `{"targeting":{"geo_locations":{"countries":["AR"]},"age_min":30,"age_max":50,
	"brand_safety_content_filter_levels":["FACEBOOK_STANDARD"],"publisher_platforms":["facebook"],"facebook_positions":["feed"]}}`

func TestUpdateTargetingKeepsPlacementsAndUnknownKeys(t *testing.T) {
	g, calls := gatewayWith(t, routes(t, map[string]string{
		"GET /v26.0/S1":  currentTargeting,
		"POST /v26.0/S1": `{"success":true}`,
	}))
	target := advertising.Targeting{Locations: []advertising.GeoLocation{{Kind: advertising.LocationCountry, Key: "BR"}}, AgeMin: 18, AgeMax: 65}
	if err := g.UpdateObject(context.Background(), "tok", "S1", advertising.LevelAdSet, advertising.EditSpec{Targeting: &target}); err != nil {
		t.Fatal(err)
	}
	if (*calls)[0].query.Get("fields") != "targeting" {
		t.Fatalf("read = %+v", (*calls)[0])
	}
	jsonEqual(t, "targeting", (*calls)[1].form.Get("targeting"), `{"geo_locations":{"countries":["BR"]},"age_min":18,"age_max":65,
		"targeting_automation":{"advantage_audience":0},"brand_safety_content_filter_levels":["FACEBOOK_STANDARD"],
		"publisher_platforms":["facebook"],"facebook_positions":["feed"]}`)
}

func TestUpdatePlacementsReplacesOnlyPlacementFields(t *testing.T) {
	g, calls := gatewayWith(t, routes(t, map[string]string{
		"GET /v26.0/S1":  currentTargeting,
		"POST /v26.0/S1": `{"success":true}`,
	}))
	placements := advertising.Placements{Platforms: []string{"instagram"}, Positions: map[string][]string{"instagram": {"reels"}}}
	if err := g.UpdateObject(context.Background(), "tok", "S1", advertising.LevelAdSet, advertising.EditSpec{Placements: &placements}); err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, "targeting", (*calls)[1].form.Get("targeting"), `{"geo_locations":{"countries":["AR"]},"age_min":30,"age_max":50,
		"brand_safety_content_filter_levels":["FACEBOOK_STANDARD"],"publisher_platforms":["instagram"],"instagram_positions":["reels"]}`)

	g, calls = gatewayWith(t, routes(t, map[string]string{
		"GET /v26.0/S1":  currentTargeting,
		"POST /v26.0/S1": `{"success":true}`,
	}))
	automatic := advertising.Placements{Automatic: true}
	if err := g.UpdateObject(context.Background(), "tok", "S1", advertising.LevelAdSet, advertising.EditSpec{Placements: &automatic}); err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, "targeting", (*calls)[1].form.Get("targeting"), `{"geo_locations":{"countries":["AR"]},"age_min":30,"age_max":50,
		"brand_safety_content_filter_levels":["FACEBOOK_STANDARD"]}`)
}

func TestUpdateObjectRejectsBeforeCallingMeta(t *testing.T) {
	target := advertising.Targeting{}
	tests := []struct {
		name  string
		level advertising.Level
		spec  advertising.EditSpec
		want  error
	}{
		{name: "nothing", level: advertising.LevelAdSet, want: advertising.ErrNothingToChange},
		{name: "budget on ad", level: advertising.LevelAd, spec: advertising.EditSpec{Budget: &advertising.Budget{Kind: advertising.BudgetDaily, Amount: 1}}, want: advertising.ErrEditNotForLevel},
		{name: "targeting on campaign", level: advertising.LevelCampaign, spec: advertising.EditSpec{Targeting: &target}, want: advertising.ErrEditNotForLevel},
		{name: "creative on ad set", level: advertising.LevelAdSet, spec: advertising.EditSpec{CreativeID: "CR"}, want: advertising.ErrEditNotForLevel},
		{name: "schedule on campaign", level: advertising.LevelCampaign, spec: advertising.EditSpec{Schedule: []advertising.DayPart{}}, want: advertising.ErrEditNotForLevel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, calls := gatewayWith(t, ok(`{"success":true}`))
			err := g.UpdateObject(context.Background(), "tok", "X1", tt.level, tt.spec)
			if !errors.Is(err, tt.want) || len(*calls) != 0 {
				t.Fatalf("err %v calls %d", err, len(*calls))
			}
		})
	}
}

func TestUpdateObjectNotAcknowledgedFails(t *testing.T) {
	g, _ := gatewayWith(t, ok(`{"success":false}`))
	if err := g.UpdateObject(context.Background(), "tok", "X1", advertising.LevelAd, advertising.EditSpec{Name: strPtr("x")}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestCopyObject(t *testing.T) {
	tests := []struct {
		name   string
		level  advertising.Level
		req    advertising.CopyRequest
		reply  string
		want   map[string]string
		absent []string
		id     string
	}{
		{
			name:  "deep ad set copy into another campaign",
			level: advertising.LevelAdSet,
			req:   advertising.CopyRequest{ParentID: "C2", DeepCopy: true, NameSuffix: " - B"},
			reply: `{"copied_adset_id":"S9","ad_object_ids":[]}`,
			want: map[string]string{
				"status_option": "PAUSED", "deep_copy": "true", "campaign_id": "C2",
				"rename_options": `{"rename_strategy":"DEEP_RENAME","rename_suffix":" - B"}`,
			},
			absent: []string{"adset_id"},
			id:     "S9",
		},
		{
			name:   "shallow campaign copy",
			level:  advertising.LevelCampaign,
			req:    advertising.CopyRequest{NameSuffix: " v2"},
			reply:  `{"copied_campaign_id":"C9"}`,
			want:   map[string]string{"deep_copy": "false", "rename_options": `{"rename_strategy":"ONLY_TOP_LEVEL_RENAME","rename_suffix":" v2"}`},
			absent: []string{"campaign_id"},
			id:     "C9",
		},
		{
			name:   "ad copy into another ad set",
			level:  advertising.LevelAd,
			req:    advertising.CopyRequest{ParentID: "S2", DeepCopy: true},
			reply:  `{"copied_ad_id":"A9"}`,
			want:   map[string]string{"adset_id": "S2", "status_option": "PAUSED"},
			absent: []string{"deep_copy", "rename_options"},
			id:     "A9",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, calls := gatewayWith(t, ok(tt.reply))
			id, err := g.CopyObject(context.Background(), "tok", "X1", tt.level, tt.req)
			if err != nil || id != tt.id {
				t.Fatalf("id %q err %v", id, err)
			}
			call := (*calls)[0]
			if call.method != http.MethodPost || call.path != "/v26.0/X1/copies" {
				t.Fatalf("call = %+v", call)
			}
			expectForm(t, call, tt.want, tt.absent...)
		})
	}
}

func TestCopyObjectFailures(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"copied_campaign_id":"C9"}`))
	if _, err := g.CopyObject(context.Background(), "tok", "C1", advertising.LevelCampaign, advertising.CopyRequest{ParentID: "X"}); err == nil || len(*calls) != 0 {
		t.Fatalf("err %v calls %d", err, len(*calls))
	}
	g, _ = gatewayWith(t, ok(`{"copied_campaign_id":"C9"}`))
	if _, err := g.CopyObject(context.Background(), "tok", "S1", advertising.LevelAdSet, advertising.CopyRequest{}); err == nil {
		t.Fatal("expected an error when the copied id for the level is missing")
	}
}

func TestSetSpendCapSendsMajorUnits(t *testing.T) {
	tests := []struct {
		currency string
		cap      int64
		want     string
	}{
		{currency: "BRL", cap: 500050, want: "5000.5"},
		{currency: "BRL", cap: 500000, want: "5000"},
		{currency: "JPY", cap: 5000, want: "5000"},
	}
	for _, tt := range tests {
		t.Run(tt.currency+tt.want, func(t *testing.T) {
			g, calls := gatewayWith(t, routes(t, map[string]string{
				"GET /v26.0/act_9":  `{"account_id":"9","currency":"` + tt.currency + `","account_status":1}`,
				"POST /v26.0/act_9": `{"success":true}`,
			}))
			if err := g.SetSpendCap(context.Background(), "tok", "9", tt.cap); err != nil {
				t.Fatal(err)
			}
			expectForm(t, (*calls)[1], map[string]string{"spend_cap": tt.want}, "spend_cap_action")
		})
	}
}

func TestSetSpendCapFailures(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"success":true}`))
	if err := g.SetSpendCap(context.Background(), "tok", "9", 0); !errors.Is(err, advertising.ErrInvalidBudget) || len(*calls) != 0 {
		t.Fatalf("err %v calls %d", err, len(*calls))
	}
	g, calls = gatewayWith(t, routes(t, map[string]string{"GET /v26.0/act_9": `{"account_id":"9","currency":"","account_status":1}`}))
	if err := g.SetSpendCap(context.Background(), "tok", "9", 100); err == nil || len(*calls) != 1 {
		t.Fatalf("err %v calls %d", err, len(*calls))
	}
}

func TestRemoveSpendCap(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"success":true}`))
	if err := g.RemoveSpendCap(context.Background(), "tok", "act_9"); err != nil {
		t.Fatal(err)
	}
	call := (*calls)[0]
	if call.path != "/v26.0/act_9" {
		t.Fatalf("path = %s", call.path)
	}
	expectForm(t, call, map[string]string{"spend_cap_action": "delete"}, "spend_cap")
}
