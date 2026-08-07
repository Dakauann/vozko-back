package container

import (
	deliveryHttp "vozko/delivery/http"
	httpServerImpl "vozko/infra/http"
	"vozko/infra/http/middleware"
)

func (c *Container) initRouter() {

	c.router = deliveryHttp.NewRouter(
		c.handlers.product,
		c.handlers.property,
		c.handlers.category,
		c.handlers.agent,
		c.handlers.aichat,
		c.handlers.auth,
		c.handlers.user,
		c.handlers.media,
		c.handlers.holdMusic,
		c.handlers.cart,
		c.handlers.address,
		c.handlers.order,
		c.handlers.cep,
		c.handlers.webhook,
		c.handlers.readMe,
		c.handlers.paymentSplit,
		c.handlers.ticket,
		c.handlers.shipping,
		c.handlers.insurance,
		c.handlers.whatsappTemplate,
		c.handlers.whatsappBusinessPhone,
		c.handlers.waba,
		c.handlers.systemConfig,
		c.handlers.whatsappCampaign,
		c.handlers.metrics,
		c.handlers.metricsQuery,
		c.handlers.businessMetrics,
		c.handlers.shop,
		c.handlers.analysis,
		c.handlers.lead,
		c.handlers.callRecording,
		c.handlers.balance,
		c.handlers.workspaceTemplateAccess,
		c.handlers.workspacePhoneAccess,
		c.handlers.conversation,
		c.handlers.conversationWS,
		c.handlers.dialerWS,
		c.handlers.stage,
		c.handlers.stageGroup,
		c.handlers.pipeline,
		c.handlers.savedView,
		c.handlers.opportunity,
		c.handlers.opportunityBoard,
		c.handlers.customField,
		c.handlers.crmBoard,
		c.handlers.crmBulk,
		c.handlers.label,
		c.handlers.messageShortcut,
		c.handlers.textRefiner,
		c.handlers.attendance,
		c.handlers.knowledgeBase,
		c.handlers.shortLink,
		c.handlers.export,
		c.handlers.invoice,
		c.handlers.callBilling,
		c.handlers.calls,
		c.handlers.analytics,
		c.services.tokenService,
		c.repositories.user,
		c.repositories.user,
		c.handlers.metaEmbeddedSignup,
		c.handlers.workspace,
		c.handlers.workspacePricing,
		c.handlers.workspaceConfig,
		c.handlers.workspacePlan,
		c.handlers.workspaceAddon,
		c.handlers.supportInbox,
		c.handlers.issue,
		c.handlers.workflow,
		c.handlers.workflowWebhook,
		c.handlers.wsWorkflowSimulator,
		c.handlers.wsWorkflowAIBuilder,
		c.handlers.builderSession,
		c.handlers.calendar,
		c.handlers.workspaceDepartment,
		c.handlers.affiliate,
		c.agentMCP,
		c.repositories.workspaceDepartment,
		c.useCases.checkWsAccess,
		c.repositories.workspace,
		c.useCases.ensureDefaultWorkspace,
		c.services.rateLimiterFactory,
		c.redisProvider.SharedState(),
		c.services.metrics,
		instagramHandler(c),
		instagramWebhookHandler(c),
		telegramHandler(c),
		telegramWebhookHandler(c),
		unofficialWhatsAppHandler(c),
		unofficialWhatsAppWebhookHandler(c),
	)
}

func (c *Container) initServer() {
	cors := middleware.NewCORSMiddleware(c.cfg.CORSTrustedOrigins)
	csrf := middleware.NewCSRFMiddleware(c.cfg.CORSTrustedOrigins)
	handler := c.router.GetHandler()
	if c.services.metrics != nil {
		metricsMW := middleware.NewHTTPMetrics(c.services.metrics, middleware.HTTPMetricsConfig{
			SkipPrefixes:   []string{"/health", "/metrics", "/ws/"},
			NormalizePaths: true,
		})
		handler = metricsMW.Record(handler)
	}
	// CORS is outermost so it answers OPTIONS preflight before CSRF; CSRF then
	// guards cookie-authenticated state-changing requests (Bearer/API clients are
	// exempt, so the open API is unaffected).
	c.server = httpServerImpl.NewHTTPServer(cors.Handler(csrf.Handler(handler)))
}

func (c *Container) initMetricsServer() {
	if c.services == nil || c.services.metrics == nil {
		return
	}
	c.metricsHTTP = newMetricsServer(c.cfg.MetricsListenAddr, c.services.metrics.Handler())
}
