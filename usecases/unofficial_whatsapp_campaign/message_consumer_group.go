package unofficial_whatsapp_campaign

import (
	"context"
	"log"
	"time"

	"vozko/domain/campaign"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	uwg "vozko/domain/unofficial_whatsapp_group"
	"vozko/usecases/campaignqueue"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

const (
	errGroupCannotPost = 910006
	errGroupSeatGone   = 910007
)

type GroupSending interface {
	Seats(ctx context.Context, workspaceID, groupJID string) ([]*uw.Group, error)
	Conversation(ctx context.Context, instance *uw.Instance, groupJID, subject string) (*uwuc.Resolved, error)
}

func (c *messageConsumerUseCase) precheckGroups(ctx context.Context, camp *uwc.Campaign) error {
	pool, err := campaignSenderPool(ctx, c.deps.Instances, camp)
	if err != nil {
		return err
	}
	if len(pool) > 0 {
		return nil
	}
	_, _ = c.deps.Campaigns.UpdateStatus(camp.ID, campaign.StatusStopped, campaign.StatusRunning)
	_ = c.deps.Campaigns.UpdateStatusReason(camp.ID, uwc.ErrGroupCampaignNoNumber.Error())
	return uwc.ErrGroupCampaignNoNumber
}

func (c *messageConsumerUseCase) handleGroup(ctx context.Context, camp *uwc.Campaign, entry *uwc.Entry) campaignqueue.Result {
	if c.deps.Groups == nil {
		log.Printf("[unofficial-whatsapp-campaign] entry %s is held (fail-closed): group sending is not wired", entry.ID)
		return campaignqueue.RetryLater(restrictionRequeueDelay)
	}
	pool, err := campaignSenderPool(ctx, c.deps.Instances, camp)
	if err != nil {
		return campaignqueue.Requeue
	}
	seats, err := c.deps.Groups.Seats(ctx, camp.WorkspaceID, entry.JID)
	if err != nil {
		return campaignqueue.Requeue
	}
	ranked, block := uwg.ChooseSender(pool.presences(seats, time.Now().UTC()), c.deps.Budget.UsedToday)
	switch block {
	case uwg.SenderNotInGroup:
		c.failEntry(entry.ID, errGroupSeatGone, "none of the numbers this campaign may use is in this group")
		return campaignqueue.Drop
	case uwg.SenderCannotPost:
		c.failEntry(entry.ID, errGroupCannotPost, "only admins can send in this group and none of the numbers this campaign may use is an admin there")
		return campaignqueue.Drop
	case uwg.SenderOffline:
		return campaignqueue.RetryLater(restrictionRequeueDelay)
	}

	wait := time.Duration(0)
	for _, id := range ranked {
		result, attempted := c.sendFromGroupNumber(ctx, camp, entry, pool[id])
		if attempted {
			return result
		}
		if wait == 0 || result.Delay < wait {
			wait = result.Delay
		}
	}
	return campaignqueue.RetryLater(wait)
}

func (c *messageConsumerUseCase) sendFromGroupNumber(ctx context.Context, camp *uwc.Campaign, entry *uwc.Entry, instance *uw.Instance) (campaignqueue.Result, bool) {
	effectiveCap := camp.EffectiveDailyCap(instance.EffectiveDailyCap(time.Now().UTC()))
	budgeted, err := c.deps.Budget.TryConsumeDaily(instance.ID, effectiveCap)
	if err != nil || !budgeted {
		return campaignqueue.RetryLater(capRequeueDelay), false
	}
	minMS, maxMS := PacingFor(camp, instance)
	if acquired, wait := c.deps.Budget.AcquirePace(instance.ID, minMS, maxMS); !acquired {
		c.deps.Budget.ReleaseDaily(instance.ID, effectiveCap)
		return campaignqueue.RetryLater(wait), false
	}
	result, sent := c.deliver(ctx, camp, entry, instance, entry.JID)
	if !sent {
		c.deps.Budget.ReleaseDaily(instance.ID, effectiveCap)
	}
	return result, true
}

func (c *messageConsumerUseCase) openConversation(
	ctx context.Context,
	instance *uw.Instance,
	camp *uwc.Campaign,
	entry *uwc.Entry,
	jid string,
) (*uwuc.Resolved, error) {
	if entry.TargetsGroup() {
		return c.deps.Groups.Conversation(ctx, instance, entry.JID, entry.Name)
	}
	return c.deps.Instances.Resolve(ctx, instance, uwuc.ResolveInput{
		JID:         jid,
		PhoneNumber: entry.Number,
		Name:        entry.Name,
		CampaignID:  camp.ID,
	})
}
