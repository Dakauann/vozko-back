package unofficial_whatsapp_campaign

import (
	"context"
	"log"

	"vozko/domain/campaign"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

// pauseCampaignsForInstanceUseCase is the circuit breaker's actuator.
//
// It pauses EVERY running campaign on a number, because the thing that went
// wrong — a WhatsApp restriction, a dead session, a ban warning — belongs to the
// number and not to the campaign that happened to discover it. Pausing only the
// noticing campaign would leave the other two blasting into the same limit.
//
// Resume is deliberately manual. An automatic resume would re-enter the exact
// condition that triggered the breaker, and on this channel the cost of being
// wrong is the customer losing their WhatsApp number.
type pauseCampaignsForInstanceUseCase struct {
	campaigns uwc.Repository
	consumer  uwc.MessageConsumerUseCase
}

func NewPauseCampaignsForInstanceUseCase(
	campaigns uwc.Repository,
	consumer uwc.MessageConsumerUseCase,
) uwc.PauseCampaignsForInstanceUseCase {
	return &pauseCampaignsForInstanceUseCase{campaigns: campaigns, consumer: consumer}
}

func (uc *pauseCampaignsForInstanceUseCase) Execute(
	ctx context.Context,
	instanceID, reason string,
) (int, error) {
	running, err := uc.campaigns.ListRunningByInstance(instanceID)
	if err != nil {
		return 0, err
	}

	paused := 0
	for _, camp := range running {
		if camp == nil {
			continue
		}
		swapped, err := uc.campaigns.UpdateStatus(camp.ID, campaign.StatusPaused, campaign.StatusRunning)
		if err != nil {
			log.Printf("[unofficial-whatsapp-campaign] could not pause %s: %v", camp.ID, err)
			continue
		}
		if !swapped {
			// Somebody else already moved it. Not an error: the goal was that it
			// stop running, and it has.
			continue
		}
		// The reason is what makes an automatic pause legible. Without it a
		// campaign the system stopped looks identical to one somebody paused by
		// hand, and an operator restarts it straight back into the restriction.
		if err := uc.campaigns.UpdateStatusReason(camp.ID, reason); err != nil {
			log.Printf("[unofficial-whatsapp-campaign] could not record why %s paused: %v", camp.ID, err)
		}
		if uc.consumer != nil {
			if err := uc.consumer.PauseCampaignConsumer(camp.ID); err != nil {
				log.Printf("[unofficial-whatsapp-campaign] could not detach the consumer for %s: %v", camp.ID, err)
			}
		}
		paused++
	}

	if paused > 0 {
		log.Printf("[unofficial-whatsapp-campaign] paused %d campaign(s) on number %s: %s",
			paused, instanceID, reason)
	}
	return paused, nil
}
