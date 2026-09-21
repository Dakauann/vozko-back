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

const (
	pauseRequeueDelay       = 3 * time.Second
	restrictionRequeueDelay = 5 * time.Minute
	capRequeueDelay         = 15 * time.Minute
)

const (
	errNoInstance          = 910001
	errInstanceUnusable    = 910002
	errResolveConversation = 910003
	errSendFailed          = 910004
)

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

func (c *messageConsumerUseCase) attachRunner() {
	c.runner = campaignqueue.New(
		c.deps.QueueSub, c.deps.QueuePub, c.deps.Shared,
		campaignStatusStore{repo: c.deps.Campaigns},
		pendingEntryCounter{repo: c.deps.Entries},
		campaignqueue.Config{
			Namespace:         uwc.QueueNamespace,
			PauseRequeueDelay: pauseRequeueDelay,
			Precheck:          c.precheck,
			Pace:              nil,
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

func (c *messageConsumerUseCase) SetPauseAll(p uwc.PauseCampaignsForInstanceUseCase) {
	c.deps.PauseAll = p
}

func (c *messageConsumerUseCase) SetWorkflows(w WorkflowTrigger) {
	c.deps.Workflows = w
}

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

func (c *messageConsumerUseCase) handle(msg campaignqueue.Message) campaignqueue.Result {
	ctx := context.Background()

	camp, err := c.deps.Campaigns.FindByID(msg.CampaignID)
	if err != nil || camp == nil {
		log.Printf("[unofficial-whatsapp-campaign] campaign %s not found: %v", msg.CampaignID, err)
		return campaignqueue.Requeue
	}

	entry, err := c.deps.Entries.FindByID(msg.EntryID)
	if err != nil || entry == nil {
		log.Printf("[unofficial-whatsapp-campaign] entry %s not found: %v", msg.EntryID, err)
		return campaignqueue.Drop
	}
	if entry.Status != campaign.SendStatusPending {
		return campaignqueue.Drop
	}

	instance, err := c.deps.Instances.Instance(ctx, camp.InstanceID)
	if err != nil || instance == nil {
		c.failEntry(entry.ID, errNoInstance, "the number for this campaign is no longer connected")
		return campaignqueue.Drop
	}

	if _, err := instance.CanSend(time.Now().UTC()); err != nil {
		c.pauseInstanceCampaigns(ctx, instance.ID, err)
		return campaignqueue.RetryLater(restrictionRequeueDelay)
	}

	effectiveCap := camp.EffectiveDailyCap(instance.EffectiveDailyCap(time.Now().UTC()))
	budgeted, budgetErr := c.deps.Budget.TryConsumeDaily(instance.ID, effectiveCap)
	if budgetErr != nil || !budgeted {
		return campaignqueue.RetryLater(capRequeueDelay)
	}
	spent := false
	defer func() {
		if !spent {
			c.deps.Budget.ReleaseDaily(instance.ID, effectiveCap)
		}
	}()

	if c.deps.Spam != nil && c.deps.Spam.ShouldSkip(ctx, camp.WorkspaceID, entry.LeadID, instance.ID) {
		c.setEntryStatus(entry.ID, campaign.SendStatusNotEligiblePossibleSpam)
		return campaignqueue.Drop
	}

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

	minMS, maxMS := PacingFor(camp, instance)
	if acquired, wait := c.deps.Budget.AcquirePace(instance.ID, minMS, maxMS); !acquired {
		return campaignqueue.RetryLater(wait)
	}

	resolved, err := c.deps.Instances.Resolve(ctx, instance, uwuc.ResolveInput{
		JID:         jid,
		PhoneNumber: entry.Number,
		Name:        entry.Name,
	})
	if err != nil {
		c.failEntry(entry.ID, errResolveConversation, "could not open a conversation with this number")
		return campaignqueue.Drop
	}

	variant := camp.Message.VariantFor(entry.ID)
	body := camp.Message.Render(variant, entry.Variables)

	message, err := c.deps.Sender.SendCampaignMessage(conversation_usecase.SendCampaignMessageInput{
		EntryID:     resolved.Conversation.ID,
		EntryType:   string(shared.EntryTypeUnofficialWhatsApp),
		Text:        body,
		MediaID:     camp.Message.MediaID,
		MediaType:   string(camp.Message.Kind.MediaKind()),
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

	c.recordSuccess(ctx, camp, instance, entry, resolved, message, variant)
	return campaignqueue.Done
}

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

	if c.deps.Assignments != nil {
		c.deps.Assignments.EnsureAssignment(
			resolved.Conversation.ID, string(shared.EntryTypeUnofficialWhatsApp), instance.ID)
	}

	if c.deps.Spam != nil {
		if err := c.deps.Spam.Record(entry.LeadID, instance.ID, camp.ID); err != nil {
			log.Printf("[unofficial-whatsapp-campaign] could not record the cooldown for lead %s: %v", entry.LeadID, err)
		}
	}

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

func interactiveOptions(spec uwc.MessageSpec) []conversation.InteractiveOption {
	if spec.Kind != uwc.KindMenu {
		return nil
	}
	out := make([]conversation.InteractiveOption, 0, len(spec.Options))
	for _, o := range spec.Options {
		out = append(out, conversation.InteractiveOption{ID: o.ID, Title: o.Title})
	}
	return out
}

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
