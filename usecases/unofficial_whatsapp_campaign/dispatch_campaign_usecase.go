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

// maxPendingFanOut bounds one dispatch, matching the campaign target ceiling.
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

	// The state machine is the shared one, so this channel and the Cloud API
	// campaign cannot drift on what "pause a stopped campaign" means.
	transition, err := campaign.ResolveTransition(camp.Status, action)
	if err != nil {
		return err
	}
	if !transition.FansOutWork {
		input.Entries = nil
	}

	// Starting has two gates the official channel has no reason to have, and
	// both run BEFORE the status swap so a refused start leaves no trace.
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
		// Another request won the race. Report the refusal the operator would
		// have seen had we read the winning status first.
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

	// START.
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

	// A campaign with nothing pending is already finished. Completing it here
	// beats leaving it RUNNING with an empty queue, which nothing would ever
	// close: completion is counted per message, and there are no messages.
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

// ensureCanStart asks the number, and then WhatsApp, whether a blast may begin.
//
// The second question is the important one. The cached restriction on the
// instance is refreshed by the health cron and can be hours old; starting a
// 40.000-number campaign into a live restriction is exactly how a temporary
// limit becomes a permanent ban.
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
		// A read failure is NOT permission. Fail closed, exactly as
		// Restriction.Active documents: an unknown answer must not authorise a
		// blast.
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

// revert puts the status back after a failed fan-out.
//
// Leaving a campaign RUNNING with nothing queued is a campaign that can never
// complete and never restart.
func (uc *dispatchCampaignUseCase) revert(campaignID string, transition campaign.Transition) {
	if _, err := uc.repos.campaigns.UpdateStatus(campaignID, transition.Revert, transition.Target); err != nil {
		log.Printf("[unofficial-whatsapp-campaign] could not revert status for %s: %v", campaignID, err)
	}
}
