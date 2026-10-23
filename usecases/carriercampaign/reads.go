package carriercampaign_usecase

import (
	"context"
	"errors"
	"fmt"
	"time"

	"vozko/domain/campaign"
	cc "vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/shared"
)

const ChannelWhatsApp = "whatsapp"

var (
	ErrRecipientStatusInvalid = errors.New("carrier campaign: unknown recipient status")
	ErrOverviewWindow         = errors.New("carrier campaign: the overview covers 7, 30 or 90 days")
)

var overviewWindows = map[int]bool{7: true, 30: true, 90: true}

type CampaignSummary struct {
	Campaign cc.Campaign
	Stats    cc.Funnel
}

func (s *Service) ListSummaries(ctx context.Context, workspaceID string, in cc.ListCampaignsInput) (*shared.PaginatedResult[CampaignSummary], error) {
	switch {
	case in.Channel != "" && !in.Channel.Valid():
		return nil, cc.ErrChannelInvalid
	case in.Status != "" && !in.Status.Valid():
		return nil, cc.ErrStatusInvalid
	case in.Kind != "" && !in.Kind.Valid():
		return nil, cc.ErrKindInvalid
	}
	page, err := s.List(ctx, workspaceID, in)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(page.Items))
	for _, c := range page.Items {
		ids = append(ids, c.ID)
	}
	stats := map[string]cc.Funnel{}
	if len(ids) > 0 {
		if stats, err = s.deps.Stats.FunnelsOf(ctx, workspaceID, ids); err != nil {
			return nil, err
		}
	}
	items := make([]CampaignSummary, 0, len(page.Items))
	for _, c := range page.Items {
		items = append(items, CampaignSummary{Campaign: *c, Stats: stats[c.ID]})
	}
	return &shared.PaginatedResult[CampaignSummary]{
		Items: items, Page: page.Page, PageSize: page.PageSize, TotalItems: page.TotalItems, TotalPages: page.TotalPages,
	}, nil
}

func liveStatus(s cc.Status) campaign.Status {
	switch s {
	case cc.StatusRunning:
		return campaign.StatusRunning
	case cc.StatusPaused:
		return campaign.StatusPaused
	case cc.StatusStopped:
		return campaign.StatusStopped
	}
	return campaign.StatusCompleted
}

func (s *Service) Timeline(ctx context.Context, workspaceID, id string, bucket campaign.TimelineBucket) (*campaign.Timeline, error) {
	c, err := s.deps.Campaigns.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	return s.deps.Timeline.Execute(ctx, campaign.LiveCampaign{ID: c.ID, WorkspaceID: c.WorkspaceID, Status: liveStatus(c.Status)}, bucket)
}

type RecipientQuery struct {
	WorkspaceID string
	CampaignID  string
	RunID       string
	Status      cc.RecipientStatus
	Pagination  shared.Pagination
}

type RecipientRow struct {
	Recipient     cc.Recipient
	LeadName      string
	MessageStatus carriermessaging.Status
}

func (s *Service) Recipients(ctx context.Context, workspaceID, id string, q RecipientQuery) (*shared.PaginatedResult[RecipientRow], error) {
	if q.Status != "" && !q.Status.Valid() {
		return nil, ErrRecipientStatusInvalid
	}
	c, err := s.deps.Campaigns.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if q.RunID != "" {
		run, err := s.deps.Runs.FindByID(ctx, q.RunID)
		if err != nil {
			return nil, err
		}
		if run.CampaignID != c.ID {
			return nil, cc.ErrRunNotFound
		}
	}
	q.WorkspaceID, q.CampaignID = workspaceID, c.ID
	return s.deps.RecipientRows.ListRecipientRows(ctx, q)
}

type OverviewDay struct {
	Day         shared.Date
	Channel     carriermessaging.Channel
	Sent        int64
	Delivered   int64
	Read        int64
	Failed      int64
	SpendMicros int64
}

type ChannelTotals struct {
	Channel     carriermessaging.Channel
	SpendMicros int64
	Conversions int64
}

type Overview struct {
	Totals   cc.Funnel
	Daily    []OverviewDay
	Channels []ChannelTotals
	Carriers []cc.CarrierStat
}

type ChannelCost struct {
	Channel                 string
	SpendMicros             int64
	Conversions             int64
	CostPerConversionMicros *int64
}

type OverviewResult struct {
	From     time.Time
	To       time.Time
	Totals   cc.Funnel
	Rates    cc.Rates
	Daily    []OverviewDay
	Channels []ChannelCost
	Carriers []cc.CarrierStat
}

func overviewChannel(raw string) (carriermessaging.Channel, error) {
	if raw == "all" {
		return "", nil
	}
	channel := carriermessaging.Channel(raw)
	if !channel.Valid() {
		return "", cc.ErrChannelInvalid
	}
	return channel, nil
}

func costOf(channel string, spend, conversions int64) ChannelCost {
	rates := cc.Funnel{SpendMicros: spend, Conversions: conversions}.Rates()
	return ChannelCost{Channel: channel, SpendMicros: spend, Conversions: conversions, CostPerConversionMicros: rates.CostPerConversionMicros}
}

func (s *Service) Overview(ctx context.Context, workspaceID string, days int, channelFilter string) (OverviewResult, error) {
	if !overviewWindows[days] {
		return OverviewResult{}, ErrOverviewWindow
	}
	channel, err := overviewChannel(channelFilter)
	if err != nil {
		return OverviewResult{}, err
	}
	tz, err := s.deps.Timezones.Timezone(ctx, workspaceID)
	if err != nil {
		return OverviewResult{}, err
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return OverviewResult{}, fmt.Errorf("workspace time zone %q: %w", tz, err)
	}
	to := s.now()
	local := to.In(loc)
	from := time.Date(local.Year(), local.Month(), local.Day()-(days-1), 0, 0, 0, 0, loc)
	o, err := s.deps.Overview.Overview(ctx, workspaceID, from, to, tz, channel)
	if err != nil {
		return OverviewResult{}, err
	}
	out := OverviewResult{From: from, To: to, Totals: o.Totals, Rates: o.Totals.Rates(), Daily: o.Daily, Carriers: o.Carriers}
	for _, line := range o.Channels {
		out.Channels = append(out.Channels, costOf(string(line.Channel), line.SpendMicros, line.Conversions))
	}
	if channel != "" {
		return out, nil
	}
	settings, err := s.deps.Settings.GetSettings(ctx, workspaceID)
	if err != nil {
		return OverviewResult{}, err
	}
	spend, conversions, err := s.deps.WhatsAppSpend.WhatsAppCampaignSpend(ctx, workspaceID, from, to, settings.AttributionDays)
	if err != nil {
		return OverviewResult{}, err
	}
	out.Channels = append(out.Channels, costOf(ChannelWhatsApp, spend, conversions))
	return out, nil
}
