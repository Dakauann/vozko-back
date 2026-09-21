package unofficial_whatsapp_campaign

import (
	"context"
	"fmt"
	"log"
	"time"

	"vozko/domain/cache"
	"vozko/domain/campaign"
	"vozko/domain/messaging"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/usecases/campaignqueue"
)

const maxPendingFanOut = uwc.MaxCampaignTargets

type dispatchCampaignUseCase struct {
	repos      campaignRepos
	instances  InstanceGateway
	consumer   uwc.MessageConsumerUseCase
	dispatcher *campaignqueue.Dispatcher
}

func NewDispatchCampaignUseCase(
	campaigns uwc.Repository,
	entries uwc.EntryRepository,
	instances InstanceGateway,
	consumer uwc.MessageConsumerUseCase,
	pub messaging.MessageQueuePub,
	sharedState cache.SharedState,
) uwc.DispatchCampaignUseCase {
	return &dispatchCampaignUseCase{
		repos:      campaignRepos{campaigns: campaigns, entries: entries},
		instances:  instances,
		consumer:   consumer,
		dispatcher: campaignqueue.NewDispatcher(pub, sharedState, uwc.QueueNamespace),
	}
}

func (uc *dispatchCampaignUseCase) Dispatch(ctx context.Context, input uwc.DispatchCampaignInput) error {
	action := input.Action
	if action == "" {
		action = campaign.ActionStart
	}
	if input.CampaignID == "" {
		return uwc.ErrCampaignNotFound
	}

	camp, err := uc.repos.campaigns.FindByID(input.CampaignID)
	if err != nil {
		return err
	}

	transition, err := campaign.ResolveTransition(camp.Status, action)
	if err != nil {
		return err
	}
	if !transition.FansOutWork {
		input.Entries = nil
	}

	if action == campaign.ActionStart {
		if err := uc.ensureCanStart(ctx, camp); err != nil {
			return err
		}
	}

	swapped, err := uc.repos.campaigns.UpdateStatus(input.CampaignID, transition.Target, camp.Status)
	if err != nil {
		return err
	}
	if !swapped {
		return campaign.SwapFailure(action)
	}

	switch action {
	case campaign.ActionPause:
		if err := uc.consumer.PauseCampaignConsumer(input.CampaignID); err != nil {
			log.Printf("[unofficial-whatsapp-campaign] could not pause the consumer: %v", err)
		}
		return nil

	case campaign.ActionStop:
		if err := uc.consumer.StopCampaignConsumer(input.CampaignID); err != nil {
			log.Printf("[unofficial-whatsapp-campaign] could not stop the consumer: %v", err)
		}
		return nil
	}

	if camp.Status == campaign.StatusPaused && uc.consumer.IsSubscribed(input.CampaignID) {
		return uc.consumer.ResumeCampaignConsumer(input.CampaignID)
	}

	if err := uc.consumer.SubscribeToCampaign(input.CampaignID); err != nil {
		uc.revert(input.CampaignID, transition)
		return fmt.Errorf("failed to subscribe to campaign: %w", err)
	}

	entries := input.Entries
	if len(entries) == 0 {
		pending, err := uc.repos.entries.ListByStatus(
			input.CampaignID, campaign.SendStatusPending, maxPendingFanOut)
		if err != nil {
			uc.revert(input.CampaignID, transition)
			return fmt.Errorf("failed to list pending entries: %w", err)
		}
		for _, e := range pending {
			entries = append(entries, uwc.DispatchEntry{EntryID: e.ID, PhoneNumber: e.Number})
		}
	}

	if len(entries) == 0 {
		_, _ = uc.repos.campaigns.UpdateStatus(
			input.CampaignID, campaign.StatusCompleted, campaign.StatusRunning)
		return nil
	}

	messages := make([]campaignqueue.Message, 0, len(entries))
	for _, e := range entries {
		messages = append(messages, campaignqueue.Message{
			CampaignID:  input.CampaignID,
			EntryID:     e.EntryID,
			PhoneNumber: e.PhoneNumber,
		})
	}

	if err := uc.dispatcher.Enqueue(input.CampaignID, messages); err != nil {
		uc.revert(input.CampaignID, transition)
		return err
	}
	return nil
}

func (uc *dispatchCampaignUseCase) ensureCanStart(ctx context.Context, camp *uwc.Campaign) error {
	instance, err := uc.instances.Instance(ctx, camp.InstanceID)
	if err != nil {
		return err
	}
	if err := ensureInstanceCanCampaign(instance); err != nil {
		return err
	}
	if _, err := instance.CanSend(time.Now().UTC()); err != nil {
		return err
	}

	ref, err := uc.instances.Ref(ctx, instance)
	if err != nil {
		return err
	}
	restriction, err := uc.instances.MessagingLimits(ctx, ref)
	if err != nil {
		return fmt.Errorf("%w: could not confirm this number may start new conversations",
			uw.ErrRestrictedByWA)
	}
	if restriction != nil {
		_ = uc.instances.CacheRestriction(ctx, instance.ID, *restriction)
		if restriction.Active(time.Now().UTC()) {
			return uw.ErrRestrictedByWA
		}
	}
	return nil
}

func (uc *dispatchCampaignUseCase) revert(campaignID string, transition campaign.Transition) {
	if _, err := uc.repos.campaigns.UpdateStatus(campaignID, transition.Revert, transition.Target); err != nil {
		log.Printf("[unofficial-whatsapp-campaign] could not revert status for %s: %v", campaignID, err)
	}
}
