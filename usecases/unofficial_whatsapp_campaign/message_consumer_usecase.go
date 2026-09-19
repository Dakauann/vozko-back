package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"vozko/domain/cache"
	"vozko/domain/campaign"
	"vozko/domain/conversation"
	"vozko/domain/messaging"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/usecases/campaignqueue"
	conversation_usecase "vozko/usecases/conversation"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

// Requeue delays. All three are "come back later", not failures, and each has a
// different natural interval.
const (
	// pauseRequeueDelay matches the official channel's.
	pauseRequeueDelay = 3 * time.Second
	// restrictionRequeueDelay is long: WhatsApp restrictions are measured in
	// hours, and hammering one is how a temporary limit becomes a ban.
	restrictionRequeueDelay = 5 * time.Minute
	// capRequeueDelay backs off when the number is out of daily budget. Short
	// enough to pick the campaign up soon after midnight UTC without a cron.
	capRequeueDelay = 15 * time.Minute
)

// Internal error codes for failures that never reached the provider, so an
// operator can tell "we could not send this" from "WhatsApp refused it".
const (
	errNoInstance          = 910001
	errInstanceUnusable    = 910002
	errResolveConversation = 910003
	errSendFailed          = 910004
)

// ConsumerDeps is everything the send step needs.
//
// A struct rather than eighteen positional parameters: the official channel's
// equivalent constructor takes sixteen, and adding one there means touching
// every test that builds it.
type ConsumerDeps struct {
	QueueSub  messaging.MessageQueueSub
	QueuePub  messaging.MessageQueuePub
	Shared    cache.SharedState
	Campaigns uwc.Repository
	Entries   uwc.EntryRepository
	Instances InstanceGateway
	Sender    CampaignSender
	Budget    *SendBudget
	Spam      SpamGuard

	// All optional. A nil one degrades that single behaviour rather than
	// stopping the channel, which is the pattern the rest of the codebase uses
	// for cross-cutting concerns.
	Assignments Assigner
	Metrics     MetricRecorder
	Workflows   WorkflowTrigger
	Broadcaster EntryBroadcaster
	PauseAll    uwc.PauseCampaignsForInstanceUseCase
}

type messageConsumerUseCase struct {
	deps   ConsumerDeps
	runner *campaignqueue.Runner
}

func NewMessageConsumerUseCase(deps ConsumerDeps) uwc.MessageConsumerUseCase {
	uc := &messageConsumerUseCase{deps: deps}
	uc.attachRunner()
	return uc
}

// attachRunner builds the shared queue runner.
//
// Separate from the constructor so a test harness that assembles the struct
// field by field gets the SAME configuration as production rather than a second
// copy that can drift from it.
func (c *messageConsumerUseCase) attachRunner() {
	c.runner = campaignqueue.New(
		c.deps.QueueSub, c.deps.QueuePub, c.deps.Shared,
		campaignStatusStore{repo: c.deps.Campaigns},
		pendingEntryCounter{repo: c.deps.Entries},
		campaignqueue.Config{
			Namespace:         uwc.QueueNamespace,
			PauseRequeueDelay: pauseRequeueDelay,
			Precheck:          c.precheck,
			// No Pace here, deliberately.
			//
			// The official channel spaces sends with a fixed sleep because Meta
			// is the throttle. Here the throttle is a per-INSTANCE lease taken
			// inside handle(), because several campaigns can target one number
			// and several replicas can be running: a per-consumer sleep would let
			// each of them send at the full rate, N times too fast against a
			// number whose owner loses their WhatsApp if we are wrong.
			Pace: nil,
			Logf: func(format string, args ...any) {
				log.Printf("[unofficial-whatsapp-campaign] "+format, args...)
			},
		},
		c.handle,
	)
}

func (c *messageConsumerUseCase) Start() error {
	if c.deps.QueueSub == nil {
		return fmt.Errorf("unofficial whatsapp campaign consumer: message queue subscriber is required")
	}
	return c.runner.Start()
}

func (c *messageConsumerUseCase) SubscribeToCampaign(id string) error {
	return c.runner.SubscribeToCampaign(id)
}
func (c *messageConsumerUseCase) PauseCampaignConsumer(id string) error {
	return c.runner.PauseCampaignConsumer(id)
}
func (c *messageConsumerUseCase) ResumeCampaignConsumer(id string) error {
	return c.runner.ResumeCampaignConsumer(id)
}
func (c *messageConsumerUseCase) StopCampaignConsumer(id string) error {
	return c.runner.StopCampaignConsumer(id)
}
func (c *messageConsumerUseCase) IsSubscribed(id string) bool { return c.runner.IsSubscribed(id) }

// SetPauseAll attaches the circuit breaker after construction.
//
// The two are mutually dependent — the breaker detaches consumers, the consumer
// calls the breaker — so one of them has to be wired second. Doing it here
// rather than merging them keeps each one's job intact.
func (c *messageConsumerUseCase) SetPauseAll(p uwc.PauseCampaignsForInstanceUseCase) {
	c.deps.PauseAll = p
}

// SetWorkflows attaches the workflow trigger after construction, for the same
// reason the official consumer's evaluator is attached late: the workflow stack
// is built later in the wiring pass.
func (c *messageConsumerUseCase) SetWorkflows(w WorkflowTrigger) {
	c.deps.Workflows = w
}

// precheck refuses to subscribe a campaign whose number cannot send.
//
// The campaign is left in whatever status it holds and the error is reported;
// the dispatch use case is what refuses the START, so this is the second gate
// for the restart-on-boot path, where nobody pressed anything.
func (c *messageConsumerUseCase) precheck(campaignID string) error {
	ctx := context.Background()
	camp, err := c.deps.Campaigns.FindByID(campaignID)
	if err != nil || camp == nil {
		return fmt.Errorf("unofficial whatsapp campaign consumer: campaign %s not found: %w", campaignID, err)
	}
	instance, err := c.deps.Instances.Instance(ctx, camp.InstanceID)
	if err != nil || instance == nil {
		return fmt.Errorf("unofficial whatsapp campaign consumer: number for campaign %s not found: %w", campaignID, err)
	}
	if err := ensureInstanceCanCampaign(instance); err != nil {
		_, _ = c.deps.Campaigns.UpdateStatus(campaignID, campaign.StatusStopped, campaign.StatusRunning)
		_ = c.deps.Campaigns.UpdateStatusReason(campaignID, err.Error())
		return err
	}
	return nil
}

// handle is this channel's send step.
//
// The numbered sequence is the ban-safety design, in order, and the order
// matters: every gate that can refuse cheaply runs before the one that costs a
// provider call, and the pacing lease is taken last so a refused send never
// spends it.
//
// There is no balance step anywhere in here. Meta charges nothing for a
// linked-device send, so charging the workspace would invent a fee. If pricing
// is ever added, it slots in at step 4 — after the cheap refusals and before the
// budget is spent — with the refund beside the send failure at step 9.
func (c *messageConsumerUseCase) handle(msg campaignqueue.Message) campaignqueue.Result {
	ctx := context.Background()

	camp, err := c.deps.Campaigns.FindByID(msg.CampaignID)
	if err != nil || camp == nil {
		log.Printf("[unofficial-whatsapp-campaign] campaign %s not found: %v", msg.CampaignID, err)
		return campaignqueue.Requeue
	}

	entry, err := c.deps.Entries.FindByID(msg.EntryID)
	if err != nil || entry == nil {
		// An entry that has been deleted is not retryable, and requeuing would
		// spin forever. Dropping counts it, so a campaign whose entries were
		// pruned still completes.
		log.Printf("[unofficial-whatsapp-campaign] entry %s not found: %v", msg.EntryID, err)
		return campaignqueue.Drop
	}
	// A redelivered message for an entry that already left is a no-op, not a
	// second send. This is what makes the queue's at-least-once delivery safe.
	if entry.Status != campaign.SendStatusPending {
		return campaignqueue.Drop
	}

	instance, err := c.deps.Instances.Instance(ctx, camp.InstanceID)
	if err != nil || instance == nil {
		c.failEntry(entry.ID, errNoInstance, "the number for this campaign is no longer connected")
		return campaignqueue.Drop
	}

	// 1. Session and restriction — the SAME authority the composer consults, so
	//    the UI and the blast can never disagree about whether this number may
	//    send. A refusal pauses every campaign on the number, not just this one:
	//    the restriction belongs to the number.
	if _, err := instance.CanSend(time.Now().UTC()); err != nil {
		c.pauseInstanceCampaigns(ctx, instance.ID, err)
		return campaignqueue.RetryLater(restrictionRequeueDelay)
	}

	// 2. Daily cap — campaign ceiling AND the number's warmup-adjusted one.
	//    Not a failure: the work is deferred to the next window and the entry
	//    stays pending, because nothing was attempted.
	effectiveCap := camp.EffectiveDailyCap(instance.EffectiveDailyCap(time.Now().UTC()))
	budgeted, budgetErr := c.deps.Budget.TryConsumeDaily(instance.ID, effectiveCap)
	if budgetErr != nil || !budgeted {
		return campaignqueue.RetryLater(capRequeueDelay)
	}
	// Anything below that does not send has to give the reservation back, or a
	// dead list burns the number's whole allowance on nobody.
	spent := false
	defer func() {
		if !spent {
			c.deps.Budget.ReleaseDaily(instance.ID, effectiveCap)
		}
	}()

	// 3. The workspace's own cooldown. The same guard cold outbound and the
	//    official campaign consult, so one setting governs every way of reaching
	//    somebody.
	if c.deps.Spam != nil && c.deps.Spam.ShouldSkip(ctx, camp.WorkspaceID, entry.LeadID, instance.ID) {
		c.setEntryStatus(entry.ID, campaign.SendStatusNotEligiblePossibleSpam)
		return campaignqueue.Drop
	}

	// 4. Is this number even on WhatsApp? Cached on the entry, so a resumed
	//    campaign does not re-verify a list it verified this morning. Sending to
	//    unregistered numbers is the loudest spam signal a linked device emits.
	ref, err := c.deps.Instances.Ref(ctx, instance)
	if err != nil {
		return campaignqueue.Requeue
	}
	jid, ok, err := c.resolveJID(ctx, ref, entry)
	if err != nil {
		return campaignqueue.Requeue
	}
	if !ok {
		c.setEntryStatus(entry.ID, campaign.SendStatusSkippedNotOnWhatsApp)
		return campaignqueue.Drop
	}

	// 5. Pacing. Taken LAST among the gates, so a message that was going to be
	//    skipped never consumes a slot and never slows the campaign down for
	//    nothing.
	minMS, maxMS := PacingFor(camp, instance)
	if acquired, wait := c.deps.Budget.AcquirePace(instance.ID, minMS, maxMS); !acquired {
		return campaignqueue.RetryLater(wait)
	}

	// 6. Contact and conversation, through the SAME resolution the inbound path
	//    uses. A campaign that created contacts its own way would duplicate every
	//    one of them the moment its recipients replied.
	resolved, err := c.deps.Instances.Resolve(ctx, instance, uwuc.ResolveInput{
		JID:         jid,
		PhoneNumber: entry.Number,
		Name:        entry.Name,
	})
	if err != nil {
		c.failEntry(entry.ID, errResolveConversation, "could not open a conversation with this number")
		return campaignqueue.Drop
	}

	// 7. Render OUR variables, then hand over. SanitizeOutboundText runs inside
	//    the channel adapter, which is the single place that knows the provider
	//    substitutes {{...}} from its own lead store.
	variant := camp.Message.VariantFor(entry.ID)
	body := camp.Message.Render(variant, entry.Variables)

	// 8. Send through the channel adapter, never the provider directly — so this
	//    inherits pacing, restriction caching, media limits and the transcript.
	message, err := c.deps.Sender.SendCampaignMessage(conversation_usecase.SendCampaignMessageInput{
		EntryID:   resolved.Conversation.ID,
		EntryType: string(shared.EntryTypeUnofficialWhatsApp),
		Text:      body,
		MediaID:   camp.Message.MediaID,
		MediaType: string(camp.Message.Kind.MediaKind()),
		// The media id names a row in the campaign's own workspace library, and
		// the send authorises it against this.
		WorkspaceID: camp.WorkspaceID,
		FileName:    camp.Message.FileName,
		Options:     interactiveOptions(camp.Message),
		Style:       camp.Message.Style,
		Footer:      camp.Message.Footer,
	})
	if err != nil {
		return c.handleSendFailure(ctx, instance, camp, entry, err)
	}
	spent = true

	// 9. Everything a delivered campaign message owes its conversation.
	c.recordSuccess(ctx, camp, instance, entry, resolved, message, variant)
	return campaignqueue.Done
}

// handleSendFailure decides whether a provider refusal is this entry's problem
// or the whole number's.
func (c *messageConsumerUseCase) handleSendFailure(
	ctx context.Context,
	instance *uw.Instance,
	camp *uwc.Campaign,
	entry *uwc.Entry,
	err error,
) campaignqueue.Result {
	if provErr, ok := uw.AsProviderError(err); ok {
		switch {
		case provErr.IsRestriction():
			// WhatsApp itself refused. The entry stays PENDING — the message was
			// never sent, and marking it failed would make a resumed campaign
			// skip someone who was never contacted.
			c.pauseInstanceCampaigns(ctx, instance.ID, err)
			return campaignqueue.RetryLater(restrictionRequeueDelay)
		case provErr.NeedsReconnect():
			c.pauseInstanceCampaigns(ctx, instance.ID, err)
			return campaignqueue.RetryLater(restrictionRequeueDelay)
		case provErr.Retryable():
			return campaignqueue.Requeue
		}
	}
	if errors.Is(err, uw.ErrRestrictedByWA) || errors.Is(err, uw.ErrInstanceNotConnected) {
		c.pauseInstanceCampaigns(ctx, instance.ID, err)
		return campaignqueue.RetryLater(restrictionRequeueDelay)
	}

	message := err.Error()
	if len(message) > 500 {
		message = message[:500]
	}
	c.failEntry(entry.ID, errSendFailed, message)
	return campaignqueue.Drop
}

// recordSuccess is the four side effects a delivered campaign message owes.
//
// All best-effort: the customer already has the message, so a failing side
// effect is logged and never turned into a resend.
func (c *messageConsumerUseCase) recordSuccess(
	ctx context.Context,
	camp *uwc.Campaign,
	instance *uw.Instance,
	entry *uwc.Entry,
	resolved *uwuc.Resolved,
	message *conversation.Message,
	variant int,
) {
	providerID, messageID := "", ""
	if message != nil {
		messageID = message.ID
		if message.ExternalMessageID != nil {
			providerID = *message.ExternalMessageID
		}
	}

	if err := c.deps.Entries.RecordSend(entry.ID, uwc.RecordSendInput{
		ContactID:         resolved.Contact.ID,
		ConversationID:    resolved.Conversation.ID,
		ProviderMessageID: providerID,
		MessageID:         messageID,
		VariantIndex:      variant,
		SentAt:            time.Now().UTC(),
	}); err != nil {
		log.Printf("[unofficial-whatsapp-campaign] could not record the send for entry %s: %v", entry.ID, err)
	}

	// Assignment at SEND time, not on first reply. That is what makes a campaign
	// a set of real conversations with owners rather than a fire-and-forget log,
	// and it is what the official campaign already does.
	if c.deps.Assignments != nil {
		c.deps.Assignments.EnsureAssignment(
			resolved.Conversation.ID, string(shared.EntryTypeUnofficialWhatsApp), instance.ID)
	}

	if c.deps.Spam != nil {
		if err := c.deps.Spam.Record(entry.LeadID, instance.ID, camp.ID); err != nil {
			log.Printf("[unofficial-whatsapp-campaign] could not record the cooldown for lead %s: %v", entry.LeadID, err)
		}
	}

	// Volume only. No money is computed anywhere; this is the ledger that makes
	// pricing addable later without a data gap.
	if c.deps.Metrics != nil {
		c.deps.Metrics.RecordCampaignSend(RecordSendMetric{
			WorkspaceID:       camp.WorkspaceID,
			CampaignID:        camp.ID,
			EntryID:           entry.ID,
			InstanceID:        instance.ID,
			VariantIndex:      variant,
			ProviderMessageID: providerID,
		})
	}

	if c.deps.Workflows != nil && camp.EnableWorkflow {
		c.deps.Workflows.CampaignSent(CampaignSentTrigger{
			WorkspaceID:       camp.WorkspaceID,
			CampaignID:        camp.ID,
			EntryID:           entry.ID,
			ConversationID:    resolved.Conversation.ID,
			PhoneNumber:       entry.Number,
			ProviderMessageID: providerID,
		})
	}

	if c.deps.Broadcaster != nil {
		c.deps.Broadcaster.BroadcastEntryUpdate(
			resolved.Conversation.ID, string(shared.EntryTypeUnofficialWhatsApp), nil)
	}
}

// resolveJID answers "is this number on WhatsApp", using the entry's cache.
func (c *messageConsumerUseCase) resolveJID(
	ctx context.Context,
	ref uw.InstanceRef,
	entry *uwc.Entry,
) (string, bool, error) {
	now := time.Now().UTC()
	if !entry.NeedsNumberCheck(now) {
		return entry.JID, true, nil
	}

	checks, err := c.deps.Instances.CheckNumbers(ctx, ref, []string{entry.Number})
	if err != nil {
		return "", false, err
	}
	if len(checks) == 0 || !checks[0].IsOnWhatsApp {
		_ = c.deps.Entries.RecordCheck(entry.ID, "", now, false)
		return "", false, nil
	}
	_ = c.deps.Entries.RecordCheck(entry.ID, checks[0].JID, now, true)
	return checks[0].JID, true, nil
}

// pauseInstanceCampaigns is the circuit breaker.
func (c *messageConsumerUseCase) pauseInstanceCampaigns(ctx context.Context, instanceID string, cause error) {
	if c.deps.PauseAll == nil {
		return
	}
	reason := "paused automatically"
	if cause != nil {
		reason = cause.Error()
	}
	if _, err := c.deps.PauseAll.Execute(ctx, instanceID, reason); err != nil {
		log.Printf("[unofficial-whatsapp-campaign] could not pause campaigns for number %s: %v", instanceID, err)
	}
}

func (c *messageConsumerUseCase) setEntryStatus(entryID string, status campaign.SendStatus) {
	if err := c.deps.Entries.UpdateStatus(entryID, status, "", 0, ""); err != nil {
		log.Printf("[unofficial-whatsapp-campaign] could not set status on entry %s: %v", entryID, err)
	}
}

func (c *messageConsumerUseCase) failEntry(entryID string, code int, message string) {
	if err := c.deps.Entries.UpdateStatus(entryID, campaign.SendStatusFailed, "", code, message); err != nil {
		log.Printf("[unofficial-whatsapp-campaign] could not fail entry %s: %v", entryID, err)
	}
}

// interactiveOptions maps the campaign's menu onto the channel-neutral shape.
func interactiveOptions(spec uwc.MessageSpec) []conversation.InteractiveOption {
	if spec.Kind != uwc.KindMenu {
		return nil
	}
	out := make([]conversation.InteractiveOption, 0, len(spec.Options))
	for _, o := range spec.Options {
		// Description is dropped rather than folded into the title: the
		// channel-neutral option carries no second line, and WhatsApp only
		// renders one for list rows anyway.
		out = append(out, conversation.InteractiveOption{ID: o.ID, Title: o.Title})
	}
	return out
}

// campaignStatusStore and pendingEntryCounter adapt this channel's repositories
// onto the two narrow ports the shared runner needs, so the runner never sees a
// campaign repository and cannot start depending on one.
type campaignStatusStore struct{ repo uwc.Repository }

func (s campaignStatusStore) ListRunningCampaignIDs() ([]string, error) {
	items, err := s.repo.ListByStatus(campaign.StatusRunning)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(items))
	for _, c := range items {
		if c != nil && c.ID != "" {
			ids = append(ids, c.ID)
		}
	}
	return ids, nil
}

func (s campaignStatusStore) CompleteCampaign(campaignID string) (bool, error) {
	return s.repo.UpdateStatus(campaignID, campaign.StatusCompleted, campaign.StatusRunning)
}

type pendingEntryCounter struct{ repo uwc.EntryRepository }

func (p pendingEntryCounter) CountPendingEntries(campaignID string) (int64, error) {
	counts, err := p.repo.CountByStatus(campaignID)
	if err != nil || counts == nil {
		return 0, err
	}
	return counts.Pending, nil
}
