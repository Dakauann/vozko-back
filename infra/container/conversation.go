package container

import (
	"context"
	"log"

	wsdelivery "vozko/delivery/ws"
	balance_domain "vozko/domain/balance"
	conversation_domain "vozko/domain/conversation"
	workflow_domain "vozko/domain/workflow"
	workspace_config "vozko/domain/workspace_config"
	conversation_infra "vozko/infra/conversation"
	whatsapp_infra "vozko/infra/conversation/whatsapp"
	"vozko/infra/conversation/whatsapp/media"
	ia_repo "vozko/infra/repositories/inbox_assignment"
	aa_usecase "vozko/usecases/ai_attendance"
	analysis_usecase "vozko/usecases/analysis"
	call_roulette_usecase "vozko/usecases/call_roulette"
	conversation_usecase "vozko/usecases/conversation"
	ce_usecase "vozko/usecases/conversation_event"
	crm_telemetry_usecase "vozko/usecases/crm_telemetry"
	ia_usecase "vozko/usecases/inbox_assignment"
	label_usecase "vozko/usecases/label"
	sip_trunk_usecase "vozko/usecases/sip_trunk"
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
	// Both fields point at the same object: the interface for existing consumers,
	// the concrete type so channels can register their per-entry lookups.
	c.services.conversationAuth = conversationAuthorizer
	if impl, ok := conversationAuthorizer.(*conversation_infra.Authorizer); ok {
		c.services.conversationAuthImpl = impl
	}

	c.services.conversationHub = wsdelivery.NewConversationHub(c.services.conversationAuth, c.repositories.user, c.redisProvider.SharedState(), c.replicaID, c.cfg.PublicReplicaURL)
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

	// Read-only workflow lookups for the inbox "which AI attends this conversation"
	// enrichment. Adapted from the concrete repos (batch methods kept off the domain
	// interfaces); a nil match simply omits workflow-run detail.
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

	StageProvider := stage_usecase.NewStageProviderService(c.repositories.stage)
	c.services.conversationHub.SetStageProvider(StageProvider)

	InitialStageAssigner := stage_usecase.NewInitialStageAssignerService(c.repositories.stage)
	c.services.conversationHub.SetInitialStageAssigner(InitialStageAssigner)

	labelProvider := label_usecase.NewLabelProviderService(c.repositories.label)
	c.services.conversationHub.SetLabelProvider(labelProvider)

	analysisProvider := analysis_usecase.NewAnalysisProviderService(c.repositories.analysis)
	c.services.conversationHub.SetAnalysisProvider(analysisProvider)

	conversationStatusUpdater := conversation_usecase.NewConversationStatusService(
		c.repositories.wcEntry,
	)
	c.services.conversationStatusService = conversationStatusUpdater
	c.services.campaignWorkspaceResolver = workspaceResolver
	// Telemetry publisher is queue-only (no DB). Never Transition/Create on the hub path.
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

	assignmentRepo := ia_repo.New(c.db)
	c.services.assignmentService = ia_usecase.NewAssignmentService(assignmentRepo, c.services.conversationHub, workspaceResolver, c.repositories.workspaceConfig)
	c.services.assignmentService.SetTelemetry(telemetryPub)
	c.services.assignmentService.SetEventLogger(eventLoggerEarly)
	// Hot path: async queue only. Consumer runs the real SessionService against DB.
	c.services.aiAttendanceService = aa_usecase.NewAsyncSessionService(telemetryPub)
	// Contained AI sessions when conversation is marked finished (WA or voice entry).
	conversationStatusUpdater.SetAISessionEnder(c.services.aiAttendanceService)

	c.services.callRouletteService = call_roulette_usecase.NewAssignmentService(
		c.repositories.callRoulette,
		c.services.conversationHub,
		callRouletteCampaignResolverAdapter{workspaceResolver},
		callRouletteConfigAdapter{c.repositories.workspaceConfig},
	)
	c.services.callRouletteService.SetTelemetry(telemetryPub)

	templateSender := conversation_usecase.NewTemplateSenderService(
		c.services.whatsappClientFactory,
		c.repositories.whatsappTemplate,
		c.repositories.conversation,
		c.repositories.lead,
		c.repositories.wcEntry,
		c.services.conversationHub,
		consumeWhatsappTemplate,
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
	// Presence: publish only (no inline Transition/DB on WS register).
	c.services.conversationHub.SetPresenceRecorder(crm_telemetry_usecase.NewPresenceAdapter(telemetryPub))
	c.services.conversationHub.SetWorkspaceConfigRepo(c.repositories.workspaceConfig)
	historyProvider.SetAssignmentRepo(assignmentRepo)
	if c.services.sipTrunkManager != nil {
		getSIPTrunkUC := sip_trunk_usecase.NewGetUseCase(c.repositories.sipTrunk)
		sipCallSource := conversation_usecase.NewSIPTrunkCallSource(
			c.services.sipTrunkManager,
			c.repositories.sipTrunk,
			getSIPTrunkUC,
			c.services.trunkOwnership,
			log.Default(),
		)

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
		if media.PortRangeOverlaps(muxPort, muxPort, c.cfg.SIPTrunkRTPPortStart, c.cfg.SIPTrunkRTPPortEnd) {
			log.Printf("[whatsapp-calls] WARNING: media UDP mux port %d is INSIDE the SIP RTP range %d-%d — choose a port outside it.",
				muxPort, c.cfg.SIPTrunkRTPPortStart, c.cfg.SIPTrunkRTPPortEnd)
		}
		if bound, err := media.EnableSharedUDPMux(muxPort); err != nil {
			log.Printf("[whatsapp-calls] WARNING: could not bind media UDP mux on :%d (%v) — WhatsApp media falls back to per-call sockets, which does NOT scale. Fix WHATSAPP_MEDIA_UDP_MUX_PORT.", muxPort, err)
		} else {
			log.Printf("[whatsapp-calls] WebRTC media UDP mux on port %d — one shared socket for all calls (scales to thousands; SIP RTP %d-%d)",
				bound, c.cfg.SIPTrunkRTPPortStart, c.cfg.SIPTrunkRTPPortEnd)
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

		callSource := conversation_usecase.NewDispatchingCallSource(sipCallSource, whatsappCallSource)
		c.services.crmCallSource = callSource
		c.services.conversationHub.SetCallSource(callSource)
	}
	c.services.conversationHub.SetBillingPub(c.services.billingQueuePub)

	c.services.conversationHub.SetEventLogger(eventLoggerEarly)

}

func (c *Container) startConversationHub() {
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
		c.repositories.analysis,
		c.repositories.stage,
		c.redisProvider.SharedState(),
	)
	messageSender.SetCallPermissionRepo(c.repositories.callPermission)
	c.services.messageSender = messageSender
	c.services.conversationHub.SetMessageSender(messageSender)
	c.services.requestCallPermission = messageSender

	// Department-scoped guard for manual conversation assignment (resolved after
	// initUseCases has populated c.useCases).
	c.services.conversationHub.SetMemberVisibility(c.useCases.memberVisibility)

	go c.services.conversationHub.Run()
}

type callRouletteCampaignResolverAdapter struct {
	inner conversation_domain.CampaignWorkspaceResolver
}

func (a callRouletteCampaignResolverAdapter) GetEntryWorkspaceID(entryID string) (string, error) {
	return a.inner.GetEntryWorkspaceID(entryID, "")
}

func (a callRouletteCampaignResolverAdapter) GetEntryDepartmentID(entryID string) (string, error) {
	return a.inner.GetEntryDepartmentID(entryID, "")
}

func (a callRouletteCampaignResolverAdapter) GetEntrySIPTrunkID(_ string) (string, error) {
	return "", nil
}

type callRouletteConfigAdapter struct {
	inner workspace_config.Repository
}

func (a callRouletteConfigAdapter) GetByWorkspaceID(workspaceID string) (bool, error) {
	cfg, err := a.inner.GetByWorkspaceID(context.Background(), workspaceID)
	if err != nil {
		return false, err
	}
	return cfg != nil, nil
}
