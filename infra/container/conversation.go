package container

import (
	"context"
	"log"
	"sort"
	"strings"

	wsdelivery "vozko/delivery/ws"
	balance_domain "vozko/domain/balance"
	conversation_domain "vozko/domain/conversation"
	"vozko/domain/shared"
	workflow_domain "vozko/domain/workflow"
	conversation_infra "vozko/infra/conversation"
	whatsapp_infra "vozko/infra/conversation/whatsapp"
	"vozko/infra/conversation/whatsapp/media"
	ia_repo "vozko/infra/repositories/inbox_assignment"
	aa_usecase "vozko/usecases/ai_attendance"
	cauc "vozko/usecases/audience"
	conversation_usecase "vozko/usecases/conversation"
	ce_usecase "vozko/usecases/conversation_event"
	crm_telemetry_usecase "vozko/usecases/crm_telemetry"
	ia_usecase "vozko/usecases/inbox_assignment"
	label_usecase "vozko/usecases/label"
	stage_usecase "vozko/usecases/stage"
)

func (c *Container) wireConversationHub(consumeWhatsappTemplate balance_domain.ConsumeWhatsappTemplateUseCase) {
	workspaceResolver := conversation_usecase.NewCampaignWorkspaceResolver(
		c.repositories.wcCampaign,
		c.repositories.wcEntry,
	)

	conversationAuthorizer := conversation_infra.NewAuthorizer(
		c.repositories.wcEntry,
		c.repositories.workspace,
		c.repositories.workspaceDepartment,
		c.repositories.inboxAssignment,
		workspaceResolver,
		c.redisProvider.SharedState(),
	)
	c.services.conversationAuth = conversationAuthorizer
	if impl, ok := conversationAuthorizer.(*conversation_infra.Authorizer); ok {
		c.services.conversationAuthImpl = impl
	}

	c.services.liveOperatorSend = conversation_domain.NewLiveOperatorSend()

	c.services.conversationHub = wsdelivery.NewConversationHub(
		c.services.conversationAuth,
		c.repositories.user,
		c.services.liveOperatorSend,
		c.redisProvider.SharedState(),
		c.replicaID,
		c.cfg.PublicReplicaURL,
	)
	c.services.conversationHub.SetWSMetrics(c.services.metrics)

	historyProvider := conversation_usecase.NewHistoryProviderService(
		c.repositories.conversation,
		c.repositories.wcEntry,
		c.repositories.lead,
		c.repositories.user,
		c.repositories.agent,
		c.repositories.leadMessageWindow,
	)
	c.services.conversationHistory = historyProvider
	c.services.conversationHub.SetHistoryProvider(historyProvider)

	if runs, ok := c.repositories.workflowRun.(interface {
		FindActiveByEntries(entryIDs []string) (map[string]*workflow_domain.WorkflowRun, error)
	}); ok {
		if wfs, ok := c.repositories.workflow.(interface {
			FindByIDs(ids []string) ([]*workflow_domain.Workflow, error)
		}); ok {
			historyProvider.SetWorkflowLookups(runs, wfs)
		}
	}

	messageMarker := conversation_usecase.NewMessageMarkerService(
		c.repositories.conversation,
		c.services.whatsappClientFactory,
		c.repositories.wcEntry,
	)
	c.services.conversationHub.SetMessageMarker(messageMarker)
	c.services.messageMarker = messageMarker

	StageProvider := stage_usecase.NewStageProviderService(c.repositories.stage)
	c.services.conversationHub.SetStageProvider(StageProvider)

	InitialStageAssigner := stage_usecase.NewInitialStageAssignerService(c.repositories.stage)
	c.services.conversationHub.SetInitialStageAssigner(InitialStageAssigner)

	labelProvider := label_usecase.NewLabelProviderService(c.repositories.label)
	c.services.conversationHub.SetLabelProvider(labelProvider)

	var analysisProvider conversation_domain.AnalysisProvider
	if c.repositories.conversationAnalyses != nil {
		analysisProvider = cauc.NewConversationAnalysisProvider(c.repositories.conversationAnalyses)
		c.services.conversationHub.SetAnalysisProvider(analysisProvider)
	}

	conversationStatusUpdater := conversation_usecase.NewConversationStatusService(
		c.repositories.wcEntry,
	)
	c.services.conversationStatusService = conversationStatusUpdater
	c.services.campaignWorkspaceResolver = workspaceResolver
	if c.services.crmTelemetryPublisher == nil && c.services.crmTelemetryPub != nil {
		drops := crm_telemetry_usecase.NewLogDropRecorder()
		c.services.crmTelemetryPublisher = crm_telemetry_usecase.NewPublisherWithDrops(c.services.crmTelemetryPub, drops)
	}
	if c.services.crmTelemetryEmitter == nil && c.services.crmTelemetryPublisher != nil {
		c.services.crmTelemetryEmitter = crm_telemetry_usecase.NewEmitter(c.services.crmTelemetryPublisher)
	}
	telemetryPub := c.services.crmTelemetryPublisher
	eventLoggerEarly := ce_usecase.NewLogger(telemetryPub)
	conversationStatusUpdater.SetEventLogger(eventLoggerEarly)
	conversationStatusUpdater.SetWorkspaceResolver(func(entryID, entryType string) string {
		if workspaceResolver == nil {
			return ""
		}
		ws, _ := workspaceResolver.GetEntryWorkspaceID(entryID, entryType)
		return ws
	})
	c.services.conversationHub.SetConversationStatusUpdater(conversationStatusUpdater)
	c.services.conversationStatusUpdater = conversationStatusUpdater

	c.services.conversationHub.SetCampaignWorkspaceResolver(workspaceResolver)
	c.services.conversationHub.SetWorkspaceDepartmentRepo(c.repositories.workspaceDepartment)

	inboxSvc := conversation_usecase.NewInboxService(
		historyProvider,
		StageProvider,
		labelProvider,
		workspaceResolver,
		InitialStageAssigner,
		c.services.conversationAuth,
		analysisProvider,
		conversationStatusUpdater,
	)
	c.services.inboxService = inboxSvc
	if setter, ok := inboxSvc.(interface {
		SetAnalysisScheduleReader(conversation_domain.AnalysisScheduleReader)
	}); ok {
		setter.SetAnalysisScheduleReader(
			conversation_usecase.NewAnalysisScheduleReader(c.redisProvider.SharedState()),
		)
	}

	assignmentRepo := ia_repo.New(c.db)
	c.services.assignmentService = ia_usecase.NewAssignmentService(assignmentRepo, c.services.conversationHub, workspaceResolver, c.repositories.workspaceConfig)
	c.services.assignmentService.SetTelemetry(telemetryPub)
	c.services.assignmentService.SetEventLogger(eventLoggerEarly)
	c.services.assignmentService.SetRoster(ia_usecase.NewRosterService(
		c.repositories.workspace,
		c.repositories.workspaceDepartment,
		c.services.conversationAuth,
		c.redisProvider.SharedState(),
	))
	c.services.assignmentService.SetPresence(c.repositories.agentPresence)
	c.services.aiAttendanceService = aa_usecase.NewAsyncSessionService(telemetryPub)
	conversationStatusUpdater.SetAISessionEnder(c.services.aiAttendanceService)

	templateSender := conversation_usecase.NewTemplateSenderService(
		c.services.whatsappClientFactory,
		c.repositories.whatsappTemplate,
		c.repositories.conversation,
		c.repositories.lead,
		c.repositories.wcEntry,
		c.services.conversationHub,
		consumeWhatsappTemplate,
		eventLoggerEarly,
	)
	c.services.conversationHub.SetTemplateSender(templateSender)

	if setter, ok := inboxSvc.(interface {
		SetTemplateSender(conversation_domain.TemplateSender)
	}); ok {
		setter.SetTemplateSender(templateSender)
	}

	c.services.conversationHub.SetInboxService(c.services.inboxService)
	c.services.conversationHub.SetMessageRepo(c.repositories.conversation)
	c.services.conversationHub.SetWACampaignRepo(c.repositories.wcCampaign)
	c.services.conversationHub.SetAssignmentRepo(assignmentRepo)
	c.services.conversationHub.SetAssignmentService(c.services.assignmentService)
	c.services.conversationHub.SetAISessionEnder(c.services.aiAttendanceService)
	operatorSendFinalizer, err := conversation_usecase.NewOperatorSendFinalizer(
		conversationStatusUpdater,
		workspaceResolver,
		eventLoggerEarly,
		c.services.aiAttendanceService,
		InitialStageAssigner,
	)
	if err != nil {
		log.Fatalf("[container] %v", err)
	}
	c.services.operatorSendFinalizer = operatorSendFinalizer
	c.services.conversationHub.SetPresenceRecorder(crm_telemetry_usecase.NewPresenceAdapter(telemetryPub))
	c.services.conversationHub.SetWorkspaceConfigRepo(c.repositories.workspaceConfig)
	historyProvider.SetAssignmentRepo(assignmentRepo)
	{
		callRegistry := whatsapp_infra.NewInMemoryCallRegistry()
		signaling := whatsapp_infra.NewCallSignalingClient(c.repositories.businessPhone, nil, "")

		var publicMediaIP string
		if ip, err := media.DiscoverPublicIP(c.cfg.WhatsAppStunServers); err == nil {
			publicMediaIP = ip
			log.Printf("[whatsapp-calls] discovered public media IP via STUN: %s", ip)
		} else {
			log.Printf("[whatsapp-calls] WARNING: could not resolve public media IP (%v); relying on STUN srflx candidates for the public mapping.", err)
		}

		muxPort := c.cfg.WhatsAppMediaUDPMuxPort
		if bound, err := media.EnableSharedUDPMux(muxPort); err != nil {
			log.Printf("[whatsapp-calls] WARNING: could not bind media UDP mux on :%d (%v), WhatsApp media falls back to per-call sockets, which does NOT scale. Fix WHATSAPP_MEDIA_UDP_MUX_PORT.", muxPort, err)
		} else {
			log.Printf("[whatsapp-calls] WebRTC media UDP mux on port %d, one shared socket for all calls (scales to thousands)", bound)
		}

		c.services.whatsappCallSignaling = signaling
		c.services.whatsappCallRegistry = callRegistry
		c.services.whatsappPublicMediaIP = publicMediaIP
		c.services.campaignWorkspaceResolver = workspaceResolver

		whatsappCallSource := conversation_usecase.NewWhatsAppCallSource(
			signaling,
			callRegistry,
			c.repositories.callPermission,
			publicMediaIP,
			c.cfg.WhatsAppStunServers,
			log.Default(),
		)
		c.services.whatsappCallWebhook = conversation_usecase.NewWhatsAppCallWebhookConsumer(callRegistry)
		c.services.whatsappCallPermissionWebhook = conversation_usecase.NewWhatsAppCallPermissionConsumer(
			c.repositories.businessPhone,
			c.repositories.callPermission,
			c.repositories.wcEntry,
			c.repositories.conversation,
			c.services.conversationHub,
		)

		callSource := conversation_usecase.NewDispatchingCallSource(whatsappCallSource)
		c.services.crmCallSource = callSource
		c.services.conversationHub.SetCallSource(callSource)
	}
	c.services.conversationHub.SetBillingPub(c.services.billingQueuePub)

	c.services.conversationHub.SetEventLogger(eventLoggerEarly)

}

func (c *Container) initConversationSenders() {
	messageSender := conversation_usecase.NewMessageSenderService(
		c.repositories.conversation,
		c.repositories.lead,
		c.repositories.wcEntry,
		c.repositories.leadMessageWindow,
		c.repositories.conversationMedia,
		c.services.whatsappClientFactory,
		c.services.conversationHub,
		c.repositories.wcCampaign,
		c.services.ai,
		c.services.toolRegistry,
		c.repositories.stage,
		c.redisProvider.SharedState(),
	)
	messageSender.SetCallPermissionRepo(c.repositories.callPermission)
	messageSender.SetMediaLibrary(c.repositories.media)
	c.services.messageSender = messageSender

	operatorSend, err := conversation_usecase.NewOperatorSendUseCase(
		messageSender,
		c.repositories.user,
		c.services.operatorSendFinalizer,
		c.services.serviceMessageBilling,
	)
	if err != nil {
		log.Fatalf("[container] %v", err)
	}
	c.services.operatorSend = operatorSend
	c.services.liveOperatorSend.Use(operatorSend)

	c.services.channelAIReply = conversation_usecase.NewChannelAIReplyService(
		c.repositories.agent,
		c.services.ai,
		c.repositories.conversation,
		messageSender,
	)
}

func (c *Container) mustChannelAIReply() *conversation_usecase.ChannelAIReplyService {
	if c.services.channelAIReply == nil {
		log.Fatal("[container] channel AI reply service read before initConversationSenders; " +
			"channels would silently never answer with an agent")
	}
	return c.services.channelAIReply
}

func (c *Container) startConversationHub() {
	messageSender := c.services.messageSender

	c.services.conversationAutomation = conversation_usecase.NewConversationAutomationService(
		c.services.conversationHub,
	)
	if c.repositories.wcEntry != nil {
		wcEntries := c.repositories.wcEntry
		c.services.conversationAutomation.Register(
			shared.EntryTypeWhatsApp,
			func(_ context.Context, entryID string, enabled *bool) error {
				return wcEntries.UpdateAutomationEnabled(entryID, enabled)
			},
		)
	}

	c.services.requestCallPermission = messageSender

	c.services.conversationHub.SetMemberVisibility(c.useCases.memberVisibility)

	go c.services.conversationHub.Run()
}

func (c *Container) registerChannelAdapter(adapter conversation_domain.ChannelAdapter) {
	if adapter == nil {
		return
	}
	c.services.channelAdapters = append(c.services.channelAdapters, adapter)
	registry := conversation_domain.NewAdapterRegistry(c.services.channelAdapters...)

	c.liveAdapterRegistry().Replace(c.services.channelAdapters...)

	if c.services.messageSender != nil {
		c.services.messageSender.SetChannelAdapters(registry)
	}
	if c.services.messageMarker != nil {
		c.services.messageMarker.SetChannelAdapters(registry)
	}
	if setter, ok := c.useCases.sendConversationMessage.(interface {
		SetChannelSender(conversation_domain.AdapterRegistry, conversation_usecase.ChannelMessageSender)
	}); ok && c.services.messageSender != nil {
		setter.SetChannelSender(registry, c.services.messageSender)
	}
	if setter, ok := c.services.conversationHistory.(interface {
		SetChannelAdapters(conversation_domain.AdapterRegistry)
	}); ok {
		setter.SetChannelAdapters(registry)
	}
}

func (c *Container) liveAdapterRegistry() *conversation_domain.LiveAdapterRegistry {
	if c.services.liveChannelAdapters == nil {
		c.services.liveChannelAdapters = conversation_domain.NewLiveAdapterRegistry()
	}
	return c.services.liveChannelAdapters
}

func logChannelCapabilities(channel string, capabilities map[string]bool) {
	names := make([]string, 0, len(capabilities))
	for name := range capabilities {
		names = append(names, name)
	}
	sort.Strings(names)

	active, missing := make([]string, 0, len(names)), make([]string, 0)
	for _, name := range names {
		if capabilities[name] {
			active = append(active, name)
			continue
		}
		missing = append(missing, name)
	}

	log.Printf("[%s] capabilities: %s", channel, strings.Join(active, ", "))
	if len(missing) > 0 {
		log.Printf("[%s] WARNING: inactive capabilities: %s (these fail silently at runtime)",
			channel, strings.Join(missing, ", "))
	}
}
