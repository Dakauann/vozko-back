package container

import (
	"context"

	uwhttp "vozko/delivery/http/unofficial_whatsapp"
	wsdelivery "vozko/delivery/ws"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	workflow_domain "vozko/domain/workflow"
	workspace_department "vozko/domain/workspace/workspace_department"
	uwrepo "vozko/infra/repositories/unofficial_whatsapp"
	uwc_repo "vozko/infra/repositories/unofficial_whatsapp_campaign"
	conversation_usecase "vozko/usecases/conversation"
	uwuc "vozko/usecases/unofficial_whatsapp"
	uwcuc "vozko/usecases/unofficial_whatsapp_campaign"
)

type unofficialWhatsAppCampaignBundle struct {
	Enabled bool

	Campaigns uwc.Repository
	Entries   uwc.EntryRepository

	Consumer     uwc.MessageConsumerUseCase
	Dispatch     uwc.DispatchCampaignUseCase
	Create       uwc.CreateCampaignUseCase
	Access       uwc.CampaignAccessUseCase
	Actions      uwc.CampaignActionUseCase
	Preview      uwc.ImportPreviewUseCase
	Instances    uwc.CampaignInstanceUseCase
	ScheduleJob  uwc.StartScheduleJob
	PauseForInst uwc.PauseCampaignsForInstanceUseCase

	Handler *uwhttp.CampaignHandler
}

func (c *Container) initUnofficialWhatsAppCampaigns(
	sender *conversation_usecase.MessageSenderService,
	departments workspace_department.CreationDepartmentResolver,
) {
	bundle := &unofficialWhatsAppCampaignBundle{}
	c.unofficialWhatsAppCampaigns = bundle

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

	spam := c.campaignSpamGuard()
	eligibility := c.campaignEligibility()

	budget := uwcuc.NewSendBudget(c.redisProvider.SharedState())

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
		Eligibility: eligibility,
		Assignments: c.services.assignmentService,
		Broadcaster: &campaignEntryBroadcaster{hub: c.services.conversationHub},
	})
	bundle.Consumer = consumer
	bundle.PauseForInst = uwcuc.NewPauseCampaignsForInstanceUseCase(bundle.Campaigns, consumer)

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
	bundle.Create = uwcuc.NewCreateCampaignUseCase(
		bundle.Campaigns, bundle.Entries, c.repositories.lead, gateway, eligibility, departments)
	c.wireUnofficialCampaignCreate(bundle)
	bundle.Access = uwcuc.NewCampaignAccessUseCase(uwcuc.NewGetCampaignUseCase(bundle.Campaigns, bundle.Entries, gateway))
	bundle.Actions = uwcuc.NewCampaignActionUseCase(bundle.Access, bundle.Dispatch)
	bundle.Preview = uwcuc.NewImportPreviewUseCase(c.useCases.readMedia)
	bundle.Instances = uwcuc.NewCampaignInstanceUseCase(gateway)

	logChannelCapabilities("unofficial-whatsapp-campaigns", map[string]bool{
		"sender":         sender != nil,
		"assignment":     c.services.assignmentService != nil,
		"broadcaster":    c.services.conversationHub != nil,
		"departments":    departments != nil,
		"spam-cooldown":  c.repositories.leadCampaignSend != nil,
		"department-acl": c.services.conversationAuthImpl != nil,
	})

	bundle.Handler = uwhttp.NewCampaignHandler(uwhttp.CampaignHandlerDeps{
		Create:      bundle.Create,
		Update:      c.automateCampaignChanges("unofficial update", uwcuc.NewUpdateCampaignUseCase(bundle.Campaigns, bundle.Entries, gateway)).(uwc.UpdateCampaignUseCase),
		Get:         uwcuc.NewGetCampaignUseCase(bundle.Campaigns, bundle.Entries, gateway),
		Access:      bundle.Access,
		Actions:     bundle.Actions,
		List:        uwcuc.NewListCampaignsUseCase(bundle.Campaigns, bundle.Entries, gateway),
		Delete:      uwcuc.NewDeleteCampaignUseCase(bundle.Campaigns, bundle.Entries),
		AssignDep:   uwcuc.NewAssignDepartmentUseCase(bundle.Campaigns, departments),
		Summary:     uwcuc.NewGetSummaryUseCase(summary),
		Entries:     uwcuc.NewListEntriesUseCase(bundle.Entries),
		Reset:       uwcuc.NewResetCampaignUseCase(bundle.Campaigns, bundle.Entries),
		Clear:       uwcuc.NewClearHistoryUseCase(bundle.Campaigns, bundle.Entries, c.repositories.conversation),
		AddEntry:    c.automateCampaignChanges("unofficial add entries", uwcuc.NewAddEntriesUseCase(bundle.Campaigns, bundle.Entries, c.repositories.lead)).(uwc.AddEntriesUseCase),
		UpdateEntry: uwcuc.NewUpdateEntryUseCase(bundle.Campaigns, bundle.Entries, c.repositories.lead),
		DeleteEntry: uwcuc.NewDeleteEntryUseCase(bundle.Campaigns, bundle.Entries),
		QuickSend: c.automateCampaignChanges("unofficial quick send", uwcuc.NewQuickSendUseCase(
			bundle.Campaigns, bundle.Entries, c.repositories.lead,
			bundle.Dispatch, c.redisProvider.SharedState())).(uwc.QuickSendUseCase),
		Validate:    uwcuc.NewValidateTargetsUseCase(bundle.Campaigns, bundle.Entries, gateway),
		Departments: c.services.conversationAuthImpl,
	})
}

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

type campaignEntryBroadcaster struct{ hub *wsdelivery.ConversationHub }

func (b *campaignEntryBroadcaster) BroadcastEntryUpdate(entryID, entryType string, _ interface{}) {
	if b.hub == nil {
		return
	}
	b.hub.BroadcastEntryUpdate(entryID, entryType, nil)
}

func unofficialWhatsAppCampaignHandler(c *Container) *uwhttp.CampaignHandler {
	if c.unofficialWhatsAppCampaigns == nil || !c.unofficialWhatsAppCampaigns.Enabled {
		return nil
	}
	return c.unofficialWhatsAppCampaigns.Handler
}
