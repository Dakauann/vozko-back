package carriercampaign_usecase

import (
	"context"

	cc "vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
)

func (s *Service) Ready(ctx context.Context, workspaceID, id string, a Actor) (*cc.Campaign, error) {
	if a.UserID == "" {
		return nil, ErrActorRequired
	}
	c, err := s.deps.Campaigns.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	target, err := cc.Transition(c.Status, cc.ActionReady)
	if err != nil {
		return nil, err
	}
	if c.ScheduledAt != nil || c.Recurrence != nil {
		return nil, cc.ErrReadyScheduled
	}
	if !s.launched(c.Channel) {
		return nil, carriermessaging.ErrChannelNotLaunched
	}
	defs, err := s.fields(workspaceID)
	if err != nil {
		return nil, err
	}
	if err := c.ReadyToLaunch(s.now(), defs); err != nil {
		return nil, err
	}
	route, err := s.ready(ctx, *c)
	if err != nil {
		return nil, err
	}
	if err := s.confirmAudience(ctx, *c, a); err != nil {
		return nil, err
	}
	q, err := s.quote(ctx, *c, a, route)
	if err != nil {
		return nil, err
	}
	if q.audience == 0 {
		return nil, ErrAudienceEmpty
	}
	if q.shortfall > 0 {
		return nil, &carriermessaging.ShortfallError{MissingMicros: q.shortfall}
	}
	swapped, err := s.deps.Campaigns.SwapStatus(ctx, c.ID, c.Status, target)
	if err != nil {
		return nil, err
	}
	if !swapped {
		return nil, cc.ErrNotDraft
	}
	return s.deps.Campaigns.FindByIDUnscoped(ctx, c.ID)
}
