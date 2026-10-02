package marketing

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"vozko/domain/advertising"
)

func TestListObjectsUsesTheLevelEdgeAndLiveStatuses(t *testing.T) {
	tests := []struct {
		level advertising.Level
		path  string
		field string
	}{
		{level: advertising.LevelCampaign, path: "/v26.0/act_9/campaigns", field: "stop_time"},
		{level: advertising.LevelAdSet, path: "/v26.0/act_9/adsets", field: "destination_type"},
		{level: advertising.LevelAd, path: "/v26.0/act_9/ads", field: "ad_review_feedback"},
	}
	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			g, calls := gatewayWith(t, ok(`{"data":[]}`))
			if _, err := g.ListObjects(context.Background(), "tok", "act_9", tt.level); err != nil {
				t.Fatal(err)
			}
			q := (*calls)[0].query
			if (*calls)[0].path != tt.path || q.Get("limit") != "200" || !strings.Contains(q.Get("fields"), tt.field) {
				t.Fatalf("call = %+v", (*calls)[0])
			}
			if !strings.HasPrefix(q.Get("effective_status"), `["ACTIVE","PAUSED"`) || strings.Contains(q.Get("effective_status"), "DELETED") {
				t.Fatalf("effective_status = %s", q.Get("effective_status"))
			}
		})
	}
}

func TestCampaignMapping(t *testing.T) {
	g, _ := gatewayWith(t, ok(`{"data":[{"id":"C1","name":"Verão","status":"ACTIVE","effective_status":"ACTIVE","objective":"OUTCOME_ENGAGEMENT",
		"daily_budget":"5000","budget_remaining":"1200","bid_strategy":"LOWEST_COST_WITHOUT_CAP",
		"start_time":"2026-09-01T10:00:00-0300","stop_time":"2026-10-01T10:00:00-0300","created_time":"2026-08-30T09:00:00+0000",
		"issues_info":[{"error_code":1815869,"error_summary":"Sem pagamento","error_message":"Adicione um método","level":"AD_ACCOUNT"}]}]}`))

	objects, err := g.ListObjects(context.Background(), "tok", "9", advertising.LevelCampaign)
	if err != nil {
		t.Fatal(err)
	}
	c := objects[0]
	if c.MetaID != "C1" || c.Level != advertising.LevelCampaign || c.Objective != "OUTCOME_ENGAGEMENT" || c.SpecialCategory != advertising.CategoryNone || c.DailyBudget != 5000 || c.LifetimeBudget != 0 || c.BudgetRemaining != 1200 {
		t.Fatalf("campaign = %+v", c)
	}
	if c.StartTime == nil || !c.StartTime.Equal(time.Date(2026, 9, 1, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("start = %v", c.StartTime)
	}
	if c.EndTime == nil || !c.EndTime.Equal(time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC)) {
		t.Fatalf("end = %v", c.EndTime)
	}
	if c.UpdatedTime != nil || c.CreatedTime == nil {
		t.Fatalf("created %v updated %v", c.CreatedTime, c.UpdatedTime)
	}
	want := advertising.Issue{Code: 1815869, Summary: "Sem pagamento", Message: "Adicione um método", Level: "AD_ACCOUNT"}
	if len(c.Issues) != 1 || c.Issues[0] != want {
		t.Fatalf("issues = %+v", c.Issues)
	}
	if c.WorkspaceID != "" || c.AdAccountID != "" || !c.SyncedAt.IsZero() {
		t.Fatalf("use case fields filled: %+v", c)
	}
}

func TestAdSetMapping(t *testing.T) {
	g, _ := gatewayWith(t, ok(`{"data":[{"id":"S1","name":"Público","campaign_id":"C1","status":"PAUSED","effective_status":"CAMPAIGN_PAUSED",
		"daily_budget":"2500","optimization_goal":"CONVERSATIONS","destination_type":"WHATSAPP","end_time":"2026-12-01T00:00:00+0000"}]}`))

	objects, err := g.ListObjects(context.Background(), "tok", "9", advertising.LevelAdSet)
	if err != nil {
		t.Fatal(err)
	}
	s := objects[0]
	if s.CampaignMetaID != "C1" || s.OptimizationGoal != "CONVERSATIONS" || s.DestinationType != "WHATSAPP" || s.DailyBudget != 2500 || s.Status != advertising.StatusPaused || s.EffectiveStatus != advertising.EffectiveCampaignPaused {
		t.Fatalf("adset = %+v", s)
	}
	if s.EndTime == nil || !s.EndTime.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("end = %v", s.EndTime)
	}
}

func TestGetAdMapsCreativeAndReviewFeedback(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{"id":"A1","name":"Anúncio","campaign_id":"C1","adset_id":"S1","status":"ACTIVE","effective_status":"DISAPPROVED",
		"ad_review_feedback":{"global":{"POLICY":"Texto proibido"}},
		"creative":{"id":"CR1","title":"Fale conosco","body":"Oferta","image_url":"https://img","thumbnail_url":"https://thumb"}}`))

	ad, err := g.GetObject(context.Background(), "tok", "A1", advertising.LevelAd)
	if err != nil {
		t.Fatal(err)
	}
	if (*calls)[0].path != "/v26.0/A1" || !strings.Contains((*calls)[0].query.Get("fields"), "creative{") {
		t.Fatalf("call = %+v", (*calls)[0])
	}
	if ad.AdSetMetaID != "S1" || ad.CampaignMetaID != "C1" || ad.Objective != "" || ad.OptimizationGoal != "" || ad.ReviewFeedback["POLICY"] != "Texto proibido" {
		t.Fatalf("ad = %+v", ad)
	}
	wantCreative := advertising.Creative{ID: "CR1", Title: "Fale conosco", Body: "Oferta", ImageURL: "https://img", ThumbnailURL: "https://thumb"}
	if ad.Creative == nil || *ad.Creative != wantCreative {
		t.Fatalf("creative = %+v", ad.Creative)
	}
}

func TestObjectWithUnreadableTimeFails(t *testing.T) {
	g, _ := gatewayWith(t, ok(`{"id":"C1","start_time":"yesterday"}`))
	if _, err := g.GetObject(context.Background(), "tok", "C1", advertising.LevelCampaign); err == nil {
		t.Fatal("expected an error")
	}
}

func TestUnknownLevelFailsBeforeCallingMeta(t *testing.T) {
	g, calls := gatewayWith(t, ok(`{}`))
	if _, err := g.ListObjects(context.Background(), "tok", "9", advertising.Level("account")); err == nil || len(*calls) != 0 {
		t.Fatalf("err %v calls %d", err, len(*calls))
	}
}

func TestStatusAndDeleteRequests(t *testing.T) {
	tests := []struct {
		name   string
		run    func(g *Gateway) error
		method string
		key    string
		value  string
	}{
		{name: "pause", run: func(g *Gateway) error {
			return g.SetStatus(context.Background(), "tok", "A1", advertising.StatusPaused)
		}, method: http.MethodPost, key: "status", value: "PAUSED"},
		{name: "delete", run: func(g *Gateway) error {
			return g.DeleteObject(context.Background(), "tok", "A1")
		}, method: http.MethodDelete},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, calls := gatewayWith(t, ok(`{"success":true}`))
			if err := tt.run(g); err != nil {
				t.Fatal(err)
			}
			call := (*calls)[0]
			if call.method != tt.method || call.path != "/v26.0/A1" || (tt.key != "" && call.form.Get(tt.key) != tt.value) {
				t.Fatalf("call = %+v", call)
			}
		})
	}
}

func TestUnacknowledgedStatusChangeFails(t *testing.T) {
	g, _ := gatewayWith(t, ok(`{}`))
	if err := g.SetStatus(context.Background(), "tok", "A1", advertising.StatusActive); err == nil {
		t.Fatal("expected an error when meta does not confirm")
	}
}

func TestCampaignSpecialCategory(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    advertising.SpecialCategory
		wantErr bool
	}{
		{name: "housing", body: `{"id":"C1","special_ad_categories":["HOUSING"]}`, want: advertising.CategoryHousing},
		{name: "none", body: `{"id":"C1","special_ad_categories":["NONE"]}`, want: advertising.CategoryNone},
		{name: "empty", body: `{"id":"C1","special_ad_categories":[]}`, want: advertising.CategoryNone},
		{name: "unknown", body: `{"id":"C1","special_ad_categories":["GAMBLING"]}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, calls := gatewayWith(t, ok(tt.body))
			c, err := g.GetObject(context.Background(), "tok", "C1", advertising.LevelCampaign)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				return
			}
			if err != nil || c.SpecialCategory != tt.want || !strings.Contains((*calls)[0].query.Get("fields"), "special_ad_categories") {
				t.Fatalf("category %q err %v", c.SpecialCategory, err)
			}
		})
	}
}

func TestAdSetLeavesSpecialCategoryEmpty(t *testing.T) {
	g, _ := gatewayWith(t, ok(`{"id":"S1","special_ad_categories":["HOUSING"]}`))
	s, err := g.GetObject(context.Background(), "tok", "S1", advertising.LevelAdSet)
	if err != nil || s.SpecialCategory != "" {
		t.Fatalf("category %q err %v", s.SpecialCategory, err)
	}
}
