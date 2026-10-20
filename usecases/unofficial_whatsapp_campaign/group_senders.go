package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"

	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	uwg "vozko/domain/unofficial_whatsapp_group"
)

var ErrGroupTargetsUnavailable = errors.New("the group directory is unavailable; try again shortly")

type GroupSenderDirectory interface {
	SeatsOf(ctx context.Context, workspaceID string, groupJIDs []string) (map[string][]*uw.Group, error)
	WorkspaceInstances(ctx context.Context, workspaceID string) ([]*uw.Instance, error)
}

func (uc *createCampaignUseCase) SetGroupSenders(senders GroupSenderDirectory) {
	uc.groupSenders = senders
}

type senderPool map[string]*uw.Instance

func scopedSenderPool(ctx context.Context, dir GroupSenderDirectory, workspaceID string, scope uw.DepartmentScope) (senderPool, error) {
	all, err := dir.WorkspaceInstances(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	pool := senderPool{}
	for _, instance := range all {
		if instance == nil || instance.WorkspaceID != workspaceID || !scope.AllowsInstance(instance) {
			continue
		}
		if ensureInstanceCanCampaign(instance) != nil {
			continue
		}
		pool[instance.ID] = instance
	}
	return pool, nil
}

func campaignSenderPool(ctx context.Context, instances InstanceLookup, camp *uwc.Campaign) (senderPool, error) {
	pool := senderPool{}
	for _, id := range camp.SenderPool() {
		instance, err := instances.Instance(ctx, id)
		if errors.Is(err, uw.ErrInstanceNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if instance == nil || instance.WorkspaceID != camp.WorkspaceID || ensureInstanceCanCampaign(instance) != nil {
			continue
		}
		pool[instance.ID] = instance
	}
	return pool, nil
}

func (p senderPool) ids() []string {
	out := make([]string, 0, len(p))
	for id := range p {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func (p senderPool) presences(seats []*uw.Group, now time.Time) []uwg.Presence {
	out := make([]uwg.Presence, 0, len(seats))
	for _, seat := range seats {
		if seat == nil {
			continue
		}
		instance, ok := p[seat.InstanceID]
		if !ok {
			continue
		}
		_, err := instance.CanSend(now)
		out = append(out, uwg.Presence{Seat: seat, Connected: err == nil})
	}
	return out
}

func (p senderPool) intervals(camp *uwc.Campaign) map[string]time.Duration {
	out := make(map[string]time.Duration, len(p))
	for id, instance := range p {
		out[id] = sendInterval(camp, instance)
	}
	return out
}

func planGroupSenders(ctx context.Context, dir GroupSenderDirectory, pool senderPool, workspaceID string, targets []uwc.TargetInput, now time.Time) ([]uwc.GroupSenders, error) {
	jids := make([]string, 0, len(targets))
	for _, t := range targets {
		jids = append(jids, t.GroupJID)
	}
	seats, err := dir.SeatsOf(ctx, workspaceID, jids)
	if err != nil {
		return nil, err
	}
	plan := make([]uwc.GroupSenders, 0, len(targets))
	for _, t := range targets {
		eligible, block := uwg.EligibleSenders(pool.presences(seats[t.GroupJID], now))
		group := uwc.GroupSenders{JID: t.GroupJID, Name: firstNonEmpty(subjectOf(seats[t.GroupJID]), t.Name), Senders: []uwc.SenderNumber{}, Block: block}
		for _, p := range eligible {
			group.Senders = append(group.Senders, uwc.SenderNumber{InstanceID: p.InstanceID(), Label: pool[p.InstanceID()].Label()})
		}
		plan = append(plan, group)
	}
	return plan, nil
}

func subjectOf(seats []*uw.Group) string {
	for _, seat := range seats {
		if seat != nil && seat.Subject != "" {
			return seat.Subject
		}
	}
	return ""
}

func (uc *createCampaignUseCase) groupEntries(ctx context.Context, in *uwc.Campaign, scope uw.DepartmentScope) ([]uwc.Entry, error) {
	if uc.groupSenders == nil {
		return nil, ErrGroupTargetsUnavailable
	}
	pool, err := scopedSenderPool(ctx, uc.groupSenders, in.WorkspaceID, scope)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	plan, err := planGroupSenders(ctx, uc.groupSenders, pool, in.WorkspaceID, in.Targets, now)
	if err != nil {
		return nil, err
	}
	entries := make([]uwc.Entry, 0, len(plan))
	for _, group := range plan {
		if err := group.Refusal(); err != nil {
			return nil, err
		}
		entry := uwc.NewGroupEntry(in.ID, in.WorkspaceID, group.JID, group.Name, now)
		entry.ID = uuid.New().String()
		entry.Normalize()
		entries = append(entries, entry)
	}
	in.SenderInstanceIDs = pool.ids()
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
