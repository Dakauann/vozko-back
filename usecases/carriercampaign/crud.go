package carriercampaign_usecase

import (
	"context"
	"fmt"
	"strings"
	"time"

	cc "vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/customfield"
	"vozko/domain/selection"
	"vozko/domain/shared"
)

type Draft struct {
	Name        string
	SenderID    string
	Channel     carriermessaging.Channel
	Kind        cc.Kind
	Variants    []cc.Variant
	Fallback    cc.Fallback
	Audience    selection.Selection
	ScheduledAt *time.Time
	Recurrence  *cc.Recurrence
}

func (d Draft) applyTo(c *cc.Campaign) {
	c.Name = strings.TrimSpace(d.Name)
	c.SenderID = strings.TrimSpace(d.SenderID)
	c.Channel = d.Channel
	c.Kind = d.Kind
	c.Variants = d.Variants
	c.Fallback = d.Fallback
	c.Audience = d.Audience
	c.ScheduledAt = d.ScheduledAt
	c.Recurrence = d.Recurrence
}

func (s *Service) fields(workspaceID string) ([]*customfield.Definition, error) {
	defs, err := s.deps.Definitions.ListByObject(workspaceID, customfield.ObjectLead)
	if err != nil {
		return nil, fmt.Errorf("lead fields of workspace %s: %w", workspaceID, err)
	}
	return defs, nil
}

func (s *Service) Create(ctx context.Context, workspaceID string, a Actor, d Draft) (*cc.Campaign, error) {
	if strings.TrimSpace(a.UserID) == "" {
		return nil, ErrActorRequired
	}
	now := s.now()
	c := cc.Campaign{ID: s.deps.NewID(), WorkspaceID: workspaceID, Status: cc.StatusDraft, CreatedBy: a.UserID, CreatedAt: now, UpdatedAt: now}
	d.applyTo(&c)
	if err := s.validate(c, now); err != nil {
		return nil, err
	}
	if err := s.deps.Campaigns.Create(ctx, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Service) validate(c cc.Campaign, now time.Time) error {
	if !s.launched(c.Channel) {
		return carriermessaging.ErrChannelNotLaunched
	}
	defs, err := s.fields(c.WorkspaceID)
	if err != nil {
		return err
	}
	return c.Validate(now, defs)
}

func (s *Service) Update(ctx context.Context, workspaceID, id string, d Draft) (*cc.Campaign, error) {
	c, err := s.deps.Campaigns.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	if !c.Status.Editable() {
		return nil, cc.ErrNotEditable
	}
	now := s.now()
	next := *c
	d.applyTo(&next)
	next.Status = cc.StatusDraft
	next.UpdatedAt = now
	if err := s.validate(next, now); err != nil {
		return nil, err
	}
	if err := s.deps.Campaigns.Update(ctx, &next, c.Status); err != nil {
		return nil, err
	}
	return &next, nil
}

func (s *Service) Get(ctx context.Context, workspaceID, id string) (*cc.Campaign, error) {
	return s.deps.Campaigns.FindByID(ctx, workspaceID, id)
}

func (s *Service) List(ctx context.Context, workspaceID string, in cc.ListCampaignsInput) (*shared.PaginatedResult[*cc.Campaign], error) {
	in.WorkspaceID = workspaceID
	return s.deps.Campaigns.List(ctx, in)
}

func (s *Service) Delete(ctx context.Context, workspaceID, id string) error {
	c, err := s.deps.Campaigns.FindByID(ctx, workspaceID, id)
	if err != nil {
		return err
	}
	if !c.Status.Deletable() {
		return ErrOnlyDraftDeletes
	}
	return s.deps.Campaigns.Delete(ctx, workspaceID, id)
}

func (s *Service) Duplicate(ctx context.Context, workspaceID, id string, a Actor, name string) (*cc.Campaign, error) {
	if strings.TrimSpace(a.UserID) == "" {
		return nil, ErrActorRequired
	}
	src, err := s.deps.Campaigns.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	now := s.now()
	copied := *src
	copied.ID = s.deps.NewID()
	copied.Name = strings.TrimSpace(name)
	copied.Status = cc.StatusDraft
	copied.ScheduledAt = nil
	copied.CreatedBy = a.UserID
	copied.CreatedAt, copied.UpdatedAt = now, now
	if err := s.validate(copied, now); err != nil {
		return nil, err
	}
	if err := s.deps.Campaigns.Create(ctx, &copied); err != nil {
		return nil, err
	}
	return &copied, nil
}
