package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"log"

	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/usecases/campaignguard"
	"vozko/usecases/campaignqueue"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

const (
	errGroupCannotPost = 910006
	errGroupSeatGone   = 910007
)

type GroupSending interface {
	Seat(ctx context.Context, instanceID, groupJID string) (*uw.Group, error)
	Conversation(ctx context.Context, instance *uw.Instance, groupJID, subject string) (*uwuc.Resolved, error)
}

func (c *messageConsumerUseCase) admitGroup(ctx context.Context, entry *uwc.Entry, instanceID string) campaignguard.Admission {
	if c.deps.Groups == nil {
		log.Printf("[unofficial-whatsapp-campaign] entry %s is held (fail-closed): group sending is not wired", entry.ID)
		return campaignguard.Admission{Result: campaignqueue.RetryLater(restrictionRequeueDelay)}
	}
	seat, err := c.deps.Groups.Seat(ctx, instanceID, entry.JID)
	switch {
	case errors.Is(err, uw.ErrGroupNotFound):
		c.failEntry(entry.ID, errGroupSeatGone, "this number is no longer in the group")
		return campaignguard.Admission{Result: campaignqueue.Drop}
	case err != nil:
		return campaignguard.Admission{Result: campaignqueue.Requeue, Err: err}
	case !seat.CanPost():
		c.failEntry(entry.ID, errGroupCannotPost, "only admins can send in this group and this number is not an admin")
		return campaignguard.Admission{Result: campaignqueue.Drop}
	}
	return campaignguard.Admission{Admitted: true}
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
