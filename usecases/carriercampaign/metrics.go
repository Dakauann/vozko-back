package carriercampaign_usecase

import (
	"context"
	"time"

	cc "vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
)

type FunnelView struct {
	Funnel cc.Funnel `json:"funnel"`
	Rates  cc.Rates  `json:"rates"`
}

func (s *Service) funnelQuery(ctx context.Context, workspaceID, id string) (cc.Campaign, cc.FunnelQuery, error) {
	c, err := s.deps.Campaigns.FindByID(ctx, workspaceID, id)
	if err != nil {
		return cc.Campaign{}, cc.FunnelQuery{}, err
	}
	return *c, cc.FunnelQuery{WorkspaceID: workspaceID, Channel: c.Channel, CampaignID: c.ID}, nil
}

func (s *Service) Funnel(ctx context.Context, workspaceID, id string) (FunnelView, error) {
	_, q, err := s.funnelQuery(ctx, workspaceID, id)
	if err != nil {
		return FunnelView{}, err
	}
	return s.view(ctx, q)
}

func (s *Service) RunFunnel(ctx context.Context, workspaceID, id, runID string) (FunnelView, error) {
	_, q, err := s.funnelQuery(ctx, workspaceID, id)
	if err != nil {
		return FunnelView{}, err
	}
	run, err := s.deps.Runs.FindByID(ctx, runID)
	if err != nil {
		return FunnelView{}, err
	}
	if run.CampaignID != q.CampaignID {
		return FunnelView{}, cc.ErrRunNotFound
	}
	q.RunID = run.ID
	return s.view(ctx, q)
}

func (s *Service) view(ctx context.Context, q cc.FunnelQuery) (FunnelView, error) {
	f, err := s.deps.Funnels.Funnel(ctx, q)
	if err != nil {
		return FunnelView{}, err
	}
	return FunnelView{Funnel: f, Rates: f.Rates()}, nil
}

func (s *Service) Variants(ctx context.Context, workspaceID, id string) ([]cc.VariantStat, error) {
	c, q, err := s.funnelQuery(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	funnels, err := s.deps.Funnels.VariantFunnels(ctx, q)
	if err != nil {
		return nil, err
	}
	return cc.VariantStats(c.Variants, funnels), nil
}

func (s *Service) Buttons(ctx context.Context, workspaceID, id string, variant int) ([]cc.ButtonStat, error) {
	c, q, err := s.funnelQuery(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if variant < 0 || variant >= len(c.Variants) {
		return nil, ErrVariantIndex
	}
	if c.Channel != carriermessaging.ChannelRCS {
		return []cc.ButtonStat{}, nil
	}
	taps, clicks, err := s.deps.Funnels.ButtonCounts(ctx, q, variant)
	if err != nil {
		return nil, err
	}
	return cc.ButtonStats(c.Variants[variant].Content, taps, clicks), nil
}

func (s *Service) Carriers(ctx context.Context, workspaceID, id string) ([]cc.CarrierStat, error) {
	_, q, err := s.funnelQuery(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	return s.deps.Funnels.Carriers(ctx, q)
}

func (s *Service) Attribute(ctx context.Context, workspaceID, leadID string, wonAt time.Time) (cc.Touch, bool, error) {
	settings, err := s.deps.Settings.GetSettings(ctx, workspaceID)
	if err != nil {
		return cc.Touch{}, false, err
	}
	days := settings.AttributionDays
	touches, err := s.deps.Funnels.Touches(ctx, workspaceID, leadID, wonAt.Add(-time.Duration(days)*24*time.Hour))
	if err != nil {
		return cc.Touch{}, false, err
	}
	touch, ok := cc.LastTouch(touches, wonAt, days)
	return touch, ok, nil
}
