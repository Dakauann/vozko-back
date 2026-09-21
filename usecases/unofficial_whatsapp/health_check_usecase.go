package unofficial_whatsapp

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

type CheckInstanceHealthUseCase struct {
	instances uw.InstanceRepository
	servers   uw.ServerRepository
	provider  uw.ProviderAPI
	sync      sessionSync

	webhookBaseURL string
	staleAfter     time.Duration
	batchLimit     int
}

func NewCheckInstanceHealthUseCase(
	instances uw.InstanceRepository,
	servers uw.ServerRepository,
	provider uw.ProviderAPI,
	webhookBaseURL string,
) *CheckInstanceHealthUseCase {
	return &CheckInstanceHealthUseCase{
		instances:      instances,
		servers:        servers,
		provider:       provider,
		sync:           sessionSync{instances: instances},
		webhookBaseURL: strings.TrimRight(strings.TrimSpace(webhookBaseURL), "/"),
		staleAfter:     15 * time.Minute,
		batchLimit:     200,
	}
}

func (uc *CheckInstanceHealthUseCase) Execute(ctx context.Context) error {
	cutoff := time.Now().UTC().Add(-uc.staleAfter)
	instances, err := uc.instances.ListForHealthCheck(ctx, cutoff, uc.batchLimit)
	if err != nil {
		return err
	}

	return uc.forEach(ctx, instances, func(ctx context.Context, server *uw.Server, instance *uw.Instance) {
		uc.reconcileSession(ctx, server, instance)
	})
}

func (uc *CheckInstanceHealthUseCase) VerifyIntegrity(ctx context.Context) error {
	instances, err := uc.instances.ListConnected(ctx, uc.batchLimit)
	if err != nil {
		return err
	}

	return uc.forEach(ctx, instances, func(ctx context.Context, server *uw.Server, instance *uw.Instance) {
		ref := uw.RefFor(server, instance)
		uc.verifyWebhook(ctx, ref, instance)
		uc.drainDeliveryErrors(ctx, ref, instance)
		uc.refreshLimits(ctx, ref, instance)
	})
}

func (uc *CheckInstanceHealthUseCase) forEach(
	ctx context.Context,
	instances []*uw.Instance,
	fn func(context.Context, *uw.Server, *uw.Instance),
) error {
	servers := newServerCache(uc.servers)
	for _, instance := range instances {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		server, err := servers.get(ctx, instance.ServerID)
		if err != nil {
			log.Printf("[unofficial-whatsapp] instance %s: server %s unavailable: %v",
				instance.ID, instance.ServerID, err)
			continue
		}
		fn(ctx, server, instance)
	}
	return nil
}

func (uc *CheckInstanceHealthUseCase) reconcileSession(ctx context.Context, server *uw.Server, instance *uw.Instance) {
	session, err := uc.provider.Status(ctx, uw.RefFor(server, instance))
	if err != nil {
		uc.handleProbeFailure(ctx, instance, err)
		return
	}
	if _, err := uc.sync.apply(ctx, instance, session); err != nil {
		log.Printf("[unofficial-whatsapp] instance %s: session sync failed: %v", instance.ID, err)
	}
}

func (uc *CheckInstanceHealthUseCase) handleProbeFailure(ctx context.Context, instance *uw.Instance, err error) {
	provErr, ok := uw.AsProviderError(err)
	if !ok || !provErr.NeedsReconnect() {
		log.Printf("[unofficial-whatsapp] instance %s: status probe failed: %v", instance.ID, err)
		return
	}
	if !instance.Status.CanTransitionTo(uw.StatusDisconnected) {
		return
	}
	if updateErr := uc.instances.UpdateStatus(ctx, instance.ID, uw.StatusDisconnected,
		"the host no longer recognises this instance"); updateErr != nil {
		log.Printf("[unofficial-whatsapp] instance %s: status update failed: %v", instance.ID, updateErr)
	}
}

func (uc *CheckInstanceHealthUseCase) verifyWebhook(ctx context.Context, ref uw.InstanceRef, instance *uw.Instance) {
	subs, err := uc.provider.GetWebhooks(ctx, ref)
	if err != nil {
		log.Printf("[unofficial-whatsapp] instance %s: webhook read failed: %v", instance.ID, err)
		return
	}

	expected := uw.WebhookURLFor(uc.webhookBaseURL, instance.DeliveryToken)
	reason := "missing or disabled"
	for _, sub := range subs {
		if !sub.Enabled || sub.URL != expected {
			continue
		}
		if missing := missingEvents(sub.Events, uw.SubscribedEvents()); len(missing) > 0 {
			reason = fmt.Sprintf("not subscribed to %v", missing)
			break
		}
		return
	}

	log.Printf("[unofficial-whatsapp] instance %s: webhook %s on the host; re-registering",
		instance.ID, reason)
	err = uc.provider.SetWebhook(ctx, ref, uw.WebhookSubscription{
		URL:             expected,
		Enabled:         true,
		Events:          uw.SubscribedEvents(),
		ExcludeMessages: []string{},
	})
	if err != nil {
		log.Printf("[unofficial-whatsapp] instance %s: webhook re-registration failed: %v", instance.ID, err)
		return
	}
	if err := uc.instances.SetWebhookRegistered(ctx, instance.ID, time.Now().UTC()); err != nil {
		log.Printf("[unofficial-whatsapp] instance %s: webhook stamp failed: %v", instance.ID, err)
	}
}

func (uc *CheckInstanceHealthUseCase) drainDeliveryErrors(ctx context.Context, ref uw.InstanceRef, instance *uw.Instance) {
	failures, err := uc.provider.WebhookErrors(ctx, ref)
	if err != nil || len(failures) == 0 {
		return
	}
	for _, failure := range failures {
		log.Printf("[unofficial-whatsapp] instance %s: DELIVERY LOST event=%s status=%d attempts=%d at=%s: %s",
			instance.ID, failure.Event, failure.StatusCode, failure.Attempts,
			failure.At.Format(time.RFC3339), failure.Error)
	}
}

func (uc *CheckInstanceHealthUseCase) refreshLimits(ctx context.Context, ref uw.InstanceRef, instance *uw.Instance) {
	restriction, err := uc.provider.MessagingLimits(ctx, ref)
	if err != nil {
		return
	}
	if err := uc.instances.UpdateRestriction(ctx, instance.ID, *restriction); err != nil {
		log.Printf("[unofficial-whatsapp] instance %s: restriction update failed: %v", instance.ID, err)
		return
	}
	instance.Restriction = *restriction

	if restriction.Active(time.Now().UTC()) {
		log.Printf("[unofficial-whatsapp] instance %s (%s): WHATSAPP RESTRICTION ACTIVE key=%s until=%v quota=%d/%d",
			instance.ID, instance.Label(), restriction.Key, restriction.Until,
			restriction.UsedQuota, restriction.TotalQuota)
	}
}

type serverCache struct {
	repo   uw.ServerRepository
	loaded map[string]*uw.Server
}

func newServerCache(repo uw.ServerRepository) *serverCache {
	return &serverCache{repo: repo, loaded: make(map[string]*uw.Server)}
}

func (c *serverCache) get(ctx context.Context, id string) (*uw.Server, error) {
	if server, ok := c.loaded[id]; ok {
		return server, nil
	}
	server, err := c.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	c.loaded[id] = server
	return server, nil
}

func missingEvents(have, want []string) []string {
	subscribed := make(map[string]struct{}, len(have))
	for _, e := range have {
		subscribed[strings.ToLower(strings.TrimSpace(e))] = struct{}{}
	}

	var missing []string
	for _, e := range want {
		if _, ok := subscribed[strings.ToLower(strings.TrimSpace(e))]; !ok {
			missing = append(missing, e)
		}
	}
	return missing
}
