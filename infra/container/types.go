package container

import (
	"context"

	"gorm.io/gorm"

	deliveryHttp "vozko/delivery/http"
	affiliatehttp "vozko/delivery/http/affiliate"
	analysishttp "vozko/delivery/http/analysis"
	analyticshttp "vozko/delivery/http/analytics"
	attendancehttp "vozko/delivery/http/attendance"
	authhttp "vozko/delivery/http/auth"
	balancehttp "vozko/delivery/http/balance"
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
	exporthttp "vozko/delivery/http/export"
	"vozko/delivery/http/handlers"
	holdmusichttp "vozko/delivery/http/holdmusic"
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
	scheduledmessagehttp "vozko/delivery/http/scheduledmessage"
	shortlinkhttp "vozko/delivery/http/shortlink"
	stagehttp "vozko/delivery/http/stage"
	supportinboxhttp "vozko/delivery/http/supportinbox"
	systemconfighttp "vozko/delivery/http/systemconfig"
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
	"vozko/domain/address"
	affiliate_domain "vozko/domain/affiliate"
	agent_domain "vozko/domain/agent"
	domainmcp "vozko/domain/agent/mcp"
	ap_domain "vozko/domain/agent_presence"
	"vozko/domain/ai"
	aa_domain "vozko/domain/ai_attendance"
	aichat_domain "vozko/domain/aichat"
	"vozko/domain/analysis"
	analytics_domain "vozko/domain/analytics"
	attendance_domain "vozko/domain/attendance"
	"vozko/domain/auth"
	balance_domain "vozko/domain/balance"
	billing_domain "vozko/domain/billing"
	branch_domain "vozko/domain/branch"
	"vozko/domain/business_metrics"
	cache "vozko/domain/cache"
	calendar_domain "vozko/domain/calendar"
	call_roulette_domain "vozko/domain/call_roulette"
	call_billing_domain "vozko/domain/calls/billing"
	call_cdr_domain "vozko/domain/calls/cdr"
	call_recordings "vozko/domain/calls/recordings"
	"vozko/domain/cart"
	"vozko/domain/category"
	"vozko/domain/cep"
	"vozko/domain/cluster"
	coexistence_domain "vozko/domain/coexistence"
	config_domain "vozko/domain/config"
	conversation_domain "vozko/domain/conversation"
	ce_domain "vozko/domain/conversation_event"
	"vozko/domain/crm_telemetry"
	"vozko/domain/customer"
	customfield_domain "vozko/domain/customfield"
	dialer_domain "vozko/domain/dialer"
	export_domain "vozko/domain/export"
	ia_domain "vozko/domain/inbox_assignment"
	"vozko/domain/insurance"
	"vozko/domain/inventory"
	invoice_domain "vozko/domain/invoice"
	issues_domain "vozko/domain/issues"
	issue_response_domain "vozko/domain/issues/issue_response"
	label_domain "vozko/domain/label"
	lead_domain "vozko/domain/lead"
	lead_campaign_send_domain "vozko/domain/lead_campaign_send"
	lead_message_window_domain "vozko/domain/lead_message_window"
	"vozko/domain/media"
	msg_shortcut_domain "vozko/domain/message_shortcut"
	"vozko/domain/messaging"
	"vozko/domain/notification"
	opportunity_domain "vozko/domain/opportunity"
	"vozko/domain/order"
	"vozko/domain/payment"
	pipeline_domain "vozko/domain/pipeline"
	"vozko/domain/product"
	"vozko/domain/property"
	qe_domain "vozko/domain/queue_event"
	rag_domain "vozko/domain/rag"
	savedview_domain "vozko/domain/savedview"
	scheduled_message_domain "vozko/domain/scheduled_message"
	"vozko/domain/shipping"
	"vozko/domain/shop"
	shortlink_domain "vozko/domain/shortlink"
	sip_trunk_domain "vozko/domain/sip_trunk"
	stage_domain "vozko/domain/stage"
	si_domain "vozko/domain/support_inbox"
	telephony_domain "vozko/domain/telephony"
	text_refiner_domain "vozko/domain/text_refiner"
	"vozko/domain/ticket"
	"vozko/domain/tools"
	"vozko/domain/user"
	webhook_domain "vozko/domain/webhook"
	businessphone "vozko/domain/whatsapp/business_phone"
	callpermission_domain "vozko/domain/whatsapp/call_permission"
	whatsapp_template "vozko/domain/whatsapp/template"
	waba_domain "vozko/domain/whatsapp/waba"
	wc_domain "vozko/domain/whatsapp_campaign"
	wc_entry_domain "vozko/domain/whatsapp_campaign_entry"
	workflow_domain "vozko/domain/workflow"
	workspace_domain "vozko/domain/workspace"
	workspace_addon_domain "vozko/domain/workspace/workspace_addon"
	workspace_department_domain "vozko/domain/workspace/workspace_department"
	workspace_plan_domain "vozko/domain/workspace/workspace_plan"
	workspace_pricing_domain "vozko/domain/workspace/workspace_pricing"
	workspace_config_domain "vozko/domain/workspace_config"
	workspace_phone_access_domain "vozko/domain/workspace_phone_access"
	workspace_template_access_domain "vozko/domain/workspace_template_access"
	asaas_service "vozko/infra/asaas"
	redisCache "vozko/infra/cache"
	"vozko/infra/cloudflare"
	"vozko/infra/config"
	conversation_infra "vozko/infra/conversation"
	cronPackage "vozko/infra/cron"
	queue "vozko/infra/messaging"
	prometheus_service "vozko/infra/prometheus"
	"vozko/infra/security"
	businessphone_infra "vozko/infra/whatsapp/business_phone"
	"vozko/infra/whisper"
	ucmcp "vozko/usecases/agent/mcp"
	aa_usecase "vozko/usecases/ai_attendance"
	aichat_usecase "vozko/usecases/aichat"
	balance_usecase "vozko/usecases/balance"
	cu_usecase "vozko/usecases/call_roulette"
	calls_usecase "vozko/usecases/calls"
	conversation_usecase "vozko/usecases/conversation"
	copilot_usecase "vozko/usecases/copilot"
	crm_telemetry_usecase "vozko/usecases/crm_telemetry"
	customfield_usecase "vozko/usecases/customfield"
	dialer_usecase "vozko/usecases/dialer"
	ia_usecase "vozko/usecases/inbox_assignment"
	notification_usecase "vozko/usecases/notification"
	opportunity_usecase "vozko/usecases/opportunity"
	workflow_usecase "vozko/usecases/workflow"
)

type Container struct {
	cfg           config.Config
	db            *gorm.DB
	redisProvider *redisCache.RedisProvider
	replicaID     string
	repositories  *repositories
	services      *services
	useCases      *useCases
	handlers      *handlers_
	agentMCP      *handlers.AgentMCPBundle
	// instagram is the Instagram channel, wired as one self-contained bundle so
	// it can be disabled without threading nil checks through the god-structs.
	instagram *instagramBundle
	// telegram is the Telegram channel, wired as one self-contained bundle for
	// the same reason: it can be enabled or skipped without threading a dozen
	// fields through the god-structs.
	telegram *telegramBundle
	// unofficialWhatsApp is WhatsApp over a linked-device session, wired as one
	// self-contained bundle like the other two channels.
	unofficialWhatsApp *unofficialWhatsAppBundle
	mcpCollection      domainmcp.CollectionRepository
	mcpRegistry        *ucmcp.Registry
	router             deliveryHttp.Router
	server             deliveryHttp.HTTPServer
	metricsHTTP        *metricsServer
	jobRunner          *cronPackage.JobRunner
	recordingPool      *calls_usecase.RecordingUploadPool

	cfPublisher       *cloudflare.Publisher
	cfPublisherCancel context.CancelFunc

	dialerTransferReaperCancel context.CancelFunc
}

type repositories struct {
	product                 product.ProductRepository
	property                property.PropertyRepository
	category                category.Repository
	agent                   agent_domain.Repository
	lead                    lead_domain.Repository
	conversation            conversation_domain.MessageRepository
	analysis                analysis.Repository
	user                    user.UserRepository
	media                   media.MediaRepository
	cart                    cart.CartRepository
	address                 address.AddressRepository
	cep                     cep.CEPRepository
	order                   order.OrderRepository
	payment                 payment.PaymentRepository
	paymentSplit            payment.PaymentSplitRepository
	ticket                  ticket.Repository
	shippingAccount         shipping.ProviderAccountRepository
	insurance               insurance.InsuranceRepository
	whatsappTemplate        whatsapp_template.Repository
	passwordResetToken      auth.PasswordResetTokenRepository
	emailVerification       auth.EmailVerificationRepository
	systemConfig            config_domain.SystemConfigRepository
	customer                customer.CustomerRepository
	businessMetrics         business_metrics.Repository
	shop                    shop.Repository
	wcCampaign              wc_domain.Repository
	wcEntry                 wc_entry_domain.Repository
	businessPhone           businessphone.Repository
	ownerPhoneReader        businessphone.OwnerPhoneReader
	callRecording           call_recordings.CallRecordRepository
	callCDR                 call_cdr_domain.Repository
	balance                 balance_domain.Repository
	workspacePricing        workspace_pricing_domain.Repository
	workspaceTemplateAccess workspace_template_access_domain.Repository
	workspacePhoneAccess    workspace_phone_access_domain.Repository
	sipTrunk                sip_trunk_domain.Repository
	branch                  branch_domain.Repository
	leadMessageWindow       lead_message_window_domain.Repository
	callPermission          callpermission_domain.Repository
	leadCampaignSend        lead_campaign_send_domain.Repository
	conversationMedia       conversation_domain.ConversationMediaRepository
	stage                   stage_domain.Repository
	stageGroup              stage_domain.StageGroupRepository
	pipeline                pipeline_domain.Repository
	savedView               savedview_domain.Repository
	opportunity             opportunity_domain.Repository
	opportunityLink         opportunity_domain.LinkRepository
	customField             customfield_domain.Repository
	label                   label_domain.Repository
	messageShortcut         msg_shortcut_domain.Repository
	scheduledMessage        scheduled_message_domain.Repository
	workspace               workspace_domain.Repository
	customRole              workspace_domain.CustomRoleRepository
	attendance              attendance_domain.Repository
	telephony               telephony_domain.Repository
	conversationEvent       ce_domain.Repository
	assignmentHistory       ia_domain.HistoryRepository
	aiAttendance            aa_domain.Repository
	queueEvent              qe_domain.Repository
	agentPresence           ap_domain.Repository
	ragKnowledgeBase        rag_domain.KnowledgeBaseRepository
	ragDocument             rag_domain.DocumentRepository
	ragChunk                rag_domain.ChunkRepository
	ragVector               rag_domain.VectorRepository
	ragAgentKB              rag_domain.AgentKnowledgeBaseRepository
	shortLink               shortlink_domain.ShortLinkRepository
	shortLinkClick          shortlink_domain.ClickRepository
	waba                    waba_domain.Repository
	invoice                 invoice_domain.Repository
	callBilling             call_billing_domain.Repository
	analytics               analytics_domain.Repository
	callRoulette            call_roulette_domain.Repository
	workspacePlan           workspace_plan_domain.PlanRepository
	workspaceSubscription   workspace_plan_domain.SubscriptionRepository
	addonDefinition         workspace_addon_domain.AddonDefinitionRepository
	addonSubscription       workspace_addon_domain.AddonSubscriptionRepository
	aichatThread            aichat_domain.ThreadRepository
	aichatMessage           aichat_domain.MessageRepository
	workspaceConfig         workspace_config_domain.Repository
	supportInbox            si_domain.Repository
	supportEntry            si_domain.EntryRepository
	supportSession          si_domain.SessionRepository
	issue                   issues_domain.Repository
	issueResponse           issue_response_domain.Repository
	workflow                workflow_domain.WorkflowRepository
	workflowRun             workflow_domain.WorkflowRunRepository
	workflowRunLog          workflow_domain.WorkflowRunLogRepository
	workflowWebhook         workflow_domain.WorkflowWebhookRepository
	entryOwnership          workflow_domain.EntryOwnershipChecker
	entryResolver           workflow_domain.EntryResolver
	builderSession          workflow_domain.BuilderSessionRepository
	calendar                calendar_domain.Repository
	inboxAssignment         ia_domain.Repository
	workspaceDepartment     workspace_department_domain.Repository
	labelGroup              label_domain.LabelGroupRepository
	session                 auth.SessionRepository
	affiliate               affiliate_domain.Repository
}

type services struct {
	amqpPool                      *queue.ConnectionPool
	workflowWakePub               messaging.MessageQueuePub
	workflowWakeSub               messaging.MessageQueueSub
	metricsQueuePub               messaging.MessageQueuePub
	metricsQueueSub               messaging.MessageQueueSub
	crmTelemetryPub               messaging.MessageQueuePub
	crmTelemetrySub               messaging.MessageQueueSub
	crmTelemetryPublisher         crm_telemetry.Publisher
	crmTelemetryEmitter           *crm_telemetry_usecase.Emitter
	notificationsQueuePub         messaging.MessageQueuePub
	notificationQueueSub          messaging.MessageQueueSub
	wcQueuePub                    messaging.MessageQueuePub
	wcQueueSub                    messaging.MessageQueueSub
	cache                         cache.Cache
	rateLimiterFactory            cache.RateLimiterFactory
	sipTrunkManager               sip_trunk_domain.TrunkManager
	branchRegistrar               interface{ Stop() } // *voipinfra.BranchRegistrar; drained on Shutdown
	trunkOwnership                *sip_trunk_domain.TrunkOwnershipManager
	clusterRegistry               *cluster.Registry
	metrics                       *prometheus_service.PrometheusService
	ai                            ai.Service
	whatsapp                      conversation_domain.WhatsAppClient
	password                      auth.PasswordService
	tokenService                  *security.JWTTokenService
	readMeTokenService            *security.JWTTokenService
	fileStorage                   media.FileStorage
	ticketFileStorage             ticket.FileStorage
	asaasService                  asaas_service.AsaasServiceUseCases
	emailService                  notification.EmailService
	templateLoaderService         notification.TemplateLoader
	inventory                     inventory.VariantStockService
	documentValidator             customer.DocumentValidator
	pricingService                payment.PricingService
	shippingGateways              map[shipping.Provider]shipping.ProviderGateway
	agentProviders                map[agent_domain.AgentProvider]agent_domain.ProviderService
	toolProviders                 map[agent_domain.AgentProvider]tools.ProviderService
	toolRegistry                  tools.Service
	insuranceProviders            []insurance.QuoteProvider
	whisperPool                   *whisper.Pool
	businessPhoneMetaAPI          businessphone.MetaAPIService
	coexistenceMetaAPI            coexistence_domain.MetaCoexistenceService
	dialog360Onboarding           *businessphone_infra.Dialog360OnboardingService
	whatsappClientFactory         conversation_domain.WhatsAppClientFactory
	crmCallSource                 conversation_domain.CallSource
	whatsappCallWebhook           conversation_domain.WhatsAppCallWebhookHandler
	whatsappCallPermissionWebhook conversation_domain.WhatsAppCallPermissionWebhookHandler
	whatsappCallSignaling         conversation_domain.WhatsAppCallSignaling
	whatsappCallRegistry          conversation_domain.WhatsAppCallRegistry
	whatsappPublicMediaIP         string
	campaignWorkspaceResolver     conversation_domain.CampaignWorkspaceResolver
	// messageSender / conversationStatusService / conversationAuthImpl are the
	// CONCRETE types (not the domain interfaces) because per-channel registration
	// setters live on the implementations. Kept so a channel can register itself
	// after the conversation stack is built.
	messageSender             *conversation_usecase.MessageSenderService
	conversationStatusService *conversation_usecase.ConversationStatusService
	conversationAuthImpl      *conversation_infra.Authorizer
	requestCallPermission     conversation_domain.RequestCallPermissionUseCase
	conversationHistory       conversation_domain.HistoryProvider
	// channelAdapters accumulates every channel's send-side adapter. The registry
	// handed to consumers is rebuilt from this slice, so wiring a second channel
	// adds to it instead of replacing the first channel's registry.
	channelAdapters []conversation_domain.ChannelAdapter
	// liveChannelAdapters is the same set behind a registry that can be handed
	// out before every channel has registered. Consumers built during container
	// startup take this instead of a snapshot, so a channel wired later is still
	// visible to them.
	liveChannelAdapters *conversation_domain.LiveAdapterRegistry
	// conversationAutomation flips the per-conversation automation override on
	// any channel. Each channel registers its own setter.
	conversationAutomation *conversation_usecase.ConversationAutomationService
	// channelAIReply lets an agent attend any adapter-backed channel.
	channelAIReply    *conversation_usecase.ChannelAIReplyService
	callAdmission     dialer_domain.CallAdmissionCoordinator
	startOutboundCall dialer_domain.StartOutboundCallUseCase
	endOutboundCall   dialer_domain.EndOutboundCallUseCase
	dialerLifecycle   *dialer_usecase.OutboundCallLifecycleRunner
	conversationHub   *wsdelivery.ConversationHub
	// conversationStatusUpdater is the single choke point for finish/reopen/auto-close.
	conversationStatusUpdater conversation_domain.ConversationStatusUpdater
	// operatorSendFinalizer is the single definition of what happens after a
	// human reply is delivered. Held because more than one send surface needs it:
	// the WebSocket composer and the scheduled-message dispatcher.
	operatorSendFinalizer conversation_domain.OperatorSendFinalizer
	// operatorSend delivers a human-authored message on any channel. The live
	// wrapper is what the hub is constructed with; initConversationSenders points
	// it at the real use case once the message sender exists.
	operatorSend      conversation_domain.OperatorSendUseCase
	liveOperatorSend  *conversation_domain.LiveOperatorSend
	inboxService      conversation_domain.InboxService
	conversationAuth  conversation_domain.ConversationAuthorizer
	assignmentService *ia_usecase.AssignmentService
	// messageMarker owns read state and read receipts for every channel.
	messageMarker       *conversation_usecase.MessageMarkerService
	aiAttendanceService *aa_usecase.AsyncSessionService
	callRouletteService *cu_usecase.AssignmentService
	ragEmbedding        rag_domain.EmbeddingService
	ragTextChunker      rag_domain.TextChunker
	ragDocProcessor     rag_domain.DocumentProcessor
	ragTextExtractor    rag_domain.TextExtractor
	ragQueuePub         messaging.MessageQueuePub
	ragQueueSub         messaging.MessageQueueSub
	shortlinkQueuePub   messaging.MessageQueuePub
	shortlinkQueueSub   messaging.MessageQueueSub
	webhookQueuePub     messaging.MessageQueuePub
	webhookQueueSub     messaging.MessageQueueSub
	billingQueuePub     messaging.MessageQueuePub
	// Scheduled messages get their own exchange so a backlog of delayed sends
	// cannot sit behind another feature's traffic.
	scheduledMsgQueuePub messaging.MessageQueuePub
	scheduledMsgQueueSub messaging.MessageQueueSub
	billingQueueSub      messaging.MessageQueueSub
	recordingQueuePub    messaging.MessageQueuePub
	recordingQueueSub    messaging.MessageQueueSub
	googleCalendar       calendar_domain.GoogleOAuthService
	cachedBalanceChecker balance_domain.CachedBalanceChecker

	dialerSessions         dialer_domain.DialerSessionRegistry
	dialerCalls            dialer_domain.DialerCallRegistry
	dialerTransferStore    dialer_domain.TransferStore
	dialerTransferUC       dialer_domain.CallTransferUseCase
	setRingChannelsUC      *dialer_usecase.SetRingChannelsUseCase
	dialerUsernameResolver *dialerUsernameResolver
	receptiveInbound       dialer_domain.ReceptiveInboundHandler

	// Live telephony concurrency board (Redis).
	callSlotManager    *workspace_domain.CallSlotManager
	telephonyBoardSync telephony_domain.BoardSync
	telephonyBoardGet  telephony_domain.GetBoardUseCase
	telephonyCapacity  telephony_domain.CapacityReader
}

type useCases struct {
	createProduct         product.CreateProductUseCase
	updateProduct         product.UpdateProductUseCase
	launchVariantStock    product.LaunchVariantStockUseCase
	handleAsaasWebhook    payment.HandleAsaasWebhookUseCase
	handleWhatsAppMessage conversation_domain.HandleWhatsAppMessageUseCase
	getProduct            product.GetProductUseCase
	listProducts          product.ListProductsUseCase
	searchProducts        product.SearchProductsUseCase

	createProperty   property.CreatePropertyUseCase
	updateProperty   property.UpdatePropertyUseCase
	getProperty      property.GetPropertyUseCase
	listProperties   property.ListPropertiesUseCase
	searchProperties property.SearchPropertiesUseCase
	deleteProperty   property.DeletePropertyUseCase

	createCategory        category.CreateCategoryUseCase
	updateCategory        category.UpdateCategoryUseCase
	deleteCategory        category.DeleteCategoryUseCase
	getCategory           category.GetCategoryUseCase
	listCategories        category.ListCategoriesUseCase
	createAgent           agent_domain.CreateAgentUseCase
	aichat                *aichat_usecase.Service
	copilot               *copilot_usecase.Service
	updateAgent           agent_domain.UpdateAgentUseCase
	assignAgentDepartment agent_domain.AssignDepartmentUseCase
	deleteAgent           agent_domain.DeleteAgentUseCase
	getAgent              agent_domain.GetAgentUseCase
	listAgents            agent_domain.ListAgentsUseCase

	findUserByID             user.FindUserByIDUseCase
	updateUser               user.UpdateUserUseCase
	deleteUser               user.DeleteUserUseCase
	listUsers                user.ListUsersUseCase
	updateUserRole           user.UpdateUserRoleUseCase
	getWorkspaceSubscription workspace_plan_domain.GetWorkspaceSubscriptionUseCase

	uploadMedia     media.UploadMediaUseCase
	listMedia       media.ListMediaUseCase
	getMedia        media.GetMediaUseCase
	deleteHoldMusic media.DeleteHoldMusicUseCase

	getCart   cart.GetCartUseCase
	clearCart cart.ClearCartUseCase

	addToCart         cart.AddToCartUseCase
	removeFromCart    cart.RemoveFromCartUseCase
	updateCartItem    cart.UpdateCartItemUseCase
	decrementCartItem cart.DecrementCartItemUseCase

	consumeNotifications notification_usecase.ConsumeEmailUseCase
	publishEmail         notification_usecase.PublishEmailUseCase
	notifier             notification.Notifier
	dashboardURL         string
	monitorLowBalance    *balance_usecase.MonitorLowBalanceUseCase

	createAddress address.CreateAddressUseCase
	getAddresses  address.GetAddressesUseCase
	updateAddress address.UpdateAddressUseCase
	deleteAddress address.DeleteAddressUseCase

	checkout           order.CheckoutUseCase
	getOrder           order.GetOrderUseCase
	listOrders         order.ListOrdersUseCase
	cancelExpiredOrder order.CancelExpiredOrderUseCase

	searchCEP cep.CEPSearchUseCase

	credentialsLogin      auth.CredentialsLoginUseCase
	register              auth.RegisterUseCase
	adminRegister         auth.AdminRegisterUseCase
	refreshToken          auth.RefreshTokenUseCase
	requestPasswordReset  auth.RequestPasswordResetUseCase
	resetPassword         auth.ResetPasswordUseCase
	sendEmailVerification auth.SendEmailVerificationUseCase
	verifyEmailToken      auth.VerifyEmailTokenUseCase
	changePassword        auth.ChangePasswordUseCase
	logout                auth.LogoutUseCase
	logoutAll             auth.LogoutAllUseCase
	listSessions          auth.ListSessionsUseCase
	revokeSession         auth.RevokeSessionUseCase

	createPaymentSplit       payment.CreatePaymentSplitUseCase
	updatePaymentSplit       payment.UpdatePaymentSplitUseCase
	deletePaymentSplit       payment.DeletePaymentSplitUseCase
	getPaymentSplit          payment.GetPaymentSplitUseCase
	listPaymentSplits        payment.ListPaymentSplitsUseCase
	getPaymentSplitSuppliers payment.GetPaymentSplitSuppliersUseCase

	createTicket        ticket.CreateTicketUseCase
	listTickets         ticket.ListTicketsUseCase
	listUserTickets     ticket.ListUserTicketsUseCase
	getTicketByOrder    ticket.GetTicketByOrderUseCase
	uploadTicketDoc     ticket.UploadTicketDocumentUseCase
	updateTicketStatus  ticket.UpdateTicketStatusUseCase
	generateTicketLabel ticket.GenerateLabelUseCase

	getShippingAuthorizationURL shipping.GetAuthorizationURLUseCase
	connectShippingAccount      shipping.ConnectProviderAccountUseCase
	listShippingAccounts        shipping.ListProviderAccountsUseCase
	calculateFreight            shipping.CalculateFreightUseCase

	quoteInsurance                insurance.QuoteInsuranceUseCase
	listInsuranceQuotations       insurance.ListUserQuotationsUseCase
	getInsuranceQuotation         insurance.GetQuotationUseCase
	listInsurancePolicies         insurance.ListPoliciesUseCase
	describeInsuranceRequirements insurance.DescribeQuoteRequirementsUseCase

	listWhatsAppTemplates         whatsapp_template.ListUseCase
	getWhatsAppTemplate           whatsapp_template.GetUseCase
	syncWhatsAppTemplates         whatsapp_template.SyncTemplatesUseCase
	reconcileWhatsAppTemplates    whatsapp_template.ReconcileTemplatesUseCase
	reconcileWhatsAppEntitlements businessphone.EntitlementReconciler
	syncWhatsAppTemplate          whatsapp_template.SyncTemplateUseCase
	sendWhatsAppTemplate          whatsapp_template.SendTemplateMessageUseCase
	createWhatsAppTemplate        whatsapp_template.CreateTemplateUseCase
	replicateWhatsAppTemplate     whatsapp_template.ReplicateTemplateUseCase
	setHeaderMediaWhatsApp        whatsapp_template.SetTemplateHeaderMediaUseCase
	deleteWhatsAppTemplate        whatsapp_template.DeleteTemplateUseCase
	handleTemplateWebhook         whatsapp_template.HandleTemplateWebhookUseCase

	getSystemConfig    config_domain.GetSystemConfigUseCase
	updateSystemConfig config_domain.UpdateSystemConfigUseCase

	getWorkspaceConfig         workspace_config_domain.GetWorkspaceConfigUseCase
	updateWorkspaceConfig      workspace_config_domain.UpdateWorkspaceConfigUseCase
	updateWorkspaceConfigOwner workspace_config_domain.UpdateWorkspaceConfigOwnerUseCase

	recordMetric         business_metrics.RecordMetricUseCase
	consumeMetric        business_metrics.ConsumeMetricUseCase
	listMetrics          business_metrics.ListMetricsUseCase
	getMetricsStats      business_metrics.GetMetricsStatsUseCase
	getMetricsTimeSeries business_metrics.GetMetricsTimeSeriesUseCase

	listAnalysis     analysis.ListAnalysisUseCase
	getAnalysisStats analysis.GetAnalysisStatsUseCase
	getEntryAnalysis analysis.GetEntryAnalysisUseCase

	createShop shop.CreateShopUseCase
	updateShop shop.UpdateShopUseCase
	deleteShop shop.DeleteShopUseCase
	getShop    shop.GetShopUseCase
	listShops  shop.ListShopsUseCase

	createWCCampaign                 wc_domain.CreateCampaignUseCase
	updateWCCampaign                 wc_domain.UpdateCampaignUseCase
	assignWCCampaignDepartment       wc_domain.AssignDepartmentUseCase
	deleteWCCampaign                 wc_domain.DeleteCampaignUseCase
	getWCCampaign                    wc_domain.GetCampaignUseCase
	listWCCampaigns                  wc_domain.ListCampaignsUseCase
	getWCCampaignsSummary            wc_domain.GetSummaryUseCase
	ensureOrganicCoexistenceCampaign wc_domain.EnsureOrganicCoexistenceCampaignUseCase
	listWCEntries                    wc_domain.ListEntriesUseCase
	resetWCCampaign                  wc_domain.ResetCampaignUseCase
	clearHistoryWCCampaign           wc_domain.ClearHistoryUseCase
	deleteEntryWCCampaign            wc_domain.DeleteEntryUseCase
	updateEntryWCCampaign            wc_domain.UpdateEntryUseCase
	addEntriesWCCampaign             wc_domain.AddEntriesUseCase
	quickSendWCCampaign              wc_domain.QuickSendUseCase
	dispatchWCCampaign               wc_domain.DispatchCampaignUseCase
	messageConsumerWCCampaign        wc_domain.MessageConsumerUseCase

	listBusinessPhones         businessphone.ListUseCase
	getBusinessPhone           businessphone.GetUseCase
	syncBusinessPhone          businessphone.SyncPhoneNumberUseCase
	registerBusinessPhone      businessphone.RegisterPhoneUseCase
	deregisterBusinessPhone    businessphone.DeregisterPhoneUseCase
	releaseBusinessPhone       businessphone.ReleasePhoneUseCase
	requestBusinessPhoneVerify businessphone.RequestVerificationCodeUseCase
	verifyBusinessPhoneCode    businessphone.VerifyCodeUseCase
	updateBusinessPhoneProfile businessphone.UpdateBusinessProfileUseCase
	getBusinessPhoneProfile    businessphone.GetBusinessProfileUseCase
	deleteBusinessPhone        businessphone.DeletePhoneNumberUseCase
	unassignBusinessPhoneOwner businessphone.UnassignOwnerUseCase
	onboardEmbeddedSignup      businessphone.OnboardEmbeddedSignupUseCase
	handlePhoneWebhook         businessphone.HandlePhoneWebhookUseCase

	listWABAs waba_domain.ListUseCase
	getWABA   waba_domain.GetUseCase

	createBalance           balance_domain.CreateBalanceUseCase
	getBalance              balance_domain.GetBalanceUseCase
	creditBalance           balance_domain.CreditBalanceUseCase
	debitBalance            balance_domain.DebitBalanceUseCase
	listTransactions        balance_domain.ListTransactionsUseCase
	creditResource          balance_domain.CreditResourceUseCase
	debitResource           balance_domain.DebitResourceUseCase
	consumeWhatsappTemplate balance_domain.ConsumeWhatsappTemplateUseCase
	checkBalance            balance_domain.CheckBalanceUseCase
	getFullBalanceSummary   balance_domain.GetFullBalanceSummaryUseCase
	getOrCreateBalance      balance_domain.GetOrCreateBalanceUseCase
	getOrCreateFullSummary  balance_domain.GetOrCreateFullBalanceSummaryUseCase

	createInvoice invoice_domain.CreateInvoiceUseCase
	listInvoices  invoice_domain.ListInvoicesUseCase
	getInvoice    invoice_domain.GetInvoiceUseCase

	emitMonthlyInvoices     billing_domain.EmitMonthlyInvoicesUseCase
	cancelBillingSweep      billing_domain.CancelSweepUseCase
	vendorChannelReconciler businessphone.VendorChannelReconciler
	channelStatusReconciler businessphone.ChannelStatusReconciler

	grantTemplateAccess  workspace_template_access_domain.GrantAccessUseCase
	revokeTemplateAccess workspace_template_access_domain.RevokeAccessUseCase
	listWorkspaceAccess  workspace_template_access_domain.ListWorkspaceAccessUseCase
	listTemplateAccess   workspace_template_access_domain.ListTemplateAccessUseCase
	checkTemplateAccess  workspace_template_access_domain.CheckAccessUseCase

	grantPhoneAccess         workspace_phone_access_domain.GrantAccessUseCase
	revokePhoneAccess        workspace_phone_access_domain.RevokeAccessUseCase
	listWorkspacePhoneAccess workspace_phone_access_domain.ListWorkspaceAccessUseCase
	listPhoneAccess          workspace_phone_access_domain.ListPhoneAccessUseCase
	checkPhoneAccess         workspace_phone_access_domain.CheckAccessUseCase

	createSIPTrunk          sip_trunk_domain.CreateUseCase
	updateSIPTrunk          sip_trunk_domain.UpdateUseCase
	deleteSIPTrunk          sip_trunk_domain.DeleteUseCase
	getSIPTrunk             sip_trunk_domain.GetUseCase
	listSIPTrunks           sip_trunk_domain.ListUseCase
	listSIPTrunksByIDs      sip_trunk_domain.ListByIDsUseCase
	listAccessibleSIPTrunks sip_trunk_domain.ListAccessibleUseCase
	enableSIPTrunk          sip_trunk_domain.EnableUseCase
	getSIPTrunkStatus       sip_trunk_domain.GetStatusUseCase
	assignOwnerSIPTrunk     sip_trunk_domain.AssignOwnerUseCase

	createBranch            branch_domain.CreateUseCase
	updateBranch            branch_domain.UpdateUseCase
	getBranch               branch_domain.GetUseCase
	listBranchesByWorkspace branch_domain.ListByWorkspaceUseCase
	listBranchesByUser      branch_domain.ListByUserUseCase
	deleteBranch            branch_domain.DeleteUseCase
	enableBranch            branch_domain.EnableUseCase
	rotateBranchSecret      branch_domain.RotateSecretUseCase

	sendConversationMessage conversation_domain.SendConversationMessageUseCase
	uploadConversationMedia conversation_domain.UploadConversationMediaUseCase
	getConversationMedia    conversation_domain.GetConversationMediaUseCase
	searchMessagesByEntry   conversation_domain.SearchMessagesByEntryUseCase
	listConversationEvents  ce_domain.ListEventsUseCase

	createStage          stage_domain.CreateStageUseCase
	cloneStagesFromGroup stage_domain.CloneStagesFromGroupUseCase
	updateStage          stage_domain.UpdateStageUseCase
	deleteStage          stage_domain.DeleteStageUseCase
	listStages           stage_domain.ListStagesUseCase
	setInitialStage      stage_domain.SetInitialStageUseCase
	assignEntryStage     stage_domain.AssignEntryStageUseCase
	removeEntryStage     stage_domain.RemoveEntryStageUseCase
	getEntryStage        stage_domain.GetEntryStageUseCase
	getBatchEntryStages  stage_domain.GetBatchEntryStagesUseCase
	reorderStages        stage_domain.ReorderStagesUseCase

	createStageGroup stage_domain.CreateStageGroupUseCase
	updateStageGroup stage_domain.UpdateStageGroupUseCase
	deleteStageGroup stage_domain.DeleteStageGroupUseCase
	listStageGroups  stage_domain.ListStageGroupsUseCase
	getStageGroup    stage_domain.GetStageGroupUseCase

	createPipeline pipeline_domain.CreatePipelineUseCase
	updatePipeline pipeline_domain.UpdatePipelineUseCase
	deletePipeline pipeline_domain.DeletePipelineUseCase
	listPipelines  pipeline_domain.ListPipelinesUseCase
	getPipeline    pipeline_domain.GetPipelineUseCase

	createSavedView     savedview_domain.CreateSavedViewUseCase
	updateSavedView     savedview_domain.UpdateSavedViewUseCase
	deleteSavedView     savedview_domain.DeleteSavedViewUseCase
	listSavedViews      savedview_domain.ListSavedViewsUseCase
	setDefaultSavedView savedview_domain.SetDefaultSavedViewUseCase

	opportunity *opportunity_usecase.Service
	customField *customfield_usecase.Service

	createLabel      label_domain.CreateLabelUseCase
	updateLabel      label_domain.UpdateLabelUseCase
	deleteLabel      label_domain.DeleteLabelUseCase
	listLabels       label_domain.ListLabelsUseCase
	assignEntryLabel label_domain.AssignEntryLabelUseCase
	removeEntryLabel label_domain.RemoveEntryLabelUseCase
	getEntryLabels   label_domain.GetEntryLabelsUseCase
	reorderLabels    label_domain.ReorderLabelsUseCase

	createMessageShortcut msg_shortcut_domain.CreateUseCase
	updateMessageShortcut msg_shortcut_domain.UpdateUseCase
	deleteMessageShortcut msg_shortcut_domain.DeleteUseCase
	listMessageShortcuts  msg_shortcut_domain.ListUseCase
	getByShortcut         msg_shortcut_domain.GetByShortcutUseCase

	refineText text_refiner_domain.RefineTextUseCase

	createWorkspace                    workspace_domain.CreateWorkspaceUseCase
	getWorkspace                       workspace_domain.GetWorkspaceUseCase
	listWorkspaces                     workspace_domain.ListWorkspacesUseCase
	updateWorkspace                    workspace_domain.UpdateWorkspaceUseCase
	inviteMember                       workspace_domain.InviteMemberUseCase
	acceptInvite                       workspace_domain.AcceptInviteUseCase
	declineInvite                      workspace_domain.DeclineInviteUseCase
	cancelInvite                       workspace_domain.CancelInviteUseCase
	listInvites                        workspace_domain.ListInvitesUseCase
	listWorkspaceInvites               workspace_domain.ListWorkspaceInvitesUseCase
	removeMember                       workspace_domain.RemoveMemberUseCase
	updateMemberRole                   workspace_domain.UpdateMemberRoleUseCase
	listWsMembers                      workspace_domain.ListMembersUseCase
	listMembersPaginated               workspace_domain.ListMembersPaginatedUseCase
	memberVisibility                   workspace_domain.MemberVisibilityUseCase
	listAssignableMembers              workspace_domain.ListAssignableMembersUseCase
	setMemberPermissions               workspace_domain.SetMemberPermissionsUseCase
	getMemberPermissions               workspace_domain.GetMemberPermissionsUseCase
	listResourcePermissions            workspace_domain.ListResourcePermissionsUseCase
	checkWsAccess                      workspace_domain.CheckAccessUseCase
	ensureDefaultWorkspace             workspace_domain.EnsureDefaultWorkspaceUseCase
	assignResource                     workspace_domain.AssignResourceUseCase
	unassignResource                   workspace_domain.UnassignResourceUseCase
	listResourceAssignments            workspace_domain.ListResourceAssignmentsUseCase
	checkResourceAccess                workspace_domain.CheckResourceAccessUseCase
	createCustomRole                   workspace_domain.CreateCustomRoleUseCase
	listCustomRoles                    workspace_domain.ListCustomRolesUseCase
	updateCustomRole                   workspace_domain.UpdateCustomRoleUseCase
	deleteCustomRole                   workspace_domain.DeleteCustomRoleUseCase
	assignCustomRole                   workspace_domain.AssignCustomRoleUseCase
	getDefaultPricingItems             workspace_pricing_domain.GetDefaultPricingItemsUseCase
	getResolvedPricing                 workspace_pricing_domain.GetResolvedPricingUseCase
	updatePricingItem                  workspace_pricing_domain.UpdatePricingItemUseCase
	getPricingAuditLog                 workspace_pricing_domain.GetPricingAuditLogUseCase
	getExchangeRate                    workspace_pricing_domain.GetExchangeRateUseCase
	updateExchangeRate                 workspace_pricing_domain.UpdateExchangeRateUseCase
	createWorkspacePlan                workspace_plan_domain.CreatePlanDefinitionUseCase
	updateWorkspacePlan                workspace_plan_domain.UpdatePlanDefinitionUseCase
	archiveWorkspacePlan               workspace_plan_domain.ArchivePlanDefinitionUseCase
	listWorkspacePlans                 workspace_plan_domain.ListPlanDefinitionsUseCase
	getWorkspacePlan                   workspace_plan_domain.GetPlanDefinitionUseCase
	createSubscriptionInvoice          workspace_plan_domain.CreateSubscriptionInvoiceUseCase
	subscribeWorkspacePlan             workspace_plan_domain.SubscribeWorkspaceUseCase
	renewWorkspaceSubscription         workspace_plan_domain.RenewWorkspaceSubscriptionUseCase
	cancelWorkspaceSubscription        workspace_plan_domain.CancelWorkspaceSubscriptionUseCase
	ensureCurrentWorkspaceSubscription workspace_plan_domain.EnsureCurrentWorkspaceSubscriptionUseCase
	ensureActiveWorkspaceSubscription  workspace_plan_domain.EnsureActiveWorkspaceSubscriptionUseCase
	expireSubscriptions                workspace_plan_domain.ExpireSubscriptionsUseCase
	remindExpiringSubscriptions        workspace_plan_domain.RemindExpiringSubscriptionsUseCase
	setPlanVisibility                  workspace_plan_domain.SetPlanVisibilityUseCase
	listVisiblePlans                   workspace_plan_domain.ListVisiblePlansUseCase
	setPlanExclusiveAffiliate          workspace_plan_domain.SetPlanExclusiveAffiliateUseCase
	listExclusivePlansByAffiliateCode  workspace_plan_domain.ListExclusivePlansByAffiliateCodeUseCase
	listMyExclusivePlans               workspace_plan_domain.ListMyExclusivePlansUseCase
	workspaceReferralReader            workspace_plan_domain.WorkspaceReferralReader
	createAddonDefinition              workspace_addon_domain.CreateAddonDefinitionUseCase
	updateAddonDefinition              workspace_addon_domain.UpdateAddonDefinitionUseCase
	archiveAddonDefinition             workspace_addon_domain.ArchiveAddonDefinitionUseCase
	listAddonDefinitions               workspace_addon_domain.ListAddonDefinitionsUseCase
	getAddonDefinition                 workspace_addon_domain.GetAddonDefinitionUseCase
	listAvailableAddons                workspace_addon_domain.ListAvailableAddonsUseCase
	purchaseAddon                      workspace_addon_domain.PurchaseAddonUseCase
	previewAddonPurchase               workspace_addon_domain.PreviewAddonPurchaseUseCase
	cancelAddonSubscription            workspace_addon_domain.CancelAddonSubscriptionUseCase
	listWorkspaceAddons                workspace_addon_domain.ListWorkspaceAddonsUseCase
	getWorkspaceEntitlements           workspace_addon_domain.GetWorkspaceEntitlementsUseCase
	entitlementResolver                workspace_addon_domain.EntitlementResolver
	phoneProvisioningGate              businessphone.ProvisioningGate
	getAttendanceStats                 attendance_domain.GetAttendanceStatsUseCase
	getWindowStats                     attendance_domain.GetWindowStatsUseCase
	getResponseTimeDistribution        attendance_domain.GetResponseTimeDistributionUseCase
	getAIAgentStats                    attendance_domain.GetAIAgentStatsUseCase
	getFRTStats                        attendance_domain.GetFRTStatsUseCase
	getOverview                        attendance_domain.GetOverviewUseCase
	getTelephonyOverview               telephony_domain.GetOverviewUseCase
	getTelephonyBoard                  telephony_domain.GetBoardUseCase
	consumeCRMTelemetry                crm_telemetry.Consumer

	createKnowledgeBase     rag_domain.CreateKnowledgeBaseUseCase
	updateKnowledgeBase     rag_domain.UpdateKnowledgeBaseUseCase
	deleteKnowledgeBase     rag_domain.DeleteKnowledgeBaseUseCase
	getKnowledgeBase        rag_domain.GetKnowledgeBaseUseCase
	listKnowledgeBases      rag_domain.ListKnowledgeBasesUseCase
	createRAGDocument       rag_domain.CreateDocumentUseCase
	deleteRAGDocument       rag_domain.DeleteDocumentUseCase
	getRAGDocument          rag_domain.GetDocumentUseCase
	listRAGDocuments        rag_domain.ListDocumentsUseCase
	linkAgentKnowledgeBases rag_domain.LinkAgentKnowledgeBasesUseCase
	getAgentKnowledgeBases  rag_domain.GetAgentKnowledgeBasesUseCase
	queryKnowledgeBase      rag_domain.QueryKnowledgeBaseUseCase
	publishDocProcessing    rag_domain.PublishDocumentProcessingUseCase
	consumeDocProcessing    rag_domain.ConsumeDocumentProcessingUseCase

	createShortLink       shortlink_domain.CreateShortLinkUseCase
	updateShortLink       shortlink_domain.UpdateShortLinkUseCase
	getShortLink          shortlink_domain.GetShortLinkUseCase
	listShortLinks        shortlink_domain.ListShortLinksUseCase
	deleteShortLink       shortlink_domain.DeleteShortLinkUseCase
	shortLinkStats        shortlink_domain.GetWorkspaceStatsUseCase
	resolveShortLink      shortlink_domain.ResolveShortLinkUseCase
	unlockShortLink       shortlink_domain.UnlockShortLinkUseCase
	publishShortLinkClick shortlink_domain.PublishClickUseCase
	consumeShortLinkClick shortlink_domain.ConsumeClickUseCase
	shortLinkAnalytics    shortlink_domain.GetAnalyticsUseCase
	shortLinkRecentClicks shortlink_domain.ListRecentClicksUseCase
	shortLinkQR           shortlink_domain.GenerateQRUseCase
	purgeShortLinkClicks  shortlink_domain.PurgeClicksUseCase

	// Scheduled messages. The dispatch use case is held because three surfaces
	// reach it: the HTTP layer (never directly), the queue consumer and the
	// sweep job.
	scheduleMessage          scheduled_message_domain.ScheduleUseCase
	rescheduleMessage        scheduled_message_domain.RescheduleUseCase
	cancelScheduledMessage   scheduled_message_domain.CancelUseCase
	listScheduledMessages    scheduled_message_domain.ListUseCase
	dispatchScheduledMessage scheduled_message_domain.DispatchUseCase
	consumeScheduledMessage  scheduled_message_domain.ConsumeFireUseCase
	sweepScheduledMessages   scheduled_message_domain.SweepJob
	purgeScheduledMessages   scheduled_message_domain.PurgeJob

	exportEntries export_domain.ExportEntriesUseCase

	publishWebhook              webhook_domain.PublishWebhookUseCase
	consumeWhatsAppMsgWebhook   conversation_domain.ConsumeWhatsAppMessageWebhookUseCase
	consumeWhatsAppPhoneWebhook businessphone.ConsumePhoneWebhookUseCase
	consumeWhatsAppTplWebhook   whatsapp_template.ConsumeTemplateWebhookUseCase
	consumeCoexistenceWebhook   coexistence_domain.ConsumeCoexistenceWebhookUseCase
	consumeAsaasWebhook         payment.ConsumeAsaasWebhookUseCase

	listBillingRecords call_billing_domain.ListBillingRecordsUseCase

	startCall          call_cdr_domain.StartCallUseCase
	completeCall       call_cdr_domain.CompleteCallUseCase
	getCall            call_cdr_domain.GetCallUseCase
	listCalls          call_cdr_domain.ListCallsUseCase
	billingQuery       call_billing_domain.QueryUseCase
	callRecordingQuery call_recordings.QueryUseCase

	getProfitReport     analytics_domain.GetProfitReportUseCase
	getCallAnalytics    analytics_domain.GetCallAnalyticsUseCase
	getAdminOverview    analytics_domain.GetAdminOverviewUseCase
	getPlanContractions analytics_domain.GetPlanContractionsUseCase

	createSupportInbox      si_domain.CreateInboxUseCase
	updateSupportInbox      si_domain.UpdateInboxUseCase
	deleteSupportInbox      si_domain.DeleteInboxUseCase
	getSupportInbox         si_domain.GetInboxUseCase
	listSupportInboxes      si_domain.ListInboxesUseCase
	createSupportSession    si_domain.CreateSessionUseCase
	reconnectSupportSession si_domain.ReconnectSessionUseCase

	createIssue       issues_domain.CreateIssueUseCase
	listIssues        issues_domain.ListIssuesUseCase
	listAllIssues     issues_domain.ListAllIssuesUseCase
	getIssue          issues_domain.GetIssueUseCase
	closeIssue        issues_domain.CloseIssueUseCase
	updateIssueStatus issues_domain.UpdateIssueStatusUseCase

	createIssueResponse issue_response_domain.CreateResponseUseCase
	listIssueResponses  issue_response_domain.ListResponsesUseCase

	createWorkflow           workflow_domain.CreateWorkflowUseCase
	updateWorkflow           workflow_domain.UpdateWorkflowUseCase
	assignWorkflowDepartment workflow_domain.AssignDepartmentUseCase
	deleteWorkflow           workflow_domain.DeleteWorkflowUseCase
	getWorkflow              workflow_domain.GetWorkflowUseCase
	listWorkflows            workflow_domain.ListWorkflowsUseCase
	activateWorkflow         workflow_domain.ActivateWorkflowUseCase
	pauseWorkflow            workflow_domain.PauseWorkflowUseCase
	startWorkflowRun         workflow_domain.StartRunUseCase
	cancelWorkflowRun        workflow_domain.CancelRunUseCase
	getWorkflowRun           workflow_domain.GetRunUseCase
	listWorkflowRuns         workflow_domain.ListRunsUseCase
	testWorkflowNode         workflow_usecase.TestNodeUseCase
	workflowWebhookConfig    workflow_usecase.WorkflowWebhookUseCase
	handleWebhookTrigger     workflow_usecase.HandleWebhookTriggerUseCase
	workflowManager          workflow_domain.WorkflowManager
	triggerEvaluator         workflow_domain.TriggerEvaluator
	consumeWorkflowWake      workflow_domain.ConsumeRunWakeUseCase
	nodeCatalogFn            func() []workflow_domain.NodeDefinition
	wsWorkflowSimulation     workflow_usecase.WSWorkflowSimulationUseCase
	wsWorkflowAIBuilder      workflow_usecase.AIBuilderUseCase

	createCalendarEvent      calendar_domain.CreateEventUseCase
	updateCalendarEvent      calendar_domain.UpdateEventUseCase
	deleteCalendarEvent      calendar_domain.DeleteEventUseCase
	getCalendarEvent         calendar_domain.GetEventUseCase
	listCalendarEvents       calendar_domain.ListEventsUseCase
	connectGoogleCalendar    calendar_domain.ConnectGoogleUseCase
	disconnectGoogleCalendar calendar_domain.DisconnectGoogleUseCase
	getGoogleConnection      calendar_domain.GetConnectionUseCase
	getGoogleAuthURL         calendar_domain.GetAuthURLUseCase

	startCalendarWatch         calendar_domain.StartWatchUseCase
	handleCalendarNotification calendar_domain.HandleNotificationUseCase
	stopCalendarWatch          calendar_domain.StopWatchUseCase
	renewCalendarChannels      calendar_domain.RenewExpiringChannelsUseCase

	createWorkspaceDepartment       workspace_department_domain.CreateDepartmentUseCase
	getWorkspaceDepartment          workspace_department_domain.GetDepartmentUseCase
	listWorkspaceDepartments        workspace_department_domain.ListDepartmentsUseCase
	listWorkspaceDepartmentsByIDs   workspace_department_domain.ListDepartmentsByIDsUseCase
	updateWorkspaceDepartment       workspace_department_domain.UpdateDepartmentUseCase
	deleteWorkspaceDepartment       workspace_department_domain.DeleteDepartmentUseCase
	addWorkspaceDepartmentMember    workspace_department_domain.AddMemberUseCase
	removeWorkspaceDepartmentMember workspace_department_domain.RemoveMemberUseCase
	listWorkspaceDepartmentMembers  workspace_department_domain.ListMembersUseCase

	affiliateRegister      affiliate_domain.RegisterAffiliateUseCase
	affiliateGetMy         affiliate_domain.GetMyAffiliateUseCase
	affiliateUpdateMy      affiliate_domain.UpdateMyAffiliateUseCase
	affiliateListReferrals affiliate_domain.ListReferralsUseCase
	affiliateListEarnings  affiliate_domain.ListEarningsUseCase
	affiliateValidateCode  affiliate_domain.ValidateReferralCodeUseCase
	affiliateTrackReferral affiliate_domain.TrackReferralUseCase
	affiliateRecordEarning affiliate_domain.RecordEarningUseCase
	affiliateStats         affiliate_domain.GetAffiliateStatsUseCase
	affiliateAdminList     affiliate_domain.AdminListAffiliatesUseCase
	affiliateAdminGet      affiliate_domain.AdminGetAffiliateUseCase
	affiliateAdminUpdate   affiliate_domain.AdminUpdateAffiliateUseCase
}

type handlers_ struct {
	product                 *handlers.ProductHandler
	property                *handlers.PropertyHandler
	category                *handlers.CategoryHandler
	agent                   *handlers.AgentHandler
	aichat                  *handlers.AIChatHandler
	auth                    *authhttp.AuthHandler
	user                    *userhttp.UserHandler
	media                   *mediashttp.MediasHandler
	holdMusic               *holdmusichttp.HoldMusicHandler
	cart                    *handlers.CartHandler
	address                 *handlers.AddressHandler
	order                   *handlers.OrderHandler
	cep                     *cephttp.CEPHandler
	webhook                 *handlers.WebhookHandler
	readMe                  *readmehttp.Handler
	paymentSplit            *paymentsplithttp.PaymentSplitHandler
	ticket                  *tickethttp.TicketHandler
	shipping                *handlers.ShippingHandler
	insurance               *handlers.InsuranceHandler
	whatsappTemplate        *whatsapptemplatehttp.WhatsAppTemplateHandler
	systemConfig            *systemconfighttp.SystemConfigHandler
	metrics                 *handlers.MetricsHandler
	metricsQuery            *handlers.MetricsQueryHandler
	businessMetrics         *businessmetricshttp.BusinessMetricsHandler
	shop                    *handlers.ShopHandler
	whatsappCampaign        *handlers.WhatsAppCampaignHandler
	whatsappBusinessPhone   *whatsappbusinessphonehttp.WhatsAppBusinessPhoneHandler
	metaEmbeddedSignup      *metaembeddedsignuphttp.MetaEmbeddedSignupHandler
	analysis                *analysishttp.AnalysisHandler
	lead                    *leadhttp.LeadHandler
	callRecording           *callrecordinghttp.CallRecordingHandler
	balance                 *balancehttp.BalanceHandler
	workspaceTemplateAccess *workspacetemplateaccesshttp.WorkspaceTemplateAccessHandler
	workspacePhoneAccess    *workspacephoneaccesshttp.WorkspacePhoneAccessHandler
	conversation            *conversationhttp.ConversationHandler
	conversationWS          *wsdelivery.ConversationWSHandler
	dialerWS                *wsdelivery.DialerWSHandler
	stage                   *stagehttp.StageHandler
	stageGroup              *handlers.StageGroupHandler
	pipeline                *pipelinehttp.PipelineHandler
	savedView               *savedviewhttp.SavedViewHandler
	opportunity             *opportunityhttp.OpportunityHandler
	opportunityBoard        *opportunityboardhttp.OpportunityBoardHandler
	customField             *customfieldhttp.CustomFieldHandler
	crmBoard                *crmboardhttp.CRMBoardHandler
	crmBulk                 *crmbulkhttp.CRMBulkHandler
	label                   *labelhttp.LabelHandler
	messageShortcut         *messageshortcuthttp.MessageShortcutHandler
	scheduledMessage        *scheduledmessagehttp.ScheduledMessageHandler
	textRefiner             *textrefinerhttp.TextRefinerHandler
	workspace               *workspacehttp.WorkspaceHandler
	workspacePricing        *workspacepricinghttp.WorkspacePricingHandler
	workspacePlan           *handlers.WorkspacePlanHandler
	workspaceAddon          *workspaceaddonhttp.WorkspaceAddonHandler
	attendance              *attendancehttp.AttendanceHandler
	knowledgeBase           *handlers.KnowledgeBaseHandler
	shortLink               *shortlinkhttp.ShortLinkHandler
	export                  *exporthttp.ExportHandler
	waba                    *wabahttp.WABAHandler
	invoice                 *invoicehttp.InvoiceHandler
	callBilling             *callbillinghttp.CallBillingHandler
	calls                   *handlers.CallsHandler
	analytics               *analyticshttp.AnalyticsHandler
	workspaceConfig         *workspaceconfighttp.WorkspaceConfigHandler
	supportInbox            *supportinboxhttp.SupportInboxHandler
	issue                   *issuehttp.IssueHandler
	workflow                *handlers.WorkflowHandler
	workflowWebhook         *workflowwebhookhttp.Handler
	wsWorkflowSimulator     *wsdelivery.WSWorkflowSimulatorHandler
	wsWorkflowAIBuilder     *wsdelivery.WSWorkflowAIBuilderHandler
	builderSession          *buildersessionhttp.BuilderSessionHandler
	calendar                *calendarhttp.CalendarHandler
	workspaceDepartment     *workspacedepartmenthttp.WorkspaceDepartmentHandler
	affiliate               *affiliatehttp.AffiliateHandler
}
