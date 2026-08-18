package container

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/brand"
	balance_domain "vozko/domain/balance"
	redisCache "vozko/infra/cache"

	"vozko/domain/messaging"
	"vozko/domain/tools"
	"vozko/domain/voip"
	workflow_domain "vozko/domain/workflow"
	workspace_domain "vozko/domain/workspace"
	workspace_pricing_domain "vozko/domain/workspace/workspace_pricing"

	agent_domain "vozko/domain/agent"
	domainmcp "vozko/domain/agent/mcp"
	conversation_domain "vozko/domain/conversation"
	label_domain "vozko/domain/label"
	media_domain "vozko/domain/media"
	rag_domain "vozko/domain/rag"
	shortlink_domain "vozko/domain/shortlink"
	businessphone_domain "vozko/domain/whatsapp/business_phone"
	template_domain "vozko/domain/whatsapp/template"
	wc_entry_domain "vozko/domain/whatsapp_campaign_entry"
	workspace_department_domain "vozko/domain/workspace/workspace_department"
	openrouter_service "vozko/infra/ai/openrouter"
	rag_infra "vozko/infra/ai/rag"
	"vozko/infra/alerting"
	asaas_service "vozko/infra/asaas"
	dialer_infra "vozko/infra/dialer"
	media_infra "vozko/infra/media"
	"vozko/infra/netguard"
	notification_service "vozko/infra/notifications"
	branch_repository "vozko/infra/repositories/branch"
	telemetry_dedupe_repository "vozko/infra/repositories/telemetry_dedupe"
	shortlink_infra "vozko/infra/shortlink"
	telephony_infra "vozko/infra/telephony"
	businessphone_infra "vozko/infra/whatsapp/business_phone"
	"vozko/infra/whisper"
	address_usecase "vozko/usecases/address"
	affiliate_usecase "vozko/usecases/affiliate"
	agent_usecase "vozko/usecases/agent"
	agentloop "vozko/usecases/agentloop"
	"vozko/usecases/agentturn"
	aichat_usecase "vozko/usecases/aichat"
	analysis_usecase "vozko/usecases/analysis"
	analytics_usecase "vozko/usecases/analytics"
	attendance_usecase "vozko/usecases/attendance"
	auth_usecase "vozko/usecases/auth"
	balance_usecase "vozko/usecases/balance"
	billing_usecase "vozko/usecases/billing"
	branch_usecase "vozko/usecases/branch"
	business_metrics_usecase "vozko/usecases/business_metrics"
	calendar_usecase "vozko/usecases/calendar"
	calls_usecase "vozko/usecases/calls"
	calls_cdr_usecase "vozko/usecases/calls_cdr"
	calls_query_usecase "vozko/usecases/calls_query"
	cart_usecase "vozko/usecases/cart"
	category_usecase "vozko/usecases/category"
	cep_usecase "vozko/usecases/cep"
	coexistence_usecase "vozko/usecases/coexistence"
	config_usecase "vozko/usecases/config"
	conversation_usecase "vozko/usecases/conversation"
	"vozko/usecases/conversation/loopguard"
	ce_usecase "vozko/usecases/conversation_event"
	copilot_usecase "vozko/usecases/copilot"
	copilottools "vozko/usecases/copilot/copilottools"
	crm_telemetry_usecase "vozko/usecases/crm_telemetry"
	customfield_usecase "vozko/usecases/customfield"
	dialer_usecase "vozko/usecases/dialer"
	ia_usecase "vozko/usecases/inbox_assignment"
	insurance_usecase "vozko/usecases/insurance"
	invoice_usecase "vozko/usecases/invoice"
	issues_usecase "vozko/usecases/issues"
	label_usecase "vozko/usecases/label"
	media_usecase "vozko/usecases/media"
	msg_shortcut_usecase "vozko/usecases/message_shortcut"
	notification_usecase "vozko/usecases/notification"
	opportunity_usecase "vozko/usecases/opportunity"
	order_usecase "vozko/usecases/order"
	payment_usecase "vozko/usecases/payment"
	pipeline_usecase "vozko/usecases/pipeline"
	product_usecase "vozko/usecases/product"
	property_usecase "vozko/usecases/property"
	rag_usecase "vozko/usecases/rag"
	savedview_usecase "vozko/usecases/savedview"
	shipping_usecase "vozko/usecases/shipping"
	shop_usecase "vozko/usecases/shop"
	shortlink_usecase "vozko/usecases/shortlink"
	sip_trunk_usecase "vozko/usecases/sip_trunk"
	stage_usecase "vozko/usecases/stage"
	si_usecase "vozko/usecases/support_inbox"
	telephony_usecase "vozko/usecases/telephony"
	ticket_usecase "vozko/usecases/ticket"
	tools_usecase "vozko/usecases/tools"
	user_usecase "vozko/usecases/user"
	webhook_usecase "vozko/usecases/webhook"
	businessphone_usecase "vozko/usecases/whatsapp/business_phone"
	whatsapp_template_usecase "vozko/usecases/whatsapp/template"
	waba_usecase "vozko/usecases/whatsapp/waba"
	wc_usecase "vozko/usecases/whatsapp_campaign"
	workflow_usecase "vozko/usecases/workflow"
	workspace_usecase "vozko/usecases/workspace"
	workspace_addon_usecase "vozko/usecases/workspace_addon"
	workspace_config_usecase "vozko/usecases/workspace_config"
	workspace_department_usecase "vozko/usecases/workspace_department"
	workspace_phone_access_usecase "vozko/usecases/workspace_phone_access"
	workspace_plan_usecase "vozko/usecases/workspace_plan"
	workspace_pricing_usecase "vozko/usecases/workspace_pricing"
	workspace_template_access_usecase "vozko/usecases/workspace_template_access"
)

// Per-account failed-login throttle (the brute-force defence that, unlike the
// per-IP backstop, does not collide for users on a shared NAT). Generous enough
// that ordinary mistyped passwords never trip it; resets on a successful login.
const (
	loginFailureThreshold = 10
	loginFailureWindow    = 15 * time.Minute
)

func (c *Container) initUseCases(consumeWhatsappTemplateUC balance_domain.ConsumeWhatsappTemplateUseCase) {

	searchCEPUC := cep_usecase.NewSearchCEPUseCase(c.repositories.cep, http.DefaultClient)

	recordMetricUC := business_metrics_usecase.NewPublishMetricUseCase(c.services.metricsQueuePub)

	openrouterCfg := openrouter_service.Config{
		APIKey:       c.cfg.OpenRouterAPIKey,
		DefaultModel: c.cfg.OpenRouterDefaultModel,
		HTTPReferer:  c.cfg.OpenRouterHTTPReferer,
		XTitle:       c.cfg.OpenRouterXTitle,
	}

	queryKnowledgeBaseUC := rag_usecase.NewQueryKnowledgeBaseUseCase(c.services.ragEmbedding, c.repositories.ragVector, c.repositories.ragChunk, c.repositories.ragDocument, c.repositories.ragKnowledgeBase)
	queryAgentKBUC := rag_usecase.NewQueryAgentKnowledgeBaseUseCase(c.repositories.ragAgentKB, queryKnowledgeBaseUC)
	ragService := rag_usecase.NewRAGService(queryKnowledgeBaseUC, queryAgentKBUC)

	// Reschedule engine (reagendamento): reused by both the AI tool and the workflow
	// node executor so the move logic lives in one place.
	rescheduleEventUC := calendar_usecase.NewRescheduleEventUseCase(
		c.repositories.calendar,
		c.services.googleCalendar,
		calendar_usecase.NewUpdateEventUseCase(c.repositories.calendar, c.services.googleCalendar),
	)

	// The messaging tools reach every channel through the live adapter registry.
	// Without it they stay WhatsApp-only, and since the agent turn now offers
	// them on Telegram and Instagram too, they would be offered and then fail.
	optionsTool := tools_usecase.NewSendWhatsappButtonMessageToolUseCase(context.Background(), c.services.whatsappClientFactory)
	mediaTool := tools_usecase.NewSendWhatsappMediaToolUseCase(context.Background(), c.services.whatsappClientFactory, c.repositories.media)
	for _, h := range []tools.Handler{optionsTool, mediaTool} {
		if setter, ok := h.(interface {
			SetAdapters(conversation_domain.AdapterRegistry)
		}); ok {
			setter.SetAdapters(c.liveAdapterRegistry())
		}
	}

	// Built before the tool registry because the memory tool is one of its
	// consumers; the same values are spread into the useCases literal below and
	// handed to the agentturn assembler, so agents and operators share one
	// write model. The emitter it captures was wired in wireConversationHub.
	leadMemories := c.buildLeadMemories()

	toolHandlers := []tools.Handler{
		tools_usecase.NewSendEmailToolUseCase(nil),
		optionsTool,
		mediaTool,
		tools_usecase.NewValidateCEPToolUseCase(searchCEPUC),
		tools_usecase.NewHTTPRequestToolUseCase(),
		func() tools.Handler {
			at := tools_usecase.NewConversationAnalysisToolUseCase(c.repositories.analysis, c.repositories.wcEntry)
			if c.services.crmTelemetryEmitter != nil {
				at.SetAnalysisTelemetry(c.services.crmTelemetryEmitter)
			}
			return at
		}(),
		tools_usecase.NewManageEntryStageToolUseCase(c.repositories.stage, c.services.conversationHub),
		tools_usecase.NewManageLeadMemoryToolUseCase(leadMemories.create, leadMemories.update, leadMemories.delete),
		tools_usecase.NewFinishConversationToolUseCase(c.services.conversationStatusUpdater, c.services.conversationHub),
		tools_usecase.NewCheckCalendarAvailabilityToolUseCase(c.repositories.calendar, c.services.googleCalendar),
		tools_usecase.NewScheduleMeetingToolUseCase(c.repositories.calendar, c.services.googleCalendar),
		tools_usecase.NewRescheduleMeetingToolUseCase(rescheduleEventUC),
		tools_usecase.NewSearchKnowledgeBaseToolUseCase(ragService),
	}
	c.services.toolRegistry = tools_usecase.NewService(toolHandlers...)
	if c.mcpRegistry != nil {
		c.services.toolRegistry = tools_usecase.NewCompositeToolService(c.services.toolRegistry, c.mcpRegistry, c.mcpCollection)
	}

	c.services.ai = openrouter_service.NewService(openrouterCfg, c.services.toolRegistry, c.services.billingQueuePub)
	log.Printf("AI provider: OpenRouter (model: %s)", c.cfg.OpenRouterDefaultModel)

	// Before ANY channel runtime below: each one captures channelAIReply by
	// value, so it has to exist first or the channel silently gets a nil.
	c.initConversationSenders()

	// The single agent-turn recipe, wired here because this is the first point
	// where all three of its inputs exist: the tool registry and AI service
	// above, and the channel AI service from initConversationSenders.
	//
	// Without it the service can only ever send plain text. Instagram and
	// Telegram agents ran with no tools, no knowledge base and no channel
	// identity, while WhatsApp had all three because it builds its own
	// assembler, and an agent configured with a knowledge base in the UI simply
	// ignored it everywhere else. That is exactly the drift the agentturn
	// package was written to prevent, so it is asserted rather than guarded:
	// a nil here is a wiring bug, not a supported configuration.
	turnAssembler := agentturn.New(c.services.toolRegistry, ragService, leadMemories.list)
	c.mustChannelAIReply().SetAssembler(turnAssembler)

	// The agent simulator: the SAME provider and turn recipe, but its AI
	// service is wired with the sandboxed registry, so every tool call the
	// model makes is intercepted and answered with a canned result. This
	// second service instance IS the sandbox boundary: never hand the
	// simulator c.services.ai.
	simulationAI := openrouter_service.NewService(openrouterCfg, tools_usecase.NewSimulatedToolService(c.services.toolRegistry), c.services.billingQueuePub)
	simulateAgentUC, err := agent_usecase.NewSimulateTurnUseCase(c.repositories.agent, turnAssembler, simulationAI)
	if err != nil {
		log.Fatalf("[container] agent simulator: %v", err)
	}

	llmPriceFetcher := openrouter_service.NewLLMPriceFetcher(c.services.ai.(*openrouter_service.Service), 1*time.Hour)

	createTicketUC := ticket_usecase.NewCreateTicketUseCase(c.repositories.ticket)

	verifyEmailTokenUC := auth_usecase.NewVerifyEmailTokenUseCase(c.repositories.emailVerification)

	createCategoryUC := category_usecase.NewCreateCategoryUseCase(c.repositories.category)
	updateCategoryUC := category_usecase.NewUpdateCategoryUseCase(c.repositories.category)
	deleteCategoryUC := category_usecase.NewDeleteCategoryUseCase(c.repositories.category)
	getCategoryUC := category_usecase.NewGetCategoryUseCase(c.repositories.category)
	listCategoriesUC := category_usecase.NewListCategoriesUseCase(c.repositories.category)
	resolveCreationDepartmentUC := workspace_department_usecase.NewResolveCreationDepartmentUseCase(c.repositories.workspace, c.repositories.workspaceDepartment)

	// The knowledge-base and MCP repositories are the workspace-ownership
	// guards for attached ids: an agent must never be pointed at another
	// workspace's knowledge base, and an id on its own carries no proof.
	createAgentUC := agent_usecase.NewCreateAgentUseCase(c.repositories.agent, c.repositories.businessPhone, c.services.toolRegistry, c.repositories.ragKnowledgeBase, c.mcpCollection, resolveCreationDepartmentUC)
	updateAgentUC := agent_usecase.NewUpdateAgentUseCase(c.repositories.agent, c.repositories.businessPhone, c.services.toolRegistry, c.repositories.ragKnowledgeBase, c.mcpCollection)
	assignAgentDepartmentUC := agent_usecase.NewAssignDepartmentUseCase(c.repositories.agent, resolveCreationDepartmentUC)
	deleteAgentUC := agent_usecase.NewDeleteAgentUseCase(c.repositories.agent)
	getAgentUC := agent_usecase.NewGetAgentUseCase(c.repositories.agent)
	listAgentsUC := agent_usecase.NewListAgentsUseCase(c.repositories.agent)

	createWCCampaignUC := wc_usecase.NewCreateCampaignUseCase(c.repositories.wcCampaign, c.repositories.wcEntry, c.repositories.lead, c.repositories.whatsappTemplate, c.repositories.businessPhone, c.repositories.workspacePhoneAccess, c.repositories.workspaceConfig, c.repositories.leadCampaignSend, resolveCreationDepartmentUC)
	updateWCCampaignUC := wc_usecase.NewUpdateCampaignUseCase(c.repositories.wcCampaign, c.repositories.wcEntry, c.repositories.whatsappTemplate, c.repositories.businessPhone, c.repositories.workspacePhoneAccess)
	assignWCCampaignDepartmentUC := wc_usecase.NewAssignDepartmentUseCase(c.repositories.wcCampaign, resolveCreationDepartmentUC)
	deleteWCCampaignUC := wc_usecase.NewDeleteCampaignUseCase(c.repositories.wcCampaign, c.repositories.wcEntry)
	getWCCampaignUC := wc_usecase.NewGetCampaignUseCase(c.repositories.wcCampaign, c.repositories.wcEntry)
	// Envios + type volume prefer the balance ledger (true charges) when available.
	var waChargeAgg balance_domain.WhatsAppChargeAggregator
	if agg, ok := c.repositories.balance.(balance_domain.WhatsAppChargeAggregator); ok {
		waChargeAgg = agg
	}
	getWCCampaignsSummaryUC := wc_usecase.NewGetSummaryUseCase(
		c.repositories.wcEntry.(wc_entry_domain.SummaryAggregator),
		waChargeAgg,
	)
	listWCEntriesUC := wc_usecase.NewListEntriesUseCase(c.repositories.wcCampaign, c.repositories.wcEntry)
	resetWCCampaignUC := wc_usecase.NewResetCampaignUseCase(c.repositories.wcCampaign, c.repositories.wcEntry)
	clearHistoryWCCampaignUC := wc_usecase.NewClearHistoryUseCase(c.repositories.wcCampaign, c.repositories.conversation)
	deleteEntryWCCampaignUC := wc_usecase.NewDeleteEntryUseCase(c.repositories.wcCampaign, c.repositories.wcEntry)
	updateEntryWCCampaignUC := wc_usecase.NewUpdateEntryUseCase(c.repositories.wcCampaign, c.repositories.wcEntry, c.repositories.lead)
	if c.services.crmTelemetryEmitter != nil {
		updateEntryWCCampaignUC.SetAIToggleTelemetry(c.services.crmTelemetryEmitter)
	}
	addEntriesWCCampaignUC := wc_usecase.NewAddEntriesUseCase(c.repositories.wcCampaign, c.repositories.wcEntry, c.repositories.lead)
	currentSubscriptionUC := workspace_plan_usecase.NewEnsureCurrentWorkspaceSubscriptionUseCase(c.repositories.workspaceSubscription)
	activeSubscriptionUC := workspace_plan_usecase.NewEnsureActiveWorkspaceSubscriptionUseCase(currentSubscriptionUC)
	getWorkspaceSubscriptionUC := workspace_plan_usecase.NewGetWorkspaceSubscriptionUseCase(c.repositories.workspacePlan, c.repositories.workspaceSubscription)
	subscribeWorkspacePlanUC := workspace_plan_usecase.NewSubscribeWorkspaceUseCase(c.repositories.workspacePlan, c.repositories.workspaceSubscription)
	trackReferralUC := affiliate_usecase.NewTrackReferralUseCase(c.repositories.affiliate)
	ensureDefaultWorkspaceUC := workspace_usecase.NewEnsureDefaultWorkspaceUseCase(c.repositories.workspace, c.repositories.workspaceConfig, trackReferralUC)
	createInvoiceUC := invoice_usecase.NewCreateInvoiceUseCase(c.repositories.invoice, c.repositories.user, c.services.asaasService, c.repositories.workspacePricing, currentSubscriptionUC, c.repositories.affiliate, trackReferralUC)
	workspaceReferralReader := workspace_plan_usecase.NewWorkspaceReferralReader(c.repositories.affiliate, newWorkspaceOwnerReaderAdapter(c.repositories.workspace))
	createSubscriptionInvoiceUC := workspace_plan_usecase.NewCreateSubscriptionInvoiceUseCase(c.repositories.workspacePlan, c.repositories.workspaceSubscription, createInvoiceUC, workspaceReferralReader)
	planPricingAdapter := workspace_plan_usecase.NewPlanPricingAdapter(c.repositories.workspaceSubscription, c.repositories.workspacePlan)
	pricer := workspace_pricing_domain.NewPricer(c.repositories.workspacePricing, workspace_pricing_domain.WithLLMPriceFetcher(llmPriceFetcher), workspace_pricing_domain.WithPlanPricingProvider(planPricingAdapter))
	listWCCampaignsUC := wc_usecase.NewListCampaignsUseCase(
		c.repositories.wcCampaign,
		c.repositories.wcEntry,
		c.repositories.whatsappTemplate,
	)

	cachedBalanceChecker := balance_usecase.NewCachedBalanceChecker(
		c.repositories.balance, c.redisProvider.SharedState(), 10*time.Second)
	inflightReserver := balance_usecase.NewInflightReserver(c.redisProvider.SharedState())
	c.services.cachedBalanceChecker = cachedBalanceChecker

	checkBalanceUC := balance_usecase.NewCheckBalanceUseCase(c.repositories.balance)

	messageHistoryManager := conversation_usecase.NewMessageHistoryManagerWithHub(c.repositories.conversation, c.services.conversationHub)
	messageConsumerWCCampaignUC := wc_usecase.NewMessageConsumerUseCase(c.services.wcQueueSub, c.services.wcQueuePub, c.repositories.wcCampaign, c.repositories.wcEntry, c.repositories.whatsappTemplate, c.repositories.businessPhone, c.services.whatsappClientFactory, recordMetricUC, consumeWhatsappTemplateUC, checkBalanceUC, messageHistoryManager, c.redisProvider.SharedState(), c.repositories.workspaceConfig, c.repositories.leadCampaignSend, inflightReserver, cachedBalanceChecker)
	dispatchWCCampaignUC := wc_usecase.NewDispatchCampaignUseCase(c.services.wcQueuePub, c.repositories.wcCampaign, c.repositories.wcEntry, messageConsumerWCCampaignUC, c.redisProvider.SharedState())
	quickSendWCCampaignUC := wc_usecase.NewQuickSendUseCase(c.repositories.wcCampaign, c.repositories.wcEntry, c.repositories.lead, c.services.wcQueuePub, messageConsumerWCCampaignUC, c.redisProvider.SharedState())

	var whisperURLs []string
	if envURLs := whisper.GetURLsFromEnv(whisper.EnvWhisperURLs); len(envURLs) > 0 {
		whisperURLs = envURLs
	} else {
		whisperURLs = []string{"http://localhost:17071"}
	}

	whisperPool, err := whisper.NewPool(whisper.PoolConfig{
		BaseURLs:   whisperURLs,
		Model:      c.cfg.WhisperModel,
		ServerType: whisper.ServerTypeWhisperCpp,
		Logger:     log.New(log.Writer(), "whisper-pool ", log.LstdFlags),
	})
	if err != nil {
		log.Printf("WARNING: Failed to create whisper pool: %v", err)
	} else {
		c.services.whisperPool = whisperPool
		log.Printf("Whisper pool initialized with %d server(s): %v", len(whisperURLs), whisperURLs)
	}

	saveRecordingUC := calls_usecase.NewSaveCallRecordingUseCase(
		c.repositories.callRecording,
		c.repositories.callBilling,
		c.services.fileStorage,
	)

	consumeRecordingUploadUC := calls_usecase.NewConsumeRecordingUploadUseCase(
		c.services.recordingQueueSub,
		saveRecordingUC,
		log.New(log.Writer(), "rec-upload ", log.LstdFlags),
	)
	if err := consumeRecordingUploadUC.Start(); err != nil {
		log.Printf("WARNING: recording upload consumer failed to start: %v", err)
	}

	c.recordingPool = calls_usecase.NewRecordingUploadPool(
		c.services.recordingQueuePub,
		c.services.fileStorage,
		c.cfg.RecordingsStagingDir,
		c.cfg.RecordingUploadWorkers,
		log.New(log.Writer(), "rec-pool ", log.LstdFlags),
	)

	if c.services.sipTrunkManager != nil {
		pool := c.recordingPool
		if setter, ok := c.services.sipTrunkManager.(interface {
			SetMediaSessionRecorder(voip.MediaSessionRecorder)
		}); ok {
			setter.SetMediaSessionRecorder(func(inner voip.MediaSession, dialogID string, meta voip.RecordingMeta) voip.MediaSession {
				callID := dialogID
				if meta.CallID != "" {
					callID = meta.CallID
				}
				return calls_usecase.NewRecordingMediaSession(inner, callID, meta.WorkspaceID, meta.EntryID, meta.LeadID).SetPool(pool)
			})
		}
	}

	insuranceProviders := c.services.insuranceProviders
	describeRequirementsUC := insurance_usecase.NewDescribeRequirementsUseCase(insuranceProviders)

	consumeMetricUC := business_metrics_usecase.NewConsumeMetricUseCase(c.services.metricsQueueSub, c.repositories.businessMetrics, c.services.metrics)
	listMetricsUC := business_metrics_usecase.NewListMetricsUseCase(c.repositories.businessMetrics)
	getMetricsStatsUC := business_metrics_usecase.NewGetMetricsStatsUseCase(c.repositories.businessMetrics)
	getMetricsTimeSeriesUC := business_metrics_usecase.NewGetMetricsTimeSeriesUseCase(c.repositories.businessMetrics)

	listAnalysisUC := analysis_usecase.NewListAnalysisUseCase(c.repositories.analysis)
	getAnalysisStatsUC := analysis_usecase.NewGetAnalysisStatsUseCase(c.repositories.analysis)
	getEntryAnalysisUC := analysis_usecase.NewGetEntryAnalysisUseCase(c.repositories.analysis)

	publishEmailUC := notification_usecase.NewPublishEmailUseCase(c.services.notificationsQueuePub)
	// Request-path senders use a queued EmailService so registration/login/invite
	// never block on the provider; the consumer keeps the real provider-backed one.
	queuedEmailSvc := notification_service.NewQueuedEmailService(publishEmailUC)
	consumeEmailUC := notification_usecase.NewConsumeEmailUseCase(c.services.notificationQueueSub, c.services.notificationsQueuePub, c.services.emailService, c.services.metrics)
	notifierUC := notification_usecase.NewNotifier(
		publishEmailUC,
		notification_service.NewDedup(c.redisProvider.SharedState()),
		notification_usecase.NewOwnerEmailResolver(c.repositories.workspace, c.repositories.user),
	)
	dashboardURL := strings.TrimRight(os.Getenv("FRONTEND_URL"), "/")
	if dashboardURL == "" {
		dashboardURL = brand.Active().SiteURL
	}

	var lowBalanceLister balance_domain.LowBalanceLister
	if l, ok := c.repositories.balance.(balance_domain.LowBalanceLister); ok {
		lowBalanceLister = l
	}
	lowBalanceThresholdMicros := int64(0)
	if v := strings.TrimSpace(os.Getenv("LOW_BALANCE_THRESHOLD_MICROS")); v != "" {
		if parsed, perr := strconv.ParseInt(v, 10, 64); perr == nil {
			lowBalanceThresholdMicros = parsed
		}
	}
	monitorLowBalanceUC := balance_usecase.NewMonitorLowBalanceUseCase(lowBalanceLister, notifierUC, c.redisProvider.SharedState(), dashboardURL, lowBalanceThresholdMicros)

	// The workspace-config source is attached separately because exactly ONE
	// entitlement kind takes its base from there rather than from the plan:
	// unofficial WhatsApp numbers, whose allowance a platform administrator
	// grants per workspace. Without this the kind resolves to zero and no
	// workspace can connect a number however many it was granted — so it is
	// attached to all three resolvers, not just the one the gate happens to use.
	entitlementResolverUC := workspace_addon_usecase.NewEntitlementResolver(
		c.repositories.workspaceSubscription, c.repositories.workspacePlan,
		c.repositories.addonSubscription, c.repositories.workspaceConfig)
	batchEntitlementResolverUC := workspace_addon_usecase.NewBatchEntitlementResolver(
		c.repositories.workspaceSubscription, c.repositories.workspacePlan,
		c.repositories.addonSubscription, c.repositories.workspaceConfig)
	getWorkspaceEntitlementsUC := workspace_addon_usecase.NewGetWorkspaceEntitlementsUseCase(
		c.repositories.workspaceSubscription, c.repositories.workspacePlan,
		c.repositories.addonSubscription, c.repositories.workspaceConfig)

	// Branch (branch) provisioning cap is plan-driven via the entitlement resolver
	// (PlanDefinition.MaxBranches + addons under EntitlementBranches), mirroring
	// how MaxCallChannels gates concurrent calls.
	branchMemberDirectory := branch_repository.NewMemberDirectory(c.repositories.workspace)
	branchProvisioningGate := branch_usecase.NewProvisioningGate(entitlementResolverUC, c.repositories.branch)
	dialog360PartnerForAddons := businessphone_infra.NewDialog360PartnerClient(c.cfg.Dialog360PartnerAPIBase, c.cfg.Dialog360PartnerID, c.cfg.Dialog360PartnerAPIKey, c.cfg.Dialog360SolutionID, &http.Client{Timeout: 30 * time.Second}).WithRateLimit(c.redisProvider.SharedState())
	phoneProvisioningGateUC := businessphone_usecase.NewPhoneProvisioningGate(getWorkspaceEntitlementsUC, c.repositories.ownerPhoneReader)
	addonPhoneDeactivatorUC := businessphone_usecase.NewDeactivateExcessPhonesUseCase(getWorkspaceEntitlementsUC, c.repositories.ownerPhoneReader, c.repositories.businessPhone, dialog360PartnerForAddons).
		WithNotifier(notifierUC, dashboardURL)
	callSlotManager := workspace_domain.NewCallSlotManager(c.redisProvider.SharedState(), c.repositories.workspaceSubscription, c.repositories.workspacePlan, c.replicaID).WithEntitlements(entitlementResolverUC)
	c.services.callSlotManager = callSlotManager

	// Live concurrency board (Redis only, paint path never hits Postgres).
	boardStore := telephony_infra.NewBoardStore(c.redisProvider.SharedState())
	capacityReader := telephony_usecase.NewSlotCapacityReader(c.redisProvider.SharedState(), callSlotManager)
	boardSvc := telephony_usecase.NewBoardService(boardStore, capacityReader)
	c.services.telephonyBoardSync = boardSvc
	c.services.telephonyBoardGet = boardSvc
	c.services.telephonyCapacity = capacityReader

	_ = c.redisProvider.SharedState().SAdd("calls:replicas", c.replicaID)
	callSlotManager.StartupCleanup()
	go callSlotManager.RunHeartbeat(context.Background())
	go callSlotManager.RunPeriodicCleanup(context.Background())

	callAdmissionCoordinator := dialer_usecase.NewCallAdmissionCoordinator(
		cachedBalanceChecker,
		pricer,
		inflightReserver,
		callSlotManager,
		c.repositories.systemConfig,
		log.Default(),
	)
	c.services.callAdmission = callAdmissionCoordinator
	c.services.startOutboundCall = dialer_usecase.NewStartOutboundCallUseCase(
		c.services.crmCallSource,
		c.services.conversationHistory,
		callAdmissionCoordinator,
	)
	c.services.endOutboundCall = dialer_usecase.NewEndOutboundCallUseCase(callAdmissionCoordinator)

	c.services.dialerLifecycle = dialer_usecase.NewOutboundCallLifecycleRunner(
		callAdmissionCoordinator,
		cachedBalanceChecker,
		inflightReserver,
		c.services.billingQueuePub,
		log.Default(),
	)

	// Temporary raw CDR until board-aware use cases are assigned below;
	// re-wired after c.useCases.startCall is constructed.
	c.services.dialerLifecycle.SetCDRStart(
		calls_cdr_usecase.NewStartCallUseCase(c.repositories.callCDR),
	)
	c.services.dialerLifecycle.SetCDRAnswered(
		calls_cdr_usecase.NewMarkCallAnsweredUseCase(c.repositories.callCDR),
	)
	publishDocProcessingUC := rag_usecase.NewPublishDocumentProcessingUseCase(c.services.ragQueuePub)

	shortlinkHostGuard := netguard.New()
	shortlinkScanner := shortlink_infra.NewThreatScanner(c.cfg.GoogleSafeBrowsingAPIKey, "", nil)
	shortlinkUA := shortlink_infra.NewUAResolver()
	shortlinkGeo := shortlink_infra.NewEdgeGeoResolver()
	shortlinkQR := shortlink_infra.NewQRGenerator()
	shortlinkBaseHost := shortlink_domain.TargetHost(c.cfg.ShortLinkBaseURL)
	shortlinkUniqueWindow := 30 * 24 * time.Hour

	creditBalanceUC := balance_usecase.NewCreditBalanceUseCase(c.repositories.balance)
	debitBalanceUC := balance_usecase.NewDebitBalanceUseCase(c.repositories.balance)

	affiliateExchangeRateProvider := newAffiliateExchangeRateAdapter(c.repositories.workspacePricing)
	affiliateRecordEarningUC := affiliate_usecase.NewRecordEarningUseCase(c.repositories.affiliate, affiliateExchangeRateProvider)

	// Unified monthly billing: one Asaas invoice per workspace (plan + active channel addons). This is
	// the full replacement for the old wallet-debit addon renewal. opsAlerter is the high-severity sink
	// for an unconfirmed 360dialog cancellation or a reconciliation divergence.
	opsAlerter := alerting.NewLogOpsAlerter()
	emitMonthlyInvoicesUC := billing_usecase.NewEmitMonthlyInvoicesUseCase(c.repositories.workspaceSubscription, c.repositories.workspacePlan, c.repositories.addonSubscription, c.repositories.workspace, c.repositories.workspacePricing, createInvoiceUC)
	confirmMonthlyBillingUC := billing_usecase.NewConfirmMonthlyBillingUseCase(c.repositories.workspaceSubscription, c.repositories.addonSubscription).WithReactivation(addonPhoneDeactivatorUC)
	cancelSweepUC := billing_usecase.NewCancelSweepUseCase(c.repositories.invoice, c.repositories.workspaceSubscription, c.repositories.addonSubscription, addonPhoneDeactivatorUC, opsAlerter)
	vendorChannelReconcilerUC := businessphone_usecase.NewReconcileVendorChannelsUseCase(dialog360PartnerForAddons, c.repositories.ownerPhoneReader, opsAlerter)
	channelStatusReconcilerUC := businessphone_usecase.NewReconcileChannelStatusUseCase(dialog360PartnerForAddons, c.repositories.ownerPhoneReader, c.repositories.businessPhone, c.repositories.waba)

	handleAsaasWebhookUC := payment_usecase.NewHandleAsaasWebhookUseCase(c.repositories.payment, c.repositories.order, c.services.emailService, c.repositories.user, createTicketUC, c.repositories.invoice, subscribeWorkspacePlanUC, creditBalanceUC, debitBalanceUC, affiliateRecordEarningUC).WithNotifier(notifierUC, dashboardURL).WithMonthlyBilling(confirmMonthlyBillingUC)
	handleWhatsAppMessageUC := conversation_usecase.NewHandleWhatsAppMessageUseCase(c.services.ai, c.services.whatsappClientFactory, c.repositories.lead, c.repositories.agent, c.services.toolRegistry, messageHistoryManager, c.repositories.conversation, c.repositories.systemConfig, recordMetricUC, c.services.whisperPool, c.repositories.analysis, c.repositories.wcCampaign, c.repositories.wcEntry, c.repositories.businessPhone, c.repositories.leadMessageWindow, c.services.fileStorage, c.repositories.conversationMedia, c.services.conversationHub, c.repositories.stage, media_infra.NewTextExtractorService(
		media_infra.NewTesseractOCR("por+eng"),
		media_infra.NewPDFParser(),
		media_infra.NewDOCXParser(),
		media_infra.NewXLSXParser(),
		media_infra.NewPlainTextParser(),
	), c.redisProvider.SharedState(), ragService, cachedBalanceChecker, llmPriceFetcher, consumeWhatsappTemplateUC)
	handleTemplateWebhookUC := whatsapp_template_usecase.NewHandleTemplateWebhook(c.repositories.whatsappTemplate)
	// Shared by the PATCH /header-media endpoint and reused by template create so a
	// media-header template always has its WhatsApp media id minted (URL -> /media
	// upload -> id, linked to the template) instead of only the public URL.
	setHeaderMediaUC := whatsapp_template_usecase.NewSetTemplateHeaderMediaUseCase(c.repositories.whatsappTemplate, c.services.whatsappClientFactory)
	handlePhoneWebhookUC := businessphone_usecase.NewHandlePhoneWebhook(c.repositories.businessPhone, c.repositories.waba).WithNotifier(notifierUC, dashboardURL)

	affiliateStatsUC := affiliate_usecase.NewGetAffiliateStatsUseCase(c.repositories.affiliate)

	affiliateWalletValidator := asaas_service.NewWalletValidator(c.services.asaasService)

	// Single source of truth for member-visibility policy, shared by the HTTP
	// assignable-members endpoint and the realtime conversation-assign guard.
	memberVisibilityUC := workspace_usecase.NewMemberVisibilityUseCase(c.repositories.workspace, c.repositories.workspaceDepartment, c.repositories.workspaceConfig)

	// CRM opportunity (sales-deal) board + workspace custom field definitions.
	customFieldSvc := customfield_usecase.NewService(c.repositories.customField)
	opportunitySvc := opportunity_usecase.NewService(c.repositories.opportunity, c.repositories.opportunityLink, c.repositories.customField)

	// Built here rather than inside initConversationSenders: its inputs exist by
	// then, but the useCases struct below does not, so writing onto it from
	// there dereferenced a nil pointer at boot.
	scheduledMessages := c.buildScheduledMessages()

	c.useCases = &useCases{
		scheduleMessage:          scheduledMessages.schedule,
		rescheduleMessage:        scheduledMessages.reschedule,
		cancelScheduledMessage:   scheduledMessages.cancel,
		listScheduledMessages:    scheduledMessages.list,
		dispatchScheduledMessage: scheduledMessages.dispatch,
		consumeScheduledMessage:  scheduledMessages.consume,
		sweepScheduledMessages:   scheduledMessages.sweep,
		purgeScheduledMessages:   scheduledMessages.purge,

		createLeadMemory: leadMemories.create,
		updateLeadMemory: leadMemories.update,
		deleteLeadMemory: leadMemories.delete,
		listLeadMemories: leadMemories.list,

		opportunity:           opportunitySvc,
		customField:           customFieldSvc,
		consumeNotifications:  consumeEmailUC,
		publishEmail:          publishEmailUC,
		notifier:              notifierUC,
		dashboardURL:          dashboardURL,
		monitorLowBalance:     monitorLowBalanceUC,
		createProduct:         product_usecase.NewCreateProductUseCase(c.repositories.product, c.repositories.category, c.repositories.shop),
		updateProduct:         product_usecase.NewUpdateProductUseCase(c.repositories.product, c.repositories.category, c.repositories.shop),
		launchVariantStock:    product_usecase.NewLaunchVariantStockUseCase(c.repositories.product, c.services.inventory),
		handleAsaasWebhook:    handleAsaasWebhookUC,
		handleWhatsAppMessage: handleWhatsAppMessageUC,
		getProduct:            product_usecase.NewGetProductUseCase(c.repositories.product),
		listProducts:          product_usecase.NewListProductsUseCase(c.repositories.product),
		searchProducts:        product_usecase.NewSearchProductsUseCase(c.repositories.product),

		createProperty:   property_usecase.NewCreatePropertyUseCase(c.repositories.property, c.repositories.shop),
		updateProperty:   property_usecase.NewUpdatePropertyUseCase(c.repositories.property, c.repositories.shop),
		getProperty:      property_usecase.NewGetPropertyUseCase(c.repositories.property),
		listProperties:   property_usecase.NewListPropertiesUseCase(c.repositories.property),
		searchProperties: property_usecase.NewSearchPropertiesUseCase(c.repositories.property),
		deleteProperty:   property_usecase.NewDeletePropertyUseCase(c.repositories.property),

		createCategory:        createCategoryUC,
		updateCategory:        updateCategoryUC,
		deleteCategory:        deleteCategoryUC,
		getCategory:           getCategoryUC,
		listCategories:        listCategoriesUC,
		createAgent:           createAgentUC,
		simulateAgentTurn:     simulateAgentUC,
		updateAgent:           updateAgentUC,
		assignAgentDepartment: assignAgentDepartmentUC,
		deleteAgent:           deleteAgentUC,
		getAgent:              getAgentUC,
		listAgents:            listAgentsUC,

		createWCCampaign:                 createWCCampaignUC,
		updateWCCampaign:                 updateWCCampaignUC,
		assignWCCampaignDepartment:       assignWCCampaignDepartmentUC,
		deleteWCCampaign:                 deleteWCCampaignUC,
		getWCCampaign:                    getWCCampaignUC,
		listWCCampaigns:                  listWCCampaignsUC,
		getWCCampaignsSummary:            getWCCampaignsSummaryUC,
		ensureOrganicCoexistenceCampaign: wc_usecase.NewEnsureOrganicCoexistenceCampaignUseCase(c.repositories.wcCampaign),
		listWCEntries:                    listWCEntriesUC,
		resetWCCampaign:                  resetWCCampaignUC,
		clearHistoryWCCampaign:           clearHistoryWCCampaignUC,
		dispatchWCCampaign:               dispatchWCCampaignUC,
		messageConsumerWCCampaign:        messageConsumerWCCampaignUC,
		deleteEntryWCCampaign:            deleteEntryWCCampaignUC,
		updateEntryWCCampaign:            updateEntryWCCampaignUC,
		addEntriesWCCampaign:             addEntriesWCCampaignUC,
		quickSendWCCampaign:              quickSendWCCampaignUC,

		findUserByID:             user_usecase.NewFindUserByIDUseCase(c.repositories.user),
		updateUser:               user_usecase.NewUpdateUserUseCase(c.repositories.user),
		deleteUser:               user_usecase.NewDeleteUserUseCase(c.repositories.user, c.repositories.workspace, c.repositories.session, c.services.password, c.redisProvider.SharedState()),
		listUsers:                user_usecase.NewListUsersUseCase(c.repositories.user),
		updateUserRole:           user_usecase.NewUpdateUserRoleUseCase(c.repositories.user),
		getWorkspaceSubscription: getWorkspaceSubscriptionUC,

		uploadMedia: media_usecase.NewUploadMediaUseCase(
			c.repositories.media,
			c.services.fileStorage,
			media_infra.NewHoldMusicTranscoder(),
			media_usecase.NewHoldMusicQuotaGate(c.repositories.workspaceSubscription, c.repositories.workspacePlan, c.repositories.media),
		),
		listMedia:       media_usecase.NewListMediaUseCase(c.repositories.media),
		getMedia:        media_usecase.NewGetMediaUseCase(c.repositories.media),
		deleteHoldMusic: media_usecase.NewDeleteHoldMusicUseCase(c.repositories.media, c.repositories.workspaceConfig),

		getCart:           cart_usecase.NewGetCartUseCase(c.repositories.cart),
		clearCart:         cart_usecase.NewClearCartUseCase(c.repositories.cart),
		addToCart:         cart_usecase.NewAddToCartUseCase(c.repositories.cart, c.repositories.product, c.repositories.user, c.services.pricingService),
		removeFromCart:    cart_usecase.NewRemoveFromCartUseCase(c.repositories.cart),
		updateCartItem:    cart_usecase.NewUpdateCartItemUseCase(c.repositories.cart, c.repositories.product, c.repositories.user, c.services.pricingService),
		decrementCartItem: cart_usecase.NewDecrementCartItemUseCase(c.repositories.cart),

		createAddress: address_usecase.NewCreateAddressUseCase(c.repositories.address),
		getAddresses:  address_usecase.NewGetAddressesUseCase(c.repositories.address),
		updateAddress: address_usecase.NewUpdateAddressUseCase(c.repositories.address),
		deleteAddress: address_usecase.NewDeleteAddressUseCase(c.repositories.address),

		checkout:           order_usecase.NewCheckoutUseCase(c.repositories.order, c.repositories.cart, c.repositories.address, c.repositories.product, c.repositories.payment, c.repositories.paymentSplit, c.services.documentValidator, c.services.asaasService, c.services.emailService, c.repositories.user, c.services.pricingService),
		getOrder:           order_usecase.NewGetOrderUseCase(c.repositories.order),
		listOrders:         order_usecase.NewListOrdersUseCase(c.repositories.order),
		cancelExpiredOrder: order_usecase.NewCancelExpiredOrderUseCase(c.repositories.order, c.repositories.payment, c.services.emailService, c.repositories.user, c.repositories.product),

		searchCEP: searchCEPUC,

		credentialsLogin: auth_usecase.NewCredentialsLoginUseCase(c.repositories.user, c.services.password, c.services.tokenService, c.repositories.session, publishEmailUC, recordMetricUC).
			WithFailureThrottle(redisCache.NewFailureThrottle(c.redisProvider.SharedState(), "loginfail", loginFailureThreshold, loginFailureWindow)).
			WithNotifier(notifierUC, dashboardURL),
		sendEmailVerification: auth_usecase.NewSendEmailVerificationUseCase(c.repositories.emailVerification, queuedEmailSvc),
		verifyEmailToken:      verifyEmailTokenUC,
		register:              auth_usecase.NewRegisterUseCase(c.repositories.user, c.services.password, c.services.tokenService, c.repositories.session, queuedEmailSvc, c.services.documentValidator, verifyEmailTokenUC, c.repositories.emailVerification, c.repositories.customer, recordMetricUC, ensureDefaultWorkspaceUC),
		adminRegister:         auth_usecase.NewAdminRegisterUseCase(c.repositories.user, c.services.password, queuedEmailSvc, c.services.documentValidator, c.repositories.customer, recordMetricUC, ensureDefaultWorkspaceUC),
		refreshToken:          auth_usecase.NewRefreshTokenUseCase(c.repositories.user, c.services.tokenService, c.repositories.session, c.redisProvider.SharedState()),
		requestPasswordReset:  auth_usecase.NewRequestPasswordResetUseCase(c.repositories.user, c.repositories.passwordResetToken, queuedEmailSvc),
		resetPassword:         auth_usecase.NewResetPasswordUseCase(c.repositories.user, c.repositories.passwordResetToken, c.services.password, c.repositories.session, c.redisProvider.SharedState()).WithNotifier(notifierUC, dashboardURL),
		changePassword:        auth_usecase.NewChangePasswordUseCase(c.repositories.user, c.services.password, c.repositories.session, c.redisProvider.SharedState()).WithNotifier(notifierUC, dashboardURL),
		logout:                auth_usecase.NewLogoutUseCase(c.repositories.session, c.redisProvider.SharedState()),
		logoutAll:             auth_usecase.NewLogoutAllUseCase(c.repositories.user, c.repositories.session, c.redisProvider.SharedState()),
		listSessions:          auth_usecase.NewListSessionsUseCase(c.repositories.session),
		revokeSession:         auth_usecase.NewRevokeSessionUseCase(c.repositories.session, c.redisProvider.SharedState()),

		createPaymentSplit: payment_usecase.NewCreatePaymentSplitUseCase(c.repositories.paymentSplit),
		updatePaymentSplit: payment_usecase.NewUpdatePaymentSplitUseCase(c.repositories.paymentSplit),
		deletePaymentSplit: payment_usecase.NewDeletePaymentSplitUseCase(c.repositories.paymentSplit),
		getPaymentSplit:    payment_usecase.NewGetPaymentSplitUseCase(c.repositories.paymentSplit),
		listPaymentSplits:  payment_usecase.NewListPaymentSplitsUseCase(c.repositories.paymentSplit),

		createTicket:        createTicketUC,
		listTickets:         ticket_usecase.NewListTicketsUseCase(c.repositories.ticket),
		listUserTickets:     ticket_usecase.NewListUserTicketsUseCase(c.repositories.ticket),
		getTicketByOrder:    ticket_usecase.NewGetTicketByOrderUseCase(c.repositories.ticket),
		uploadTicketDoc:     ticket_usecase.NewUploadTicketDocumentUseCase(c.repositories.ticket, c.services.ticketFileStorage),
		updateTicketStatus:  ticket_usecase.NewUpdateTicketStatusUseCase(c.repositories.ticket, c.repositories.user, c.services.emailService),
		generateTicketLabel: ticket_usecase.NewGenerateLabelUseCase(c.repositories.ticket, c.repositories.order, c.repositories.address),

		getShippingAuthorizationURL: shipping_usecase.NewGetAuthorizationURL(c.services.shippingGateways),
		connectShippingAccount:      shipping_usecase.NewConnectProviderAccount(c.repositories.shippingAccount, c.services.shippingGateways),
		listShippingAccounts:        shipping_usecase.NewListProviderAccounts(c.repositories.shippingAccount),
		calculateFreight:            shipping_usecase.NewCalculateFreight(c.repositories.shippingAccount, c.services.shippingGateways, 5*time.Minute),

		quoteInsurance:                insurance_usecase.NewQuoteUseCase(c.repositories.insurance, insuranceProviders, recordMetricUC),
		listInsuranceQuotations:       insurance_usecase.NewListUserQuotationsUseCase(c.repositories.insurance),
		getInsuranceQuotation:         insurance_usecase.NewGetQuotationUseCase(c.repositories.insurance),
		listInsurancePolicies:         insurance_usecase.NewListPoliciesUseCase(),
		describeInsuranceRequirements: describeRequirementsUC,

		listWhatsAppTemplates:         whatsapp_template_usecase.NewListTemplatesUseCase(c.repositories.whatsappTemplate),
		getWhatsAppTemplate:           whatsapp_template_usecase.NewGetTemplateUseCase(c.repositories.whatsappTemplate),
		syncWhatsAppTemplates:         whatsapp_template_usecase.NewSyncTemplatesUseCase(c.repositories.whatsappTemplate, c.services.whatsappClientFactory),
		reconcileWhatsAppTemplates:    whatsapp_template_usecase.NewReconcileTemplatesUseCase(c.repositories.businessPhone, whatsapp_template_usecase.NewSyncTemplatesUseCase(c.repositories.whatsappTemplate, c.services.whatsappClientFactory)),
		reconcileWhatsAppEntitlements: businessphone_usecase.NewReconcileWhatsAppEntitlementsUseCase(c.repositories.ownerPhoneReader, batchEntitlementResolverUC, addonPhoneDeactivatorUC),
		syncWhatsAppTemplate:          whatsapp_template_usecase.NewSyncTemplateUseCase(c.repositories.whatsappTemplate, c.services.whatsappClientFactory),
		sendWhatsAppTemplate:          whatsapp_template_usecase.NewSendTemplateMessageUseCase(c.services.whatsappClientFactory, c.repositories.whatsappTemplate, recordMetricUC, consumeWhatsappTemplateUC),
		createWhatsAppTemplate:        whatsapp_template_usecase.NewCreateTemplateUseCase(c.services.whatsappClientFactory, c.repositories.whatsappTemplate, setHeaderMediaUC),
		replicateWhatsAppTemplate:     whatsapp_template_usecase.NewReplicateTemplateUseCase(c.repositories.whatsappTemplate, c.services.whatsappClientFactory),
		setHeaderMediaWhatsApp:        setHeaderMediaUC,
		deleteWhatsAppTemplate:        whatsapp_template_usecase.NewDeleteTemplateUseCase(c.services.whatsappClientFactory, c.repositories.whatsappTemplate),
		handleTemplateWebhook:         handleTemplateWebhookUC,

		getSystemConfig:    config_usecase.NewGetSystemConfigUseCase(c.repositories.systemConfig),
		updateSystemConfig: config_usecase.NewUpdateSystemConfigUseCase(c.repositories.systemConfig),

		getWorkspaceConfig:         workspace_config_usecase.NewGetWorkspaceConfigUseCase(c.repositories.workspaceConfig),
		updateWorkspaceConfig:      workspace_config_usecase.NewUpdateWorkspaceConfigUseCase(c.repositories.workspaceConfig),
		updateWorkspaceConfigOwner: workspace_config_usecase.NewUpdateWorkspaceConfigOwnerUseCase(c.repositories.workspaceConfig, c.repositories.workspace, dialer_infra.NewHoldMusicTrackValidator(c.repositories.media)),

		recordMetric:         recordMetricUC,
		consumeMetric:        consumeMetricUC,
		listMetrics:          listMetricsUC,
		getMetricsStats:      getMetricsStatsUC,
		getMetricsTimeSeries: getMetricsTimeSeriesUC,

		listAnalysis:     listAnalysisUC,
		getAnalysisStats: getAnalysisStatsUC,
		getEntryAnalysis: getEntryAnalysisUC,

		createShop: shop_usecase.NewCreateShopUseCase(c.repositories.shop, c.repositories.media),
		updateShop: shop_usecase.NewUpdateShopUseCase(c.repositories.shop, c.repositories.media),
		deleteShop: shop_usecase.NewDeleteShopUseCase(c.repositories.shop),
		getShop:    shop_usecase.NewGetShopUseCase(c.repositories.shop),
		listShops:  shop_usecase.NewListShopsUseCase(c.repositories.shop),

		listBusinessPhones:         businessphone_usecase.NewListUseCase(c.repositories.businessPhone),
		getBusinessPhone:           businessphone_usecase.NewGetUseCase(c.repositories.businessPhone),
		syncBusinessPhone:          businessphone_usecase.NewSyncPhoneNumberUseCase(c.repositories.businessPhone, c.services.businessPhoneMetaAPI),
		registerBusinessPhone:      businessphone_usecase.NewRegisterPhoneUseCase(c.repositories.businessPhone, c.services.businessPhoneMetaAPI),
		deregisterBusinessPhone:    businessphone_usecase.NewDeregisterPhoneUseCase(c.repositories.businessPhone, c.services.businessPhoneMetaAPI),
		releaseBusinessPhone:       businessphone_usecase.NewReleasePhoneUseCase(c.repositories.businessPhone, c.repositories.waba, c.services.businessPhoneMetaAPI, dialog360PartnerForAddons),
		requestBusinessPhoneVerify: businessphone_usecase.NewRequestVerificationCodeUseCase(c.repositories.businessPhone, c.services.businessPhoneMetaAPI),
		verifyBusinessPhoneCode:    businessphone_usecase.NewVerifyCodeUseCase(c.repositories.businessPhone, c.services.businessPhoneMetaAPI),
		updateBusinessPhoneProfile: businessphone_usecase.NewUpdateBusinessProfileUseCase(c.repositories.businessPhone, c.services.businessPhoneMetaAPI, c.services.whatsappClientFactory),
		getBusinessPhoneProfile:    businessphone_usecase.NewGetBusinessProfileUseCase(c.repositories.businessPhone, c.services.businessPhoneMetaAPI, c.services.whatsappClientFactory),
		onboardEmbeddedSignup:      businessphone_usecase.NewOnboardEmbeddedSignupUseCase(c.repositories.businessPhone, c.repositories.waba, c.services.businessPhoneMetaAPI),
		deleteBusinessPhone:        businessphone_usecase.NewDeletePhoneNumberUseCase(c.repositories.businessPhone),
		unassignBusinessPhoneOwner: businessphone_usecase.NewUnassignOwnerUseCase(c.repositories.businessPhone, c.repositories.ownerPhoneReader, dialog360PartnerForAddons),
		handlePhoneWebhook:         handlePhoneWebhookUC,

		listWABAs: waba_usecase.NewListUseCase(c.repositories.waba),
		getWABA:   waba_usecase.NewGetUseCase(c.repositories.waba),

		createBalance:           balance_usecase.NewCreateBalanceUseCase(c.repositories.balance),
		getBalance:              balance_usecase.NewGetBalanceUseCase(c.repositories.balance),
		creditBalance:           creditBalanceUC,
		debitBalance:            balance_usecase.NewDebitBalanceUseCase(c.repositories.balance),
		listTransactions:        balance_usecase.NewListTransactionsUseCase(c.repositories.balance),
		creditResource:          balance_usecase.NewCreditResourceUseCase(c.repositories.balance),
		debitResource:           balance_usecase.NewDebitResourceUseCase(c.repositories.balance),
		consumeWhatsappTemplate: consumeWhatsappTemplateUC,
		checkBalance:            balance_usecase.NewCheckBalanceUseCase(c.repositories.balance),
		getFullBalanceSummary:   balance_usecase.NewGetFullBalanceSummaryUseCase(c.repositories.balance),
		getOrCreateBalance:      balance_usecase.NewGetOrCreateBalanceUseCase(c.repositories.balance),
		getOrCreateFullSummary:  balance_usecase.NewGetOrCreateFullBalanceSummaryUseCase(c.repositories.balance),

		createInvoice: createInvoiceUC,
		listInvoices:  invoice_usecase.NewListInvoicesUseCase(c.repositories.invoice),
		getInvoice:    invoice_usecase.NewGetInvoiceUseCase(c.repositories.invoice),

		emitMonthlyInvoices:     emitMonthlyInvoicesUC,
		cancelBillingSweep:      cancelSweepUC,
		vendorChannelReconciler: vendorChannelReconcilerUC,
		channelStatusReconciler: channelStatusReconcilerUC,

		listBillingRecords: calls_usecase.NewListBillingRecordsUseCase(c.repositories.callBilling),

		startCall:    calls_cdr_usecase.NewStartCallUseCase(c.repositories.callCDR),
		completeCall: calls_cdr_usecase.NewCompleteCallUseCase(c.repositories.callCDR),
		getCall:      calls_cdr_usecase.NewGetCallUseCase(c.repositories.callCDR),
		listCalls:    calls_cdr_usecase.NewListCallsUseCase(c.repositories.callCDR),

		billingQuery:       calls_query_usecase.NewBillingQueryUseCase(c.repositories.callBilling),
		callRecordingQuery: calls_query_usecase.NewRecordingQueryUseCase(c.repositories.callRecording),

		getProfitReport:     analytics_usecase.NewGetProfitReportUseCase(c.repositories.analytics),
		getCallAnalytics:    analytics_usecase.NewGetCallAnalyticsUseCase(c.repositories.analytics),
		getAdminOverview:    analytics_usecase.NewGetAdminOverviewUseCase(c.repositories.analytics),
		getPlanContractions: analytics_usecase.NewGetPlanContractionsUseCase(c.repositories.analytics),

		grantTemplateAccess:  workspace_template_access_usecase.NewGrantAccessUseCase(c.repositories.workspaceTemplateAccess),
		revokeTemplateAccess: workspace_template_access_usecase.NewRevokeAccessUseCase(c.repositories.workspaceTemplateAccess),
		listWorkspaceAccess:  workspace_template_access_usecase.NewListWorkspaceAccessUseCase(c.repositories.workspaceTemplateAccess),
		listTemplateAccess:   workspace_template_access_usecase.NewListTemplateAccessUseCase(c.repositories.workspaceTemplateAccess),
		checkTemplateAccess:  workspace_template_access_usecase.NewCheckAccessUseCase(c.repositories.workspaceTemplateAccess),

		grantPhoneAccess:         workspace_phone_access_usecase.NewGrantAccessUseCase(c.repositories.workspacePhoneAccess),
		revokePhoneAccess:        workspace_phone_access_usecase.NewRevokeAccessUseCase(c.repositories.workspacePhoneAccess),
		listWorkspacePhoneAccess: workspace_phone_access_usecase.NewListWorkspaceAccessUseCase(c.repositories.workspacePhoneAccess),
		listPhoneAccess:          workspace_phone_access_usecase.NewListPhoneAccessUseCase(c.repositories.workspacePhoneAccess),
		checkPhoneAccess:         workspace_phone_access_usecase.NewCheckAccessUseCase(c.repositories.workspacePhoneAccess),

		createSIPTrunk:          sip_trunk_usecase.NewCreateUseCase(c.repositories.sipTrunk, c.services.sipTrunkManager, c.cfg.SIPTrunkMaxPerWorkspace, netguard.New()),
		updateSIPTrunk:          sip_trunk_usecase.NewUpdateUseCase(c.repositories.sipTrunk, c.services.sipTrunkManager, netguard.New()),
		deleteSIPTrunk:          sip_trunk_usecase.NewDeleteUseCase(c.repositories.sipTrunk, c.services.sipTrunkManager),
		getSIPTrunk:             sip_trunk_usecase.NewGetUseCase(c.repositories.sipTrunk),
		listSIPTrunks:           sip_trunk_usecase.NewListUseCase(c.repositories.sipTrunk),
		listSIPTrunksByIDs:      sip_trunk_usecase.NewListByIDsUseCase(c.repositories.sipTrunk),
		listAccessibleSIPTrunks: sip_trunk_usecase.NewListAccessibleUseCase(c.repositories.sipTrunk),
		enableSIPTrunk:          sip_trunk_usecase.NewEnableUseCase(c.repositories.sipTrunk, c.services.sipTrunkManager),
		getSIPTrunkStatus:       sip_trunk_usecase.NewGetStatusUseCase(c.services.sipTrunkManager),
		assignOwnerSIPTrunk:     sip_trunk_usecase.NewAssignOwnerUseCase(c.repositories.sipTrunk),

		createBranch:            branch_usecase.NewCreateUseCase(c.repositories.branch, branchMemberDirectory, branchProvisioningGate, c.cfg.SIPRealm),
		updateBranch:            branch_usecase.NewUpdateUseCase(c.repositories.branch),
		getBranch:               branch_usecase.NewGetUseCase(c.repositories.branch),
		listBranchesByWorkspace: branch_usecase.NewListByWorkspaceUseCase(c.repositories.branch),
		listBranchesByUser:      branch_usecase.NewListByUserUseCase(c.repositories.branch),
		deleteBranch:            branch_usecase.NewDeleteUseCase(c.repositories.branch),
		enableBranch:            branch_usecase.NewEnableUseCase(c.repositories.branch),
		rotateBranchSecret:      branch_usecase.NewRotateSecretUseCase(c.repositories.branch, c.cfg.SIPRealm),

		sendConversationMessage: conversation_usecase.NewSendConversationMessageUseCase(
			c.repositories.conversation,
			c.repositories.lead,
			c.repositories.wcEntry,
			c.services.whatsappClientFactory,
			c.services.conversationHub,
			c.repositories.wcCampaign,
			c.services.ai,
			c.services.toolRegistry,
			c.repositories.analysis,
			c.repositories.stage,
			c.redisProvider.SharedState(),
		),
		uploadConversationMedia: conversation_usecase.NewUploadConversationMediaUseCase(c.repositories.conversationMedia, c.services.fileStorage),
		getConversationMedia:    conversation_usecase.NewGetConversationMediaUseCase(c.repositories.conversationMedia),
		searchMessagesByEntry:   conversation_usecase.NewSearchMessagesByEntryUseCase(c.repositories.conversation),
		listConversationEvents:  ce_usecase.NewListEventsUseCase(c.repositories.conversationEvent),

		createStage:          stage_usecase.NewCreateStageUseCase(c.repositories.stage),
		cloneStagesFromGroup: stage_usecase.NewCloneStagesFromGroupUseCase(c.repositories.stageGroup, c.repositories.stage),
		updateStage:          stage_usecase.NewUpdateStageUseCase(c.repositories.stage),
		deleteStage:          stage_usecase.NewDeleteStageUseCase(c.repositories.stage),
		listStages:           stage_usecase.NewListStagesUseCase(c.repositories.stage),
		setInitialStage:      stage_usecase.NewSetInitialStageUseCase(c.repositories.stage),
		assignEntryStage:     stage_usecase.NewAssignEntryStageUseCase(c.repositories.stage),
		removeEntryStage:     stage_usecase.NewRemoveEntryStageUseCase(c.repositories.stage),
		getEntryStage:        stage_usecase.NewGetEntryStageUseCase(c.repositories.stage),
		getBatchEntryStages:  stage_usecase.NewGetBatchEntryStagesUseCase(c.repositories.stage),
		reorderStages:        stage_usecase.NewReorderStagesUseCase(c.repositories.stage),

		createStageGroup: stage_usecase.NewCreateStageGroupUseCase(c.repositories.stageGroup, resolveCreationDepartmentUC),
		updateStageGroup: stage_usecase.NewUpdateStageGroupUseCase(c.repositories.stageGroup, c.repositories.stage),
		deleteStageGroup: stage_usecase.NewDeleteStageGroupUseCase(c.repositories.stageGroup),
		listStageGroups:  stage_usecase.NewListStageGroupsUseCase(c.repositories.stageGroup),
		getStageGroup:    stage_usecase.NewGetStageGroupUseCase(c.repositories.stageGroup),

		createPipeline: pipeline_usecase.NewCreatePipelineUseCase(c.repositories.pipeline),
		updatePipeline: pipeline_usecase.NewUpdatePipelineUseCase(c.repositories.pipeline),
		deletePipeline: pipeline_usecase.NewDeletePipelineUseCase(c.repositories.pipeline),
		listPipelines:  pipeline_usecase.NewListPipelinesUseCase(c.repositories.pipeline),
		getPipeline:    pipeline_usecase.NewGetPipelineUseCase(c.repositories.pipeline),

		createSavedView:     savedview_usecase.NewCreateSavedViewUseCase(c.repositories.savedView),
		updateSavedView:     savedview_usecase.NewUpdateSavedViewUseCase(c.repositories.savedView),
		deleteSavedView:     savedview_usecase.NewDeleteSavedViewUseCase(c.repositories.savedView),
		listSavedViews:      savedview_usecase.NewListSavedViewsUseCase(c.repositories.savedView),
		setDefaultSavedView: savedview_usecase.NewSetDefaultSavedViewUseCase(c.repositories.savedView),

		createLabel:      label_usecase.NewCreateLabelUseCase(c.repositories.label),
		updateLabel:      label_usecase.NewUpdateLabelUseCase(c.repositories.label),
		deleteLabel:      label_usecase.NewDeleteLabelUseCase(c.repositories.label),
		listLabels:       label_usecase.NewListLabelsUseCase(c.repositories.label),
		assignEntryLabel: label_usecase.NewAssignEntryLabelUseCase(c.repositories.label),
		removeEntryLabel: label_usecase.NewRemoveEntryLabelUseCase(c.repositories.label),
		getEntryLabels:   label_usecase.NewGetEntryLabelsUseCase(c.repositories.label),
		reorderLabels:    label_usecase.NewReorderLabelsUseCase(c.repositories.label),

		createMessageShortcut: msg_shortcut_usecase.NewCreateUseCase(c.repositories.messageShortcut),
		updateMessageShortcut: msg_shortcut_usecase.NewUpdateUseCase(c.repositories.messageShortcut),
		deleteMessageShortcut: msg_shortcut_usecase.NewDeleteUseCase(c.repositories.messageShortcut),
		listMessageShortcuts:  msg_shortcut_usecase.NewListUseCase(c.repositories.messageShortcut),
		getByShortcut:         msg_shortcut_usecase.NewGetByShortcutUseCase(c.repositories.messageShortcut),


		createWorkspace:                    workspace_usecase.NewCreateWorkspaceUseCase(c.repositories.workspace),
		getWorkspace:                       workspace_usecase.NewGetWorkspaceUseCase(c.repositories.workspace, c.repositories.user, c.repositories.workspaceSubscription),
		listWorkspaces:                     workspace_usecase.NewListWorkspacesUseCase(c.repositories.workspace, c.repositories.user, c.repositories.workspaceSubscription),
		updateWorkspace:                    workspace_usecase.NewUpdateWorkspaceUseCase(c.repositories.workspace),
		inviteMember:                       workspace_usecase.NewInviteMemberUseCase(c.repositories.workspace, c.repositories.customRole, c.repositories.user, c.repositories.workspaceDepartment, queuedEmailSvc),
		acceptInvite:                       workspace_usecase.NewAcceptInviteUseCase(c.repositories.workspace, c.repositories.workspaceDepartment),
		declineInvite:                      workspace_usecase.NewDeclineInviteUseCase(c.repositories.workspace),
		cancelInvite:                       workspace_usecase.NewCancelInviteUseCase(c.repositories.workspace),
		listInvites:                        workspace_usecase.NewListInvitesUseCase(c.repositories.workspace),
		listWorkspaceInvites:               workspace_usecase.NewListWorkspaceInvitesUseCase(c.repositories.workspace),
		removeMember:                       workspace_usecase.NewRemoveMemberUseCase(c.repositories.workspace),
		updateMemberRole:                   workspace_usecase.NewUpdateMemberRoleUseCase(c.repositories.workspace),
		listWsMembers:                      workspace_usecase.NewListMembersUseCase(c.repositories.workspace),
		listMembersPaginated:               workspace_usecase.NewListMembersPaginatedUseCase(c.repositories.workspace),
		memberVisibility:                   memberVisibilityUC,
		listAssignableMembers:              workspace_usecase.NewListAssignableMembersUseCase(c.repositories.workspace, memberVisibilityUC),
		setMemberPermissions:               workspace_usecase.NewSetMemberPermissionsUseCase(c.repositories.workspace),
		getMemberPermissions:               workspace_usecase.NewGetMemberPermissionsUseCase(c.repositories.workspace),
		listResourcePermissions:            workspace_usecase.NewListResourcePermissionsUseCase(),
		checkWsAccess:                      workspace_usecase.NewCheckAccessUseCase(c.repositories.workspace),
		ensureDefaultWorkspace:             ensureDefaultWorkspaceUC,
		assignResource:                     workspace_usecase.NewAssignResourceUseCase(c.repositories.workspace),
		unassignResource:                   workspace_usecase.NewUnassignResourceUseCase(c.repositories.workspace),
		listResourceAssignments:            workspace_usecase.NewListResourceAssignmentsUseCase(c.repositories.workspace),
		checkResourceAccess:                workspace_usecase.NewCheckResourceAccessUseCase(c.repositories.workspace),
		createCustomRole:                   workspace_usecase.NewCreateCustomRoleUseCase(c.repositories.workspace, c.repositories.customRole),
		listCustomRoles:                    workspace_usecase.NewListCustomRolesUseCase(c.repositories.workspace, c.repositories.customRole),
		updateCustomRole:                   workspace_usecase.NewUpdateCustomRoleUseCase(c.repositories.workspace, c.repositories.customRole),
		deleteCustomRole:                   workspace_usecase.NewDeleteCustomRoleUseCase(c.repositories.workspace, c.repositories.customRole),
		assignCustomRole:                   workspace_usecase.NewAssignCustomRoleUseCase(c.repositories.workspace, c.repositories.customRole),
		getDefaultPricingItems:             workspace_pricing_usecase.NewGetDefaultPricingItemsUseCase(c.repositories.workspacePricing),
		getResolvedPricing:                 workspace_pricing_usecase.NewGetResolvedPricingUseCase(pricer),
		updatePricingItem:                  workspace_pricing_usecase.NewUpdatePricingItemUseCase(c.repositories.workspacePricing),
		getPricingAuditLog:                 workspace_pricing_usecase.NewGetPricingAuditLogUseCase(c.repositories.workspacePricing),
		getExchangeRate:                    workspace_pricing_usecase.NewGetExchangeRateUseCase(c.repositories.workspacePricing),
		updateExchangeRate:                 workspace_pricing_usecase.NewUpdateExchangeRateUseCase(c.repositories.workspacePricing),
		createWorkspacePlan:                workspace_plan_usecase.NewCreatePlanDefinitionUseCase(c.repositories.workspacePlan),
		updateWorkspacePlan:                workspace_plan_usecase.NewUpdatePlanDefinitionUseCase(c.repositories.workspacePlan),
		archiveWorkspacePlan:               workspace_plan_usecase.NewArchivePlanDefinitionUseCase(c.repositories.workspacePlan),
		listWorkspacePlans:                 workspace_plan_usecase.NewListPlanDefinitionsUseCase(c.repositories.workspacePlan),
		getWorkspacePlan:                   workspace_plan_usecase.NewGetPlanDefinitionUseCase(c.repositories.workspacePlan),
		createSubscriptionInvoice:          createSubscriptionInvoiceUC,
		subscribeWorkspacePlan:             subscribeWorkspacePlanUC,
		renewWorkspaceSubscription:         workspace_plan_usecase.NewRenewWorkspaceSubscriptionUseCase(c.repositories.workspacePlan, c.repositories.workspaceSubscription),
		cancelWorkspaceSubscription:        workspace_plan_usecase.NewCancelWorkspaceSubscriptionUseCase(c.repositories.workspacePlan, c.repositories.workspaceSubscription),
		ensureCurrentWorkspaceSubscription: currentSubscriptionUC,
		ensureActiveWorkspaceSubscription:  activeSubscriptionUC,
		expireSubscriptions:                workspace_plan_usecase.NewExpireSubscriptionsUseCase(c.repositories.workspaceSubscription, addonPhoneDeactivatorUC),
		remindExpiringSubscriptions:        workspace_plan_usecase.NewRemindExpiringSubscriptionsUseCase(c.repositories.workspaceSubscription, c.repositories.workspacePlan, notifierUC, dashboardURL),
		setPlanVisibility:                  workspace_plan_usecase.NewSetPlanVisibilityUseCase(c.repositories.workspacePlan),
		listVisiblePlans:                   workspace_plan_usecase.NewListVisiblePlansUseCase(c.repositories.workspacePlan, workspaceReferralReader),
		setPlanExclusiveAffiliate:          workspace_plan_usecase.NewSetPlanExclusiveAffiliateUseCase(c.repositories.workspacePlan, c.repositories.affiliate),
		listExclusivePlansByAffiliateCode:  workspace_plan_usecase.NewListExclusivePlansByAffiliateCodeUseCase(c.repositories.workspacePlan, c.repositories.affiliate),
		listMyExclusivePlans:               workspace_plan_usecase.NewListMyExclusivePlansUseCase(c.repositories.workspacePlan, c.repositories.affiliate),
		workspaceReferralReader:            workspaceReferralReader,
		createAddonDefinition:              workspace_addon_usecase.NewCreateAddonDefinitionUseCase(c.repositories.addonDefinition),
		updateAddonDefinition:              workspace_addon_usecase.NewUpdateAddonDefinitionUseCase(c.repositories.addonDefinition),
		archiveAddonDefinition:             workspace_addon_usecase.NewArchiveAddonDefinitionUseCase(c.repositories.addonDefinition),
		listAddonDefinitions:               workspace_addon_usecase.NewListAddonDefinitionsUseCase(c.repositories.addonDefinition),
		getAddonDefinition:                 workspace_addon_usecase.NewGetAddonDefinitionUseCase(c.repositories.addonDefinition),
		listAvailableAddons:                workspace_addon_usecase.NewListAvailableAddonsUseCase(c.repositories.addonDefinition),
		purchaseAddon:                      workspace_addon_usecase.NewPurchaseAddonUseCase(c.repositories.addonDefinition, c.repositories.addonSubscription, c.repositories.balance, addonPhoneDeactivatorUC),
		previewAddonPurchase:               workspace_addon_usecase.NewPreviewAddonPurchaseUseCase(c.repositories.addonDefinition, c.repositories.addonSubscription),
		cancelAddonSubscription:            workspace_addon_usecase.NewCancelAddonSubscriptionUseCase(c.repositories.addonSubscription),
		listWorkspaceAddons:                workspace_addon_usecase.NewListWorkspaceAddonsUseCase(c.repositories.addonSubscription),
		getWorkspaceEntitlements:           getWorkspaceEntitlementsUC,
		entitlementResolver:                entitlementResolverUC,
		phoneProvisioningGate:              phoneProvisioningGateUC,
		getAttendanceStats:                 attendance_usecase.NewGetAttendanceStatsUseCase(c.repositories.attendance),
		getWindowStats:                     attendance_usecase.NewGetWindowStatsUseCase(c.repositories.attendance),
		getResponseTimeDistribution:        attendance_usecase.NewGetResponseTimeDistributionUseCase(c.repositories.attendance),
		getAIAgentStats:                    attendance_usecase.NewGetAIAgentStatsUseCase(c.repositories.attendance),
		getFRTStats:                        attendance_usecase.NewGetFRTStatsUseCase(c.repositories.attendance),
		getOverview: attendance_usecase.NewGetOverviewUseCaseWithDeps(
			c.repositories.attendance,
			c.repositories.queueEvent,
			c.repositories.agentPresence,
			c.services.dialerSessions,
		),
		getTelephonyOverview: telephony_usecase.NewGetOverviewUseCaseWithDeps(
			c.repositories.telephony,
			c.repositories.queueEvent,
			c.repositories.agentPresence,
			c.services.dialerSessions,
		),
		getTelephonyBoard: c.services.telephonyBoardGet,
		consumeCRMTelemetry: crm_telemetry_usecase.NewConsumerWithDeps(crm_telemetry_usecase.ConsumerDeps{
			QueueSub:  c.services.crmTelemetrySub,
			Events:    c.repositories.conversationEvent,
			History:   c.repositories.assignmentHistory,
			AIRepo:    c.repositories.aiAttendance,
			QueueRepo: c.repositories.queueEvent,
			Presence:  c.repositories.agentPresence,
			Dedupe:    telemetry_dedupe_repository.New(c.db),
			Drops:     crm_telemetry_usecase.NewLogDropRecorder(),
		}),

		createKnowledgeBase:     rag_usecase.NewCreateKnowledgeBaseUseCase(c.repositories.ragKnowledgeBase),
		updateKnowledgeBase:     rag_usecase.NewUpdateKnowledgeBaseUseCase(c.repositories.ragKnowledgeBase),
		deleteKnowledgeBase:     rag_usecase.NewDeleteKnowledgeBaseUseCase(c.repositories.ragKnowledgeBase, c.repositories.ragChunk, c.repositories.ragVector, c.repositories.ragAgentKB),
		getKnowledgeBase:        rag_usecase.NewGetKnowledgeBaseUseCase(c.repositories.ragKnowledgeBase),
		listKnowledgeBases:      rag_usecase.NewListKnowledgeBasesUseCase(c.repositories.ragKnowledgeBase),
		createRAGDocument:       rag_usecase.NewCreateDocumentUseCase(c.repositories.ragDocument, c.repositories.ragKnowledgeBase, publishDocProcessingUC),
		deleteRAGDocument:       rag_usecase.NewDeleteDocumentUseCase(c.repositories.ragDocument, c.repositories.ragChunk, c.repositories.ragVector, c.repositories.ragKnowledgeBase),
		getRAGDocument:          rag_usecase.NewGetDocumentUseCase(c.repositories.ragDocument),
		listRAGDocuments:        rag_usecase.NewListDocumentsUseCase(c.repositories.ragDocument),
		linkAgentKnowledgeBases: rag_usecase.NewLinkAgentKnowledgeBasesUseCase(c.repositories.ragAgentKB, c.repositories.ragKnowledgeBase),
		getAgentKnowledgeBases:  rag_usecase.NewGetAgentKnowledgeBasesUseCase(c.repositories.ragAgentKB, c.repositories.ragKnowledgeBase),
		queryKnowledgeBase:      queryKnowledgeBaseUC,
		publishDocProcessing:    publishDocProcessingUC,
		consumeDocProcessing: rag_usecase.NewConsumeDocumentProcessingUseCase(
			c.services.ragQueueSub,
			c.services.ragQueuePub,
			rag_infra.NewDocumentProcessor(
				c.repositories.ragDocument,
				c.repositories.ragKnowledgeBase,
				c.repositories.ragChunk,
				c.services.ragEmbedding,
				c.services.ragTextChunker,
				c.services.ragTextExtractor,
			),
			c.repositories.ragDocument,
		),

		createShortLink:       shortlink_usecase.NewCreateShortLinkUseCase(c.repositories.shortLink, shortlinkHostGuard, shortlinkScanner, c.services.password, c.cfg.ShortLinkCodeLength, shortlinkBaseHost),
		updateShortLink:       shortlink_usecase.NewUpdateShortLinkUseCase(c.repositories.shortLink, shortlinkHostGuard, shortlinkScanner, c.services.password, c.redisProvider.SharedState(), shortlinkBaseHost),
		getShortLink:          shortlink_usecase.NewGetShortLinkUseCase(c.repositories.shortLink),
		listShortLinks:        shortlink_usecase.NewListShortLinksUseCase(c.repositories.shortLink),
		deleteShortLink:       shortlink_usecase.NewDeleteShortLinkUseCase(c.repositories.shortLink, c.redisProvider.SharedState()),
		shortLinkStats:        shortlink_usecase.NewGetWorkspaceStatsUseCase(c.repositories.shortLink),
		resolveShortLink:      shortlink_usecase.NewResolveShortLinkUseCase(c.repositories.shortLink, c.redisProvider.SharedState()),
		unlockShortLink:       shortlink_usecase.NewUnlockShortLinkUseCase(c.repositories.shortLink, c.services.password),
		publishShortLinkClick: shortlink_usecase.NewPublishClickUseCase(c.services.shortlinkQueuePub),
		consumeShortLinkClick: shortlink_usecase.NewConsumeClickUseCase(c.services.shortlinkQueueSub, c.services.shortlinkQueuePub, c.repositories.shortLinkClick, c.repositories.shortLink, shortlinkUA, shortlinkGeo, c.redisProvider.SharedState(), c.cfg.ShortLinkIPHashSalt, shortlinkUniqueWindow),
		shortLinkAnalytics:    shortlink_usecase.NewGetAnalyticsUseCase(c.repositories.shortLinkClick),
		shortLinkRecentClicks: shortlink_usecase.NewListRecentClicksUseCase(c.repositories.shortLinkClick),
		shortLinkQR:           shortlink_usecase.NewGenerateQRUseCase(c.repositories.shortLink, shortlinkQR, c.cfg.ShortLinkBaseURL),
		purgeShortLinkClicks:  shortlink_usecase.NewPurgeClicksUseCase(c.repositories.shortLinkClick, c.cfg.ShortLinkClickRetentionDays),

		exportEntries: c.buildExportEntriesUseCase(),

		publishWebhook:              webhook_usecase.NewPublishWebhookUseCase(c.services.webhookQueuePub),
		consumeWhatsAppMsgWebhook:   conversation_usecase.NewConsumeWhatsAppMessageWebhookUseCaseWithPublisher(c.services.webhookQueueSub, c.services.webhookQueuePub, handleWhatsAppMessageUC, c.redisProvider.SharedState()),
		consumeWhatsAppPhoneWebhook: businessphone_usecase.NewConsumePhoneWebhookUseCase(c.services.webhookQueueSub, handlePhoneWebhookUC, c.redisProvider.SharedState()),
		consumeWhatsAppTplWebhook:   whatsapp_template_usecase.NewConsumeTemplateWebhookUseCase(c.services.webhookQueueSub, handleTemplateWebhookUC, c.redisProvider.SharedState()),
		consumeCoexistenceWebhook:   coexistence_usecase.NewConsumeCoexistenceWebhookUseCase(c.services.webhookQueueSub, c.repositories.businessPhone, c.repositories.wcCampaign, c.repositories.wcEntry, c.repositories.conversation, c.repositories.lead),
		consumeAsaasWebhook:         payment_usecase.NewConsumeAsaasWebhookUseCase(c.services.webhookQueueSub, handleAsaasWebhookUC, c.redisProvider.SharedState()),

		createSupportInbox: si_usecase.NewCreateInboxUseCase(c.repositories.supportInbox),
		updateSupportInbox: si_usecase.NewUpdateInboxUseCase(c.repositories.supportInbox),
		deleteSupportInbox: si_usecase.NewDeleteInboxUseCase(c.repositories.supportInbox),
		getSupportInbox:    si_usecase.NewGetInboxUseCase(c.repositories.supportInbox),
		listSupportInboxes: si_usecase.NewListInboxesUseCase(c.repositories.supportInbox),
		createSupportSession: si_usecase.NewCreateSessionUseCase(
			c.repositories.supportInbox,
			c.repositories.supportEntry,
			c.repositories.supportSession,
			c.cfg.AuthJWTSecret,
		),
		reconnectSupportSession: si_usecase.NewReconnectSessionUseCase(
			c.repositories.supportInbox,
			c.repositories.supportSession,
			c.cfg.AuthJWTSecret,
		),

		createIssue:       issues_usecase.NewCreateIssueUseCase(c.repositories.issue, c.repositories.workspace, c.repositories.user, publishEmailUC),
		listIssues:        issues_usecase.NewListIssuesUseCase(c.repositories.issue),
		listAllIssues:     issues_usecase.NewListAllIssuesUseCase(c.repositories.issue),
		getIssue:          issues_usecase.NewGetIssueUseCase(c.repositories.issue),
		closeIssue:        issues_usecase.NewCloseIssueUseCase(c.repositories.issue, c.repositories.workspace, c.repositories.user, publishEmailUC),
		updateIssueStatus: issues_usecase.NewUpdateIssueStatusUseCase(c.repositories.issue, c.repositories.workspace, c.repositories.user, publishEmailUC),

		createIssueResponse: issues_usecase.NewCreateResponseUseCase(c.repositories.issue, c.repositories.issueResponse, c.repositories.workspace, c.repositories.user, publishEmailUC),
		listIssueResponses:  issues_usecase.NewListResponsesUseCase(c.repositories.issueResponse),

		createWorkflow:           workflow_usecase.NewCreateWorkflowUseCase(c.repositories.workflow, resolveCreationDepartmentUC),
		updateWorkflow:           workflow_usecase.NewUpdateWorkflowUseCase(c.repositories.workflow),
		assignWorkflowDepartment: workflow_usecase.NewAssignDepartmentUseCase(c.repositories.workflow, resolveCreationDepartmentUC),
		deleteWorkflow:           workflow_usecase.NewDeleteWorkflowUseCase(c.repositories.workflow, c.repositories.workflowRun),
		getWorkflow:              workflow_usecase.NewGetWorkflowUseCase(c.repositories.workflow),
		listWorkflows:            workflow_usecase.NewListWorkflowsUseCase(c.repositories.workflow),
		activateWorkflow:         workflow_usecase.NewActivateWorkflowUseCase(c.repositories.workflow),
		pauseWorkflow:            workflow_usecase.NewPauseWorkflowUseCase(c.repositories.workflow),
		startWorkflowRun:         workflow_usecase.NewStartRunUseCase(c.repositories.workflow, c.repositories.workflowRun),
		cancelWorkflowRun:        workflow_usecase.NewCancelRunUseCase(c.repositories.workflowRun),
		getWorkflowRun:           workflow_usecase.NewGetRunUseCase(c.repositories.workflowRun, c.repositories.workflowRunLog),
		listWorkflowRuns:         workflow_usecase.NewListRunsUseCase(c.repositories.workflowRun),
		workflowWebhookConfig:    workflow_usecase.NewWorkflowWebhookUseCase(c.repositories.workflowWebhook, c.repositories.workflow),

		createCalendarEvent:      calendar_usecase.NewCreateEventUseCase(c.repositories.calendar, c.services.googleCalendar),
		updateCalendarEvent:      calendar_usecase.NewUpdateEventUseCase(c.repositories.calendar, c.services.googleCalendar),
		deleteCalendarEvent:      calendar_usecase.NewDeleteEventUseCase(c.repositories.calendar, c.services.googleCalendar),
		getCalendarEvent:         calendar_usecase.NewGetEventUseCase(c.repositories.calendar),
		listCalendarEvents:       calendar_usecase.NewListEventsUseCase(c.repositories.calendar, c.services.googleCalendar),
		connectGoogleCalendar:    calendar_usecase.NewConnectGoogleUseCase(c.repositories.calendar, c.services.googleCalendar, calendar_usecase.NewStartWatchUseCase(c.repositories.calendar, c.services.googleCalendar)),
		disconnectGoogleCalendar: calendar_usecase.NewDisconnectGoogleUseCase(c.repositories.calendar, c.services.googleCalendar, calendar_usecase.NewStopWatchUseCase(c.repositories.calendar, c.services.googleCalendar)),
		getGoogleConnection:      calendar_usecase.NewGetConnectionUseCase(c.repositories.calendar),
		getGoogleAuthURL:         calendar_usecase.NewGetAuthURLUseCase(c.services.googleCalendar),

		startCalendarWatch:         calendar_usecase.NewStartWatchUseCase(c.repositories.calendar, c.services.googleCalendar),
		handleCalendarNotification: calendar_usecase.NewHandleNotificationUseCase(c.repositories.calendar, c.services.googleCalendar),
		stopCalendarWatch:          calendar_usecase.NewStopWatchUseCase(c.repositories.calendar, c.services.googleCalendar),
		renewCalendarChannels:      calendar_usecase.NewRenewExpiringChannelsUseCase(c.repositories.calendar, c.services.googleCalendar, calendar_usecase.NewStartWatchUseCase(c.repositories.calendar, c.services.googleCalendar)),

		createWorkspaceDepartment:       workspace_department_usecase.NewCreateDepartmentUseCase(c.repositories.workspaceDepartment),
		getWorkspaceDepartment:          workspace_department_usecase.NewGetDepartmentUseCase(c.repositories.workspaceDepartment),
		listWorkspaceDepartments:        workspace_department_usecase.NewListDepartmentsUseCase(c.repositories.workspaceDepartment),
		listWorkspaceDepartmentsByIDs:   workspace_department_usecase.NewListDepartmentsByIDsUseCase(c.repositories.workspaceDepartment),
		updateWorkspaceDepartment:       workspace_department_usecase.NewUpdateDepartmentUseCase(c.repositories.workspaceDepartment),
		deleteWorkspaceDepartment:       workspace_department_usecase.NewDeleteDepartmentUseCase(c.repositories.workspaceDepartment),
		addWorkspaceDepartmentMember:    workspace_department_usecase.NewAddMemberUseCase(c.repositories.workspaceDepartment),
		removeWorkspaceDepartmentMember: workspace_department_usecase.NewRemoveMemberUseCase(c.repositories.workspaceDepartment),
		listWorkspaceDepartmentMembers:  workspace_department_usecase.NewListMembersUseCase(c.repositories.workspaceDepartment),

		affiliateRegister:      affiliate_usecase.NewRegisterAffiliateUseCase(c.repositories.affiliate, c.repositories.user, c.repositories.systemConfig, affiliateWalletValidator),
		affiliateGetMy:         affiliate_usecase.NewGetMyAffiliateUseCase(c.repositories.affiliate, affiliateStatsUC),
		affiliateUpdateMy:      affiliate_usecase.NewUpdateMyAffiliateUseCase(c.repositories.affiliate, c.repositories.user, affiliateWalletValidator),
		affiliateListReferrals: affiliate_usecase.NewListReferralsUseCase(c.repositories.affiliate),
		affiliateListEarnings:  affiliate_usecase.NewListEarningsUseCase(c.repositories.affiliate),
		affiliateValidateCode:  affiliate_usecase.NewValidateReferralCodeUseCase(c.repositories.affiliate),
		affiliateTrackReferral: trackReferralUC,
		affiliateRecordEarning: affiliateRecordEarningUC,
		affiliateStats:         affiliateStatsUC,
		affiliateAdminList:     affiliate_usecase.NewAdminListAffiliatesUseCase(c.repositories.affiliate),
		affiliateAdminGet:      affiliate_usecase.NewAdminGetAffiliateUseCase(c.repositories.affiliate, affiliateStatsUC),
		affiliateAdminUpdate:   affiliate_usecase.NewAdminUpdateAffiliateUseCase(c.repositories.affiliate),
	}

	// Re-wire all call paths to board-aware CDR (AI campaign bridge, dialer, workflow).
	// Bridges are constructed before startCall exists; SetCDR* swaps in the live hooks.
	if c.useCases.startCall != nil {
		if c.services.dialerLifecycle != nil {
			c.services.dialerLifecycle.SetCDRStart(c.useCases.startCall)
			c.services.dialerLifecycle.SetCDRAnswered(calls_cdr_usecase.NewMarkCallAnsweredUseCase(c.repositories.callCDR))
		}
	}

	wfRegistry := workflow_usecase.NewNodeExecutorRegistry()
	executorDeps := workflow_usecase.ExecutorDeps{
		AIService:               c.services.ai,
		AgentRepo:               c.repositories.agent,
		CalendarRepo:            c.repositories.calendar,
		GoogleCalendar:          c.services.googleCalendar,
		RescheduleEventUC:       rescheduleEventUC,
		MessageRepo:             c.repositories.conversation,
		HistoryManager:          messageHistoryManager,
		LeadRepo:                c.repositories.lead,
		WhatsAppEntryRepo:       c.repositories.wcEntry,
		BusinessPhoneRepo:       c.repositories.businessPhone,
		MessageWindowRepo:       c.repositories.leadMessageWindow,
		WhatsAppClientFactory:   c.services.whatsappClientFactory,
		ToolRegistry:            c.services.toolRegistry,
		TemplateRepo:            c.repositories.whatsappTemplate,
		MediaRepo:               c.repositories.media,
		ConsumeWhatsappTemplate: consumeWhatsappTemplateUC,
		WorkspacePhoneAccess:    c.repositories.workspacePhoneAccess,
		SharedState:             c.redisProvider.SharedState(),
		LabelRepo:               c.repositories.label,
		DepartmentRepo:          c.repositories.workspaceDepartment,
		InboxAssignmentRepo:     c.repositories.inboxAssignment,
		WorkspaceRepo:           c.repositories.workspace,
		CachedBalanceChecker:    cachedBalanceChecker,
		BillingPub:              c.services.billingQueuePub,
		RAGService:              ragService,
		FileStorage:             c.services.fileStorage,
		ConversationMediaRepo:   c.repositories.conversationMedia,
		AIAttendance:            c.services.aiAttendanceService,
		ConversationStatus:      c.services.conversationStatusUpdater,
		// The LIVE registry, not a snapshot: channel adapters register as each
		// channel initializes, and several do so after this point.
		Adapters: c.liveAdapterRegistry(),
	}
	workflow_usecase.RegisterDefaultExecutors(wfRegistry, executorDeps)
	wfEngine := workflow_usecase.NewRunEngine(c.repositories.workflowRun, c.repositories.workflowRunLog, wfRegistry)
	wfEngine.SetWakeScheduler(workflow_usecase.NewQueueWakeScheduler(c.services.workflowWakePub))
	wfEngine.SetRunLocker(c.redisProvider.RunLocker())
	c.useCases.aichat = aichat_usecase.NewService(
		c.repositories.aichatThread,
		c.repositories.aichatMessage,
		c.services.ai,
		cachedBalanceChecker,
		c.repositories.workspaceSubscription,
	)

	// The in-app AI chat is the copilot: it runs the shared agentloop harness with a
	// workspace-scoped tool registry over the same aichat threads (sessions).
	c.useCases.copilot = copilot_usecase.NewService(
		agentloop.Engine{AI: c.services.ai},
		copilot_usecase.NewRegistry(
			// agents
			copilottools.NewListAgentsTool(listAgentsUC),
			copilottools.NewCountAgentsTool(listAgentsUC),
			copilottools.NewGetAgentTool(getAgentUC),
			copilottools.NewCreateAgentTool(createAgentUC),
			copilottools.NewUpdateAgentTool(getAgentUC, updateAgentUC),
			copilottools.NewDeleteAgentTool(getAgentUC, deleteAgentUC),
			// reference / catalog lookups
			copilottools.NewListDepartmentsTool(c.useCases.listWorkspaceDepartments),
			copilottools.NewListModelsTool(c.services.ai),
			copilottools.NewListAgentToolsTool(c.services.toolRegistry),
		),
		c.useCases.checkWsAccess,
		c.repositories.aichatThread,
		c.repositories.aichatMessage,
		copilot_usecase.NewInMemoryPendingStore(),
		func() string { return uuid.New().String() },
	)

	c.useCases.workflowManager = workflow_usecase.NewWorkflowManager(c.repositories.workflow, c.repositories.workflowRun, wfEngine)
	c.useCases.triggerEvaluator = workflow_usecase.NewTriggerEvaluator(c.repositories.workflow, c.repositories.workflowRun, wfEngine, c.redisProvider.SharedState())
	c.useCases.handleWebhookTrigger = workflow_usecase.NewHandleWebhookTriggerUseCase(
		c.repositories.workflowWebhook,
		c.repositories.workflow,
		c.repositories.workflowRun,
		c.repositories.entryOwnership,
		c.repositories.entryResolver,
		workflow_usecase.NewEngineRunLauncher(wfEngine, c.redisProvider.SharedState()),
		webhook_usecase.NewIdempotencyGuard(c.redisProvider.SharedState(), 6*time.Hour),
		c.redisProvider.SharedState(),
	)
	c.useCases.consumeWorkflowWake = workflow_usecase.NewConsumeRunWakeUseCase(
		c.services.workflowWakeSub,
		c.services.workflowWakePub,
		c.repositories.workflow,
		c.repositories.workflowRun,
		wfEngine,
	)
	c.useCases.nodeCatalogFn = wfRegistry.Catalog
	c.useCases.wsWorkflowSimulation = workflow_usecase.NewWSWorkflowSimulationUseCase(workflow_usecase.WSWorkflowSimulationDeps{
		WorkflowRepo:   c.repositories.workflow,
		TemplateRepo:   c.repositories.whatsappTemplate,
		MediaRepo:      c.repositories.media,
		AIService:      c.services.ai,
		AgentRepo:      c.repositories.agent,
		ToolRegistry:   c.services.toolRegistry,
		SharedState:    c.redisProvider.SharedState(),
		CalendarRepo:   c.repositories.calendar,
		GoogleCalendar: c.services.googleCalendar,
		BillingPub:     c.services.billingQueuePub,
	})
	// Shared, TTL-cached model-id validity check (same caching discipline as the
	// LLM price fetcher). Reused by the builder and the activate-time validator so
	// neither re-fetches the ~300-model catalog per check, they validate only the
	// model ids actually used.
	var aiModelValidator workflow_usecase.ModelLookup
	if orSvc, ok := c.services.ai.(*openrouter_service.Service); ok {
		aiModelValidator = openrouter_service.NewModelValidator(orSvc, 1*time.Hour)
	}
	c.useCases.wsWorkflowAIBuilder = workflow_usecase.NewAIBuilderUseCase(workflow_usecase.AIBuilderUseCaseDeps{
		WorkflowRepo:  c.repositories.workflow,
		AIService:     c.services.ai,
		NodeCatalogFn: wfRegistry.Catalog,
		ResourceResolver: workflow_usecase.NewBuilderResourceResolver(workflow_usecase.BuilderResourceResolverDeps{
			Models:      c.services.ai,
			Agents:      c.repositories.agent,
			Departments: c.repositories.workspaceDepartment,
			Labels:      c.repositories.label,
			Workflows:   c.repositories.workflow,
			Members:     builderMemberLister{repo: c.repositories.workspace},
		}),

		NoProgressStop:            30,
		ModelLookup:               aiModelValidator,
		MaxTokens:                 1_000_000,
		SessionTimeout:            10 * time.Minute,
		MaxConcurrentPerWorkspace: 3,
		// Gate each build turn on a positive workspace balance (fail-closed). A
		// session that crosses zero mid-build is still bounded by MaxTokens.
		BalanceGate:      cachedBalanceChecker,
		MinBalanceMicros: 0,
	})
	c.useCases.testWorkflowNode = workflow_usecase.NewTestNodeUseCase(workflow_usecase.TestNodeDeps{
		WorkflowRepo: c.repositories.workflow,
		Registry:     wfRegistry,
		ExecutorDeps: executorDeps,
	})

	if setter, ok := c.useCases.activateWorkflow.(interface {
		SetCatalogFn(func() []workflow_domain.NodeDefinition)
	}); ok {
		setter.SetCatalogFn(wfRegistry.Catalog)
	}
	if setter, ok := c.useCases.activateWorkflow.(interface {
		SetTemplateRepo(template_domain.Repository)
	}); ok {
		setter.SetTemplateRepo(c.repositories.whatsappTemplate)
	}
	if setter, ok := c.useCases.activateWorkflow.(interface {
		SetAgentRepo(agent_domain.Repository)
	}); ok {
		setter.SetAgentRepo(c.repositories.agent)
	}
	if setter, ok := c.useCases.activateWorkflow.(interface {
		SetMediaRepo(media_domain.MediaRepository)
	}); ok {
		setter.SetMediaRepo(c.repositories.media)
	}
	if setter, ok := c.useCases.activateWorkflow.(interface {
		SetLabelRepo(label_domain.Repository)
	}); ok {
		setter.SetLabelRepo(c.repositories.label)
	}
	if setter, ok := c.useCases.activateWorkflow.(interface {
		SetDepartmentRepo(workspace_department_domain.Repository)
	}); ok {
		setter.SetDepartmentRepo(c.repositories.workspaceDepartment)
	}
	if setter, ok := c.useCases.activateWorkflow.(interface {
		SetWorkspaceRepo(workspace_domain.Repository)
	}); ok {
		setter.SetWorkspaceRepo(c.repositories.workspace)
	}
	if setter, ok := c.useCases.activateWorkflow.(interface {
		SetBusinessPhoneRepo(businessphone_domain.Repository)
	}); ok {
		setter.SetBusinessPhoneRepo(c.repositories.businessPhone)
	}
	if c.mcpCollection != nil {
		if setter, ok := c.useCases.activateWorkflow.(interface {
			SetMCPCollectionRepo(domainmcp.CollectionRepository)
		}); ok {
			setter.SetMCPCollectionRepo(c.mcpCollection)
		}
	}
	if c.repositories.ragKnowledgeBase != nil {
		if setter, ok := c.useCases.activateWorkflow.(interface {
			SetKnowledgeBaseRepo(rag_domain.KnowledgeBaseRepository)
		}); ok {
			setter.SetKnowledgeBaseRepo(c.repositories.ragKnowledgeBase)
		}
	}
	if aiModelValidator != nil {
		if setter, ok := c.useCases.activateWorkflow.(interface {
			SetModelLookup(workflow_usecase.ModelLookup)
		}); ok {
			setter.SetModelLookup(aiModelValidator)
		}
	}

	if emailSvc, ok := c.services.emailService.(*notification_service.EmailService); ok {
		emailSvc.SetRecordMetric(c.useCases.recordMetric)
	}

	// TODO: validate all this messy checking, its not optional!
	if c.services.assignmentService != nil {
		if setter, ok := c.useCases.handleWhatsAppMessage.(interface {
			SetAssignmentService(*ia_usecase.AssignmentService)
		}); ok {
			setter.SetAssignmentService(c.services.assignmentService)
		}
	}
	if c.services.aiAttendanceService != nil {
		if setter, ok := c.useCases.handleWhatsAppMessage.(interface {
			SetAIAttendance(conversation_usecase.AIAttendanceRecorder)
		}); ok {
			setter.SetAIAttendance(c.services.aiAttendanceService)
		}
	}

	if c.useCases.triggerEvaluator != nil {
		if setter, ok := c.useCases.handleWhatsAppMessage.(interface {
			SetTriggerEvaluator(workflow_domain.TriggerEvaluator)
		}); ok {
			setter.SetTriggerEvaluator(c.useCases.triggerEvaluator)
		}

		if setter, ok := c.useCases.messageConsumerWCCampaign.(interface {
			SetTriggerEvaluator(workflow_domain.TriggerEvaluator)
		}); ok {
			setter.SetTriggerEvaluator(c.useCases.triggerEvaluator)
		}
	}

	if c.services.billingQueuePub != nil {
		if setter, ok := c.useCases.handleWhatsAppMessage.(interface {
			SetBillingPub(messaging.MessageQueuePub)
		}); ok {
			setter.SetBillingPub(c.services.billingQueuePub)
		}
	}

	// WhatsApp uses the same recipe as every other channel. Its three agent
	// turns (text, media, audio) previously carried three copies of it.
	if setter, ok := c.useCases.handleWhatsAppMessage.(interface {
		SetTurnAssembler(*agentturn.Assembler)
	}); ok {
		setter.SetTurnAssembler(turnAssembler)
	}

	if setter, ok := c.useCases.handleWhatsAppMessage.(interface {
		SetLoopGuard(loopguard.Guard)
	}); ok {
		setter.SetLoopGuard(loopguard.NewGuard(
			c.redisProvider.SharedState(),
			loopguard.DefaultConfig(),
			c.services.metrics,
		))
	}

	if err := c.useCases.consumeNotifications.Start(); err != nil {
		log.Fatal("Failed to start email notification consumer:", err)
	}

	if err := c.useCases.messageConsumerWCCampaign.Start(); err != nil {
		log.Fatal("Failed to start whatsapp campaign consumer:", err)
	}

	if c.useCases.consumeCRMTelemetry != nil {
		if err := c.useCases.consumeCRMTelemetry.Start(); err != nil {
			log.Printf("Failed to start CRM telemetry consumer: %v", err)
		}
	}

	if err := c.useCases.consumeMetric.Start(); err != nil {
		log.Fatal("Failed to start business metrics consumer:", err)
	}

	if err := c.useCases.consumeDocProcessing.Start(); err != nil {
		log.Fatal("Failed to start RAG document processing consumer:", err)
	}

	if err := c.useCases.consumeShortLinkClick.Start(); err != nil {
		log.Fatal("Failed to start short link click consumer:", err)
	}

	// Scheduled messages: this is the timely trigger. The sweep would still
	// deliver everything within a minute if it never started, which is why a
	// failure here is loud but not fatal — degraded latency beats refusing to
	// boot the whole platform.
	if err := c.useCases.consumeScheduledMessage.Start(); err != nil {
		log.Printf("Failed to start the scheduled message consumer: %v; the sweep will deliver messages up to a minute late", err)
	}

	if err := c.useCases.consumeWhatsAppMsgWebhook.Start(); err != nil {
		log.Fatal("Failed to start WhatsApp message webhook consumer:", err)
	}

	if err := c.useCases.consumeWhatsAppPhoneWebhook.Start(); err != nil {
		log.Fatal("Failed to start WhatsApp phone webhook consumer:", err)
	}

	if err := c.useCases.consumeWhatsAppTplWebhook.Start(); err != nil {
		log.Fatal("Failed to start WhatsApp template webhook consumer:", err)
	}

	if err := c.useCases.consumeCoexistenceWebhook.Start(); err != nil {
		log.Fatal("Failed to start coexistence webhook consumer:", err)
	}

	if err := c.useCases.consumeAsaasWebhook.Start(); err != nil {
		log.Fatal("Failed to start Asaas webhook consumer:", err)
	}

	// Instagram's runtime half is wired here rather than earlier: it needs both the
	// shared history manager (a local in this function) and c.useCases.publishWebhook,
	// which only exists once the useCases struct literal above has been assigned.
	c.initInstagramRuntime(messageHistoryManager)

	// Instagram subscribes three topics (messages, comments, account events).
	// Unlike the WhatsApp consumers this is not fatal on failure: the channel is
	// optional, so a broker hiccup here must not stop the product from booting.
	if c.instagram != nil && c.instagram.Enabled && c.instagram.Consume != nil {
		if err := c.instagram.Consume.Start(); err != nil {
			log.Printf("[instagram] failed to start webhook consumers: %v", err)
		} else {
			log.Printf("[instagram] webhook consumers started")
		}
	}

	// Telegram's runtime half, for the same reason and with the same
	// non-fatal-on-failure rule: the channel is optional.
	c.initTelegramRuntime(messageHistoryManager)
	if c.telegram != nil && c.telegram.Enabled && c.telegram.Consume != nil {
		if err := c.telegram.Consume.Start(); err != nil {
			log.Printf("[telegram] failed to start webhook consumers: %v", err)
		} else {
			log.Printf("[telegram] webhook consumers started")
		}
	}

	c.initUnofficialWhatsAppRuntime(messageHistoryManager)
	if c.unofficialWhatsApp != nil && c.unofficialWhatsApp.Enabled && c.unofficialWhatsApp.Consume != nil {
		if err := c.unofficialWhatsApp.Consume.Start(); err != nil {
			log.Printf("[unofficial-whatsapp] failed to start webhook consumers: %v", err)
		} else {
			log.Printf("[unofficial-whatsapp] webhook consumers started")
		}
	}

	if c.useCases.consumeWorkflowWake != nil {
		if err := c.useCases.consumeWorkflowWake.Start(); err != nil {
			log.Fatal("Failed to start workflow wake consumer:", err)
		}
	}

	billingConsumer := calls_usecase.NewConsumeCallBillingUseCase(
		c.services.billingQueueSub,
		c.repositories.callBilling,
		c.repositories.balance,
		pricer,
		c.useCases.completeCall, // board-aware complete (AI seats Decr)
		log.New(log.Writer(), "call-billing ", log.LstdFlags),
	)
	if err := billingConsumer.Start(); err != nil {
		log.Fatal("Failed to start call billing consumer:", err)
	}

	aiBillingConsumer := balance_usecase.NewConsumeAIBillingUseCase(
		c.services.billingQueueSub,
		c.repositories.balance,
		pricer,
		c.services.metrics,
	)
	if err := aiBillingConsumer.Start(); err != nil {
		log.Fatal("Failed to start AI billing consumer:", err)
	}

	reconciler := calls_usecase.NewReconcileOrphanedBillingUseCase(
		c.repositories.callBilling,
		log.New(log.Writer(), "billing-reconcile ", log.LstdFlags),
	)
	reconciler.RunPeriodic(5*time.Minute, 15*time.Minute)
}
