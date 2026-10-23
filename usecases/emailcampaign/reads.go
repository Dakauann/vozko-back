package emailcampaign_usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	"vozko/domain/cache"
	"vozko/domain/emailcampaign"
	"vozko/domain/emailsending"
)

const (
	liveFreshness     = 15 * time.Second
	idleFreshness     = time.Minute
	finishedFreshness = 10 * time.Minute
	overviewFreshness = 2 * time.Minute
	linksShown        = 50
	topCampaigns      = 5
	maxOverviewDays   = 90
	recipientPageCap  = 200
)

func freshness(status emailcampaign.Status) time.Duration {
	switch {
	case status.Sending():
		return liveFreshness
	case status.Finished():
		return finishedFreshness
	}
	return idleFreshness
}

func refreshAfter(status emailcampaign.Status) int {
	if status.Finished() {
		return 0
	}
	return int(freshness(status) / time.Second)
}

func memoKey(kind string, c emailcampaign.Campaign) string {
	return fmt.Sprintf("email_campaign:%s:%s:%s", kind, c.ID, c.Status)
}

type CampaignMetrics struct {
	emailcampaign.Metrics
	Status              emailcampaign.Status                                  `json:"status"`
	HoldReason          emailcampaign.HoldReason                              `json:"hold_reason"`
	Variants            map[emailcampaign.Variant]emailcampaign.VariantResult `json:"variants,omitempty"`
	Winner              emailcampaign.Variant                                 `json:"winner,omitempty"`
	RefreshAfterSeconds int                                                   `json:"refresh_after_seconds"`
}

func (s *Service) Metrics(ctx context.Context, a Actor, campaignID string) (CampaignMetrics, error) {
	c, err := s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
	if err != nil {
		return CampaignMetrics{}, err
	}
	return cache.Remember(ctx, s.deps.Memo, memoKey("metrics", *c), freshness(c.Status), func(ctx context.Context) (CampaignMetrics, error) {
		tally, err := s.deps.Metrics.Tally(ctx, c.WorkspaceID, c.ID)
		if err != nil {
			return CampaignMetrics{}, err
		}
		out := CampaignMetrics{Metrics: emailcampaign.NewMetrics(tally), Status: c.Status, HoldReason: c.HoldReason, RefreshAfterSeconds: refreshAfter(c.Status)}
		if c.ABTest != nil {
			if out.Variants, err = s.deps.Metrics.VariantResults(ctx, c.WorkspaceID, c.ID); err != nil {
				return CampaignMetrics{}, err
			}
			out.Winner = c.ABTest.Winner
		}
		return out, nil
	})
}

type Timeline struct {
	Bucket              emailcampaign.Bucket          `json:"bucket"`
	Points              []emailcampaign.TimelinePoint `json:"points"`
	RefreshAfterSeconds int                           `json:"refresh_after_seconds"`
	TimeZone            string                        `json:"time_zone" example:"America/Sao_Paulo"`
}

func (s *Service) Timeline(ctx context.Context, a Actor, campaignID string) (Timeline, error) {
	c, err := s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
	if err != nil {
		return Timeline{}, err
	}
	return cache.Remember(ctx, s.deps.Memo, memoKey("timeline", *c), freshness(c.Status), func(ctx context.Context) (Timeline, error) {
		bucket := emailcampaign.BucketHour
		if c.StartedAt != nil {
			end := s.now()
			if c.CompletedAt != nil {
				end = *c.CompletedAt
			}
			bucket = emailcampaign.TimelineBucketFor(*c.StartedAt, end)
		}
		points, err := s.deps.Metrics.Timeline(ctx, c.WorkspaceID, c.ID, bucket)
		if err != nil {
			return Timeline{}, err
		}
		if points == nil {
			points = []emailcampaign.TimelinePoint{}
		}
		return Timeline{Bucket: bucket, Points: points, RefreshAfterSeconds: refreshAfter(c.Status), TimeZone: emailsending.TimeZoneName}, nil
	})
}

func (s *Service) Links(ctx context.Context, a Actor, campaignID string) ([]emailcampaign.LinkStat, error) {
	c, err := s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
	if err != nil {
		return nil, err
	}
	return cache.Remember(ctx, s.deps.Memo, memoKey("links", *c), freshness(c.Status), func(ctx context.Context) ([]emailcampaign.LinkStat, error) {
		return s.deps.Metrics.Links(ctx, c.WorkspaceID, c.ID, linksShown)
	})
}

func (s *Service) Domains(ctx context.Context, a Actor, campaignID string) ([]emailcampaign.DomainStat, error) {
	c, err := s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
	if err != nil {
		return nil, err
	}
	return cache.Remember(ctx, s.deps.Memo, memoKey("domains", *c), freshness(c.Status), func(ctx context.Context) ([]emailcampaign.DomainStat, error) {
		counts, err := s.deps.Metrics.Domains(ctx, c.WorkspaceID, c.ID)
		if err != nil {
			return nil, err
		}
		return emailcampaign.GroupDomains(counts), nil
	})
}

type RecipientQuery struct {
	Filter   emailcampaign.RecipientFilter
	Page     int
	PageSize int
}

type RecipientPage struct {
	Items    []emailcampaign.RecipientRow
	Total    int64
	Page     int
	PageSize int
}

func (s *Service) Recipients(ctx context.Context, a Actor, campaignID string, q RecipientQuery) (RecipientPage, error) {
	if q.Filter.Status != "" && !q.Filter.Status.Valid() {
		return RecipientPage{}, emailcampaign.ErrRecipientFilterInvalid
	}
	if q.Filter.SkipReason != "" && !q.Filter.SkipReason.Valid() {
		return RecipientPage{}, emailcampaign.ErrRecipientFilterInvalid
	}
	c, err := s.deps.Campaigns.Get(ctx, a.WorkspaceID, campaignID)
	if err != nil {
		return RecipientPage{}, err
	}
	page := max(q.Page, 1)
	size := q.PageSize
	if size <= 0 || size > recipientPageCap {
		size = recipientPageCap / 4
	}
	q.Filter.Search = strings.TrimSpace(q.Filter.Search)
	items, total, err := s.deps.Recipients.List(ctx, a.WorkspaceID, c.ID, q.Filter, (page-1)*size, size)
	if err != nil {
		return RecipientPage{}, err
	}
	return RecipientPage{Items: items, Total: total, Page: page, PageSize: size}, nil
}

type Overview struct {
	Days       int                             `json:"days"`
	TimeZone   string                          `json:"time_zone" example:"America/Sao_Paulo"`
	Totals     emailcampaign.Metrics           `json:"totals"`
	Daily      []emailcampaign.TimelinePoint   `json:"daily"`
	Top        []emailcampaign.CampaignSummary `json:"top"`
	Reputation emailcampaign.Reputation        `json:"reputation"`
}

func (s *Service) Overview(ctx context.Context, a Actor, days int) (Overview, error) {
	if days < 1 || days > maxOverviewDays {
		return Overview{}, ErrDaysInvalid
	}
	key := fmt.Sprintf("email_overview:%s:%d", a.WorkspaceID, days)
	return cache.Remember(ctx, s.deps.Memo, key, overviewFreshness, func(ctx context.Context) (Overview, error) {
		since := emailsending.DayStart(s.now()).AddDate(0, 0, -(days - 1))
		data, err := s.deps.Metrics.Overview(ctx, a.WorkspaceID, since, topCampaigns)
		if err != nil {
			return Overview{}, err
		}
		if data.Daily == nil {
			data.Daily = []emailcampaign.TimelinePoint{}
		}
		if data.Top == nil {
			data.Top = []emailcampaign.CampaignSummary{}
		}
		return Overview{Days: days, TimeZone: emailsending.TimeZoneName, Totals: emailcampaign.NewMetrics(data.Totals), Daily: data.Daily, Top: data.Top, Reputation: emailcampaign.NewReputation(data.Totals)}, nil
	})
}
