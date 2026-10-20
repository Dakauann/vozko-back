package unofficial_whatsapp_group_usecase

import (
	"context"
	"log"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
	uwg "vozko/domain/unofficial_whatsapp_group"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

var _ uwuc.GroupWatcher = (*Service)(nil)

const primaryRecheck = time.Minute

func (s *Service) GroupMessage(ctx context.Context, instance *uw.Instance, groupJID string, hasText bool) {
	g, err := s.d.Groups.FindByJID(ctx, instance.WorkspaceID, groupJID)
	if err != nil || !g.Followed() {
		return
	}
	if g.PrimaryInstanceID != instance.ID && !s.promoteIfPrimaryChanged(ctx, g, instance) {
		return
	}
	if !hasText {
		return
	}
	if err := s.d.Analyses.AddPending(ctx, g.ID, 1); err != nil {
		log.Printf("[groups] could not count a message of group %s: %v", g.ID, err)
	}
}

func (s *Service) promoteIfPrimaryChanged(ctx context.Context, g *uwg.Group, instance *uw.Instance) bool {
	if !s.primaryCheckDue(g.ID, s.d.Clock()) {
		return false
	}
	instances, err := s.workspaceInstances(ctx, g.WorkspaceID)
	if err != nil {
		log.Printf("[groups] could not read the numbers of workspace %s: %v", g.WorkspaceID, err)
		return false
	}
	if err := s.refreshPrimary(ctx, g, instances); err != nil {
		log.Printf("[groups] could not refresh the primary number of group %s: %v", g.ID, err)
		return false
	}
	return g.PrimaryInstanceID == instance.ID
}

func (s *Service) primaryCheckDue(groupID string, now time.Time) bool {
	if last, ok := s.primaryChecks.Load(groupID); ok && now.Sub(last.(time.Time)) < primaryRecheck {
		return false
	}
	s.primaryChecks.Store(groupID, now)
	return true
}

func (s *Service) GroupChanged(ctx context.Context, instance *uw.Instance, groupJID string, change uw.GroupParticipantChange) {
	g, err := s.d.Groups.FindByJID(ctx, instance.WorkspaceID, groupJID)
	if err != nil || !g.Followed() || g.PrimaryInstanceID != instance.ID {
		return
	}
	seat, err := s.d.SeatSync.Get(ctx, instance.WorkspaceID, instance.ID, groupJID, true, uw.Unrestricted())
	if err != nil {
		log.Printf("[groups] could not resync group %s after a change: %v", g.ID, err)
		return
	}
	s.applyFromWebhook(ctx, g, seat, func(current []uwg.Member) uwg.DepartureCauses { return uwg.CausesFromChange(change, current) })
}

func (s *Service) SeatSynced(ctx context.Context, instance *uw.Instance, seat *uw.Group) {
	if seat == nil || !uw.IsGroupJID(seat.JID) {
		return
	}
	used, err := s.d.Groups.Exists(ctx, instance.WorkspaceID)
	if err != nil || !used {
		return
	}
	instances, err := s.workspaceInstances(ctx, instance.WorkspaceID)
	if err != nil {
		return
	}
	g := &uwg.Group{WorkspaceID: instance.WorkspaceID, JID: seat.JID, Origin: uwg.OriginOf(seat, ownNumbers(instances))}
	g.AbsorbSeat(seat)
	if err := s.d.Groups.UpsertSeen(ctx, g); err != nil {
		log.Printf("[groups] could not record group %s: %v", seat.JID, err)
		return
	}
	if err := s.refreshPrimary(ctx, g, instances); err != nil {
		log.Printf("[groups] could not refresh the primary number of group %s: %v", g.ID, err)
		return
	}
	if g.Followed() && g.PrimaryInstanceID == instance.ID {
		s.applyFromWebhook(ctx, g, seat, nil)
	}
}

func (s *Service) applyFromWebhook(ctx context.Context, g *uwg.Group, seat *uw.Group, explain departures) {
	instances, err := s.workspaceInstances(ctx, g.WorkspaceID)
	if err != nil {
		return
	}
	if err := s.applySeatExplained(ctx, g, seat, instances, explain); err != nil {
		log.Printf("[groups] could not update the members of group %s: %v", g.ID, err)
	}
}
