package http

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"time"
	affiliatehttp "vozko/delivery/http/affiliate"
	analysishttp "vozko/delivery/http/analysis"
	analyticshttp "vozko/delivery/http/analytics"
	attendancehttp "vozko/delivery/http/attendance"
	authhttp "vozko/delivery/http/auth"
	balancehttp "vozko/delivery/http/balance"
	branchhttp "vozko/delivery/http/branch"
	buildersessionhttp "vozko/delivery/http/buildersession"
	businessmetricshttp "vozko/delivery/http/businessmetrics"
	calendarhttp "vozko/delivery/http/calendar"
	callbillinghttp "vozko/delivery/http/callbilling"
	callrecordinghttp "vozko/delivery/http/callrecording"
	cephttp "vozko/delivery/http/cep"
	conversationhttp "vozko/delivery/http/conversation"
	crmboardhttp "vozko/delivery/http/crmboard"
	crmbulkhttp "vozko/delivery/http/crmbulk"
	customfieldhttp "vozko/delivery/http/customfield"
	dialerringchannelshttp "vozko/delivery/http/dialerringchannels"
	exporthttp "vozko/delivery/http/export"
	"vozko/delivery/http/handlers"
	holdmusichttp "vozko/delivery/http/holdmusic"
	instagramhttp "vozko/delivery/http/instagram"
	invoicehttp "vozko/delivery/http/invoice"
	issuehttp "vozko/delivery/http/issue"
	labelhttp "vozko/delivery/http/label"
	leadhttp "vozko/delivery/http/lead"
	mediashttp "vozko/delivery/http/medias"
	messageshortcuthttp "vozko/delivery/http/messageshortcut"
	metaembeddedsignuphttp "vozko/delivery/http/metaembeddedsignup"
	opportunityhttp "vozko/delivery/http/opportunity"
	opportunityboardhttp "vozko/delivery/http/opportunityboard"
	paymentsplithttp "vozko/delivery/http/paymentsplit"
	pipelinehttp "vozko/delivery/http/pipeline"
	readmehttp "vozko/delivery/http/readme"
	savedviewhttp "vozko/delivery/http/savedview"
	shortlinkhttp "vozko/delivery/http/shortlink"
	siptrunkhttp "vozko/delivery/http/siptrunk"
	stagehttp "vozko/delivery/http/stage"
	supportinboxhttp "vozko/delivery/http/supportinbox"
	systemconfighttp "vozko/delivery/http/systemconfig"
	telegramhttp "vozko/delivery/http/telegram"
	telephonyhttp "vozko/delivery/http/telephony"
	textrefinerhttp "vozko/delivery/http/textrefiner"
	tickethttp "vozko/delivery/http/ticket"
	userhttp "vozko/delivery/http/user"
	wabahttp "vozko/delivery/http/waba"
	whatsappbusinessphonehttp "vozko/delivery/http/whatsappbusinessphone"
	whatsapptemplatehttp "vozko/delivery/http/whatsapptemplate"
	workflowwebhookhttp "vozko/delivery/http/workflowwebhook"
	workspacehttp "vozko/delivery/http/workspace"
	workspaceaddonhttp "vozko/delivery/http/workspaceaddon"
	workspaceconfighttp "vozko/delivery/http/workspaceconfig"
	workspacedepartmenthttp "vozko/delivery/http/workspacedepartment"
	workspacephoneaccesshttp "vozko/delivery/http/workspacephoneaccess"
	workspacepricinghttp "vozko/delivery/http/workspacepricing"
	workspacetemplateaccesshttp "vozko/delivery/http/workspacetemplateaccess"
	wsdelivery "vozko/delivery/ws"
	vozkodocs "vozko/docs"
	"vozko/domain/auth"
	"vozko/domain/cache"
	"vozko/domain/metrics"
	"vozko/domain/user"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"

	"github.com/gorilla/mux"
	httpSwagger "github.com/swaggo/http-swagger"
)

const enableRequestLogging = true

type router struct {
	mux                            *mux.Router
	productHandler                 *handlers.ProductHandler
	propertyHandler                *handlers.PropertyHandler
	categoryHandler                *handlers.CategoryHandler
	agentHandler                   *handlers.AgentHandler
	aiChatHandler                  *handlers.AIChatHandler
	authHandler                    *authhttp.AuthHandler
	userHandler                    *userhttp.UserHandler
	mediasHandler                  *mediashttp.MediasHandler
	holdMusicHandler               *holdmusichttp.HoldMusicHandler
	cartHandler                    *handlers.CartHandler
	addressHandler                 *handlers.AddressHandler
	orderHandler                   *handlers.OrderHandler
	cepHandler                     *cephttp.CEPHandler
	webhookHandler                 *handlers.WebhookHandler
	readMeHandler                  *readmehttp.Handler
	paymentSplitHandler            *paymentsplithttp.PaymentSplitHandler
	ticketHandler                  *tickethttp.TicketHandler
	shippingHandler                *handlers.ShippingHandler
	insuranceHandler               *handlers.InsuranceHandler
	whatsappTemplateHandler        *whatsapptemplatehttp.WhatsAppTemplateHandler
	whatsappBusinessPhoneHandler   *whatsappbusinessphonehttp.WhatsAppBusinessPhoneHandler
	wabaHandler                    *wabahttp.WABAHandler
	systemConfigHandler            *systemconfighttp.SystemConfigHandler
	whatsappCampaignHandler        *handlers.WhatsAppCampaignHandler
	authMiddleware                 *middleware.AuthMiddleware
	metricsHandler                 *handlers.MetricsHandler
	metricsQueryHandler            *handlers.MetricsQueryHandler
	businessMetricsHandler         *businessmetricshttp.BusinessMetricsHandler
	shopHandler                    *handlers.ShopHandler
	analysisHandler                *analysishttp.AnalysisHandler
	leadHandler                    *leadhttp.LeadHandler
	callRecordingHandler           *callrecordinghttp.CallRecordingHandler
	balanceHandler                 *balancehttp.BalanceHandler
	workspaceTemplateAccessHandler *workspacetemplateaccesshttp.WorkspaceTemplateAccessHandler
	workspacePhoneAccessHandler    *workspacephoneaccesshttp.WorkspacePhoneAccessHandler
	sipTrunkHandler                *siptrunkhttp.SIPTrunkHandler
	branchHandler                  *branchhttp.BranchHandler
	dialerRingChannelsHandler      *dialerringchannelshttp.DialerRingChannelsHandler
	conversationHandler            *conversationhttp.ConversationHandler
	conversationWSHandler          *wsdelivery.ConversationWSHandler
	dialerWSHandler                *wsdelivery.DialerWSHandler
	stageHandler                   *stagehttp.StageHandler
	stageGroupHandler              *handlers.StageGroupHandler
	pipelineHandler                *pipelinehttp.PipelineHandler
	savedViewHandler               *savedviewhttp.SavedViewHandler
	opportunityHandler             *opportunityhttp.OpportunityHandler
	opportunityBoardHandler        *opportunityboardhttp.OpportunityBoardHandler
	customFieldHandler             *customfieldhttp.CustomFieldHandler
	crmBoardHandler                *crmboardhttp.CRMBoardHandler
	crmBulkHandler                 *crmbulkhttp.CRMBulkHandler
	labelHandler                   *labelhttp.LabelHandler
	messageShortcutHandler         *messageshortcuthttp.MessageShortcutHandler
	textRefinerHandler             *textrefinerhttp.TextRefinerHandler
	attendanceHandler              *attendancehttp.AttendanceHandler
	telephonyHandler               *telephonyhttp.TelephonyHandler
	knowledgeBaseHandler           *handlers.KnowledgeBaseHandler
	shortLinkHandler               *shortlinkhttp.ShortLinkHandler
	exportHandler                  *exporthttp.ExportHandler
	invoiceHandler                 *invoicehttp.InvoiceHandler
	callBillingHandler             *callbillinghttp.CallBillingHandler
	callsHandler                   *handlers.CallsHandler
	analyticsHandler               *analyticshttp.AnalyticsHandler
	rolesMiddleware                *middleware.RolesMiddleware
	rateLimiterMiddleware          *middleware.RateLimiterMiddleware
	cepRateLimiter                 *middleware.RateLimiterMiddleware
	shortLinkRateLimiter           *middleware.RateLimiterMiddleware
	workflowWebhookRateLimiter     *middleware.RateLimiterMiddleware
	forgotPasswordRateLimiter      *middleware.RateLimiterMiddleware
	emailVerificationRateLimiter   *middleware.RateLimiterMiddleware
	mediaUploadRateLimiter         *middleware.RateLimiterMiddleware
	registerRateLimiter            *middleware.RateLimiterMiddleware
	loginRateLimiter               *middleware.RateLimiterMiddleware
	resetPasswordRateLimiter       *middleware.RateLimiterMiddleware
	phoneVerificationRateLimiter   *middleware.RateLimiterMiddleware
	metaEmbeddedSignupHandler      *metaembeddedsignuphttp.MetaEmbeddedSignupHandler
	// Instagram handlers are nil when the channel is disabled, so both route
	// registrations are guarded.
	instagramHandler           *instagramhttp.Handler
	instagramWebhookHandler    *instagramhttp.WebhookHandler
	telegramHandler            *telegramhttp.Handler
	telegramWebhookHandler     *telegramhttp.WebhookHandler
	workspaceHandler           *workspacehttp.WorkspaceHandler
	workspacePricingHandler    *workspacepricinghttp.WorkspacePricingHandler
	workspaceConfigHandler     *workspaceconfighttp.WorkspaceConfigHandler
	workspacePlanHandler       *handlers.WorkspacePlanHandler
	workspaceAddonHandler      *workspaceaddonhttp.WorkspaceAddonHandler
	supportInboxHandler        *supportinboxhttp.SupportInboxHandler
	issueHandler               *issuehttp.IssueHandler
	workflowHandler            *handlers.WorkflowHandler
	workflowWebhookHandler     *workflowwebhookhttp.Handler
	wsWorkflowSimulatorHandler *wsdelivery.WSWorkflowSimulatorHandler
	wsWorkflowAIBuilderHandler *wsdelivery.WSWorkflowAIBuilderHandler
	builderSessionHandler      *buildersessionhttp.BuilderSessionHandler
	calendarHandler            *calendarhttp.CalendarHandler
	workspaceDepartmentHandler *workspacedepartmenthttp.WorkspaceDepartmentHandler
	affiliateHandler           *affiliatehttp.AffiliateHandler
	agentMCP                   *handlers.AgentMCPBundle
	workspaceMiddleware        *middleware.WorkspaceMiddleware
	departmentMiddleware       *middleware.DepartmentMiddleware
}

func (r *router) ac(resource workspace_domain.Resource, action workspace_domain.Action, handler http.HandlerFunc) http.HandlerFunc {
	return r.workspaceMiddleware.CheckAccess(resource, action, handler)
}

func NewRouter(productHandler *handlers.ProductHandler,
	propertyHandler *handlers.PropertyHandler,
	categoryHandler *handlers.CategoryHandler,
	agentHandler *handlers.AgentHandler,
	aiChatHandler *handlers.AIChatHandler,
	authHandler *authhttp.AuthHandler,
	userHandler *userhttp.UserHandler,
	mediasHandler *mediashttp.MediasHandler,
	holdMusicHandler *holdmusichttp.HoldMusicHandler,
	cartHandler *handlers.CartHandler,
	addressHandler *handlers.AddressHandler,
	orderHandler *handlers.OrderHandler,
	cepHandler *cephttp.CEPHandler,
	webhookHandler *handlers.WebhookHandler,
	readMeHandler *readmehttp.Handler,
	paymentSplitHandler *paymentsplithttp.PaymentSplitHandler,
	ticketHandler *tickethttp.TicketHandler,
	shippingHandler *handlers.ShippingHandler,
	insuranceHandler *handlers.InsuranceHandler,
	whatsappTemplateHandler *whatsapptemplatehttp.WhatsAppTemplateHandler,
	whatsappBusinessPhoneHandler *whatsappbusinessphonehttp.WhatsAppBusinessPhoneHandler,
	wabaHandler *wabahttp.WABAHandler,
	systemConfigHandler *systemconfighttp.SystemConfigHandler,
	whatsappCampaignHandler *handlers.WhatsAppCampaignHandler,
	metricsHandler *handlers.MetricsHandler,
	metricsQueryHandler *handlers.MetricsQueryHandler,
	businessMetricsHandler *businessmetricshttp.BusinessMetricsHandler,
	shopHandler *handlers.ShopHandler,
	analysisHandler *analysishttp.AnalysisHandler,
	leadHandler *leadhttp.LeadHandler,
	callRecordingHandler *callrecordinghttp.CallRecordingHandler,
	balanceHandler *balancehttp.BalanceHandler,
	workspaceTemplateAccessHandler *workspacetemplateaccesshttp.WorkspaceTemplateAccessHandler,
	workspacePhoneAccessHandler *workspacephoneaccesshttp.WorkspacePhoneAccessHandler,
	sipTrunkHandler *siptrunkhttp.SIPTrunkHandler,
	branchHandler *branchhttp.BranchHandler,
	dialerRingChannelsHandler *dialerringchannelshttp.DialerRingChannelsHandler,
	conversationHandler *conversationhttp.ConversationHandler,
	conversationWSHandler *wsdelivery.ConversationWSHandler,
	dialerWSHandler *wsdelivery.DialerWSHandler,
	stageHandler *stagehttp.StageHandler,
	stageGroupHandler *handlers.StageGroupHandler,
	pipelineHandler *pipelinehttp.PipelineHandler,
	savedViewHandler *savedviewhttp.SavedViewHandler,
	opportunityHandler *opportunityhttp.OpportunityHandler,
	opportunityBoardHandler *opportunityboardhttp.OpportunityBoardHandler,
	customFieldHandler *customfieldhttp.CustomFieldHandler,
	crmBoardHandler *crmboardhttp.CRMBoardHandler,
	crmBulkHandler *crmbulkhttp.CRMBulkHandler,
	labelHandler *labelhttp.LabelHandler,
	messageShortcutHandler *messageshortcuthttp.MessageShortcutHandler,
	textRefinerHandler *textrefinerhttp.TextRefinerHandler,
	attendanceHandler *attendancehttp.AttendanceHandler,
	telephonyHandler *telephonyhttp.TelephonyHandler,
	knowledgeBaseHandler *handlers.KnowledgeBaseHandler,
	shortLinkHandler *shortlinkhttp.ShortLinkHandler,
	exportHandler *exporthttp.ExportHandler,
	invoiceHandler *invoicehttp.InvoiceHandler,
	callBillingHandler *callbillinghttp.CallBillingHandler,
	callsHandler *handlers.CallsHandler,
	analyticsHandler *analyticshttp.AnalyticsHandler,
	verifier auth.TokenVerifier,
	roleFetcher middleware.RoleFetcher,
	versionFetcher middleware.TokenVersionFetcher,
	metaEmbeddedSignupHandler *metaembeddedsignuphttp.MetaEmbeddedSignupHandler,
	workspaceHandler *workspacehttp.WorkspaceHandler,
	workspacePricingHandler *workspacepricinghttp.WorkspacePricingHandler,
	workspaceConfigHandler *workspaceconfighttp.WorkspaceConfigHandler,
	workspacePlanHandler *handlers.WorkspacePlanHandler,
	workspaceAddonHandler *workspaceaddonhttp.WorkspaceAddonHandler,
	supportInboxHandler *supportinboxhttp.SupportInboxHandler,
	issueHandler *issuehttp.IssueHandler,
	workflowHandler *handlers.WorkflowHandler,
	workflowWebhookHandler *workflowwebhookhttp.Handler,
	wsWorkflowSimulatorHandler *wsdelivery.WSWorkflowSimulatorHandler,
	wsWorkflowAIBuilderHandler *wsdelivery.WSWorkflowAIBuilderHandler,
	builderSessionHandler *buildersessionhttp.BuilderSessionHandler,
	calendarHandler *calendarhttp.CalendarHandler,
	workspaceDepartmentHandler *workspacedepartmenthttp.WorkspaceDepartmentHandler,
	affiliateHandler *affiliatehttp.AffiliateHandler,
	agentMCP *handlers.AgentMCPBundle,
	deptIDsResolver middleware.DepartmentIDsResolver,
	wsAccessChecker middleware.WorkspaceAccessChecker,
	wsMembershipChecker middleware.WorkspaceMembershipChecker,
	wsDefaultResolver middleware.DefaultWorkspaceResolver,
	rateLimiterFactory cache.RateLimiterFactory,
	shared cache.SharedState,
	rateLimitMetrics metrics.RateLimitMetricsRecorder,
	instagramHandler *instagramhttp.Handler,
	instagramWebhookHandler *instagramhttp.WebhookHandler,
	telegramHandler *telegramhttp.Handler,
	telegramWebhookHandler *telegramhttp.WebhookHandler) Router {
	r := &router{
		instagramHandler:               instagramHandler,
		instagramWebhookHandler:        instagramWebhookHandler,
		telegramHandler:                telegramHandler,
		telegramWebhookHandler:         telegramWebhookHandler,
		mux:                            mux.NewRouter(),
		productHandler:                 productHandler,
		propertyHandler:                propertyHandler,
		categoryHandler:                categoryHandler,
		agentHandler:                   agentHandler,
		aiChatHandler:                  aiChatHandler,
		authHandler:                    authHandler,
		userHandler:                    userHandler,
		mediasHandler:                  mediasHandler,
		holdMusicHandler:               holdMusicHandler,
		cartHandler:                    cartHandler,
		addressHandler:                 addressHandler,
		orderHandler:                   orderHandler,
		cepHandler:                     cepHandler,
		webhookHandler:                 webhookHandler,
		readMeHandler:                  readMeHandler,
		paymentSplitHandler:            paymentSplitHandler,
		ticketHandler:                  ticketHandler,
		shippingHandler:                shippingHandler,
		insuranceHandler:               insuranceHandler,
		whatsappTemplateHandler:        whatsappTemplateHandler,
		whatsappBusinessPhoneHandler:   whatsappBusinessPhoneHandler,
		wabaHandler:                    wabaHandler,
		whatsappCampaignHandler:        whatsappCampaignHandler,
		metricsHandler:                 metricsHandler,
		metricsQueryHandler:            metricsQueryHandler,
		businessMetricsHandler:         businessMetricsHandler,
		shopHandler:                    shopHandler,
		analysisHandler:                analysisHandler,
		leadHandler:                    leadHandler,
		callRecordingHandler:           callRecordingHandler,
		balanceHandler:                 balanceHandler,
		workspaceTemplateAccessHandler: workspaceTemplateAccessHandler,
		workspacePhoneAccessHandler:    workspacePhoneAccessHandler,
		sipTrunkHandler:                sipTrunkHandler,
		branchHandler:                  branchHandler,
		dialerRingChannelsHandler:      dialerRingChannelsHandler,
		conversationHandler:            conversationHandler,
		conversationWSHandler:          conversationWSHandler,
		dialerWSHandler:                dialerWSHandler,
		stageHandler:                   stageHandler,
		stageGroupHandler:              stageGroupHandler,
		pipelineHandler:                pipelineHandler,
		savedViewHandler:               savedViewHandler,
		opportunityHandler:             opportunityHandler,
		opportunityBoardHandler:        opportunityBoardHandler,
		customFieldHandler:             customFieldHandler,
		crmBoardHandler:                crmBoardHandler,
		crmBulkHandler:                 crmBulkHandler,
		labelHandler:                   labelHandler,
		messageShortcutHandler:         messageShortcutHandler,
		textRefinerHandler:             textRefinerHandler,
		attendanceHandler:              attendanceHandler,
		telephonyHandler:               telephonyHandler,
		knowledgeBaseHandler:           knowledgeBaseHandler,
		shortLinkHandler:               shortLinkHandler,
		exportHandler:                  exportHandler,
		invoiceHandler:                 invoiceHandler,
		callBillingHandler:             callBillingHandler,
		callsHandler:                   callsHandler,
		analyticsHandler:               analyticsHandler,
		systemConfigHandler:            systemConfigHandler,
		workspaceConfigHandler:         workspaceConfigHandler,
		workspacePlanHandler:           workspacePlanHandler,
		workspaceAddonHandler:          workspaceAddonHandler,
		supportInboxHandler:            supportInboxHandler,
		issueHandler:                   issueHandler,
		workflowHandler:                workflowHandler,
		workflowWebhookHandler:         workflowWebhookHandler,
		wsWorkflowSimulatorHandler:     wsWorkflowSimulatorHandler,
		wsWorkflowAIBuilderHandler:     wsWorkflowAIBuilderHandler,
		builderSessionHandler:          builderSessionHandler,
		calendarHandler:                calendarHandler,
		workspaceDepartmentHandler:     workspaceDepartmentHandler,
		affiliateHandler:               affiliateHandler,
		agentMCP:                       agentMCP,
		authMiddleware:                 middleware.NewAuthMiddleware(verifier, roleFetcher, versionFetcher, shared),
		rolesMiddleware:                middleware.NewRolesMiddleware(),
		rateLimiterMiddleware: middleware.NewRateLimiterMiddleware(rateLimiterFactory("global", 500, 60*time.Second)).
			Named("global").WithMetrics(rateLimitMetrics).
			WithUserIdentity(middleware.UserFromToken(verifier)).
			SkipPathPrefixes("/webhooks/", "/health"),
		cepRateLimiter:               middleware.NewRateLimiterMiddleware(rateLimiterFactory("cep", 20, 60*time.Second)).Named("cep").WithMetrics(rateLimitMetrics),
		shortLinkRateLimiter:         middleware.NewRateLimiterMiddleware(rateLimiterFactory("shortlink_redirect", 120, 60*time.Second)).Named("shortlink_redirect").WithMetrics(rateLimitMetrics),
		workflowWebhookRateLimiter:   middleware.NewRateLimiterMiddleware(rateLimiterFactory("wf_webhook", 120, 60*time.Second)).Named("wf_webhook").WithMetrics(rateLimitMetrics),
		forgotPasswordRateLimiter:    middleware.NewRateLimiterMiddleware(rateLimiterFactory("forgot_pw", 3, 3600*time.Second)).Named("forgot_pw").WithMetrics(rateLimitMetrics),
		emailVerificationRateLimiter: middleware.NewRateLimiterMiddleware(rateLimiterFactory("email_ver", 3, 3600*time.Second)).Named("email_ver").WithMetrics(rateLimitMetrics),
		mediaUploadRateLimiter:       middleware.NewRateLimiterMiddleware(rateLimiterFactory("media_upload", 10, 1*time.Second)).Named("media_upload").WithMetrics(rateLimitMetrics),
		registerRateLimiter:          middleware.NewRateLimiterMiddleware(rateLimiterFactory("register", 20, 3600*time.Second)).Named("register").WithMetrics(rateLimitMetrics),
		// Coarse per-IP backstop, generous so a shared office/NAT does not collide;
		// the precise brute-force defence is the per-account failed-login throttle in
		// the credentials login use case.
		loginRateLimiter:             middleware.NewRateLimiterMiddleware(rateLimiterFactory("login", 100, 60*time.Second)).Named("login").WithMetrics(rateLimitMetrics),
		resetPasswordRateLimiter:     middleware.NewRateLimiterMiddleware(rateLimiterFactory("reset_pw", 3, 3600*time.Second)).Named("reset_pw").WithMetrics(rateLimitMetrics),
		phoneVerificationRateLimiter: middleware.NewRateLimiterMiddleware(rateLimiterFactory("phone_ver", 5, 86400*time.Second)).Named("phone_ver").WithMetrics(rateLimitMetrics),
		metaEmbeddedSignupHandler:    metaEmbeddedSignupHandler,
		workspaceHandler:             workspaceHandler,
		workspacePricingHandler:      workspacePricingHandler,
		workspaceMiddleware:          middleware.NewWorkspaceMiddleware(wsAccessChecker, wsMembershipChecker, wsDefaultResolver),
		departmentMiddleware:         middleware.NewDepartmentMiddleware(deptIDsResolver, wsMembershipChecker),
	}
	r.setupRoutes()
	return r
}

func (r *router) setupRoutes() {
	if enableRequestLogging {
		r.mux.Use(r.requestLogger)
	}
	r.mux.Use(r.rateLimiterMiddleware.Validate)

	r.setupHealthRoutes()
	r.setupSwaggerRoutes()
	authhttp.RegisterPublicRoutes(r.mux, r.authHandler, authhttp.RateLimiters{
		Register:          r.registerRateLimiter,
		Login:             r.loginRateLimiter,
		ForgotPassword:    r.forgotPasswordRateLimiter,
		ResetPassword:     r.resetPasswordRateLimiter,
		EmailVerification: r.emailVerificationRateLimiter,
	})
	r.setupWhatsAppOnboardRoute()
	r.setupPublicProductRoutes()
	r.setupPublicPropertyRoutes()
	r.setupPublicCategoryRoutes()
	r.setupWebhookRoutes()
	r.setupPublicInsuranceRoutes()
	r.setupPublicWorkspacePlanRoutes()
	r.setupCEPRoutes()
	r.setupShortLinkPublicRoutes()
	r.setupPublicSupportRoutes()
	r.setupPublicAffiliateRoutes()
	r.setupPublicMCPRoutes()

	protected := r.mux.PathPrefix("").Subrouter()
	protected.Use(r.authMiddleware.Authenticate)
	protected.Use(r.workspaceMiddleware.ResolveWorkspace())
	protected.Use(r.departmentMiddleware.ResolveDepartment())

	r.setupUserRoutes(protected)
	authhttp.RegisterProtectedRoutes(protected, r.authHandler)
	r.setupCartRoutes(protected)
	r.setupAddressRoutes(protected)
	r.setupOrderRoutes(protected)
	r.setupInsuranceRoutes(protected)
	r.setupTicketRoutes(protected)
	r.setupShopRoutes(protected)
	r.setupShopProductRoutes(protected)
	r.setupShopPropertyRoutes(protected)
	r.setupUserBalanceRoutes(protected)
	r.setupUserInvoiceRoutes(protected)
	r.setupCallBillingRoutes(protected)
	r.setupCallsRoutes(protected)
	r.setupWhatsAppCampaignRoutes(protected)
	r.setupSupportInboxRoutes(protected)
	r.setupWhatsAppTemplateRoutes(protected)
	r.setupWhatsAppBusinessPhoneRoutes(protected)
	r.setupMetaEmbeddedSignupRoutes(protected)
	r.setupInstagramRoutes(protected)
	r.setupTelegramRoutes(protected)
	r.setupWABARoutes(protected)
	r.setupSIPTrunkRoutes(protected)
	r.setupBranchRoutes(protected)
	r.setupAgentRoutes(protected)
	r.setupAIChatRoutes(protected)
	r.setupMCPRoutes(protected)
	r.setupKnowledgeBaseRoutes(protected)
	r.setupShortLinkRoutes(protected)
	r.setupWorkspaceConfigRoutes(protected)
	r.setupWorkspaceSubscriptionRoutes(protected)
	r.setupWorkspaceAddonRoutes(protected)

	workspacepricinghttp.RegisterRoutes(protected, r.workspacePricingHandler)

	adminRoutes := protected.PathPrefix("").Subrouter()
	adminRoutes.Use(r.rolesMiddleware.RequireRole(user.RoleAdmin))

	r.setupAdminSystemConfigRoutes(adminRoutes)
	r.setupAdminCategoryRoutes(adminRoutes)
	r.setupAdminMetricsRoutes(adminRoutes)

	r.setupAdminWhatsAppTemplateRoutes(adminRoutes)
	r.setupAdminWhatsAppBusinessPhoneRoutes(adminRoutes)
	r.setupAdminAgentRoutes(adminRoutes)
	r.setupAdminPaymentSplitRoutes(adminRoutes)
	r.setupAdminTicketRoutes(adminRoutes)
	r.setupAdminShippingRoutes(adminRoutes)
	r.setupAdminBusinessMetricsRoutes(adminRoutes)
	r.setupAnalysisRoutes(protected)
	r.setupAdminAnalysisRoutes(adminRoutes)
	r.setupLeadRoutes(protected)
	r.setupAdminCallRecordingRoutes(adminRoutes)
	r.setupAdminUserRoutes(adminRoutes)
	authhttp.RegisterAdminRoutes(adminRoutes, r.authHandler)
	r.setupAdminBalanceRoutes(adminRoutes)
	r.setupAdminWorkspaceTemplateAccessRoutes(adminRoutes)
	r.setupAdminWorkspacePhoneAccessRoutes(adminRoutes)
	r.setupAdminSIPTrunkRoutes(adminRoutes)
	r.setupAdminBranchRoutes(adminRoutes)
	r.setupAdminWorkspaceMembersRoutes(adminRoutes)
	r.setupAdminWorkspacePricingRoutes(adminRoutes)
	r.setupAdminWorkspacesConfigRoutes(adminRoutes)
	r.setupAdminWorkspacePlanRoutes(adminRoutes)
	r.setupAdminWorkspaceAddonRoutes(adminRoutes)
	r.setupAdminAnalyticsRoutes(adminRoutes)
	r.setupAdminIssueRoutes(adminRoutes)
	r.setupAdminAffiliateRoutes(adminRoutes)

	r.setupConversationRoutes(protected)
	r.setupEntryConversationRoutes(protected)
	r.setupStageRoutes(protected)
	r.setupStageGroupRoutes(protected)
	r.setupPipelineRoutes(protected)
	r.setupSavedViewRoutes(protected)
	r.setupOpportunityRoutes(protected)
	r.setupCRMBoardRoutes(protected)
	r.setupCustomFieldRoutes(protected)
	r.setupLabelRoutes(protected)
	r.setupMessageShortcutRoutes(protected)
	r.setupTextRefinerRoutes(protected)
	r.setupAttendanceRoutes(protected)
	r.setupWorkspaceRoutes(protected)
	r.setupIssueRoutes(protected)
	r.setupWorkflowRoutes(protected)
	r.setupCalendarRoutes(protected)
	r.setupDepartmentRoutes(protected)
	r.setupAffiliateRoutes(protected)
	r.setupMediaRoutes()

	calendarhttp.RegisterPublicRoutes(r.mux, r.calendarHandler)
}

func (r *router) setupSupportInboxRoutes(protected *mux.Router) {
	supportinboxhttp.RegisterProtectedRoutes(protected, r.supportInboxHandler, r.ac)
}

func (r *router) setupPublicSupportRoutes() {
	supportinboxhttp.RegisterPublicRoutes(r.mux, r.supportInboxHandler)
}

func (r *router) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, req)
		log.Printf("[HTTP] %s %s from=%s duration=%s", req.Method, req.URL.Path, req.RemoteAddr, time.Since(start))
	})
}

func (r *router) setupSwaggerRoutes() {
	if os.Getenv("APP_ENV") == "production" && os.Getenv("ENABLE_SWAGGER") != "true" {
		return
	}
	if title := os.Getenv("SWAGGER_TITLE"); title != "" {
		vozkodocs.SwaggerInfo.Title = title
	}
	if host := os.Getenv("SWAGGER_HOST"); host != "" {
		vozkodocs.SwaggerInfo.Host = host
	}
	r.mux.PathPrefix("/swagger/").Handler(httpSwagger.WrapHandler)
}

func (r *router) setupHealthRoutes() {
	r.mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		container := os.Getenv("HOSTNAME")

		var memStats runtime.MemStats
		runtime.ReadMemStats(&memStats)

		goroutines := runtime.NumGoroutine()
		allocMiB := memStats.Alloc / 1024 / 1024

		const maxGoroutines = 50_000
		const maxAllocMiB = 3_500

		if goroutines > maxGoroutines || allocMiB > maxAllocMiB {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, "overloaded - goroutines=%d alloc=%dMiB - %s", goroutines, allocMiB, container)
			return
		}

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "ok - goroutines=%d alloc=%dMiB - %s", goroutines, allocMiB, container)
	}).Methods(http.MethodGet)
}

func (r *router) setupAdminMetricsRoutes(s *mux.Router) {
	s.HandleFunc("/metrics", r.metricsHandler.Handle).Methods(http.MethodGet)
	s.HandleFunc("/metrics/query", r.metricsQueryHandler.QueryRange).Methods(http.MethodGet)
	s.HandleFunc("/metrics/query/instant", r.metricsQueryHandler.QueryInstant).Methods(http.MethodGet)
}

func (r *router) setupWhatsAppOnboardRoute() {
	r.mux.HandleFunc("/whatsapp/onboard", func(w http.ResponseWriter, req *http.Request) {
		appID := os.Getenv("WHATSAPP_APP_ID")
		configID := os.Getenv("META_CONFIG_ID")
		redirectURL := "https://business.facebook.com/messaging/whatsapp/onboard/" +
			"?app_id=" + appID +
			"&config_id=" + configID +
			"&extras=%7B%22featureType%22%3A%22whatsapp_business_app_onboarding%22%2C%22sessionInfoVersion%22%3A%223%22%2C%22version%22%3A%22v3-public-preview%22%7D"
		http.Redirect(w, req, redirectURL, http.StatusFound)
	}).Methods(http.MethodGet)
}

// setupInstagramRoutes registers the Instagram channel. A nil handler means the
// channel is disabled, in which case no routes exist at all.
func (r *router) setupInstagramRoutes(protected *mux.Router) {
	if r.instagramHandler == nil {
		return
	}
	instagramhttp.RegisterProtectedRoutes(protected, r.instagramHandler, r.ac)
}

// setupTelegramRoutes registers the Telegram channel. A nil handler means the
// channel is disabled, in which case no routes exist at all.
func (r *router) setupTelegramRoutes(protected *mux.Router) {
	if r.telegramHandler == nil {
		return
	}
	telegramhttp.RegisterProtectedRoutes(protected, r.telegramHandler, r.ac)
}

func (r *router) setupMetaEmbeddedSignupRoutes(protected *mux.Router) {
	metaembeddedsignuphttp.RegisterProtectedRoutes(protected, r.metaEmbeddedSignupHandler, r.ac)
}

func (r *router) setupPublicProductRoutes() {
	r.mux.HandleFunc("/products", r.productHandler.List).Methods(http.MethodGet)
	r.mux.HandleFunc("/products/search", r.productHandler.Search).Methods(http.MethodGet)
	r.mux.HandleFunc("/products/{id}", r.productHandler.Get).Methods(http.MethodGet)
}

func (r *router) setupPublicPropertyRoutes() {
	r.mux.HandleFunc("/properties", r.propertyHandler.List).Methods(http.MethodGet)
	r.mux.HandleFunc("/properties/search", r.propertyHandler.Search).Methods(http.MethodGet)
	r.mux.HandleFunc("/properties/{id}", r.propertyHandler.Get).Methods(http.MethodGet)
}

func (r *router) setupPublicCategoryRoutes() {
	r.mux.HandleFunc("/categories", r.categoryHandler.List).Methods(http.MethodGet)
	r.mux.HandleFunc("/categories/{id}", r.categoryHandler.Get).Methods(http.MethodGet)
}

func (r *router) setupWebhookRoutes() {
	r.mux.HandleFunc("/webhooks/asaas", r.webhookHandler.HandleAsaasWebhook).Methods(http.MethodPost)
	readmehttp.RegisterPublicRoutes(r.mux, r.readMeHandler)
	r.mux.HandleFunc("/webhooks/whatsapp", r.webhookHandler.HandleWhatsAppWebhook).Methods(http.MethodGet, http.MethodPost)
	metaembeddedsignuphttp.RegisterPublicRoutes(r.mux, r.metaEmbeddedSignupHandler)
	// The OAuth callback is public because Instagram redirects the browser to it
	// directly, and the webhook is public because Meta calls it. Both are
	// authenticated by other means: a signed single-use state, and the
	// X-Hub-Signature-256 HMAC.
	instagramhttp.RegisterPublicRoutes(r.mux, r.instagramHandler, r.instagramWebhookHandler)
	// Telegram calls its webhook directly. There is no body signature to verify,
	// the endpoint is authenticated by the per-account secret token Telegram
	// echoes in X-Telegram-Bot-Api-Secret-Token, and the path carries our own
	// account uuid because an Update object identifies no bot.
	telegramhttp.RegisterPublicRoutes(r.mux, r.telegramWebhookHandler)
	// 360dialog inbound messaging webhook (messages, statuses, template + coexistence
	// events). Reuses the Meta envelope pipeline; authenticated by shared secret.
	r.mux.HandleFunc("/webhooks/360dialog/messages", r.webhookHandler.HandleDialog360MessageWebhook).Methods(http.MethodGet, http.MethodPost)
	workflowwebhookhttp.RegisterPublicRoutes(r.mux, r.workflowWebhookHandler, r.workflowWebhookRateLimiter)
}

func (r *router) setupPublicInsuranceRoutes() {
	insuranceOpenRoutes := r.mux.PathPrefix("/insurance").Subrouter()
	insuranceOpenRoutes.HandleFunc("/policies", r.insuranceHandler.ListPolicies).Methods(http.MethodGet)
	insuranceOpenRoutes.HandleFunc("/policies/{policyType}", r.insuranceHandler.DescribePolicy).Methods(http.MethodGet)
}

func (r *router) setupPublicWorkspacePlanRoutes() {
	r.mux.HandleFunc("/plans", r.workspacePlanHandler.ListPublic).Methods(http.MethodGet)
}

func (r *router) setupCEPRoutes() {
	cephttp.RegisterPublicRoutes(r.mux, r.cepHandler, r.cepRateLimiter)
}

func (r *router) setupUserRoutes(protected *mux.Router) {
	userhttp.RegisterProtectedRoutes(protected, r.userHandler)
}

func (r *router) setupCartRoutes(protected *mux.Router) {
	protected.HandleFunc("/cart", r.cartHandler.GetCart).Methods(http.MethodGet)
	protected.HandleFunc("/cart", r.cartHandler.AddToCart).Methods(http.MethodPost)
	protected.HandleFunc("/cart/clear", r.cartHandler.ClearCart).Methods(http.MethodDelete)
	protected.HandleFunc("/cart/items/{itemId}", r.cartHandler.UpdateCartItem).Methods(http.MethodPut)
	protected.HandleFunc("/cart/items/{itemId}/decrement", r.cartHandler.DecrementCartItem).Methods(http.MethodPatch)
	protected.HandleFunc("/cart/items/{itemId}", r.cartHandler.RemoveFromCart).Methods(http.MethodDelete)
}

func (r *router) setupAddressRoutes(protected *mux.Router) {
	protected.HandleFunc("/addresses", r.addressHandler.List).Methods(http.MethodGet)
	protected.HandleFunc("/addresses", r.addressHandler.Create).Methods(http.MethodPost)
	protected.HandleFunc("/addresses/{id}", r.addressHandler.Update).Methods(http.MethodPut)
	protected.HandleFunc("/addresses/{id}", r.addressHandler.Delete).Methods(http.MethodDelete)
}

func (r *router) setupOrderRoutes(protected *mux.Router) {
	protected.HandleFunc("/orders", r.orderHandler.ListOrders).Methods(http.MethodGet)
	protected.HandleFunc("/orders/{id}", r.orderHandler.GetOrder).Methods(http.MethodGet)
	protected.HandleFunc("/orders/checkout", r.orderHandler.Checkout).Methods(http.MethodPost)
}

func (r *router) setupInsuranceRoutes(protected *mux.Router) {
	insuranceRoutes := protected.PathPrefix("/insurance").Subrouter()
	insuranceRoutes.HandleFunc("/quotations", r.insuranceHandler.ListQuotations).Methods(http.MethodGet)
	insuranceRoutes.HandleFunc("/quotations/{quotationId}", r.insuranceHandler.GetQuotation).Methods(http.MethodGet)
	insuranceRoutes.HandleFunc("/quotations", r.insuranceHandler.Quote).Methods(http.MethodPost)
}

func (r *router) setupTicketRoutes(protected *mux.Router) {
	tickethttp.RegisterProtectedRoutes(protected, r.ticketHandler)
}

func (r *router) setupAdminWorkspacesConfigRoutes(adminRoutes *mux.Router) {
	workspaceconfighttp.RegisterAdminRoutes(adminRoutes, r.workspaceConfigHandler)
}

func (r *router) setupAdminWorkspacePlanRoutes(adminRoutes *mux.Router) {
	adminRoutes.HandleFunc("/admin/plans", r.workspacePlanHandler.ListAdmin).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/admin/plans", r.workspacePlanHandler.CreatePlan).Methods(http.MethodPost)
	adminRoutes.HandleFunc("/admin/plans/{planId}", r.workspacePlanHandler.GetPlan).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/admin/plans/{planId}", r.workspacePlanHandler.UpdatePlan).Methods(http.MethodPut)
	adminRoutes.HandleFunc("/admin/plans/{planId}/archive", r.workspacePlanHandler.ArchivePlan).Methods(http.MethodPatch)
	adminRoutes.HandleFunc("/admin/plans/{planId}/visibility", r.workspacePlanHandler.SetVisibility).Methods(http.MethodPut)
	adminRoutes.HandleFunc("/admin/plans/{planId}/exclusive-affiliate", r.workspacePlanHandler.SetExclusiveAffiliate).Methods(http.MethodPut)
	adminRoutes.HandleFunc("/admin/workspaces/{workspaceId}/subscription", r.workspacePlanHandler.GetSubscription).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/admin/workspaces/{workspaceId}/subscription", r.workspacePlanHandler.CreateSubscriptionInvoice).Methods(http.MethodPost)
	adminRoutes.HandleFunc("/admin/workspaces/{workspaceId}/subscription/cancel", r.workspacePlanHandler.Cancel).Methods(http.MethodPost)
}

func (r *router) setupAdminWorkspaceAddonRoutes(adminRoutes *mux.Router) {
	workspaceaddonhttp.RegisterAdminRoutes(adminRoutes, r.workspaceAddonHandler)
}

func (r *router) setupAdminAnalyticsRoutes(adminRoutes *mux.Router) {
	analyticshttp.RegisterAdminRoutes(adminRoutes, r.analyticsHandler)
}

func (r *router) setupAdminSystemConfigRoutes(adminRoutes *mux.Router) {
	systemconfighttp.RegisterAdminRoutes(adminRoutes, r.systemConfigHandler)
}

func (r *router) setupAdminCategoryRoutes(adminRoutes *mux.Router) {
	adminRoutes.HandleFunc("/categories", r.categoryHandler.Create).Methods(http.MethodPost)
	adminRoutes.HandleFunc("/categories/{id}", r.categoryHandler.Update).Methods(http.MethodPut)
	adminRoutes.HandleFunc("/categories/{id}", r.categoryHandler.Delete).Methods(http.MethodDelete)
}

func (r *router) setupAdminWhatsAppTemplateRoutes(adminRoutes *mux.Router) {
	whatsapptemplatehttp.RegisterAdminRoutes(adminRoutes, r.whatsappTemplateHandler)
}

func (r *router) setupWhatsAppBusinessPhoneRoutes(protected *mux.Router) {
	whatsappbusinessphonehttp.RegisterProtectedRoutes(protected, r.whatsappBusinessPhoneHandler, r.ac, r.phoneVerificationRateLimiter)
}

func (r *router) setupWABARoutes(protected *mux.Router) {
	wabahttp.RegisterRoutes(protected, r.wabaHandler, r.ac)
}

func (r *router) setupAdminWhatsAppBusinessPhoneRoutes(adminRoutes *mux.Router) {
	whatsappbusinessphonehttp.RegisterAdminRoutes(adminRoutes, r.whatsappBusinessPhoneHandler)
}

func (r *router) setupAdminAgentRoutes(adminRoutes *mux.Router) {
	adminRoutes.HandleFunc("/agents", r.agentHandler.List).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/agents", r.agentHandler.Create).Methods(http.MethodPost)
	adminRoutes.HandleFunc("/agents/tools", r.agentHandler.ListTools).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/agents/options", r.agentHandler.ListOptions).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/agents/{id}", r.agentHandler.Get).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/agents/{id}", r.agentHandler.Update).Methods(http.MethodPut)
	adminRoutes.HandleFunc("/agents/{id}", r.agentHandler.Delete).Methods(http.MethodDelete)
}

func (r *router) setupAdminPaymentSplitRoutes(adminRoutes *mux.Router) {
	paymentsplithttp.RegisterAdminRoutes(adminRoutes, r.paymentSplitHandler)
}

func (r *router) setupAdminTicketRoutes(adminRoutes *mux.Router) {
	tickethttp.RegisterAdminRoutes(adminRoutes, r.ticketHandler)
}

func (r *router) setupAdminShippingRoutes(adminRoutes *mux.Router) {
	adminRoutes.HandleFunc("/shipping/providers/{provider}", r.shippingHandler.ListProviderAccounts).Methods(http.MethodGet)
	adminRoutes.HandleFunc("/shipping/providers/{provider}/authorization-url", r.shippingHandler.GetAuthorizationURL).Methods(http.MethodPost)
	adminRoutes.HandleFunc("/shipping/providers/{provider}/connect", r.shippingHandler.ConnectProviderAccount).Methods(http.MethodPost)
	adminRoutes.HandleFunc("/shipping/providers/{provider}/{accountId}/reconnect", r.shippingHandler.ReconnectProviderAccount).Methods(http.MethodPost)
}

func (r *router) setupAdminBusinessMetricsRoutes(adminRoutes *mux.Router) {
	businessmetricshttp.RegisterAdminRoutes(adminRoutes, r.businessMetricsHandler)
}

func (r *router) setupAnalysisRoutes(protected *mux.Router) {
	analysishttp.RegisterProtectedRoutes(protected, r.analysisHandler, r.ac)
}

func (r *router) setupAdminAnalysisRoutes(adminRoutes *mux.Router) {
	analysishttp.RegisterAdminRoutes(adminRoutes, r.analysisHandler)
}

func (r *router) setupLeadRoutes(protected *mux.Router) {
	leadhttp.RegisterRoutes(protected, r.leadHandler, r.ac)
}
func (r *router) setupAdminCallRecordingRoutes(adminRoutes *mux.Router) {
	callrecordinghttp.RegisterAdminRoutes(adminRoutes, r.callRecordingHandler)
}

func (r *router) setupAdminUserRoutes(adminRoutes *mux.Router) {
	userhttp.RegisterAdminRoutes(adminRoutes, r.userHandler)
}

func (r *router) setupMediaRoutes() {
	mediasRoutes := r.mux.PathPrefix("").Subrouter()
	mediasRoutes.Use(r.authMiddleware.Authenticate)
	mediasRoutes.Use(r.workspaceMiddleware.ResolveWorkspace())
	mediashttp.RegisterRoutes(mediasRoutes, r.mediasHandler, r.ac, r.mediaUploadRateLimiter)
	holdmusichttp.RegisterRoutes(mediasRoutes, r.holdMusicHandler, r.ac)
}

func (r *router) setupShopProductRoutes(protected *mux.Router) {
	protected.HandleFunc("/products", r.productHandler.Create).Methods(http.MethodPost)
	protected.HandleFunc("/products/{id}", r.productHandler.Update).Methods(http.MethodPut)
	protected.HandleFunc("/products/{id}/variants/{variantId}/stock", r.productHandler.LaunchVariantStock).Methods(http.MethodPost)
}

func (r *router) setupShopPropertyRoutes(protected *mux.Router) {
	protected.HandleFunc("/properties", r.propertyHandler.Create).Methods(http.MethodPost)
	protected.HandleFunc("/properties/{id}", r.propertyHandler.Update).Methods(http.MethodPut)
	protected.HandleFunc("/properties/{id}", r.propertyHandler.Delete).Methods(http.MethodDelete)
}

func (r *router) setupShopRoutes(protected *mux.Router) {
	protected.HandleFunc("/shops", r.shopHandler.List).Methods(http.MethodGet)
	protected.HandleFunc("/shops", r.shopHandler.Create).Methods(http.MethodPost)
	protected.HandleFunc("/shops/me", r.shopHandler.GetMyShops).Methods(http.MethodGet)
	protected.HandleFunc("/shops/{id}", r.shopHandler.Get).Methods(http.MethodGet)
	protected.HandleFunc("/shops/{id}", r.shopHandler.Update).Methods(http.MethodPut)
}

func (r *router) setupUserBalanceRoutes(protected *mux.Router) {
	balancehttp.RegisterProtectedRoutes(protected, r.balanceHandler, r.ac)
}

func (r *router) setupUserInvoiceRoutes(protected *mux.Router) {
	invoicehttp.RegisterUserRoutes(protected, r.invoiceHandler, r.ac)
}

func (r *router) setupWorkspaceSubscriptionRoutes(protected *mux.Router) {
	// Subscription = "Planos e Faturamento": contract→plans:create, cancel→plans:delete.
	pl := workspace_domain.ResourcePlans
	protected.HandleFunc("/workspaces/{workspaceId}/subscription", r.ac(pl, workspace_domain.ActionCreate, r.workspacePlanHandler.CreateSubscriptionInvoice)).Methods(http.MethodPost)
	protected.HandleFunc("/workspaces/{workspaceId}/subscription/cancel", r.ac(pl, workspace_domain.ActionDelete, r.workspacePlanHandler.Cancel)).Methods(http.MethodPost)
	protected.HandleFunc("/workspaces/{workspaceId}/subscription", r.workspacePlanHandler.GetPublicSubscription).Methods(http.MethodGet)
}

func (r *router) setupWorkspaceAddonRoutes(protected *mux.Router) {
	workspaceaddonhttp.RegisterProtectedRoutes(protected, r.workspaceAddonHandler, r.ac)
}

func (r *router) setupCallBillingRoutes(protected *mux.Router) {
	callbillinghttp.RegisterRoutes(protected, r.callBillingHandler, r.ac)
}

func (r *router) setupCallsRoutes(protected *mux.Router) {
	cb := workspace_domain.ResourceCallRecordings
	protected.HandleFunc("/calls", r.ac(cb, workspace_domain.ActionRead, r.callsHandler.List)).Methods(http.MethodGet)
	protected.HandleFunc("/calls/{callId}", r.ac(cb, workspace_domain.ActionRead, r.callsHandler.Get)).Methods(http.MethodGet)
}

func (r *router) setupWhatsAppCampaignRoutes(protected *mux.Router) {
	wc := workspace_domain.ResourceWhatsAppCampaigns
	wcRoutes := protected.PathPrefix("/whatsapp/campaigns").Subrouter()
	wcRoutes.HandleFunc("", r.ac(wc, workspace_domain.ActionRead, r.whatsappCampaignHandler.List)).Methods(http.MethodGet)
	wcRoutes.HandleFunc("/archived", r.ac(wc, workspace_domain.ActionRead, r.whatsappCampaignHandler.ListArchived)).Methods(http.MethodGet)
	wcRoutes.HandleFunc("/summary", r.ac(wc, workspace_domain.ActionRead, r.whatsappCampaignHandler.Summary)).Methods(http.MethodGet)
	wcRoutes.HandleFunc("", r.ac(wc, workspace_domain.ActionCreate, r.whatsappCampaignHandler.Create)).Methods(http.MethodPost)
	wcRoutes.HandleFunc("/{id}", r.ac(wc, workspace_domain.ActionRead, r.whatsappCampaignHandler.Get)).Methods(http.MethodGet)
	wcRoutes.HandleFunc("/{id}", r.ac(wc, workspace_domain.ActionUpdate, r.whatsappCampaignHandler.Update)).Methods(http.MethodPut)
	wcRoutes.HandleFunc("/{id}/department", r.ac(wc, workspace_domain.ActionUpdate, r.whatsappCampaignHandler.AssignDepartment)).Methods(http.MethodPatch)
	wcRoutes.HandleFunc("/{id}", r.ac(wc, workspace_domain.ActionDelete, r.whatsappCampaignHandler.Delete)).Methods(http.MethodDelete)
	wcRoutes.HandleFunc("/{id}/entries", r.ac(wc, workspace_domain.ActionRead, r.whatsappCampaignHandler.ListEntries)).Methods(http.MethodGet)
	wcRoutes.HandleFunc("/{id}/entries", r.ac(wc, workspace_domain.ActionUpdate, r.whatsappCampaignHandler.AddEntries)).Methods(http.MethodPost)
	wcRoutes.HandleFunc("/{id}/entries/{entryId}", r.ac(wc, workspace_domain.ActionUpdate, r.whatsappCampaignHandler.UpdateEntry)).Methods(http.MethodPatch)
	wcRoutes.HandleFunc("/{id}/entries/{entryId}/ai", r.ac(wc, workspace_domain.ActionStop, r.whatsappCampaignHandler.ToggleEntryAI)).Methods(http.MethodPatch)
	wcRoutes.HandleFunc("/{id}/entries/{entryId}", r.ac(wc, workspace_domain.ActionDelete, r.whatsappCampaignHandler.DeleteEntry)).Methods(http.MethodDelete)
	wcRoutes.HandleFunc("/{id}/start", r.ac(wc, workspace_domain.ActionStart, r.whatsappCampaignHandler.StartCampaign)).Methods(http.MethodPost)
	wcRoutes.HandleFunc("/{id}/quick-send", r.ac(wc, workspace_domain.ActionStart, r.whatsappCampaignHandler.QuickSend)).Methods(http.MethodPost)
	wcRoutes.HandleFunc("/{id}/pause", r.ac(wc, workspace_domain.ActionStop, r.whatsappCampaignHandler.PauseCampaign)).Methods(http.MethodPost)
	wcRoutes.HandleFunc("/{id}/stop", r.ac(wc, workspace_domain.ActionStop, r.whatsappCampaignHandler.StopCampaign)).Methods(http.MethodPost)
	wcRoutes.HandleFunc("/{id}/reset/prepare", r.ac(wc, workspace_domain.ActionUpdate, r.whatsappCampaignHandler.PrepareReset)).Methods(http.MethodPost)
	wcRoutes.HandleFunc("/{id}/reset", r.ac(wc, workspace_domain.ActionUpdate, r.whatsappCampaignHandler.ConfirmReset)).Methods(http.MethodPost)
	wcRoutes.HandleFunc("/{id}/clear-history/prepare", r.ac(wc, workspace_domain.ActionUpdate, r.whatsappCampaignHandler.PrepareClearHistory)).Methods(http.MethodPost)
	wcRoutes.HandleFunc("/{id}/clear-history", r.ac(wc, workspace_domain.ActionUpdate, r.whatsappCampaignHandler.ConfirmClearHistory)).Methods(http.MethodPost)
	wcRoutes.HandleFunc("/{id}/archive", r.ac(wc, workspace_domain.ActionUpdate, r.whatsappCampaignHandler.Archive)).Methods(http.MethodPatch)
	wcRoutes.HandleFunc("/{id}/unarchive", r.ac(wc, workspace_domain.ActionUpdate, r.whatsappCampaignHandler.Unarchive)).Methods(http.MethodPatch)
}

func (r *router) setupWhatsAppTemplateRoutes(protected *mux.Router) {
	whatsapptemplatehttp.RegisterProtectedRoutes(protected, r.whatsappTemplateHandler, r.ac)
}

func (r *router) setupSIPTrunkRoutes(protected *mux.Router) {
	siptrunkhttp.RegisterProtectedRoutes(protected, r.sipTrunkHandler, r.ac)
}

func (r *router) setupBranchRoutes(protected *mux.Router) {
	branchhttp.RegisterProtectedRoutes(protected, r.branchHandler, r.ac)
	dialerringchannelshttp.RegisterRoutes(protected, r.dialerRingChannelsHandler, r.ac)
}

func (r *router) setupAdminBranchRoutes(adminRoutes *mux.Router) {
	branchhttp.RegisterAdminRoutes(adminRoutes, r.branchHandler)
}

func (r *router) setupAgentRoutes(protected *mux.Router) {
	ag := workspace_domain.ResourceAgents
	protected.HandleFunc("/agents", r.ac(ag, workspace_domain.ActionRead, r.agentHandler.List)).Methods(http.MethodGet)
	protected.HandleFunc("/agents/archived", r.ac(ag, workspace_domain.ActionRead, r.agentHandler.ListArchived)).Methods(http.MethodGet)
	protected.HandleFunc("/agents", r.ac(ag, workspace_domain.ActionCreate, r.agentHandler.Create)).Methods(http.MethodPost)
	protected.HandleFunc("/agents/tools", r.ac(ag, workspace_domain.ActionRead, r.agentHandler.ListTools)).Methods(http.MethodGet)
	protected.HandleFunc("/agents/options", r.ac(ag, workspace_domain.ActionRead, r.agentHandler.ListOptions)).Methods(http.MethodGet)
	protected.HandleFunc("/agents/{id}", r.ac(ag, workspace_domain.ActionReadDetails, r.agentHandler.Get)).Methods(http.MethodGet)
	protected.HandleFunc("/agents/{id}/required-variables", r.ac(ag, workspace_domain.ActionRead, r.agentHandler.RequiredVariables)).Methods(http.MethodGet)
	protected.HandleFunc("/agents/{id}", r.ac(ag, workspace_domain.ActionUpdate, r.agentHandler.Update)).Methods(http.MethodPut)
	protected.HandleFunc("/agents/{id}/department", r.ac(ag, workspace_domain.ActionUpdate, r.agentHandler.AssignDepartment)).Methods(http.MethodPatch)
	protected.HandleFunc("/agents/{id}", r.ac(ag, workspace_domain.ActionDelete, r.agentHandler.Delete)).Methods(http.MethodDelete)
	protected.HandleFunc("/agents/{id}/archive", r.ac(ag, workspace_domain.ActionUpdate, r.agentHandler.Archive)).Methods(http.MethodPatch)
	protected.HandleFunc("/agents/{id}/unarchive", r.ac(ag, workspace_domain.ActionUpdate, r.agentHandler.Unarchive)).Methods(http.MethodPatch)
}

func (r *router) setupAIChatRoutes(protected *mux.Router) {
	ch := workspace_domain.ResourceAIChat
	protected.HandleFunc("/chat/threads", r.ac(ch, workspace_domain.ActionRead, r.aiChatHandler.ListThreads)).Methods(http.MethodGet)
	protected.HandleFunc("/chat/threads", r.ac(ch, workspace_domain.ActionCreate, r.aiChatHandler.CreateThread)).Methods(http.MethodPost)
	protected.HandleFunc("/chat/threads/{id}/messages", r.ac(ch, workspace_domain.ActionRead, r.aiChatHandler.ListMessages)).Methods(http.MethodGet)
	protected.HandleFunc("/chat/threads/{id}/messages", r.ac(ch, workspace_domain.ActionCreate, r.aiChatHandler.StreamMessage)).Methods(http.MethodPost)
	protected.HandleFunc("/chat/threads/{id}/actions/{actionId}/approve", r.ac(ch, workspace_domain.ActionUpdate, r.aiChatHandler.ApproveAction)).Methods(http.MethodPost)
	protected.HandleFunc("/chat/threads/{id}/actions/{actionId}/reject", r.ac(ch, workspace_domain.ActionUpdate, r.aiChatHandler.RejectAction)).Methods(http.MethodPost)
	protected.HandleFunc("/chat/threads/{id}", r.ac(ch, workspace_domain.ActionUpdate, r.aiChatHandler.RenameThread)).Methods(http.MethodPatch)
	protected.HandleFunc("/chat/threads/{id}", r.ac(ch, workspace_domain.ActionDelete, r.aiChatHandler.DeleteThread)).Methods(http.MethodDelete)
}

func (r *router) setupShortLinkRoutes(protected *mux.Router) {
	shortlinkhttp.RegisterRoutes(protected, r.shortLinkHandler, r.ac)
}

func (r *router) setupShortLinkPublicRoutes() {
	shortlinkhttp.RegisterPublicRoutes(r.mux, r.shortLinkHandler, r.shortLinkRateLimiter)
}

func (r *router) setupKnowledgeBaseRoutes(protected *mux.Router) {
	kb := workspace_domain.ResourceKnowledgeBases
	protected.HandleFunc("/knowledge-bases", r.ac(kb, workspace_domain.ActionRead, r.knowledgeBaseHandler.List)).Methods(http.MethodGet)
	protected.HandleFunc("/knowledge-bases", r.ac(kb, workspace_domain.ActionCreate, r.knowledgeBaseHandler.Create)).Methods(http.MethodPost)
	protected.HandleFunc("/knowledge-bases/query", r.ac(kb, workspace_domain.ActionRead, r.knowledgeBaseHandler.Query)).Methods(http.MethodPost)
	protected.HandleFunc("/knowledge-bases/{id}", r.ac(kb, workspace_domain.ActionRead, r.knowledgeBaseHandler.Get)).Methods(http.MethodGet)
	protected.HandleFunc("/knowledge-bases/{id}", r.ac(kb, workspace_domain.ActionUpdate, r.knowledgeBaseHandler.Update)).Methods(http.MethodPut)
	protected.HandleFunc("/knowledge-bases/{id}", r.ac(kb, workspace_domain.ActionDelete, r.knowledgeBaseHandler.Delete)).Methods(http.MethodDelete)
	protected.HandleFunc("/knowledge-bases/{id}/documents", r.ac(kb, workspace_domain.ActionRead, r.knowledgeBaseHandler.ListDocuments)).Methods(http.MethodGet)
	protected.HandleFunc("/knowledge-bases/{id}/documents", r.ac(kb, workspace_domain.ActionCreate, r.knowledgeBaseHandler.CreateDocument)).Methods(http.MethodPost)
	protected.HandleFunc("/knowledge-bases/{id}/documents/upload", r.ac(kb, workspace_domain.ActionCreate, r.knowledgeBaseHandler.UploadDocument)).Methods(http.MethodPost)
	protected.HandleFunc("/knowledge-bases/{id}/documents/{documentId}", r.ac(kb, workspace_domain.ActionRead, r.knowledgeBaseHandler.GetDocument)).Methods(http.MethodGet)
	protected.HandleFunc("/knowledge-bases/{id}/documents/{documentId}", r.ac(kb, workspace_domain.ActionDelete, r.knowledgeBaseHandler.DeleteDocument)).Methods(http.MethodDelete)
	ag := workspace_domain.ResourceAgents
	protected.HandleFunc("/agents/{agentId}/knowledge-bases", r.ac(ag, workspace_domain.ActionReadDetails, r.knowledgeBaseHandler.GetAgentKnowledgeBases)).Methods(http.MethodGet)
	protected.HandleFunc("/agents/{agentId}/knowledge-bases", r.ac(ag, workspace_domain.ActionUpdate, r.knowledgeBaseHandler.LinkToAgent)).Methods(http.MethodPut)
}

func (r *router) setupAdminBalanceRoutes(adminRoutes *mux.Router) {
	balancehttp.RegisterAdminRoutes(adminRoutes, r.balanceHandler)
	invoicehttp.RegisterAdminRoutes(adminRoutes, r.invoiceHandler)
}

func (r *router) setupAdminWorkspaceTemplateAccessRoutes(adminRoutes *mux.Router) {
	workspacetemplateaccesshttp.RegisterAdminRoutes(adminRoutes, r.workspaceTemplateAccessHandler)
}

func (r *router) setupAdminWorkspacePhoneAccessRoutes(adminRoutes *mux.Router) {
	workspacephoneaccesshttp.RegisterAdminRoutes(adminRoutes, r.workspacePhoneAccessHandler)
}

func (r *router) setupAdminSIPTrunkRoutes(adminRoutes *mux.Router) {
	siptrunkhttp.RegisterAdminRoutes(adminRoutes, r.sipTrunkHandler)
}

func (r *router) setupAdminWorkspaceMembersRoutes(adminRoutes *mux.Router) {
	workspacehttp.RegisterAdminRoutes(adminRoutes, r.workspaceHandler)
}

func (r *router) setupAdminWorkspacePricingRoutes(adminRoutes *mux.Router) {
	workspacepricinghttp.RegisterAdminRoutes(adminRoutes, r.workspacePricingHandler)
}

func (r *router) setupEntryConversationRoutes(protected *mux.Router) {
	leadhttp.RegisterEntryConversationRoutes(protected, r.leadHandler, r.ac)

	callrecordinghttp.RegisterRoutes(protected, r.callRecordingHandler, r.ac)
}

func (r *router) setupConversationRoutes(protected *mux.Router) {
	cv := workspace_domain.ResourceConversations

	conversationhttp.RegisterProtectedRoutes(protected, r.conversationHandler, r.ac)

	protected.HandleFunc("/ws/conversations", r.ac(cv, workspace_domain.ActionRead, r.conversationWSHandler.HandleWebSocket))
	protected.HandleFunc("/ws/dialer", r.ac(cv, workspace_domain.ActionUpdate, r.dialerWSHandler.HandleWebSocket))
}

func (r *router) setupStageRoutes(protected *mux.Router) {
	stagehttp.RegisterRoutes(protected, r.stageHandler, r.ac)
}

func (r *router) setupStageGroupRoutes(protected *mux.Router) {
	tg := workspace_domain.ResourceStageGroups
	tgRoutes := protected.PathPrefix("/stage-groups").Subrouter()
	tgRoutes.HandleFunc("", r.ac(tg, workspace_domain.ActionRead, r.stageGroupHandler.List)).Methods(http.MethodGet)
	tgRoutes.HandleFunc("", r.ac(tg, workspace_domain.ActionCreate, r.stageGroupHandler.Create)).Methods(http.MethodPost)
	tgRoutes.HandleFunc("/{id}", r.ac(tg, workspace_domain.ActionRead, r.stageGroupHandler.Get)).Methods(http.MethodGet)
	tgRoutes.HandleFunc("/{id}", r.ac(tg, workspace_domain.ActionUpdate, r.stageGroupHandler.Update)).Methods(http.MethodPut)
	tgRoutes.HandleFunc("/{id}", r.ac(tg, workspace_domain.ActionDelete, r.stageGroupHandler.Delete)).Methods(http.MethodDelete)
}

func (r *router) setupPipelineRoutes(protected *mux.Router) {
	pipelinehttp.RegisterRoutes(protected, r.pipelineHandler, r.ac)
}

func (r *router) setupSavedViewRoutes(protected *mux.Router) {
	savedviewhttp.RegisterRoutes(protected, r.savedViewHandler, r.ac)
}

func (r *router) setupOpportunityRoutes(protected *mux.Router) {
	opportunityboardhttp.RegisterProtectedRoutes(protected, r.opportunityBoardHandler, r.ac)
	opportunityhttp.RegisterProtectedRoutes(protected, r.opportunityHandler, r.ac)
}

func (r *router) setupCRMBoardRoutes(protected *mux.Router) {
	crmboardhttp.RegisterProtectedRoutes(protected, r.crmBoardHandler, r.ac)
	crmbulkhttp.RegisterRoutes(protected, r.crmBulkHandler, r.ac)
}

func (r *router) setupCustomFieldRoutes(protected *mux.Router) {
	customfieldhttp.RegisterRoutes(protected, r.customFieldHandler, r.ac)
}

func (r *router) setupLabelRoutes(protected *mux.Router) {
	labelhttp.RegisterRoutes(protected, r.labelHandler, r.ac)
}

func (r *router) setupMessageShortcutRoutes(protected *mux.Router) {
	messageshortcuthttp.RegisterRoutes(protected, r.messageShortcutHandler, r.ac)
}

func (r *router) setupTextRefinerRoutes(protected *mux.Router) {
	textrefinerhttp.RegisterProtectedRoutes(protected, r.textRefinerHandler)
}

func (r *router) setupWorkspaceRoutes(protected *mux.Router) {
	workspacehttp.RegisterProtectedRoutes(protected, r.workspaceHandler, r.ac, r.workspaceMiddleware)
}

func (r *router) setupIssueRoutes(protected *mux.Router) {
	issuehttp.RegisterProtectedRoutes(protected, r.issueHandler, r.ac)
}

func (r *router) setupAdminIssueRoutes(admin *mux.Router) {
	issuehttp.RegisterAdminRoutes(admin, r.issueHandler)
}

func (r *router) setupWorkflowRoutes(protected *mux.Router) {
	wf := workspace_domain.ResourceWorkflows
	protected.HandleFunc("/workflows/node-types", r.ac(wf, workspace_domain.ActionRead, r.workflowHandler.NodeTypes)).Methods(http.MethodGet)
	protected.HandleFunc("/workflows/resolve-handles", r.ac(wf, workspace_domain.ActionRead, r.workflowHandler.ResolveHandles)).Methods(http.MethodPost)
	protected.HandleFunc("/workflows/validate", r.ac(wf, workspace_domain.ActionRead, r.workflowHandler.ValidateGraph)).Methods(http.MethodPost)
	protected.HandleFunc("/workflows", r.ac(wf, workspace_domain.ActionRead, r.workflowHandler.List)).Methods(http.MethodGet)
	protected.HandleFunc("/workflows", r.ac(wf, workspace_domain.ActionCreate, r.workflowHandler.Create)).Methods(http.MethodPost)
	protected.HandleFunc("/workflows/import", r.ac(wf, workspace_domain.ActionCreate, r.workflowHandler.Import)).Methods(http.MethodPost)
	protected.HandleFunc("/workflows/{id}", r.ac(wf, workspace_domain.ActionRead, r.workflowHandler.Get)).Methods(http.MethodGet)
	protected.HandleFunc("/workflows/{id}", r.ac(wf, workspace_domain.ActionUpdate, r.workflowHandler.Update)).Methods(http.MethodPut)
	protected.HandleFunc("/workflows/{id}/department", r.ac(wf, workspace_domain.ActionUpdate, r.workflowHandler.AssignDepartment)).Methods(http.MethodPatch)
	protected.HandleFunc("/workflows/{id}", r.ac(wf, workspace_domain.ActionDelete, r.workflowHandler.Delete)).Methods(http.MethodDelete)
	protected.HandleFunc("/workflows/{id}/export", r.ac(wf, workspace_domain.ActionRead, r.workflowHandler.Export)).Methods(http.MethodGet)
	protected.HandleFunc("/workflows/{id}/activate", r.ac(wf, workspace_domain.ActionUpdate, r.workflowHandler.Activate)).Methods(http.MethodPost)
	protected.HandleFunc("/workflows/{id}/pause", r.ac(wf, workspace_domain.ActionUpdate, r.workflowHandler.Pause)).Methods(http.MethodPost)
	protected.HandleFunc("/workflows/{id}/runs", r.ac(wf, workspace_domain.ActionRead, r.workflowHandler.ListRuns)).Methods(http.MethodGet)
	protected.HandleFunc("/workflows/{id}/runs", r.ac(wf, workspace_domain.ActionCreate, r.workflowHandler.StartRun)).Methods(http.MethodPost)
	protected.HandleFunc("/workflows/{id}/nodes/{nodeId}/analyze", r.ac(wf, workspace_domain.ActionRead, r.workflowHandler.AnalyzeNode)).Methods(http.MethodGet)
	protected.HandleFunc("/workflows/{id}/nodes/{nodeId}/test", r.ac(wf, workspace_domain.ActionUpdate, r.workflowHandler.TestNode)).Methods(http.MethodPost)
	workflowwebhookhttp.RegisterRoutes(protected, r.workflowWebhookHandler, r.ac)
	protected.HandleFunc("/workflow-runs/{runId}", r.ac(wf, workspace_domain.ActionRead, r.workflowHandler.GetRun)).Methods(http.MethodGet)
	protected.HandleFunc("/workflow-runs/{runId}/cancel", r.ac(wf, workspace_domain.ActionUpdate, r.workflowHandler.CancelRun)).Methods(http.MethodPost)
	if r.builderSessionHandler != nil {
		buildersessionhttp.RegisterRoutes(protected, r.builderSessionHandler, r.ac)
	}
	if r.wsWorkflowSimulatorHandler != nil {
		protected.HandleFunc("/ws/workflows/{id}/simulate", r.ac(wf, workspace_domain.ActionUpdate, r.wsWorkflowSimulatorHandler.HandleSimulate))
	}
	if r.wsWorkflowAIBuilderHandler != nil {
		// Edit an existing workflow (ActionUpdate) and build a new one from
		// scratch (ActionCreate). Drafts only, persistence stays on the normal
		// create/update HTTP path.
		protected.HandleFunc("/ws/workflows/{id}/ai-builder", r.ac(wf, workspace_domain.ActionUpdate, r.wsWorkflowAIBuilderHandler.HandleSession))
		protected.HandleFunc("/ws/workflows/ai-builder", r.ac(wf, workspace_domain.ActionCreate, r.wsWorkflowAIBuilderHandler.HandleSession))
	}
}

func (r *router) setupCalendarRoutes(protected *mux.Router) {
	calendarhttp.RegisterProtectedRoutes(protected, r.calendarHandler, r.ac)
}

func (r *router) setupDepartmentRoutes(protected *mux.Router) {
	workspacedepartmenthttp.RegisterRoutes(protected, r.workspaceDepartmentHandler, r.ac)
}

func (r *router) setupWorkspaceConfigRoutes(protected *mux.Router) {
	workspaceconfighttp.RegisterProtectedRoutes(protected, r.workspaceConfigHandler)
}

func (r *router) setupAttendanceRoutes(protected *mux.Router) {
	attendancehttp.RegisterProtectedRoutes(protected, r.attendanceHandler, r.ac)

	// Telephony metrics dashboard (volume, queue, occupancy, voice AI).
	// Gated by attendance:read, same metrics RBAC as omnichannel service metrics.
	// Dialer:use remains only for placing/transferring calls, not for viewing stats.
	telephonyhttp.RegisterRoutes(protected, r.telephonyHandler, r.ac)
}

func (r *router) GetHandler() http.Handler {
	return r.mux
}

func (r *router) setupPublicAffiliateRoutes() {
	affiliatehttp.RegisterPublicRoutes(r.mux, r.affiliateHandler)
	r.mux.HandleFunc("/affiliate/{code}/plans", r.workspacePlanHandler.ListByAffiliateCode).Methods(http.MethodGet)
}

func (r *router) setupAffiliateRoutes(protected *mux.Router) {
	affiliatehttp.RegisterProtectedRoutes(protected, r.affiliateHandler)
	protected.HandleFunc("/affiliate/plans", r.workspacePlanHandler.ListMyExclusivePlans).Methods(http.MethodGet)
}

func (r *router) setupAdminAffiliateRoutes(adminRoutes *mux.Router) {
	affiliatehttp.RegisterAdminRoutes(adminRoutes, r.affiliateHandler)
}
