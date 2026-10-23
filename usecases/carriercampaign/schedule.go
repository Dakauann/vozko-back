package carriercampaign_usecase

import (
	"context"
	"time"

	cc "vozko/domain/carriercampaign"
)

type ScheduleReport struct {
	Started []string
	Failed  map[string]error
}

func (s *Service) RunSchedules(ctx context.Context, limit int) (ScheduleReport, error) {
	due, err := s.deps.Campaigns.ListScheduledDue(ctx, s.now(), limit)
	if err != nil {
		return ScheduleReport{}, err
	}
	return s.startDue(ctx, due)
}

func (s *Service) startDue(ctx context.Context, due []*cc.Campaign) (ScheduleReport, error) {
	report := ScheduleReport{Failed: map[string]error{}}
	for _, c := range due {
		if c == nil || c.ScheduledAt == nil {
			continue
		}
		occurrence := *c.ScheduledAt
		claimed, err := s.deps.Campaigns.ClaimScheduledStart(c.ID, s.now())
		if err != nil {
			report.Failed[c.ID] = err
			continue
		}
		if !claimed {
			continue
		}
		if err := s.startScheduled(ctx, *c, occurrence); err != nil {
			report.Failed[c.ID] = err
			s.deps.Logf("[carrier-campaign] scheduled campaign %s did not start: %v", c.ID, err)
			if _, rerr := s.deps.Campaigns.Reschedule(ctx, c.ID, cc.StatusScheduled, cc.StatusDraft, nil); rerr != nil {
				s.deps.Logf("[carrier-campaign] return campaign %s to draft: %v", c.ID, rerr)
			}
			continue
		}
		report.Started = append(report.Started, c.ID)
	}
	return report, nil
}

func (s *Service) startScheduled(ctx context.Context, c cc.Campaign, occurrence time.Time) error {
	c.ScheduledAt = nil
	if err := s.validateForSend(c); err != nil {
		return err
	}
	route, err := s.ready(ctx, c)
	if err != nil {
		return err
	}
	_, err = s.startRun(ctx, c, creatorOf(c), route, occurrence, false)
	return err
}
