package unofficial_whatsapp

import (
	"context"
	"log"
	"strings"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

// CheckInstanceHealthUseCase is the BACKSTOP for session state, and the only
// source of truth for everything a webhook structurally cannot report.
//
// The provider does push session changes: `connection` is a subscribed event
// documented as "alterações no estado da conexão", so a dropped phone or a
// removed linked device arrives in seconds through the normal pipeline, not
// here. Polling for that would be a duplicate.
//
// What no event can ever tell us — and what this job exists for:
//
//   - Our webhook is no longer registered on the host. A tenant or an operator
//     with console access can unhook it, and an event announcing that could
//     only arrive through the thing that was unhooked.
//   - The host cannot reach our ingest at all. Same circularity.
//   - WhatsApp is restricting the number. There is no event for it in the
//     provider's catalogue; it is a diagnostics endpoint and a send-time error
//     code, and it is the last warning before a ban.
//   - Deliveries were attempted and failed. The host keeps a short in-memory
//     log and offers no replay, so reading it is the only forensic window
//     there is.
//
// Session state is still reconciled here, deliberately, because the backstop
// has to cover the case where the webhook path itself is broken — but at a
// relaxed cadence, and skipping any instance the webhook already spoke for.
type CheckInstanceHealthUseCase struct {
	instances uw.InstanceRepository
	servers   uw.ServerRepository
	provider  uw.ProviderAPI
	sync      sessionSync

	webhookBaseURL string
	// staleAfter is how long an instance may go without an authoritative signal
	// before the backstop re-confirms it. Signals include the `connection`
	// webhook, not only a poll: the handler stamps the same clock through the
	// shared sessionSync, so an instance that just reported is skipped here.
	staleAfter time.Duration
	// batchLimit bounds one run so a large tenant cannot starve the others.
	batchLimit int
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

// Execute re-confirms session state for instances the webhook has not spoken
// for recently. One cheap call per instance, and usually zero instances.
//
// One tenant's failure never aborts the loop: stopping at the first error would
// let one broken instance blind us to every other.
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

// VerifyIntegrity runs the probes no event can replace.
//
// Separate from Execute, and on a slower schedule, because these are three
// extra host calls per instance and none of them is urgent in the way a dropped
// session is: a webhook that fell off is discovered within the hour, and a
// WhatsApp restriction changes on the scale of hours to days. Folding them into
// the session backstop would triple its cost for no gain — the reason they were
// split once the `connection` event was accounted for.
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

// forEach resolves each instance's host and applies fn, tolerating per-instance
// failures. Shared by both sweeps so the cancellation, host caching and
// isolation rules cannot drift between them.
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

// reconcileSession asks the host what state this instance is really in.
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

// handleProbeFailure records what a failed status call means.
func (uc *CheckInstanceHealthUseCase) handleProbeFailure(ctx context.Context, instance *uw.Instance, err error) {
	provErr, ok := uw.AsProviderError(err)
	if !ok || !provErr.NeedsReconnect() {
		// A transient host failure is not evidence about the session. Recording
		// DISCONNECTED here would close every composer on the channel each time
		// the host had a bad minute.
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

// verifyWebhook confirms the host is still pointed at us.
//
// This is the probe with no event equivalent, by construction: a notification
// that our webhook had been removed could only be delivered through the webhook
// that was removed. The symptom is identical to "nobody messaged today", so it
// is checked rather than waited for.
//
// Re-registering automatically is safe — the call is an upsert — and the
// alternative is an inbox that stays quiet until someone thinks to look.
func (uc *CheckInstanceHealthUseCase) verifyWebhook(ctx context.Context, ref uw.InstanceRef, instance *uw.Instance) {
	subs, err := uc.provider.GetWebhooks(ctx, ref)
	if err != nil {
		log.Printf("[unofficial-whatsapp] instance %s: webhook read failed: %v", instance.ID, err)
		return
	}

	expected := uw.WebhookURLFor(uc.webhookBaseURL, instance.DeliveryToken)
	for _, sub := range subs {
		if sub.Enabled && sub.URL == expected {
			return
		}
	}

	log.Printf("[unofficial-whatsapp] instance %s: webhook missing or disabled on the host; re-registering",
		instance.ID)
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

// drainDeliveryErrors reads the host's in-memory delivery failures.
//
// Read-only, and it logs rather than acts, because there is nothing to act on:
// the provider offers no replay endpoint, so a delivery that failed past its
// retries is gone. Surfacing it is the difference between knowing we lost
// messages and hearing it from the customer.
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

// refreshLimits caches WhatsApp's own restriction state.
//
// The other probe with no event equivalent: the provider's catalogue has
// nothing for it, so a number sliding toward a ban is invisible unless asked
// about. It is the earliest warning available: a number under a reachout
// timelock that keeps sending is a number about to be disabled, and every send
// path reads the cached result.
func (uc *CheckInstanceHealthUseCase) refreshLimits(ctx context.Context, ref uw.InstanceRef, instance *uw.Instance) {
	restriction, err := uc.provider.MessagingLimits(ctx, ref)
	if err != nil {
		// Not every host exposes this, and a missing diagnosis must not read as
		// a restriction — that would block sending on a healthy number.
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

// serverCache avoids re-reading the same host once per instance on it.
// A workspace commonly has several numbers on one host.
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
