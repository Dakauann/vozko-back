package container

import (
	"context"
	"log"
	"time"

	uwhttp "vozko/delivery/http/unofficial_whatsapp"
	wsdelivery "vozko/delivery/ws"
	"vozko/domain/business_metrics"
	lcs "vozko/domain/lead_campaign_send"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	workflow_domain "vozko/domain/workflow"
	workspace_department "vozko/domain/workspace/workspace_department"
	wsc "vozko/domain/workspace_config"
	uwrepo "vozko/infra/repositories/unofficial_whatsapp"
	uwc_repo "vozko/infra/repositories/unofficial_whatsapp_campaign"
	conversation_usecase "vozko/usecases/conversation"
	uwuc "vozko/usecases/unofficial_whatsapp"
	uwcuc "vozko/usecases/unofficial_whatsapp_campaign"
)

// unofficialWhatsAppCampaignBundle groups the campaign feature so it can be
// wired, or skipped, as one unit.
//
// Built in the use-case pass rather than alongside the channel, because it needs
// the message sender, the assignment service and the workflow evaluator — none
// of which exist when initUnofficialWhatsApp runs.
type unofficialWhatsAppCampaignBundle struct {
	Enabled bool

	Campaigns uwc.Repository
	Entries   uwc.EntryRepository

	Consumer     uwc.MessageConsumerUseCase
	Dispatch     uwc.DispatchCampaignUseCase
	ScheduleJob  uwc.StartScheduleJob
	PauseForInst uwc.PauseCampaignsForInstanceUseCase

	Handler *uwhttp.CampaignHandler
}

// initUnofficialWhatsAppCampaigns builds the campaign feature.
// The parameters are explicit rather than read off the container, and that is
// the point: c.useCases is not assigned until the END of initUseCases, so an
// earlier call reaching for c.useCases.recordMetric dereferences nil and takes
// the whole process down at boot. A signature that names what it needs makes
// the wiring order a compile-time conversation instead of a segfault.
func (c *Container) initUnofficialWhatsAppCampaigns(
	sender *conversation_usecase.MessageSenderService,
	departments workspace_department.CreationDepartmentResolver,
	recordMetric business_metrics.RecordMetricUseCase,
) {
	bundle := &unofficialWhatsAppCampaignBundle{}
	c.unofficialWhatsAppCampaigns = bundle

	// Campaigns exist only where the channel does. A workspace with no connected
	// numbers has nothing to campaign from, and registering the routes anyway
	// would offer a screen every request 500s on.
	if c.unofficialWhatsApp == nil || !c.unofficialWhatsApp.Enabled {
		return
	}
	bundle.Enabled = true

	bundle.Campaigns = uwc_repo.NewRepository(c.db)
	bundle.Entries = uwc_repo.NewEntryRepository(c.db)
	summary := uwc_repo.NewSummaryRepository(c.db)

	gateway := &unofficialCampaignGateway{
		instances: c.unofficialWhatsApp.Instances,
		servers:   c.unofficialWhatsApp.Servers,
		messaging: c.unofficialWhatsApp.Messaging,
		provider:  c.unofficialWhatsApp.Provider,
		resolver: uwuc.NewConversationResolver(
			c.unofficialWhatsApp.Contacts,
			c.unofficialWhatsApp.Conversations,
			uwrepo.NewLeadLinker(c.repositories.lead),
		),
	}

	spam := &workspaceSpamGuard{
		config: c.repositories.workspaceConfig,
		sends:  c.repositories.leadCampaignSend,
	}

	budget := uwcuc.NewSendBudget(c.redisProvider.SharedState())

	// The consumer and the circuit breaker are mutually dependent: the breaker
	// has to detach consumers, and the consumer has to call the breaker. Built
	// in two steps rather than merged, so neither grows the other's job.
	consumer := uwcuc.NewMessageConsumerUseCase(uwcuc.ConsumerDeps{
		QueueSub:    c.services.wcQueueSub,
		QueuePub:    c.services.wcQueuePub,
		Shared:      c.redisProvider.SharedState(),
		Campaigns:   bundle.Campaigns,
		Entries:     bundle.Entries,
		Instances:   gateway,
		Sender:      sender,
		Budget:      budget,
		Spam:        spam,
		Assignments: c.services.assignmentService,
		Metrics:     &campaignMetricRecorder{record: recordMetric},
		Broadcaster: &campaignEntryBroadcaster{hub: c.services.conversationHub},
	})
	bundle.Consumer = consumer
	bundle.PauseForInst = uwcuc.NewPauseCampaignsForInstanceUseCase(bundle.Campaigns, consumer)

	// Attached after construction for the same reason the official consumer's
	// trigger evaluator is: the workflow stack is built later in the pass.
	if attachable, ok := consumer.(interface {
		SetPauseAll(uwc.PauseCampaignsForInstanceUseCase)
	}); ok {
		attachable.SetPauseAll(bundle.PauseForInst)
	}

	bundle.Dispatch = uwcuc.NewDispatchCampaignUseCase(
		bundle.Campaigns, bundle.Entries, gateway, consumer,
		c.services.wcQueuePub, c.redisProvider.SharedState(),
	)
	bundle.ScheduleJob = uwcuc.NewScheduleStartJob(bundle.Campaigns, bundle.Dispatch)

	logChannelCapabilities("unofficial-whatsapp-campaigns", map[string]bool{
		"sender":         sender != nil,
		"assignment":     c.services.assignmentService != nil,
		"metrics":        recordMetric != nil,
		"broadcaster":    c.services.conversationHub != nil,
		"departments":    departments != nil,
		"spam-cooldown":  c.repositories.leadCampaignSend != nil,
		"department-acl": c.services.conversationAuthImpl != nil,
	})

	bundle.Handler = uwhttp.NewCampaignHandler(uwhttp.CampaignHandlerDeps{
		Create: uwcuc.NewCreateCampaignUseCase(
			bundle.Campaigns, bundle.Entries, c.repositories.lead, gateway, spam,
			departments),
		Update:      uwcuc.NewUpdateCampaignUseCase(bundle.Campaigns, bundle.Entries, gateway),
		Get:         uwcuc.NewGetCampaignUseCase(bundle.Campaigns, bundle.Entries, gateway),
		List:        uwcuc.NewListCampaignsUseCase(bundle.Campaigns, bundle.Entries, gateway),
		Delete:      uwcuc.NewDeleteCampaignUseCase(bundle.Campaigns, bundle.Entries),
		AssignDep:   uwcuc.NewAssignDepartmentUseCase(bundle.Campaigns, departments),
		Summary:     uwcuc.NewGetSummaryUseCase(summary),
		Entries:     uwcuc.NewListEntriesUseCase(bundle.Entries),
		Dispatch:    bundle.Dispatch,
		Reset:       uwcuc.NewResetCampaignUseCase(bundle.Campaigns, bundle.Entries),
		Clear:       uwcuc.NewClearHistoryUseCase(bundle.Campaigns, bundle.Entries, c.repositories.conversation),
		AddEntry:    uwcuc.NewAddEntriesUseCase(bundle.Campaigns, bundle.Entries, c.repositories.lead),
		UpdateEntry: uwcuc.NewUpdateEntryUseCase(bundle.Campaigns, bundle.Entries, c.repositories.lead),
		DeleteEntry: uwcuc.NewDeleteEntryUseCase(bundle.Campaigns, bundle.Entries),
		QuickSend: uwcuc.NewQuickSendUseCase(
			bundle.Campaigns, bundle.Entries, c.repositories.lead,
			bundle.Dispatch, c.redisProvider.SharedState()),
		Validate:    uwcuc.NewValidateTargetsUseCase(bundle.Campaigns, bundle.Entries, gateway),
		Departments: c.services.conversationAuthImpl,
	})
}

// unofficialCampaignGateway adapts the channel's repositories and provider onto
// the single port the campaign package needs.
//
// One adapter rather than five constructor arguments: every campaign call site
// needs the instance AND its server to build a provider ref, and threading both
// through each use case duplicated the same two lookups eleven times.
type unofficialCampaignGateway struct {
	instances uw.InstanceRepository
	servers   uw.ServerRepository
	messaging uw.MessagingAPI
	provider  uw.ProviderAPI
	resolver  *uwuc.ConversationResolver
}

func (g *unofficialCampaignGateway) Instance(ctx context.Context, instanceID string) (*uw.Instance, error) {
	return g.instances.FindByID(ctx, instanceID)
}

func (g *unofficialCampaignGateway) Ref(ctx context.Context, instance *uw.Instance) (uw.InstanceRef, error) {
	server, err := g.servers.FindByID(ctx, instance.ServerID)
	if err != nil {
		return uw.InstanceRef{}, err
	}
	return uw.RefFor(server, instance), nil
}

func (g *unofficialCampaignGateway) CheckNumbers(ctx context.Context, ref uw.InstanceRef, numbers []string) ([]uw.NumberCheck, error) {
	return g.messaging.CheckNumbers(ctx, ref, numbers)
}

func (g *unofficialCampaignGateway) MessagingLimits(ctx context.Context, ref uw.InstanceRef) (*uw.Restriction, error) {
	return g.provider.MessagingLimits(ctx, ref)
}

func (g *unofficialCampaignGateway) CacheRestriction(ctx context.Context, instanceID string, r uw.Restriction) error {
	return g.instances.UpdateRestriction(ctx, instanceID, r)
}

func (g *unofficialCampaignGateway) Resolve(ctx context.Context, instance *uw.Instance, in uwuc.ResolveInput) (*uwuc.Resolved, error) {
	return g.resolver.Resolve(ctx, instance, in)
}

// workspaceSpamGuard is the workspace's own re-contact cooldown.
//
// It reads the SAME setting and the SAME ledger the official campaign and both
// cold-outbound dialogs use, so one switch in workspace settings governs every
// way of reaching somebody. The sender id here is the INSTANCE id where the
// official channel passes a business phone id; both are UUIDs from disjoint
// tables, so the shared ledger cannot confuse them.
type workspaceSpamGuard struct {
	config workspaceConfigReader
	sends  lcs.Repository
}

type workspaceConfigReader interface {
	GetByWorkspaceID(ctx context.Context, workspaceID string) (*wsc.WorkspaceConfig, error)
}

func (g *workspaceSpamGuard) protectionDays(ctx context.Context, workspaceID string) int {
	if g.config == nil {
		return 0
	}
	cfg, err := g.config.GetByWorkspaceID(ctx, workspaceID)
	if err != nil || cfg == nil {
		return 0
	}
	return cfg.CampaignSpamProtectionDays
}

func (g *workspaceSpamGuard) ShouldSkip(ctx context.Context, workspaceID, leadID, senderID string) bool {
	days := g.protectionDays(ctx, workspaceID)
	if days <= 0 || g.sends == nil {
		return false
	}
	lastSent, err := g.sends.GetLastSendTime(leadID, senderID)
	if err != nil {
		return false
	}
	return lcs.WithinSpamWindow(lastSent, days, time.Now())
}

func (g *workspaceSpamGuard) SkipMany(ctx context.Context, workspaceID string, leadIDs []string, senderID string) map[string]bool {
	out := map[string]bool{}
	days := g.protectionDays(ctx, workspaceID)
	if days <= 0 || g.sends == nil || len(leadIDs) == 0 {
		return out
	}
	lastSends, err := g.sends.GetLastSendTimesBatch(leadIDs, senderID)
	if err != nil {
		return out
	}
	now := time.Now()
	for leadID, at := range lastSends {
		stamp := at
		if lcs.WithinSpamWindow(&stamp, days, now) {
			out[leadID] = true
		}
	}
	return out
}

func (g *workspaceSpamGuard) Record(leadID, senderID, campaignID string) error {
	if g.sends == nil {
		return nil
	}
	return g.sends.Record(leadID, senderID, campaignID)
}

// campaignMetricRecorder meters send VOLUME. No money is computed anywhere in
// this channel; this is what makes pricing addable later without a data gap.
type campaignMetricRecorder struct {
	record business_metrics.RecordMetricUseCase
}

func (r *campaignMetricRecorder) RecordCampaignSend(in uwcuc.RecordSendMetric) {
	if r.record == nil {
		return
	}
	entityID := in.ProviderMessageID
	if entityID == "" {
		entityID = in.EntryID
	}
	if err := r.record.Execute(business_metrics.RecordMetricInput{
		EventType:  business_metrics.EventUnofficialWhatsAppMessageSent,
		EntityID:   entityID,
		EntityType: business_metrics.EntityTypeMessage,
		Metadata: map[string]string{
			"campaign_id": in.CampaignID,
			"entry_id":    in.EntryID,
			"instance_id": in.InstanceID,
			"source":      "unofficial_whatsapp_campaign",
		},
	}); err != nil {
		log.Printf("[unofficial-whatsapp-campaign] could not record the send metric: %v", err)
	}
}

// campaignWorkflowTrigger fires the campaign-sent trigger.
type campaignWorkflowTrigger struct {
	evaluator workflow_domain.TriggerEvaluator
}

func (t *campaignWorkflowTrigger) CampaignSent(in uwcuc.CampaignSentTrigger) {
	if t.evaluator == nil {
		return
	}
	go t.evaluator.Evaluate(workflow_domain.TriggerEvent{
		WorkspaceID: in.WorkspaceID,
		EntryID:     in.ConversationID,
		EntryType:   "unofficial_whatsapp",
		TriggerType: workflow_domain.TriggerCampaignSent,
		Data: map[string]interface{}{
			"campaign_id":  in.CampaignID,
			"entry_id":     in.EntryID,
			"phone_number": in.PhoneNumber,
			"message_id":   in.ProviderMessageID,
		},
	})
}

// campaignEntryBroadcaster narrows the hub onto the port the campaign package
// declares.
//
// The hub's signature carries a *conversation.Message; a campaign entry update
// carries no message, so the adapter passes nil rather than the campaign
// package importing the websocket layer to say so.
type campaignEntryBroadcaster struct{ hub *wsdelivery.ConversationHub }

func (b *campaignEntryBroadcaster) BroadcastEntryUpdate(entryID, entryType string, _ interface{}) {
	if b.hub == nil {
		return
	}
	b.hub.BroadcastEntryUpdate(entryID, entryType, nil)
}

// unofficialWhatsAppCampaignHandler returns the handler, or nil when campaigns
// are not wired.
func unofficialWhatsAppCampaignHandler(c *Container) *uwhttp.CampaignHandler {
	if c.unofficialWhatsAppCampaigns == nil || !c.unofficialWhatsAppCampaigns.Enabled {
		return nil
	}
	return c.unofficialWhatsAppCampaigns.Handler
}
