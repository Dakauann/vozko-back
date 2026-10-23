package carriercampaign

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/campaign"
	cc "vozko/domain/carriercampaign"
	cm "vozko/domain/carriermessaging"
	"vozko/domain/rcs"
	"vozko/domain/selection"
	"vozko/domain/shared"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
	ccuc "vozko/usecases/carriercampaign"
)

var created = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

type fakeAPI struct {
	draft      ccuc.Draft
	actor      ccuc.Actor
	list       cc.ListCampaignsInput
	campaign   *cc.Campaign
	estimate   ccuc.EstimateResult
	testPhone  string
	testVar    int
	message    *cm.Message
	funnel     ccuc.FunnelView
	runID      string
	recipients ccuc.RecipientQuery
	rows       []ccuc.RecipientRow
	days       int
	channel    string
	overview   ccuc.OverviewResult
	bucket     campaign.TimelineBucket
	paused     string
	readied    string
	deleted    string
	buttonVar  int
	err        error
}

func sampleCampaign() *cc.Campaign {
	return &cc.Campaign{
		ID: "c1", WorkspaceID: "ws-1", Name: "Oferta", SenderID: "s1", Channel: cm.ChannelRCS, Status: cc.StatusDraft, Kind: cc.KindMarketing,
		Variants:  []cc.Variant{{Name: "A", Content: rcs.Content{Text: "Oi {{1}}"}, Bindings: []campaign.VariableBinding{{Source: campaign.BindFirstName}}}},
		Audience:  selection.Selection{Mode: selection.ModeIDs, IDs: []string{"l1"}},
		CreatedBy: "user-1", CreatedAt: created, UpdatedAt: created,
	}
}

func (f *fakeAPI) ListSummaries(_ context.Context, ws string, in cc.ListCampaignsInput) (*shared.PaginatedResult[ccuc.CampaignSummary], error) {
	f.list = in
	items := []ccuc.CampaignSummary{{Campaign: *sampleCampaign(), Stats: cc.Funnel{Targeted: 9, Sent: 8, Delivered: 7, Read: 6, Clicks: 5, Replies: 4, Taps: 99}}}
	return &shared.PaginatedResult[ccuc.CampaignSummary]{Items: items, TotalItems: 31}, f.err
}
func (f *fakeAPI) Create(_ context.Context, ws string, a ccuc.Actor, d ccuc.Draft) (*cc.Campaign, error) {
	f.draft, f.actor = d, a
	return sampleCampaign(), f.err
}
func (f *fakeAPI) Get(context.Context, string, string) (*cc.Campaign, error) {
	if f.campaign != nil {
		return f.campaign, f.err
	}
	return sampleCampaign(), f.err
}
func (f *fakeAPI) Update(_ context.Context, ws, id string, d ccuc.Draft) (*cc.Campaign, error) {
	f.draft = d
	return sampleCampaign(), f.err
}
func (f *fakeAPI) Delete(_ context.Context, ws, id string) error { f.deleted = id; return f.err }
func (f *fakeAPI) Duplicate(_ context.Context, ws, id string, a ccuc.Actor, name string) (*cc.Campaign, error) {
	c := sampleCampaign()
	c.ID, c.Name = "c2", name
	return c, f.err
}
func (f *fakeAPI) Estimate(_ context.Context, ws, id string, a ccuc.Actor) (ccuc.EstimateResult, error) {
	f.actor = a
	return f.estimate, f.err
}
func (f *fakeAPI) TestSend(_ context.Context, ws, id string, a ccuc.Actor, phone string, variant int) (*cm.Message, error) {
	f.testPhone, f.testVar = phone, variant
	return f.message, f.err
}
func (f *fakeAPI) Launch(_ context.Context, ws, id string, a ccuc.Actor) (ccuc.LaunchResult, error) {
	f.actor = a
	c := sampleCampaign()
	c.Status = cc.StatusRunning
	return ccuc.LaunchResult{Campaign: c}, f.err
}
func (f *fakeAPI) Ready(_ context.Context, ws, id string, a ccuc.Actor) (*cc.Campaign, error) {
	f.actor, f.readied = a, id
	c := sampleCampaign()
	c.Status = cc.StatusReady
	return c, f.err
}
func (f *fakeAPI) Pause(_ context.Context, ws, id string) error  { f.paused = id; return f.err }
func (f *fakeAPI) Resume(_ context.Context, ws, id string) error { return f.err }
func (f *fakeAPI) Stop(_ context.Context, ws, id string) error   { return f.err }
func (f *fakeAPI) Funnel(context.Context, string, string) (ccuc.FunnelView, error) {
	return f.funnel, f.err
}
func (f *fakeAPI) RunFunnel(_ context.Context, ws, id, runID string) (ccuc.FunnelView, error) {
	f.runID = runID
	return f.funnel, f.err
}
func (f *fakeAPI) Variants(context.Context, string, string) ([]cc.VariantStat, error) {
	return []cc.VariantStat{{Index: 1, Name: "B", Funnel: cc.Funnel{Sent: 2}, Rates: cc.Funnel{Sent: 2}.Rates()}}, f.err
}
func (f *fakeAPI) Buttons(_ context.Context, ws, id string, variant int) ([]cc.ButtonStat, error) {
	f.buttonVar = variant
	return []cc.ButtonStat{{Key: rcs.ButtonKey{Card: -1, Index: 0}, Label: "Quero", Kind: rcs.SuggestReply, Taps: 3}}, f.err
}
func (f *fakeAPI) Carriers(context.Context, string, string) ([]cc.CarrierStat, error) {
	return []cc.CarrierStat{{Operator: "Vivo", Sent: 10, Delivered: 8, FellBack: 2}, {Operator: "Claro"}}, f.err
}
func (f *fakeAPI) Timeline(_ context.Context, ws, id string, bucket campaign.TimelineBucket) (*campaign.Timeline, error) {
	f.bucket = bucket
	return &campaign.Timeline{Bucket: campaign.TimelineHour, Timezone: "America/Sao_Paulo", Points: []campaign.TimelinePoint{{At: created, Sent: 1}}}, f.err
}
func (f *fakeAPI) Recipients(_ context.Context, ws, id string, q ccuc.RecipientQuery) (*shared.PaginatedResult[ccuc.RecipientRow], error) {
	f.recipients = q
	return &shared.PaginatedResult[ccuc.RecipientRow]{Items: f.rows, TotalItems: int64(len(f.rows))}, f.err
}
func (f *fakeAPI) Overview(_ context.Context, ws string, days int, channel string) (ccuc.OverviewResult, error) {
	f.days, f.channel = days, channel
	return f.overview, f.err
}

func serve(t *testing.T, api *fakeAPI, method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	router := mux.NewRouter()
	RegisterProtectedRoutes(router, NewHandler(api), func(_ workspace_domain.Resource, _ workspace_domain.Action, h http.HandlerFunc) http.HandlerFunc {
		return h
	})
	var reader *bytes.Reader
	switch b := body.(type) {
	case nil:
		reader = bytes.NewReader(nil)
	case string:
		reader = bytes.NewReader([]byte(b))
	default:
		raw, _ := json.Marshal(b)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	ctx := context.WithValue(req.Context(), middleware.WorkspaceIDContextKey, "ws-1")
	ctx = context.WithValue(ctx, middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1"})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req.WithContext(ctx))
	var out map[string]any
	if rec.Body.Len() > 0 && rec.Body.Bytes()[0] == '{' {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("body %q", rec.Body.String())
		}
	}
	return rec, out
}

func decodeList(t *testing.T, rec *httptest.ResponseRecorder) []any {
	t.Helper()
	var out []any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("body %q", rec.Body.String())
	}
	return out
}

const createBody = `{
  "name": "Black Friday",
  "channel": "rcs",
  "kind": "marketing",
  "sender_id": "s1",
  "variants": [
    {"name": "A", "content": {"text": "Oi {{1}}"}, "bindings": [{"source": "lead.first_name"}]},
    {"name": "B", "content": {"cards": [{"title": "Oferta"}]}, "bindings": []}
  ],
  "fallback": {
    "whatsapp": {"business_phone_id": "bp1", "template_id": "t1", "body_bindings": [{"source": "literal", "value": "VOZ"}], "header_bindings": []},
    "sms": {"route_id": "r1", "text": "Oi {{1}}", "bindings": [{"source": "lead.first_name"}]}
  },
  "audience": {"mode": "all_matching", "filter": {"groups": []}, "excludeIds": ["l9"], "expectedCount": 12, "fingerprint": "abc"},
  "scheduled_at": "2026-10-22T12:00:00Z",
  "recurrence": {"frequency": "weekly", "time": "09:00", "weekdays": [1, 3], "timezone": "America/Sao_Paulo", "until": "2026-12-31"}
}`

func TestCreateDecodesTheContract(t *testing.T) {
	api := &fakeAPI{}
	rec, body := serve(t, api, http.MethodPost, "/carrier/campaigns", createBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	d := api.draft
	if d.Name != "Black Friday" || d.Channel != cm.ChannelRCS || d.Kind != cc.KindMarketing || d.SenderID != "s1" || len(d.Variants) != 2 {
		t.Fatalf("draft %+v", d)
	}
	if d.Variants[0].Content.Text != "Oi {{1}}" || d.Variants[0].Bindings[0].Source != campaign.BindFirstName || len(d.Variants[1].Content.Cards) != 1 {
		t.Fatalf("variants %+v", d.Variants)
	}
	if wa := d.Fallback.WhatsApp; wa == nil || wa.BusinessPhoneID != "bp1" || wa.BodyBindings[0].Value != "VOZ" {
		t.Fatalf("whatsapp %+v", wa)
	}
	if s := d.Fallback.SMS; s == nil || s.RouteID != "r1" || s.Text != "Oi {{1}}" || len(s.Bindings) != 1 {
		t.Fatalf("sms %+v", s)
	}
	if a := d.Audience; a.Mode != selection.ModeAllMatching || a.Filter == nil || a.ExcludeIDs[0] != "l9" || a.ExpectedCount != 12 || a.Fingerprint != "abc" {
		t.Fatalf("audience %+v", a)
	}
	if d.ScheduledAt == nil || !d.ScheduledAt.Equal(time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("scheduled %v", d.ScheduledAt)
	}
	if r := d.Recurrence; r == nil || r.Frequency != cc.FrequencyWeekly || len(r.Weekdays) != 2 || r.Until == nil || r.Until.String() != "2026-12-31" {
		t.Fatalf("recurrence %+v", r)
	}
	if api.actor.UserID != "user-1" {
		t.Fatalf("actor %+v", api.actor)
	}
	for _, key := range []string{"id", "name", "channel", "kind", "sender_id", "status", "variants", "fallback", "audience", "scheduled_at", "recurrence", "created_at", "updated_at"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("response lacks %q: %v", key, body)
		}
	}
	if body["scheduled_at"] != nil || body["recurrence"] != nil {
		t.Fatalf("absent schedule must be null: %v %v", body["scheduled_at"], body["recurrence"])
	}
	variant := body["variants"].([]any)[0].(map[string]any)
	if variant["content"].(map[string]any)["text"] != "Oi {{1}}" || variant["bindings"].([]any)[0].(map[string]any)["source"] != "lead.first_name" {
		t.Fatalf("variant %v", variant)
	}
	if _, has := variant["text"]; has {
		t.Fatalf("an rcs variant has no text: %v", variant)
	}
	if audience := body["audience"].(map[string]any); audience["mode"] != "ids" || audience["ids"].([]any)[0] != "l1" {
		t.Fatalf("audience %v", audience)
	}
}

func TestCreateRefusesUnknownFields(t *testing.T) {
	rec, _ := serve(t, &fakeAPI{}, http.MethodPost, "/carrier/campaigns", `{"name":"x","surprise":1}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestListCampaigns(t *testing.T) {
	api := &fakeAPI{}
	rec, body := serve(t, api, http.MethodGet, "/carrier/campaigns?channel=sms&status=running&page=2&limit=10", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	if api.list.Channel != cm.ChannelSMS || api.list.Status != cc.StatusRunning || api.list.Pagination != (shared.Pagination{Page: 2, PageSize: 10}) {
		t.Fatalf("query %+v", api.list)
	}
	if body["total"] != float64(31) {
		t.Fatalf("total %v", body["total"])
	}
	item := body["items"].([]any)[0].(map[string]any)
	stats := item["stats"].(map[string]any)
	want := map[string]float64{"targeted": 9, "sent": 8, "delivered": 7, "read": 6, "clicks": 5, "replies": 4}
	if len(stats) != len(want) {
		t.Fatalf("stats %v", stats)
	}
	for k, v := range want {
		if stats[k] != v {
			t.Fatalf("stats %v", stats)
		}
	}
	for _, key := range []string{"id", "name", "channel", "status", "kind", "sender_id", "scheduled_at", "recurrence", "created_at"} {
		if _, ok := item[key]; !ok {
			t.Fatalf("summary lacks %q", key)
		}
	}
}

func TestListRefusesBadPaging(t *testing.T) {
	for _, q := range []string{"page=x", "limit=0", "limit=101", "page=-1"} {
		rec, body := serve(t, &fakeAPI{}, http.MethodGet, "/carrier/campaigns?"+q, nil)
		if rec.Code != http.StatusBadRequest || body["code"] != "carrier_campaign_query_invalid" {
			t.Fatalf("%s: %d %v", q, rec.Code, body)
		}
	}
}

func TestEstimateResponse(t *testing.T) {
	api := &fakeAPI{estimate: ccuc.EstimateResult{
		Audience:            10,
		AudienceFingerprint: "fp-1",
		Estimate:            cc.Estimate{Channel: cm.ChannelRCS, Messages: 10, WorstCaseMicros: 250_000, ExpectedMicros: 100_000, PerRecipientMicros: 25_000},
		ShortfallMicros:     5, Readiness: ccuc.Readiness{Sender: true, Route: false, Blocker: ccuc.BlockerRouteNotReady},
	}}
	rec, body := serve(t, api, http.MethodPost, "/carrier/campaigns/c1/estimate", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body.String())
	}
	want := map[string]any{"channel": "rcs", "recipients": float64(10), "messages": float64(10), "worst_case_micros": float64(250_000), "expected_micros": float64(100_000), "per_recipient_micros": float64(25_000), "shortfall_micros": float64(5), "audience_fingerprint": "fp-1"}
	for k, v := range want {
		if body[k] != v {
			t.Fatalf("%s = %v in %v", k, body[k], body)
		}
	}
	if _, has := body["worst_case_segments"]; has {
		t.Fatal("rcs without sms fallback has no segments")
	}
	ready := body["ready"].(map[string]any)
	if ready["sender"] != true || ready["route"] != false || ready["blocker"] != "route_not_ready" {
		t.Fatalf("ready %v", ready)
	}
}

func TestTestSendResponse(t *testing.T) {
	api := &fakeAPI{message: &cm.Message{ID: "m1", Channel: cm.ChannelSMS, Reference: "ref", Status: cm.StatusAccepted, Segments: 2}}
	rec, body := serve(t, api, http.MethodPost, "/carrier/campaigns/c1/test", `{"phone":"+5511999990000","variant":1}`)
	if rec.Code != http.StatusOK || api.testPhone != "+5511999990000" || api.testVar != 1 {
		t.Fatalf("status %d phone %q variant %d", rec.Code, api.testPhone, api.testVar)
	}
	if body["id"] != "m1" || body["channel"] != "sms" || body["reference"] != "ref" || body["status"] != "accepted" || body["error_code"] != "" || body["segments"] != float64(2) {
		t.Fatalf("body %v", body)
	}
	if _, has := body["class"]; has {
		t.Fatal("an sms has no rcs class")
	}
}

func TestFunnelResponse(t *testing.T) {
	f := cc.Funnel{Sent: 10, Delivered: 8, Taps: 1, FellBackSMS: 2, ConversionValueMicros: 7}
	api := &fakeAPI{funnel: ccuc.FunnelView{Funnel: f, Rates: f.Rates()}}
	rec, body := serve(t, api, http.MethodGet, "/carrier/campaigns/c1/funnel?run_id=r9", nil)
	if rec.Code != http.StatusOK || api.runID != "r9" {
		t.Fatalf("status %d run %q", rec.Code, api.runID)
	}
	for _, key := range []string{"targeted", "skipped", "sent", "accepted", "rejected", "delivered", "read", "failed", "expired", "taps", "clicks", "replies", "opt_outs", "fell_back_whatsapp", "fell_back_sms", "conversions", "conversion_value_micros", "spend_micros"} {
		if _, ok := body[key]; !ok {
			t.Fatalf("funnel lacks %q", key)
		}
	}
	rates := body["rates"].(map[string]any)
	if rates["delivery"] != 0.8 || rates["read"] != 0.0 || rates["fallback"] != 0.2 || rates["conversion"] != 0.0 {
		t.Fatalf("rates %v", rates)
	}
	if len(rates) != 9 || rates["tap_through"] == nil {
		t.Fatalf("rates keys %v", rates)
	}
	empty := &fakeAPI{funnel: ccuc.FunnelView{Rates: cc.Funnel{}.Rates()}}
	_, body = serve(t, empty, http.MethodGet, "/carrier/campaigns/c1/funnel", nil)
	if r := body["rates"].(map[string]any); r["delivery"] != nil {
		t.Fatalf("empty rates must be null: %v", r)
	}
}

func TestVariantsButtonsCarriers(t *testing.T) {
	api := &fakeAPI{}
	rec, _ := serve(t, api, http.MethodGet, "/carrier/campaigns/c1/variants", nil)
	variants := decodeList(t, rec)
	v := variants[0].(map[string]any)
	if v["variant"] != float64(1) || v["name"] != "B" || v["funnel"].(map[string]any)["sent"] != float64(2) || v["rates"] == nil {
		t.Fatalf("variant %v", v)
	}

	rec, _ = serve(t, api, http.MethodGet, "/carrier/campaigns/c1/buttons?variant=2", nil)
	b := decodeList(t, rec)[0].(map[string]any)
	if api.buttonVar != 2 || b["card"] != float64(-1) || b["index"] != float64(0) || b["label"] != "Quero" || b["kind"] != "reply" || b["taps"] != float64(3) || b["clicks"] != float64(0) {
		t.Fatalf("button %v", b)
	}

	rec, _ = serve(t, api, http.MethodGet, "/carrier/campaigns/c1/carriers", nil)
	carriers := decodeList(t, rec)
	vivo, claro := carriers[0].(map[string]any), carriers[1].(map[string]any)
	if vivo["operator"] != "Vivo" || vivo["fell_back"] != float64(2) || vivo["fallback_rate"] != 0.2 || claro["fallback_rate"] != nil {
		t.Fatalf("carriers %v", carriers)
	}
}

func TestTimelineResponse(t *testing.T) {
	api := &fakeAPI{}
	rec, body := serve(t, api, http.MethodGet, "/carrier/campaigns/c1/timeline?bucket=hour", nil)
	if rec.Code != http.StatusOK || api.bucket != campaign.TimelineHour {
		t.Fatalf("status %d bucket %q", rec.Code, api.bucket)
	}
	point := body["points"].([]any)[0].(map[string]any)
	if body["bucket"] != "hour" || point["sent"] != float64(1) || point["at"] == nil {
		t.Fatalf("timeline %v", body)
	}
}

func TestRecipientsResponse(t *testing.T) {
	api := &fakeAPI{rows: []ccuc.RecipientRow{{
		Recipient:     cc.Recipient{Phone: "5511999990000", LeadID: "l1", Variant: 1, Status: cc.RecipientSkipped, SkipReason: cc.SkipSuppressed, UpdatedAt: created},
		LeadName:      "Ana",
		MessageStatus: cm.StatusDelivered,
	}}}
	rec, body := serve(t, api, http.MethodGet, "/carrier/campaigns/c1/recipients?status=skipped&page=3&limit=5", nil)
	if rec.Code != http.StatusOK || api.recipients.Status != cc.RecipientSkipped || api.recipients.Pagination != (shared.Pagination{Page: 3, PageSize: 5}) {
		t.Fatalf("status %d query %+v", rec.Code, api.recipients)
	}
	item := body["items"].([]any)[0].(map[string]any)
	want := map[string]any{"phone": "5511999990000", "lead_id": "l1", "lead_name": "Ana", "variant": float64(1), "status": "skipped", "skip_reason": "suppressed", "message_status": "delivered", "updated_at": "2026-10-21T12:00:00Z"}
	for k, v := range want {
		if item[k] != v {
			t.Fatalf("%s = %v in %v", k, item[k], item)
		}
	}
	if body["total"] != float64(1) {
		t.Fatalf("total %v", body["total"])
	}
}

func TestOverviewResponse(t *testing.T) {
	cost := int64(300_000)
	totals := cc.Funnel{Sent: 10, Delivered: 5, SpendMicros: 9}
	api := &fakeAPI{overview: ccuc.OverviewResult{
		Totals: totals, Rates: totals.Rates(),
		Daily:    []ccuc.OverviewDay{{Day: shared.Date{Year: 2026, Month: 10, Day: 21}, Channel: cm.ChannelSMS, Sent: 3, SpendMicros: 4}},
		Channels: []ccuc.ChannelCost{{Channel: "rcs", SpendMicros: 900_000, Conversions: 3, CostPerConversionMicros: &cost}, {Channel: "whatsapp"}},
		Carriers: []cc.CarrierStat{{Operator: "Tim", Sent: 4, FellBack: 1}},
	}}
	rec, body := serve(t, api, http.MethodGet, "/carrier/overview", nil)
	if rec.Code != http.StatusOK || api.days != 7 || api.channel != "all" {
		t.Fatalf("status %d days %d channel %q", rec.Code, api.days, api.channel)
	}
	if tot := body["totals"].(map[string]any); tot["sent"] != float64(10) || tot["spend_micros"] != float64(9) || len(tot) != 13 {
		t.Fatalf("totals %v", tot)
	}
	if r := body["rates"].(map[string]any); r["delivery"] != 0.5 {
		t.Fatalf("rates %v", r)
	}
	day := body["daily"].([]any)[0].(map[string]any)
	if day["day"] != "2026-10-21" || day["channel"] != "sms" || day["sent"] != float64(3) || day["spend_micros"] != float64(4) {
		t.Fatalf("day %v", day)
	}
	channels := body["channels"].([]any)
	if c := channels[0].(map[string]any); c["channel"] != "rcs" || c["cost_per_conversion_micros"] != float64(300_000) {
		t.Fatalf("channel %v", c)
	}
	if c := channels[1].(map[string]any); c["channel"] != "whatsapp" || c["cost_per_conversion_micros"] != nil {
		t.Fatalf("channel %v", c)
	}
	if c := body["carriers"].([]any)[0].(map[string]any); c["operator"] != "Tim" || c["fallback_rate"] != 0.25 {
		t.Fatalf("carrier %v", c)
	}

	serve(t, api, http.MethodGet, "/carrier/overview?days=30&channel=rcs", nil)
	if api.days != 30 || api.channel != "rcs" {
		t.Fatalf("days %d channel %q", api.days, api.channel)
	}
	rec, _ = serve(t, api, http.MethodGet, "/carrier/overview?days=abc", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad days status %d", rec.Code)
	}
}

func TestLifecycleReturnsTheCampaign(t *testing.T) {
	api := &fakeAPI{}
	rec, body := serve(t, api, http.MethodPost, "/carrier/campaigns/c1/pause", nil)
	if rec.Code != http.StatusOK || api.paused != "c1" || body["id"] != "c1" {
		t.Fatalf("pause %d %v", rec.Code, body)
	}
	rec, body = serve(t, api, http.MethodPost, "/carrier/campaigns/c1/launch", nil)
	if rec.Code != http.StatusOK || body["status"] != "running" || api.actor.UserID != "user-1" {
		t.Fatalf("launch %d %v", rec.Code, body)
	}
	rec, _ = serve(t, api, http.MethodDelete, "/carrier/campaigns/c1", nil)
	if rec.Code != http.StatusNoContent || api.deleted != "c1" {
		t.Fatalf("delete %d", rec.Code)
	}
	rec, body = serve(t, api, http.MethodPost, "/carrier/campaigns/c1/duplicate", `{"name":"Cópia"}`)
	if rec.Code != http.StatusCreated || body["name"] != "Cópia" || body["id"] != "c2" {
		t.Fatalf("duplicate %d %v", rec.Code, body)
	}
}

func TestReadyReturnsTheReadyCampaign(t *testing.T) {
	api := &fakeAPI{}
	rec, body := serve(t, api, http.MethodPost, "/carrier/campaigns/c1/ready", nil)
	if rec.Code != http.StatusOK || body["status"] != "ready" || api.readied != "c1" || api.actor.UserID != "user-1" {
		t.Fatalf("ready %d %v", rec.Code, body)
	}
}

func TestReadyRefusalsAreCoded(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{cc.ErrNotDraft, http.StatusConflict, "carrier_campaign_not_draft"},
		{cc.ErrReadyScheduled, http.StatusConflict, "carrier_campaign_ready_scheduled"},
		{&cm.ShortfallError{MissingMicros: 9}, http.StatusPaymentRequired, "carrier_campaign_insufficient_balance"},
		{ccuc.ErrAudienceEmpty, http.StatusUnprocessableEntity, "carrier_campaign_audience_empty"},
	}
	for _, tc := range cases {
		rec, body := serve(t, &fakeAPI{err: tc.err}, http.MethodPost, "/carrier/campaigns/c1/ready", nil)
		if rec.Code != tc.status || body["code"] != tc.code {
			t.Fatalf("%v: %d %v", tc.err, rec.Code, body)
		}
	}
}

func TestHandlerErrorsUseTheTable(t *testing.T) {
	api := &fakeAPI{err: &cm.ShortfallError{MissingMicros: 9}}
	rec, body := serve(t, api, http.MethodPost, "/carrier/campaigns/c1/launch", nil)
	if rec.Code != http.StatusPaymentRequired || body["code"] != "carrier_campaign_insufficient_balance" {
		t.Fatalf("launch error %d %v", rec.Code, body)
	}
}

var _ API = (*ccuc.Service)(nil)

func TestCreateRefusesAnInvalidUntilDate(t *testing.T) {
	api := &fakeAPI{}
	rec, body := serve(t, api, http.MethodPost, "/carrier/campaigns", `{"name":"x","channel":"rcs","kind":"marketing","sender_id":"s1","variants":[],"fallback":{},"audience":{"mode":"ids","ids":["l1"]},"scheduled_at":null,"recurrence":{"frequency":"daily","time":"09:00","timezone":"America/Sao_Paulo","until":"31/12/2026"}}`)
	if rec.Code != http.StatusUnprocessableEntity || body["code"] != "carrier_campaign_recurrence_until_invalid" {
		t.Fatalf("status %d %v", rec.Code, body)
	}
	if api.draft.Name != "" {
		t.Fatal("an invalid body must not reach the use case")
	}
}

func TestCampaignResponseRecurrence(t *testing.T) {
	c := sampleCampaign()
	until, _ := shared.ParseDate("2026-12-31")
	c.Recurrence = &cc.Recurrence{Frequency: cc.FrequencyWeekly, Time: "09:00", Weekdays: []time.Weekday{time.Monday, time.Friday}, Timezone: "America/Sao_Paulo", Until: &until}
	_, body := serve(t, &fakeAPI{campaign: c}, http.MethodGet, "/carrier/campaigns/c1", nil)
	r := body["recurrence"].(map[string]any)
	if r["frequency"] != "weekly" || r["until"] != "2026-12-31" || len(r["weekdays"].([]any)) != 2 || r["weekdays"].([]any)[1] != float64(5) {
		t.Fatalf("recurrence %v", r)
	}
	if _, has := r["day_of_month"]; has {
		t.Fatalf("weekly has no day of month: %v", r)
	}
}
