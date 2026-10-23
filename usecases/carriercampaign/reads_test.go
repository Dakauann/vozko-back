package carriercampaign_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/campaign"
	cc "vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/shared"
)

type fakeStats struct {
	byID map[string]cc.Funnel
	asks [][]string
}

func (f *fakeStats) FunnelsOf(_ context.Context, ws string, ids []string) (map[string]cc.Funnel, error) {
	f.asks = append(f.asks, ids)
	return f.byID, nil
}

type fakeTimeline struct {
	live   campaign.LiveCampaign
	bucket campaign.TimelineBucket
}

func (f *fakeTimeline) Execute(_ context.Context, live campaign.LiveCampaign, bucket campaign.TimelineBucket) (*campaign.Timeline, error) {
	f.live, f.bucket = live, bucket
	return &campaign.Timeline{Bucket: campaign.TimelineHour, Points: []campaign.TimelinePoint{{Sent: 3}}}, nil
}

type fakeRows struct {
	query RecipientQuery
	rows  []RecipientRow
}

func (f *fakeRows) ListRecipientRows(_ context.Context, q RecipientQuery) (*shared.PaginatedResult[RecipientRow], error) {
	f.query = q
	return shared.NewPaginatedResult(f.rows, q.Pagination, int64(len(f.rows))), nil
}

type fakeOverview struct {
	ws       string
	from, to time.Time
	tz       string
	channel  carriermessaging.Channel
	result   Overview
}

func (f *fakeOverview) Overview(_ context.Context, ws string, from, to time.Time, tz string, channel carriermessaging.Channel) (Overview, error) {
	f.ws, f.from, f.to, f.tz, f.channel = ws, from, to, tz, channel
	return f.result, nil
}

type fakeWhatsAppSpend struct {
	spend, conversions int64
	days               int
	calls              int
}

func (f *fakeWhatsAppSpend) WhatsAppCampaignSpend(_ context.Context, ws string, from, to time.Time, attributionDays int) (int64, int64, error) {
	f.calls++
	f.days = attributionDays
	return f.spend, f.conversions, nil
}

type fakeTimezones struct{}

func (fakeTimezones) Timezone(context.Context, string) (string, error) {
	return "America/Sao_Paulo", nil
}

func TestListSummaries(t *testing.T) {
	h := newHarness(t)
	h.campaigns.put(rcsCampaign("c1"))
	h.campaigns.put(smsCampaign("c2"))
	h.stats.byID = map[string]cc.Funnel{"c1": {Targeted: 5, Sent: 4}}
	page, err := h.svc.ListSummaries(context.Background(), wsID, cc.ListCampaignsInput{Channel: carriermessaging.ChannelRCS, Pagination: shared.Pagination{Page: 1, PageSize: 20}})
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalItems != 1 || len(page.Items) != 1 || page.Items[0].Campaign.ID != "c1" || page.Items[0].Stats.Sent != 4 {
		t.Fatalf("page %+v", page)
	}
	if len(h.stats.asks) != 1 || len(h.stats.asks[0]) != 1 || h.stats.asks[0][0] != "c1" {
		t.Fatalf("stats asked for %v", h.stats.asks)
	}
}

func TestListSummariesRefusesUnknownFilters(t *testing.T) {
	h := newHarness(t)
	if _, err := h.svc.ListSummaries(context.Background(), wsID, cc.ListCampaignsInput{Channel: "email"}); !errors.Is(err, cc.ErrChannelInvalid) {
		t.Fatalf("channel err = %v", err)
	}
	if _, err := h.svc.ListSummaries(context.Background(), wsID, cc.ListCampaignsInput{Status: "held"}); !errors.Is(err, cc.ErrStatusInvalid) {
		t.Fatalf("status err = %v", err)
	}
	if _, err := h.svc.ListSummaries(context.Background(), wsID, cc.ListCampaignsInput{Kind: "promo"}); !errors.Is(err, cc.ErrKindInvalid) {
		t.Fatalf("kind err = %v", err)
	}
}

func TestTimeline(t *testing.T) {
	cases := []struct {
		status cc.Status
		live   campaign.Status
	}{
		{cc.StatusRunning, campaign.StatusRunning},
		{cc.StatusPaused, campaign.StatusPaused},
		{cc.StatusStopped, campaign.StatusStopped},
		{cc.StatusCompleted, campaign.StatusCompleted},
		{cc.StatusScheduled, campaign.StatusCompleted},
	}
	for _, tc := range cases {
		h := newHarness(t)
		c := rcsCampaign("c1")
		c.Status = tc.status
		h.campaigns.put(c)
		got, err := h.svc.Timeline(context.Background(), wsID, "c1", campaign.TimelineMinute)
		if err != nil || got.Bucket != campaign.TimelineHour || len(got.Points) != 1 {
			t.Fatalf("timeline %+v %v", got, err)
		}
		if h.timeline.live != (campaign.LiveCampaign{ID: "c1", WorkspaceID: wsID, Status: tc.live}) || h.timeline.bucket != campaign.TimelineMinute {
			t.Fatalf("%s: live %+v bucket %s", tc.status, h.timeline.live, h.timeline.bucket)
		}
	}
	h := newHarness(t)
	h.campaigns.put(rcsCampaign("c1"))
	if _, err := h.svc.Timeline(context.Background(), "ws2", "c1", ""); !errors.Is(err, cc.ErrCampaignNotFound) {
		t.Fatalf("foreign err = %v", err)
	}
}

func TestRecipients(t *testing.T) {
	h := newHarness(t)
	h.campaigns.put(rcsCampaign("c1"))
	h.rows.rows = []RecipientRow{{Recipient: cc.Recipient{ID: "r1"}, LeadName: "Ana", MessageStatus: carriermessaging.StatusDelivered}}
	page, err := h.svc.Recipients(context.Background(), wsID, "c1", RecipientQuery{WorkspaceID: "ws2", CampaignID: "other", Status: cc.RecipientSent, Pagination: shared.Pagination{Page: 2, PageSize: 10}})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("page %+v %v", page, err)
	}
	if q := h.rows.query; q.WorkspaceID != wsID || q.CampaignID != "c1" || q.Status != cc.RecipientSent || q.Pagination.Page != 2 {
		t.Fatalf("query %+v", q)
	}
	if _, err := h.svc.Recipients(context.Background(), wsID, "c1", RecipientQuery{Status: "lost"}); !errors.Is(err, ErrRecipientStatusInvalid) {
		t.Fatalf("status err = %v", err)
	}
	h.runs.put(cc.Run{ID: "run-x", CampaignID: "c9"})
	if _, err := h.svc.Recipients(context.Background(), wsID, "c1", RecipientQuery{RunID: "run-x"}); !errors.Is(err, cc.ErrRunNotFound) {
		t.Fatalf("foreign run err = %v", err)
	}
}

func TestOverview(t *testing.T) {
	sp, _ := time.LoadLocation("America/Sao_Paulo")
	h := newHarness(t)
	h.overview.result = Overview{
		Totals:   cc.Funnel{Sent: 100, Delivered: 80, Conversions: 4, SpendMicros: 1_000_000},
		Channels: []ChannelTotals{{Channel: carriermessaging.ChannelRCS, SpendMicros: 900_000, Conversions: 3}, {Channel: carriermessaging.ChannelSMS, SpendMicros: 100_000}},
		Daily:    []OverviewDay{{Day: shared.Date{Year: 2026, Month: 10, Day: 21}, Channel: carriermessaging.ChannelRCS, Sent: 100}},
		Carriers: []cc.CarrierStat{{Operator: "Vivo", Sent: 100}},
	}
	h.whatsapp.spend, h.whatsapp.conversions = 2_000_000, 5

	got, err := h.svc.Overview(context.Background(), wsID, 7, "all")
	if err != nil {
		t.Fatal(err)
	}
	wantFrom := time.Date(2026, 10, 15, 0, 0, 0, 0, sp)
	if !h.overview.from.Equal(wantFrom) || !h.overview.to.Equal(testNow) || h.overview.tz != "America/Sao_Paulo" || h.overview.channel != "" || h.overview.ws != wsID {
		t.Fatalf("window %s to %s tz %s channel %q", h.overview.from, h.overview.to, h.overview.tz, h.overview.channel)
	}
	if got.Rates.Delivery == nil || *got.Rates.Delivery != 0.8 || got.Totals.Sent != 100 || len(got.Daily) != 1 || len(got.Carriers) != 1 {
		t.Fatalf("overview %+v", got)
	}
	if len(got.Channels) != 3 {
		t.Fatalf("channels %+v", got.Channels)
	}
	rcsLine, smsLine, waLine := got.Channels[0], got.Channels[1], got.Channels[2]
	if rcsLine.Channel != "rcs" || rcsLine.CostPerConversionMicros == nil || *rcsLine.CostPerConversionMicros != 300_000 {
		t.Fatalf("rcs %+v", rcsLine)
	}
	if smsLine.Channel != "sms" || smsLine.CostPerConversionMicros != nil {
		t.Fatalf("sms %+v", smsLine)
	}
	if waLine.Channel != ChannelWhatsApp || waLine.SpendMicros != 2_000_000 || *waLine.CostPerConversionMicros != 400_000 || h.whatsapp.days != 7 {
		t.Fatalf("whatsapp %+v days %d", waLine, h.whatsapp.days)
	}
}

func TestOverviewChannelFilter(t *testing.T) {
	h := newHarness(t)
	h.overview.result = Overview{Channels: []ChannelTotals{{Channel: carriermessaging.ChannelSMS, SpendMicros: 1}}}
	got, err := h.svc.Overview(context.Background(), wsID, 30, "sms")
	if err != nil {
		t.Fatal(err)
	}
	if h.overview.channel != carriermessaging.ChannelSMS || len(got.Channels) != 1 || h.whatsapp.calls != 0 {
		t.Fatalf("channel %q lines %+v whatsapp calls %d", h.overview.channel, got.Channels, h.whatsapp.calls)
	}
}

func TestOverviewRefusals(t *testing.T) {
	cases := []struct {
		days    int
		channel string
		err     error
	}{
		{14, "all", ErrOverviewWindow},
		{0, "all", ErrOverviewWindow},
		{7, "whatsapp", cc.ErrChannelInvalid},
		{7, "", cc.ErrChannelInvalid},
	}
	for _, tc := range cases {
		h := newHarness(t)
		if _, err := h.svc.Overview(context.Background(), wsID, tc.days, tc.channel); !errors.Is(err, tc.err) {
			t.Fatalf("%d %q err = %v", tc.days, tc.channel, err)
		}
	}
}
