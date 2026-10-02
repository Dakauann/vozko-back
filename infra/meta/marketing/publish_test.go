package marketing

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"vozko/domain/advertising"
)

func decodeField(t *testing.T, raw string) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return out
}

func expectForm(t *testing.T, call recordedCall, want map[string]string, absent ...string) {
	t.Helper()
	for k, v := range want {
		if call.form.Get(k) != v {
			t.Fatalf("%s = %q, want %q (form %v)", k, call.form.Get(k), v, call.form)
		}
	}
	for _, k := range absent {
		if call.form.Has(k) {
			t.Fatalf("%s must not be sent: %v", k, call.form)
		}
	}
}

func TestUploadImageSendsMultipartAndReturnsHash(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"images":{"ad.jpg":{"hash":"abc123","url":"https://img"}}}`))
	image := []byte{0xFF, 0xD8, 0xFF, 0xE0, 1, 2, 3}

	hash, err := g.UploadImage(context.Background(), "tok", "9", image, "ad.jpg")
	if err != nil || hash != "abc123" {
		t.Fatalf("hash %q err %v", hash, err)
	}
	call := (*calls)[0]
	if call.path != "/v26.0/act_9/adimages" || call.file == nil || call.file.field != "filename" || call.file.fileName != "ad.jpg" || !reflect.DeepEqual(call.file.data, image) {
		t.Fatalf("call = %+v file = %+v", call, call.file)
	}
}

func TestUploadImageWithoutHashFails(t *testing.T) {
	g, _ := gatewayWith(t, ok(`{"images":{"ad.jpg":{"hash":""}}}`))
	if _, err := g.UploadImage(context.Background(), "tok", "9", []byte{1}, "ad.jpg"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestUploadVideoSendsSourcePart(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"id":"V1"}`))
	video := []byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p'}

	id, err := g.UploadVideo(context.Background(), "tok", "9", video, "clip.mp4")
	if err != nil || id != "V1" {
		t.Fatalf("id %q err %v", id, err)
	}
	call := (*calls)[0]
	if call.method != http.MethodPost || call.path != "/v26.0/act_9/advideos" || call.file == nil || call.file.field != "source" || call.file.fileName != "clip.mp4" || !reflect.DeepEqual(call.file.data, video) || call.form.Get("name") != "clip.mp4" {
		t.Fatalf("call = %+v file = %+v", call, call.file)
	}
}

func TestUploadVideoRejectsEmptyFileBeforeCallingMeta(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"id":"V1"}`))
	if _, err := g.UploadVideo(context.Background(), "tok", "9", nil, "clip.mp4"); err == nil || len(*calls) != 0 {
		t.Fatalf("err %v calls %d", err, len(*calls))
	}
}

func TestVideoStatus(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    advertising.RemoteVideo
		wantErr bool
	}{
		{
			name: "ready with preferred thumbnail",
			body: `{"id":"V1","status":{"video_status":"ready"},"thumbnails":{"data":[{"uri":"https://t/1","is_preferred":false},{"uri":"https://t/2","is_preferred":true}]}}`,
			want: advertising.RemoteVideo{ID: "V1", State: advertising.VideoReady, ThumbnailURL: "https://t/2"},
		},
		{
			name: "processing",
			body: `{"id":"V1","status":{"video_status":"processing"}}`,
			want: advertising.RemoteVideo{ID: "V1", State: advertising.VideoProcessing},
		},
		{name: "unknown status", body: `{"id":"V1","status":{"video_status":"uploading"}}`, wantErr: true},
		{name: "no status", body: `{"id":"V1"}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, calls := gatewayWith(t, ok(tt.body))
			video, err := g.VideoStatus(context.Background(), "tok", "V1")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil || *video != tt.want {
				t.Fatalf("video %+v err %v", video, err)
			}
			if (*calls)[0].path != "/v26.0/V1" || (*calls)[0].query.Get("fields") != "id,status,thumbnails{uri,is_preferred}" {
				t.Fatalf("call = %+v", (*calls)[0])
			}
		})
	}
}

func TestCreateCampaignForm(t *testing.T) {
	tests := []struct {
		name   string
		spec   advertising.CampaignSpec
		want   map[string]string
		absent []string
	}{
		{
			name:   "ad set budgets",
			spec:   advertising.CampaignSpec{Name: "Verão", Objective: advertising.ObjectiveEngagement, Status: advertising.StatusPaused},
			want:   map[string]string{"special_ad_categories": `[]`, "is_adset_budget_sharing_enabled": "false"},
			absent: []string{"daily_budget", "lifetime_budget", "bid_strategy", "bid_amount", "promoted_object"},
		},
		{
			name: "daily campaign budget with cost cap",
			spec: advertising.CampaignSpec{
				Name: "Verão", Objective: advertising.ObjectiveEngagement, Status: advertising.StatusPaused,
				SpecialCategories: []advertising.SpecialCategory{advertising.CategoryHousing},
				Budget:            &advertising.Budget{Kind: advertising.BudgetDaily, Amount: 9000},
				Bid:               advertising.Bid{Strategy: advertising.BidCostCap, Amount: 1500},
			},
			want:   map[string]string{"special_ad_categories": `["HOUSING"]`, "daily_budget": "9000", "bid_strategy": "COST_CAP", "bid_amount": "1500"},
			absent: []string{"is_adset_budget_sharing_enabled", "lifetime_budget"},
		},
		{
			name: "lifetime catalog campaign",
			spec: advertising.CampaignSpec{
				Name: "Verão", Objective: advertising.ObjectiveSales, Status: advertising.StatusPaused, ProductCatalogID: "CAT1",
				Budget: &advertising.Budget{Kind: advertising.BudgetLifetime, Amount: 100000},
				Bid:    advertising.Bid{Strategy: advertising.BidLowestCost},
			},
			want:   map[string]string{"lifetime_budget": "100000", "bid_strategy": "LOWEST_COST_WITHOUT_CAP", "promoted_object": `{"product_catalog_id":"CAT1"}`, "objective": "OUTCOME_SALES"},
			absent: []string{"bid_amount", "daily_budget"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, calls := gatewayWith(t, ok(`{"id":"C1"}`))
			id, err := g.CreateCampaign(context.Background(), "tok", "9", tt.spec)
			if err != nil || id != "C1" {
				t.Fatalf("id %q err %v", id, err)
			}
			call := (*calls)[0]
			if call.path != "/v26.0/act_9/campaigns" {
				t.Fatalf("path = %s", call.path)
			}
			expectForm(t, call, map[string]string{"name": "Verão", "status": "PAUSED", "buying_type": "AUCTION"})
			expectForm(t, call, tt.want, tt.absent...)
		})
	}
}

func TestCreateCampaignRejectsUnknownBudgetKind(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"id":"C1"}`))
	spec := advertising.CampaignSpec{Name: "x", Budget: &advertising.Budget{Kind: "WEEKLY", Amount: 1}, Bid: advertising.Bid{Strategy: advertising.BidLowestCost}}
	if _, err := g.CreateCampaign(context.Background(), "tok", "9", spec); err == nil || len(*calls) != 0 {
		t.Fatalf("err %v calls %d", err, len(*calls))
	}
}

func TestCreateWithoutIDFails(t *testing.T) {
	g, _ := gatewayWith(t, ok(`{}`))
	if _, err := g.CreateAd(context.Background(), "tok", "9", advertising.AdSpec{Name: "A", AdSetID: "S1", CreativeID: "CR1", Status: advertising.StatusPaused}); err == nil {
		t.Fatal("expected an error")
	}
}

func adSetSpec(destination advertising.Destination, whatsapp string) advertising.AdSetSpec {
	start := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	return advertising.AdSetSpec{
		Name:           "Público",
		CampaignID:     "C1",
		Budget:         &advertising.Budget{Kind: advertising.BudgetDaily, Amount: 5000},
		Bid:            advertising.Bid{Strategy: advertising.BidLowestCost},
		BillingEvent:   advertising.BillingImpressions,
		Goal:           advertising.GoalConversations,
		Destination:    destination,
		PromotedObject: advertising.PromotedObject{PageID: "P1", WhatsAppPhoneNumber: whatsapp},
		Targeting: advertising.Targeting{
			Locations: []advertising.GeoLocation{
				{Kind: advertising.LocationCountry, Key: "BR"},
				{Kind: advertising.LocationRegion, Key: "460"},
				{Kind: advertising.LocationCity, Key: "269969", RadiusKm: 25},
				{Kind: advertising.LocationCity, Key: "270000"},
			},
			AgeMin:            18,
			AgeMax:            65,
			AdvantageAudience: true,
		},
		Placements: advertising.Placements{Automatic: true},
		StartTime:  &start,
		Status:     advertising.StatusPaused,
	}
}

func TestCreateAdSetForWhatsApp(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"id":"S1"}`))

	id, err := g.CreateAdSet(context.Background(), "tok", "9", adSetSpec(advertising.DestinationWhatsApp, "5511999990000"))
	if err != nil || id != "S1" {
		t.Fatalf("id %q err %v", id, err)
	}
	call := (*calls)[0]
	if call.path != "/v26.0/act_9/adsets" {
		t.Fatalf("path = %s", call.path)
	}
	expectForm(t, call, map[string]string{
		"name": "Público", "campaign_id": "C1", "daily_budget": "5000", "billing_event": "IMPRESSIONS",
		"optimization_goal": "CONVERSATIONS", "bid_strategy": "LOWEST_COST_WITHOUT_CAP", "destination_type": "WHATSAPP",
		"status": "PAUSED", "start_time": "2026-10-05T12:00:00Z",
		"promoted_object": `{"page_id":"P1","whatsapp_phone_number":"5511999990000"}`,
	}, "end_time", "bid_amount", "is_dynamic_creative", "adset_schedule", "pacing_type", "lifetime_budget")
	jsonEqual(t, "targeting", call.form.Get("targeting"), `{"geo_locations":{"countries":["BR"],"regions":[{"key":"460"}],
		"cities":[{"key":"269969","radius":25,"distance_unit":"kilometer"},{"key":"270000"}]},
		"age_min":18,"targeting_automation":{"advantage_audience":1}}`)
}

func TestCreateAdSetVariants(t *testing.T) {
	end := time.Date(2026, 11, 1, 3, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		edit   func(*advertising.AdSetSpec)
		want   map[string]string
		absent []string
	}{
		{
			name: "website conversions with min roas",
			edit: func(s *advertising.AdSetSpec) {
				s.Destination, s.Goal = advertising.DestinationWebsite, advertising.GoalValue
				s.PromotedObject = advertising.PromotedObject{PixelID: "PX1", PixelEvent: advertising.EventPurchase}
				s.Bid = advertising.Bid{Strategy: advertising.BidMinROAS, ROASFloor: 1.5}
			},
			want: map[string]string{
				"destination_type": "WEBSITE", "promoted_object": `{"pixel_id":"PX1","custom_event_type":"PURCHASE"}`,
				"bid_strategy": "LOWEST_COST_WITH_MIN_ROAS", "bid_constraints": `{"roas_average_floor":15000}`,
			},
			absent: []string{"bid_amount"},
		},
		{
			name: "catalog omits destination type",
			edit: func(s *advertising.AdSetSpec) {
				s.Destination, s.Goal = advertising.DestinationCatalog, advertising.GoalOffsiteConversion
				s.PromotedObject = advertising.PromotedObject{ProductSetID: "PS1", PixelEvent: advertising.EventPurchase}
			},
			want:   map[string]string{"promoted_object": `{"custom_event_type":"PURCHASE","product_set_id":"PS1"}`},
			absent: []string{"destination_type"},
		},
		{
			name: "app promotion",
			edit: func(s *advertising.AdSetSpec) {
				s.Destination, s.Goal = advertising.DestinationApp, advertising.GoalAppInstalls
				s.PromotedObject = advertising.PromotedObject{AppID: "APP1", AppStoreURL: "https://play.google.com/store/apps/details?id=x"}
			},
			want: map[string]string{"destination_type": "APP", "promoted_object": `{"application_id":"APP1","object_store_url":"https://play.google.com/store/apps/details?id=x"}`},
		},
		{
			name: "awareness without promoted object or destination",
			edit: func(s *advertising.AdSetSpec) {
				s.Destination, s.Goal, s.PromotedObject = advertising.DestinationNone, advertising.GoalReach, advertising.PromotedObject{}
			},
			absent: []string{"destination_type", "promoted_object"},
		},
		{
			name: "campaign budget leaves budget and bid off the ad set",
			edit: func(s *advertising.AdSetSpec) {
				s.Budget, s.Bid = nil, advertising.Bid{}
			},
			absent: []string{"daily_budget", "lifetime_budget", "bid_strategy"},
		},
		{
			name: "lifetime budget with schedule and dynamic creative",
			edit: func(s *advertising.AdSetSpec) {
				s.Budget = &advertising.Budget{Kind: advertising.BudgetLifetime, Amount: 100000}
				s.EndTime = &end
				s.DynamicCreative = true
				s.Schedule = []advertising.DayPart{{Days: []int{1, 2}, StartMinute: 540, EndMinute: 720}}
			},
			want: map[string]string{
				"lifetime_budget": "100000", "end_time": "2026-11-01T03:00:00Z", "is_dynamic_creative": "true",
				"pacing_type": `["day_parting"]`, "adset_schedule": `[{"start_minute":540,"end_minute":720,"days":[1,2],"timezone_type":"USER"}]`,
			},
			absent: []string{"daily_budget"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := adSetSpec(advertising.DestinationWhatsApp, "")
			tt.edit(&spec)
			g, calls := gatewayWith(t, ok(`{"id":"S1"}`))
			if _, err := g.CreateAdSet(context.Background(), "tok", "9", spec); err != nil {
				t.Fatal(err)
			}
			expectForm(t, (*calls)[0], tt.want, tt.absent...)
		})
	}
}

func TestCreateAdSetRejectsUnknownBidStrategy(t *testing.T) {
	spec := adSetSpec(advertising.DestinationWhatsApp, "")
	spec.Bid = advertising.Bid{Strategy: "TARGET_COST"}
	g, calls := gatewayWith(t, ok(`{"id":"S1"}`))
	if _, err := g.CreateAdSet(context.Background(), "tok", "9", spec); err == nil || len(*calls) != 0 {
		t.Fatalf("err %v calls %d", err, len(*calls))
	}
}

func TestCreateAdForm(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"id":"A1"}`))
	id, err := g.CreateAd(context.Background(), "tok", "act_9", advertising.AdSpec{Name: "Anúncio", AdSetID: "S1", CreativeID: "CR1", Status: advertising.StatusPaused})
	if err != nil || id != "A1" {
		t.Fatalf("id %q err %v", id, err)
	}
	call := (*calls)[0]
	if call.path != "/v26.0/act_9/ads" {
		t.Fatalf("path = %s", call.path)
	}
	expectForm(t, call, map[string]string{"adset_id": "S1", "creative": `{"creative_id":"CR1"}`, "status": "PAUSED", "name": "Anúncio"}, "creative_asset_groups_spec")
}

func groupsSpec() advertising.CreativeSpec {
	return advertising.CreativeSpec{
		Name:         "Grupo",
		AssetMode:    advertising.AssetsGroups,
		Identity:     advertising.Identity{PageID: "P1"},
		Destination:  advertising.DestinationWebsite,
		CallToAction: advertising.CTAShopNow,
		Creative: advertising.CreativeDraft{
			Format:       advertising.FormatFlexible,
			Texts:        []string{"Promo"},
			Headlines:    []string{"50% off"},
			Descriptions: []string{"Hoje"},
			Medias:       []advertising.MediaRef{{Kind: advertising.MediaImage, MediaID: "m1"}, {Kind: advertising.MediaVideo, MediaID: "m2"}},
			Link:         "https://loja.com",
		},
		Media: advertising.UploadedMedia{ImageHashes: map[string]string{"m1": "h1"}, VideoIDs: map[string]string{"m2": "V2"}},
	}
}

func TestCreateAdWithAssetGroups(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"id":"A1"}`))
	groups := groupsSpec()
	if _, err := g.CreateAd(context.Background(), "tok", "9", advertising.AdSpec{Name: "A", AdSetID: "S1", CreativeID: "CR1", Status: advertising.StatusPaused, AssetGroups: &groups}); err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, "creative_asset_groups_spec", (*calls)[0].form.Get("creative_asset_groups_spec"), `{"groups":[{
		"images":[{"hash":"h1"}],"videos":[{"video_id":"V2"}],
		"texts":[{"text":"Promo","text_type":"primary_text"},{"text":"50% off","text_type":"headline"},{"text":"Hoje","text_type":"description"}],
		"call_to_action":{"type":"SHOP_NOW","value":{"link":"https://loja.com"}}}]}`)
}

func TestCreateAdWithAssetGroupsMissingMediaNeverCallsMeta(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"id":"A1"}`))
	groups := groupsSpec()
	groups.Media = advertising.UploadedMedia{ImageHashes: map[string]string{"m1": "h1"}}
	_, err := g.CreateAd(context.Background(), "tok", "9", advertising.AdSpec{Name: "A", AdSetID: "S1", CreativeID: "CR1", Status: advertising.StatusPaused, AssetGroups: &groups})
	if err == nil || !strings.Contains(err.Error(), "m2") || len(*calls) != 0 {
		t.Fatalf("err %v calls %d", err, len(*calls))
	}
}
