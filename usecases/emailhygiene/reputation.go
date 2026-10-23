package emailhygiene

import (
	"context"
	"time"

	"vozko/domain/emailaddress"
	h "vozko/domain/emailhygiene"
	es "vozko/domain/emailsending"
)

type ReputationDeps struct {
	Bad      h.BadAddressStore
	Activity h.ActivityStore
}

type Reputation struct {
	d ReputationDeps
}

func NewReputation(d ReputationDeps) *Reputation {
	return &Reputation{d: d}
}

func (r *Reputation) Record(ctx context.Context, m es.Message, ev es.DeliveryEvent, effect es.EventEffect) (es.SuppressReason, error) {
	hash := h.HashOf(m.Address)
	switch ev.Kind {
	case es.EventBounce:
		return r.bounced(ctx, m, hash, ev, effect)
	case es.EventDelivery:
		return "", r.d.Activity.Delivered(ctx, m.WorkspaceID, hash, m.ID, m.RunKind == es.RunCampaign, ev.OccurredAt)
	}
	if h.Engages(ev) {
		return "", r.d.Activity.Engaged(ctx, m.WorkspaceID, hash, ev.OccurredAt)
	}
	return "", nil
}

func (r *Reputation) bounced(ctx context.Context, m es.Message, hash h.AddressHash, ev es.DeliveryEvent, effect es.EventEffect) (es.SuppressReason, error) {
	if effect.Suppress && effect.SuppressReason == es.SuppressHardBounce {
		return "", r.d.Bad.Add(ctx, hash, ev.OccurredAt)
	}
	if !h.CountsTowardSoftStreak(ev.Bounce) {
		return "", nil
	}
	streak, err := r.d.Activity.SoftBounced(ctx, m.WorkspaceID, hash, m.ID, ev.OccurredAt)
	if err != nil {
		return "", err
	}
	if h.SoftStreakSuppresses(streak.Count, streak.StartedAt, ev.OccurredAt) {
		return es.SuppressSoftBounce, nil
	}
	return "", nil
}

func (r *Reputation) Replied(ctx context.Context, workspaceID string, address emailaddress.Address, at time.Time) error {
	return r.d.Activity.Engaged(ctx, workspaceID, h.HashOf(address), at)
}
