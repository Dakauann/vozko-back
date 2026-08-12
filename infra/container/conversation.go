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

	// Sending has a construction cycle: the hub needs the send use case, which
	// needs the message sender, which needs the hub to broadcast through. The
	// late binding is confined to this one object so every consumer around the
	// ring still takes plain constructor injection. initConversationSenders
	// points it at the real use case.
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
	// Held so registerChannelAdapter can hand it the accumulated registry; this
	// service is built before any channel exists.
	c.services.messageMarker = messageMarker

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
	// Everything a delivered human reply means to the conversation, shared by
	// every send surface. Required dependencies are validated here, so a missing
	// one stops the boot instead of silently costing every reply its status
	// transition and its timeline entry.
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
			log.Printf("[whatsapp-calls] WARNING: media UDP mux port %d is INSIDE the SIP RTP range %d-%d, choose a port outside it.",
				muxPort, c.cfg.SIPTrunkRTPPortStart, c.cfg.SIPTrunkRTPPortEnd)
		}
		if bound, err := media.EnableSharedUDPMux(muxPort); err != nil {
			log.Printf("[whatsapp-calls] WARNING: could not bind media UDP mux on :%d (%v), WhatsApp media falls back to per-call sockets, which does NOT scale. Fix WHATSAPP_MEDIA_UDP_MUX_PORT.", muxPort, err)
		} else {
			log.Printf("[whatsapp-calls] WebRTC media UDP mux on port %d, one shared socket for all calls (scales to thousands; SIP RTP %d-%d)",
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

// initConversationSenders builds the message sender and the channel-agnostic AI
// attendance service.
//
// Called from initUseCases rather than from startConversationHub, and that
// ordering is load-bearing: every channel captures c.services.channelAIReply
// while initUseCases wires its runtime, so building it afterwards handed all of
// them a nil. Because the field is a concrete pointer assigned into an interface,
// the result was a NON-nil interface holding a nil pointer — every `!= nil` guard
// passed and the first inbound message that had an agent panicked.
//
// Its dependencies are ready by then: the repositories exist from
// initRepositories, and the AI service and tool registry are built at the top of
// initUseCases, well before any channel runtime.
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
		c.repositories.analysis,
		c.repositories.stage,
		c.redisProvider.SharedState(),
	)
	messageSender.SetCallPermissionRepo(c.repositories.callPermission)
	c.services.messageSender = messageSender

	// Close the send ring: the hub was constructed with the live wrapper, and
	// this is the point where the real use case exists to fill it.
	operatorSend, err := conversation_usecase.NewOperatorSendUseCase(
		messageSender,
		c.repositories.user,
		c.services.operatorSendFinalizer,
	)
	if err != nil {
		log.Fatalf("[container] %v", err)
	}
	c.services.operatorSend = operatorSend
	c.services.liveOperatorSend.Use(operatorSend)

	// Channel-agnostic AI attendance. WhatsApp keeps its own richer pipeline;
	// this serves every adapter-backed channel so a channel gains an agent by
	// wiring rather than by growing AI code.
	c.services.channelAIReply = conversation_usecase.NewChannelAIReplyService(
		c.repositories.agent,
		c.services.ai,
		c.repositories.conversation,
		messageSender,
	)
}

// mustChannelAIReply returns the AI attendance service, refusing to boot if it
// does not exist yet.
//
// The channels capture this BY VALUE while initUseCases wires their runtimes, so
// reading it too early hands them a nil that no `!= nil` check can catch (a nil
// pointer in an interface is not nil). That shipped once and cost every channel
// its agent replies; asserting here turns the same mistake into a boot failure
// that names itself.
func (c *Container) mustChannelAIReply() *conversation_usecase.ChannelAIReplyService {
	if c.services.channelAIReply == nil {
		log.Fatal("[container] channel AI reply service read before initConversationSenders; " +
			"channels would silently never answer with an agent")
	}
	return c.services.channelAIReply
}

func (c *Container) startConversationHub() {
	messageSender := c.services.messageSender

	// The per-conversation automation override, for every channel. Each channel
	// registers its own setter as it initialises; WhatsApp is registered here
	// because its entry repository already exists at this point.
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

// registerChannelAdapter adds one channel's send-side adapter and refreshes
// every consumer of the adapter registry.
//
// Adapters ACCUMULATE: the registry is rebuilt from all channels registered so
// far. Handing each channel's adapter straight to SetChannelAdapters would make
// the last channel wired silently replace the previous ones, disabling their
// send path and window checks.
//
// Both consumers are refreshed together so the composer's "can I reply now?"
// and the actual send path can never disagree.
func (c *Container) registerChannelAdapter(adapter conversation_domain.ChannelAdapter) {
	if adapter == nil {
		return
	}
	c.services.channelAdapters = append(c.services.channelAdapters, adapter)
	registry := conversation_domain.NewAdapterRegistry(c.services.channelAdapters...)

	// Consumers that were built before this channel registered hold the live
	// registry rather than a snapshot, so updating it is what makes the new
	// adapter visible to them.
	c.liveAdapterRegistry().Replace(c.services.channelAdapters...)

	if c.services.messageSender != nil {
		c.services.messageSender.SetChannelAdapters(registry)
	}
	// Read receipts travel the same registry as sends. Refreshed here for the
	// same reason: a snapshot taken at build time would hold only the channels
	// registered before this one.
	if c.services.messageMarker != nil {
		c.services.messageMarker.SetChannelAdapters(registry)
	}
	// The HTTP send endpoint shares the same sender rather than carrying a second
	// per-channel implementation. Without this it stays WhatsApp-only while its
	// route accepts every known entry type, which fails as a misleading
	// "conversation not found" instead of an honest refusal.
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

// liveAdapterRegistry returns the container's swappable adapter registry,
// creating it on first use.
//
// It exists because channel adapters register one at a time during startup
// while several consumers are constructed in between. A consumer handed a
// snapshot sees only the channels registered so far, and a missing adapter is
// indistinguishable from "this channel cannot send", which is how every
// workflow send node came to be skipped on Instagram and Telegram.
func (c *Container) liveAdapterRegistry() *conversation_domain.LiveAdapterRegistry {
	if c.services.liveChannelAdapters == nil {
		c.services.liveChannelAdapters = conversation_domain.NewLiveAdapterRegistry()
	}
	return c.services.liveChannelAdapters
}

// logChannelCapabilities reports which optional capabilities a channel got.
//
// Shared rather than per-channel because the failure it guards against is not
// channel-specific: every channel wires the same set behind nil checks, and a
// capability that quietly went missing looks identical to a quiet channel.
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
		// Warned, not fatal: a deployment may legitimately run without workflows
		// or media. Naming them beats an operator wondering why nothing fires.
		log.Printf("[%s] WARNING: inactive capabilities: %s (these fail silently at runtime)",
			channel, strings.Join(missing, ", "))
	}
}
