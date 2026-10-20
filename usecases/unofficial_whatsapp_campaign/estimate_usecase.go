package unofficial_whatsapp_campaign

import (
	"context"
	"time"

	"vozko/domain/campaign"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	uwg "vozko/domain/unofficial_whatsapp_group"
)

type estimateCampaignUseCase struct {
	entries   uwc.EntryRepository
	instances InstanceGateway
	senders   GroupSenderDirectory
}

func NewEstimateCampaignUseCase(entries uwc.EntryRepository, instances InstanceGateway, senders GroupSenderDirectory) uwc.EstimateCampaignUseCase {
	return &estimateCampaignUseCase{entries: entries, instances: instances, senders: senders}
}

func (uc *estimateCampaignUseCase) Draft(ctx context.Context, in uwc.EstimateInput, scope uw.DepartmentScope) (*uwc.DraftEstimate, error) {
	draft := &uwc.Campaign{
		WorkspaceID: in.WorkspaceID, Audience: in.Audience, InstanceID: in.InstanceID,
		SendDelayMinMS: in.SendDelayMinMS, SendDelayMaxMS: in.SendDelayMaxMS,
	}
	pacingUnset := draft.SendDelayMinMS <= 0 && draft.SendDelayMaxMS <= 0
	draft.Normalize()
	if !draft.Audience.Valid() {
		return nil, uwc.ErrCampaignAudienceInvalid
	}
	if draft.TargetsGroups() {
		return uc.draftGroups(ctx, draft, in.GroupJIDs, scope)
	}

	instance, err := usableInstance(ctx, uc.instances, in.WorkspaceID, scope, in.InstanceID)
	if err != nil {
		return nil, err
	}
	if pacingUnset {
		draft.SendDelayMinMS, draft.SendDelayMaxMS = instance.SendDelayRange()
	}
	estimate := estimateThrough(draft, instance, max(in.Targets, 0))
	return &uwc.DraftEstimate{Estimate: estimate, Groups: []uwc.GroupSenders{}}, nil
}

func (uc *estimateCampaignUseCase) draftGroups(ctx context.Context, draft *uwc.Campaign, jids []string, scope uw.DepartmentScope) (*uwc.DraftEstimate, error) {
	if uc.senders == nil {
		return nil, ErrGroupTargetsUnavailable
	}
	pool, err := scopedSenderPool(ctx, uc.senders, draft.WorkspaceID, scope)
	if err != nil {
		return nil, err
	}
	targets := make([]uwc.TargetInput, 0, len(jids))
	for _, jid := range jids {
		targets = append(targets, uwc.TargetInput{GroupJID: jid})
	}
	draft.Targets = targets
	draft.Normalize()
	plan, err := planGroupSenders(ctx, uc.senders, pool, draft.WorkspaceID, draft.Targets, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	candidates := make([][]string, 0, len(plan))
	for _, group := range plan {
		ids := make([]string, 0, len(group.Senders))
		for _, sender := range group.Senders {
			ids = append(ids, sender.InstanceID)
		}
		candidates = append(candidates, ids)
	}
	return &uwc.DraftEstimate{Estimate: uwc.EstimateSendTime(candidates, pool.intervals(draft)), Groups: plan}, nil
}

func (uc *estimateCampaignUseCase) Remaining(ctx context.Context, c *uwc.Campaign) (*uwc.SendEstimate, error) {
	if c.TargetsGroups() {
		return uc.remainingInGroups(ctx, c)
	}
	instance, err := uc.instances.Instance(ctx, c.InstanceID)
	if err != nil {
		return nil, err
	}
	counts, err := uc.entries.CountByStatus(c.ID)
	if err != nil {
		return nil, err
	}
	pending := 0
	if counts != nil {
		pending = int(counts.Pending)
	}
	estimate := estimateThrough(c, instance, pending)
	return &estimate, nil
}

func (uc *estimateCampaignUseCase) remainingInGroups(ctx context.Context, c *uwc.Campaign) (*uwc.SendEstimate, error) {
	if uc.senders == nil {
		return nil, ErrGroupTargetsUnavailable
	}
	pending, err := uc.entries.ListByStatus(c.ID, campaign.SendStatusPending, uwc.MaxCampaignTargets)
	if err != nil {
		return nil, err
	}
	pool, err := campaignSenderPool(ctx, uc.instances, c)
	if err != nil {
		return nil, err
	}
	jids := make([]string, 0, len(pending))
	for _, e := range pending {
		jids = append(jids, e.JID)
	}
	seats, err := uc.senders.SeatsOf(ctx, c.WorkspaceID, jids)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	candidates := make([][]string, 0, len(pending))
	for _, e := range pending {
		eligible, _ := uwg.EligibleSenders(pool.presences(seats[e.JID], now))
		ids := make([]string, 0, len(eligible))
		for _, p := range eligible {
			ids = append(ids, p.InstanceID())
		}
		candidates = append(candidates, ids)
	}
	estimate := uwc.EstimateSendTime(candidates, pool.intervals(c))
	return &estimate, nil
}

func estimateThrough(c *uwc.Campaign, instance *uw.Instance, pending int) uwc.SendEstimate {
	candidates := make([][]string, pending)
	for i := range candidates {
		candidates[i] = []string{instance.ID}
	}
	return uwc.EstimateSendTime(candidates, map[string]time.Duration{instance.ID: sendInterval(c, instance)})
}
