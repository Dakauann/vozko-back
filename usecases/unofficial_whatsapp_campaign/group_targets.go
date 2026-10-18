package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

var ErrGroupTargetsUnavailable = errors.New("the group directory is unavailable; try again shortly")

type GroupSeats interface {
	FindByJID(ctx context.Context, instanceID, jid string) (*uw.Group, error)
}

func (uc *createCampaignUseCase) SetGroupSeats(seats GroupSeats) {
	uc.groupSeats = seats
}

func (uc *createCampaignUseCase) entriesFor(ctx context.Context, in *uwc.Campaign, instance *uw.Instance) ([]uwc.Entry, error) {
	if in.TargetsGroups() {
		return groupEntries(ctx, uc.groupSeats, in, instance)
	}
	return uc.materializeTargets(ctx, in, instance)
}

func groupEntries(ctx context.Context, seats GroupSeats, in *uwc.Campaign, instance *uw.Instance) ([]uwc.Entry, error) {
	if seats == nil {
		return nil, ErrGroupTargetsUnavailable
	}
	now := time.Now().UTC()
	entries := make([]uwc.Entry, 0, len(in.Targets))
	for _, t := range in.Targets {
		seat, err := seats.FindByJID(ctx, instance.ID, t.GroupJID)
		if errors.Is(err, uw.ErrGroupNotFound) {
			return nil, fmt.Errorf("%w: %s is not in the group %q", uwc.ErrCampaignTargetInvalid, instance.Label(), t.GroupJID)
		}
		if err != nil {
			return nil, err
		}
		if !seat.CanPost() {
			return nil, fmt.Errorf("%w: only admins send in %q and %s is not an admin", uwc.ErrCampaignTargetInvalid, seat.Subject, instance.Label())
		}
		entry := uwc.NewGroupEntry(in.ID, in.WorkspaceID, seat.JID, firstNonEmpty(seat.Subject, t.Name), now)
		entry.ID = uuid.New().String()
		entry.Normalize()
		entries = append(entries, entry)
	}
	return entries, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
