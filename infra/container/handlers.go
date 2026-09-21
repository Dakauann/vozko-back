package container

import (
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	affiliatehttp "vozko/delivery/http/affiliate"
	analyticshttp "vozko/delivery/http/analytics"
	attendancehttp "vozko/delivery/http/attendance"
	authhttp "vozko/delivery/http/auth"
	balancehttp "vozko/delivery/http/balance"
	buildersessionhttp "vozko/delivery/http/buildersession"
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
	invoicehttp "vozko/delivery/http/invoice"
	issuehttp "vozko/delivery/http/issue"
	labelhttp "vozko/delivery/http/label"
	leadhttp "vozko/delivery/http/lead"
	leadmemoryhttp "vozko/delivery/http/leadmemory"
	mediashttp "vozko/delivery/http/medias"
	mercadopagohttp "vozko/delivery/http/mercadopago"
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
	tickethttp "vozko/delivery/http/ticket"
	userhttp "vozko/delivery/http/user"
	wabahttp "vozko/delivery/http/waba"
	whatsappbusinessphonehttp "vozko/delivery/http/whatsappbusinessphone"
	whatsappoutreachhttp "vozko/delivery/http/whatsappoutreach"
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
	payment_domain "vozko/domain/payment"
	"vozko/domain/user"
	lead_memory_repository "vozko/infra/repositories/lead_memory"
	businessphone_infra "vozko/infra/whatsapp/business_phone"
	callsession_usecase "vozko/usecases/callsession"
	conversation_usecase "vozko/usecases/conversation"
	crmboard_usecase "vozko/usecases/crmboard"
	crmbulk_usecase "vozko/usecases/crmbulk"
	oppboard_usecase "vozko/usecases/oppboard"
	"vozko/usecases/opportunityio"
	payment_usecase "vozko/usecases/payment"
	workflow_usecase "vozko/usecases/workflow"
)

func (c *Container) initHandlers() {
	metricsHandler := handlers.NewMetricsHandler(c.services.metrics)
	if c.cfg.ReadMeWebhookSecret == "" {
		log.Println("[WARN] README_WEBHOOK_SECRET is empty; ReadMe personalized docs requests will be rejected")
	}

	log.Printf("Prometheus metrics query URL: %s", c.cfg.PrometheusURL)
	metricsQueryClient := &http.Client{Timeout: 30 * time.Second}
	metricsQueryHandler := handlers.NewMetricsQueryHandler(c.cfg.PrometheusURL, metricsQueryClient)

	crmBoardService := crmboard_usecase.NewService(
		c.repositories.conversation,
		c.repositories.stage,
		c.useCases.listLabels,
		c.services.conversationAuth,
		c.repositories.inboxAssignment,
	)

	crmBulkService := crmbulk_usecase.NewService(
		c.useCases.assignEntryStage,
		c.useCases.assignEntryLabel,
		c.useCases.removeEntryLabel,
		c.services.assignmentService,
		c.services.conversationAuth,
		c.services.conversationHub,
	)
	crmBulkService.SetTargetResolver(crmBoardTargetResolver{board: crmBoardService})

	c.services.dialog360Onboarding = businessphone_infra.NewDialog360OnboardingService(
		businessphone_infra.NewDialog360PartnerClient(
			c.cfg.Dialog360PartnerAPIBase,
			c.cfg.Dialog360PartnerID,
			c.cfg.Dialog360PartnerAPIKey,
			c.cfg.Dialog360SolutionID,
			&http.Client{Timeout: 30 * time.Second},
		).WithRateLimit(c.redisProvider.SharedState()),
		c.repositories.businessPhone,
		c.repositories.waba,
		c.useCases.ensureOrganicCoexistenceCampaign,
	)
	c.services.dialog360Onboarding.WithProvisioningGate(c.useCases.phoneProvisioningGate)
	c.services.dialog360Onboarding.WithNotifier(c.useCases.notifier, c.useCases.dashboardURL)
	if c.cfg.Dialog360WebhookBaseURL != "" {
		base := strings.TrimRight(c.cfg.Dialog360WebhookBaseURL, "/")
		msgWebhookURL := base + "/webhooks/360dialog/messages"
		if c.cfg.Dialog360WebhookSecret != "" {
			msgWebhookURL += "?secret=" + url.QueryEscape(c.cfg.Dialog360WebhookSecret)
		}
		c.services.dialog360Onboarding.WithMessagingWebhook(msgWebhookURL, &http.Client{Timeout: 15 * time.Second})
	}
	if c.cfg.Dialog360PartnerAPIKey != "" {
		c.services.dialog360Onboarding.RunPeriodicReconcile(15*time.Minute, 1*time.Hour)
	}

	c.handlers = &handlers_{
		product:            handlers.NewProductHandler(c.useCases.createProduct, c.useCases.updateProduct, c.useCases.launchVariantStock, c.useCases.getProduct, c.useCases.listProducts, c.useCases.searchProducts),
		property:           handlers.NewPropertyHandler(c.useCases.createProperty, c.useCases.updateProperty, c.useCases.getProperty, c.useCases.listProperties, c.useCases.searchProperties, c.useCases.deleteProperty),
		category:           handlers.NewCategoryHandler(c.useCases.createCategory, c.useCases.updateCategory, c.useCases.deleteCategory, c.useCases.getCategory, c.useCases.listCategories),
		agent:              handlers.NewAgentHandler(c.useCases.createAgent, c.useCases.updateAgent, c.useCases.assignAgentDepartment, c.useCases.deleteAgent, c.useCases.getAgent, c.useCases.listAgents, c.useCases.simulateAgentTurn, c.services.toolRegistry, c.services.ai, c.repositories.whatsappTemplate),
		aichat:             handlers.NewAIChatHandler(c.useCases.aichat, c.useCases.copilot),
		auth:               c.newAuthHandler(),
		user:               userhttp.NewUserHandler(c.useCases.listUsers, c.useCases.updateUserRole, c.useCases.findUserByID, c.useCases.updateUser, c.useCases.deleteUser, c.useCases.getWorkspaceSubscription, c.services.documentValidator),
		media:              mediashttp.NewMediasHandler(c.useCases.uploadMedia, c.useCases.listMedia, c.useCases.getMedia),
		cart:               handlers.NewCartHandler(c.useCases.addToCart, c.useCases.removeFromCart, c.useCases.updateCartItem, c.useCases.decrementCartItem, c.useCases.getCart, c.useCases.clearCart),
		address:            handlers.NewAddressHandler(c.useCases.createAddress, c.useCases.getAddresses, c.useCases.updateAddress, c.useCases.deleteAddress),
		order:              handlers.NewOrderHandler(c.useCases.checkout, c.useCases.getOrder, c.useCases.listOrders),
		cep:                cephttp.NewCEPHandler(c.useCases.searchCEP),
		webhook:            c.buildWebhookHandler(),
		mercadoPagoWebhook: c.buildMercadoPagoWebhookHandler(),
		readMe:             readmehttp.NewHandler(c.cfg.ReadMeWebhookSecret, c.repositories.user, c.services.readMeTokenService),
		paymentSplit: paymentsplithttp.NewPaymentSplitHandler(
			c.useCases.createPaymentSplit,
			c.useCases.updatePaymentSplit,
			c.useCases.deletePaymentSplit,
			c.useCases.getPaymentSplit,
			c.useCases.listPaymentSplits,
			payment_usecase.NewGetPaymentSplitSuppliersUseCase(c.repositories.paymentSplit),
		),
		ticket:    tickethttp.NewTicketHandler(c.useCases.listUserTickets, c.useCases.getTicketByOrder, c.useCases.uploadTicketDoc, c.useCases.listTickets, c.useCases.updateTicketStatus, c.useCases.generateTicketLabel),
		shipping:  handlers.NewShippingHandler(c.useCases.getShippingAuthorizationURL, c.useCases.connectShippingAccount, c.useCases.listShippingAccounts),
		insurance: handlers.NewInsuranceHandler(c.useCases.quoteInsurance, c.useCases.listInsuranceQuotations, c.useCases.getInsuranceQuotation, c.useCases.listInsurancePolicies, c.useCases.describeInsuranceRequirements),
		whatsappTemplate: whatsapptemplatehttp.NewWhatsAppTemplateHandler(
			c.useCases.listWhatsAppTemplates,
			c.useCases.getWhatsAppTemplate,
			c.useCases.syncWhatsAppTemplates,
			c.useCases.syncWhatsAppTemplate,
			c.useCases.createWhatsAppTemplate,
			c.useCases.replicateWhatsAppTemplate,
			c.useCases.setHeaderMediaWhatsApp,
			c.useCases.deleteWhatsAppTemplate,
			c.services.whatsappClientFactory,
			c.repositories.workspaceTemplateAccess,
			c.repositories.businessPhone,
		),
		whatsappOutreach: whatsappoutreachhttp.NewHandler(whatsappoutreachhttp.HandlerDeps{
			Start:       c.useCases.startOfficialConversation,
			Quote:       c.useCases.quoteTemplateSend,
			Departments: c.services.conversationAuthImpl,
		}),
		systemConfig:    systemconfighttp.NewSystemConfigHandler(c.useCases.getSystemConfig, c.useCases.updateSystemConfig),
		workspaceConfig: workspaceconfighttp.NewWorkspaceConfigHandler(c.useCases.getWorkspaceConfig, c.useCases.updateWorkspaceConfig, c.useCases.updateWorkspaceConfigOwner),
		workspacePlan: handlers.NewWorkspacePlanHandler(
			c.useCases.createWorkspacePlan,
			c.useCases.updateWorkspacePlan,
			c.useCases.archiveWorkspacePlan,
			c.useCases.listWorkspacePlans,
			c.useCases.getWorkspacePlan,
			c.useCases.getWorkspaceSubscription,
			c.useCases.createSubscriptionInvoice,
			c.useCases.cancelWorkspaceSubscription,
			c.useCases.setPlanVisibility,
			c.useCases.listVisiblePlans,
			c.useCases.setPlanExclusiveAffiliate,
			c.useCases.listExclusivePlansByAffiliateCode,
			c.useCases.listMyExclusivePlans,
			c.repositories.affiliate,
			c.useCases.workspaceReferralReader,
		),
		workspaceAddon: workspaceaddonhttp.NewWorkspaceAddonHandler(
			c.useCases.createAddonDefinition,
			c.useCases.updateAddonDefinition,
			c.useCases.archiveAddonDefinition,
			c.useCases.listAddonDefinitions,
			c.useCases.getAddonDefinition,
			c.useCases.listAvailableAddons,
			c.useCases.purchaseAddon,
			c.useCases.previewAddonPurchase,
			c.useCases.cancelAddonSubscription,
			c.useCases.listWorkspaceAddons,
			c.useCases.getWorkspaceEntitlements,
		),
		metrics:      metricsHandler,
		metricsQuery: metricsQueryHandler,
		shop: handlers.NewShopHandler(
			c.useCases.createShop,
			c.useCases.updateShop,
			c.useCases.getShop,
			c.useCases.listShops,
		),
		whatsappCampaign: handlers.NewWhatsAppCampaignHandler(
			c.useCases.createWCCampaign,
			c.useCases.updateWCCampaign,
			c.useCases.assignWCCampaignDepartment,
			c.useCases.deleteWCCampaign,
			c.useCases.getWCCampaign,
			c.useCases.listWCCampaigns,
			c.useCases.listWCEntries,
			c.useCases.dispatchWCCampaign,
			c.useCases.messageConsumerWCCampaign,
			c.useCases.resetWCCampaign,
			c.useCases.clearHistoryWCCampaign,
			c.useCases.deleteEntryWCCampaign,
			c.useCases.updateEntryWCCampaign,
			c.useCases.addEntriesWCCampaign,
			c.useCases.quickSendWCCampaign,
			c.services.conversationHub,
			c.useCases.cloneStagesFromGroup,
			c.useCases.getWorkflow,
			c.useCases.ensureActiveWorkspaceSubscription,
			c.useCases.getAgent,
			c.useCases.getWCCampaignsSummary,
		),
		lead: withLeadInboxSeeding(c, leadhttp.NewLeadHandler(
			c.repositories.lead,
			c.repositories.wcEntry,
			c.repositories.conversation,
			c.repositories.leadMessageWindow,
			c.repositories.conversationAnalyses,
			c.repositories.businessPhone,
			c.services.businessPhoneMetaAPI,
		)),
		callRecording: callrecordinghttp.NewCallRecordingHandler(c.useCases.callRecordingQuery),
		whatsappBusinessPhone: whatsappbusinessphonehttp.NewWhatsAppBusinessPhoneHandler(
			whatsappbusinessphonehttp.WhatsAppBusinessPhoneHandlerConfig{
				AccessToken: c.cfg.WhatsAppAccessToken,
			},
			c.useCases.listBusinessPhones,
			c.useCases.getBusinessPhone,
			c.useCases.syncBusinessPhone,
			c.useCases.registerBusinessPhone,
			c.useCases.deregisterBusinessPhone,
			c.useCases.releaseBusinessPhone,
			c.useCases.requestBusinessPhoneVerify,
			c.useCases.verifyBusinessPhoneCode,
			c.useCases.updateBusinessPhoneProfile,
			c.useCases.getBusinessPhoneProfile,
			c.useCases.deleteBusinessPhone,
			c.useCases.unassignBusinessPhoneOwner,
			c.repositories.businessPhone,
			c.repositories.workspacePhoneAccess,
			c.services.businessPhoneMetaAPI,
			c.services.whatsappClientFactory,
		),
		waba: wabahttp.NewWABAHandler(c.useCases.listWABAs, c.useCases.getWABA),
		metaEmbeddedSignup: metaembeddedsignuphttp.NewMetaEmbeddedSignupHandler(
			metaembeddedsignuphttp.GetMetaEmbeddedSignupConfigFromEnv(),
		).WithDialog360(
			c.services.dialog360Onboarding,
			c.cfg.Dialog360WebhookSecret,
		).WithTemplateWebhook(
			c.useCases.handleTemplateWebhook,
		).WithMeta(
			c.cfg.MetaAppSecret,
			c.useCases.onboardEmbeddedSignup,
			c.services.coexistenceMetaAPI,
			c.useCases.ensureOrganicCoexistenceCampaign,
			c.cfg.Dialog360OnboardingEnabled,
		),
		balance: balancehttp.NewBalanceHandler(
			c.useCases.createBalance,
			c.useCases.getBalance,
			c.useCases.creditBalance,
			c.useCases.debitBalance,
			c.useCases.listTransactions,
			c.useCases.creditResource,
			c.useCases.debitResource,
			c.useCases.getFullBalanceSummary,
			c.useCases.getOrCreateBalance,
			c.useCases.getOrCreateFullSummary,
			c.useCases.getExchangeRate,
		),
		workspaceTemplateAccess: workspacetemplateaccesshttp.NewWorkspaceTemplateAccessHandler(
			c.useCases.grantTemplateAccess,
			c.useCases.revokeTemplateAccess,
			c.useCases.listWorkspaceAccess,
			c.useCases.listTemplateAccess,
		),
		workspacePhoneAccess: workspacephoneaccesshttp.NewWorkspacePhoneAccessHandler(
			c.useCases.grantPhoneAccess,
			c.useCases.revokePhoneAccess,
			c.useCases.listWorkspacePhoneAccess,
			c.useCases.listPhoneAccess,
		),
		conversation: func() *conversationhttp.ConversationHandler {
			h := conversationhttp.NewConversationHandler(
				c.useCases.sendConversationMessage,
				c.useCases.uploadConversationMedia,
				c.useCases.getConversationMedia,
				c.services.inboxService,
				c.useCases.searchMessagesByEntry,
				c.useCases.listConversationEvents,
			)
			if c.services.conversationAutomation != nil {
				h.SetAutomationService(c.services.conversationAutomation)
			}
			return h
		}(),
		conversationWS: wsdelivery.NewConversationWSHandler(
			c.services.conversationHub,
			log.Default(),
		),
		callSessionWS: buildCallSessionWSHandler(c),
		stage: withFunnelStages(c, stagehttp.NewStageHandler(
			c.useCases.createStage,
			c.useCases.updateStage,
			c.useCases.deleteStage,
			c.useCases.listStages,
			c.useCases.setInitialStage,
			c.useCases.assignEntryStage,
			c.useCases.removeEntryStage,
			c.useCases.getEntryStage,
			c.useCases.getBatchEntryStages,
			c.useCases.reorderStages,
			c.services.conversationHub,
		)),
		stageGroup: handlers.NewStageGroupHandler(
			c.useCases.createStageGroup,
			c.useCases.updateStageGroup,
			c.useCases.deleteStageGroup,
			c.useCases.listStageGroups,
			c.useCases.getStageGroup,
		),
		pipeline: pipelinehttp.NewPipelineHandler(
			c.useCases.createPipeline,
			c.useCases.updatePipeline,
			c.useCases.deletePipeline,
			c.useCases.listPipelines,
			c.useCases.getPipeline,
			c.useCases.pipelineUsage,
		),
		savedView: savedviewhttp.NewSavedViewHandler(
			c.useCases.createSavedView,
			c.useCases.updateSavedView,
			c.useCases.deleteSavedView,
			c.useCases.listSavedViews,
			c.useCases.setDefaultSavedView,
		),
		opportunity: opportunityhttp.NewOpportunityHandler(
			c.useCases.opportunity,
			opportunityio.NewService(c.useCases.opportunity, c.repositories.customField),
			c.services.conversationAuth,
		),
		opportunityBoard: opportunityboardhttp.NewOpportunityBoardHandler(oppboard_usecase.NewService(
			c.repositories.opportunity,
			c.repositories.stage,
			c.services.conversationAuth,
		)),
		customField: customfieldhttp.NewCustomFieldHandler(c.useCases.customField),
		crmBoard:    crmboardhttp.NewCRMBoardHandler(crmBoardService),
		crmBulk:     crmbulkhttp.NewCRMBulkHandler(crmBulkService),
		label: labelhttp.NewLabelHandler(
			c.useCases.createLabel,
			c.useCases.updateLabel,
			c.useCases.deleteLabel,
			c.useCases.listLabels,
			c.useCases.assignEntryLabel,
			c.useCases.removeEntryLabel,
			c.useCases.getEntryLabels,
			c.useCases.reorderLabels,
			c.services.conversationHub,
		),
		messageShortcut: messageshortcuthttp.NewMessageShortcutHandler(
			c.useCases.createMessageShortcut,
			c.useCases.updateMessageShortcut,
			c.useCases.deleteMessageShortcut,
			c.useCases.listMessageShortcuts,
			c.useCases.getByShortcut,
		),
		scheduledMessage: scheduledmessagehttp.NewScheduledMessageHandler(
			c.useCases.scheduleMessage,
			c.useCases.rescheduleMessage,
			c.useCases.cancelScheduledMessage,
			c.useCases.listScheduledMessages,
			c.services.conversationAuth,
		),
		leadMemory: leadmemoryhttp.NewLeadMemoryHandler(
			c.useCases.createLeadMemory,
			c.useCases.updateLeadMemory,
			c.useCases.deleteLeadMemory,
			c.useCases.listLeadMemories,
			lead_memory_repository.NewLeadRefResolver(c.db),
		),
		workspace: workspacehttp.NewWorkspaceHandler(
			c.useCases.createWorkspace,
			c.useCases.getWorkspace,
			c.useCases.listWorkspaces,
			c.useCases.updateWorkspace,
			c.useCases.inviteMember,
			c.useCases.acceptInvite,
			c.useCases.declineInvite,
			c.useCases.cancelInvite,
			c.useCases.listInvites,
			c.useCases.listWorkspaceInvites,
			c.useCases.removeMember,
			c.useCases.updateMemberRole,
			c.useCases.listWsMembers,
			c.useCases.listMembersPaginated,
			c.useCases.listAssignableMembers,
			c.useCases.setMemberPermissions,
			c.useCases.getMemberPermissions,
			c.useCases.listResourcePermissions,
			c.useCases.checkWsAccess,
			c.useCases.ensureDefaultWorkspace,
			c.useCases.assignResource,
			c.useCases.unassignResource,
			c.useCases.listResourceAssignments,
			c.useCases.checkResourceAccess,
			c.useCases.createCustomRole,
			c.useCases.listCustomRoles,
			c.useCases.updateCustomRole,
			c.useCases.deleteCustomRole,
			c.useCases.assignCustomRole,
			c.useCases.affiliateTrackReferral,
		),
		affiliate: affiliatehttp.NewAffiliateHandler(
			c.useCases.affiliateRegister,
			c.useCases.affiliateGetMy,
			c.useCases.affiliateUpdateMy,
			c.useCases.affiliateListReferrals,
			c.useCases.affiliateListEarnings,
			c.useCases.affiliateStats,
			c.useCases.affiliateValidateCode,
			c.useCases.affiliateAdminList,
			c.useCases.affiliateAdminGet,
			c.useCases.affiliateAdminUpdate,
		),
		workspacePricing: workspacepricinghttp.NewWorkspacePricingHandler(
			c.useCases.getDefaultPricingItems,
			c.useCases.getResolvedPricing,
			c.useCases.updatePricingItem,
			c.useCases.getPricingAuditLog,
			c.useCases.getExchangeRate,
			c.useCases.updateExchangeRate,
		),
		attendance: func() *attendancehttp.AttendanceHandler {
			h := attendancehttp.NewAttendanceHandler(
				c.useCases.getAttendanceStats,
				c.useCases.getWindowStats,
				c.useCases.getResponseTimeDistribution,
				c.useCases.getAIAgentStats,
			)
			h.SetFRTStats(c.useCases.getFRTStats)
			h.SetOverview(c.useCases.getOverview)
			h.SetQueueRepo(c.repositories.queueEvent)
			h.SetPresenceRepo(c.repositories.agentPresence)
			return h
		}(),
		knowledgeBase: handlers.NewKnowledgeBaseHandler(
			c.useCases.createKnowledgeBase,
			c.useCases.updateKnowledgeBase,
			c.useCases.deleteKnowledgeBase,
			c.useCases.getKnowledgeBase,
			c.useCases.listKnowledgeBases,
			c.useCases.createRAGDocument,
			c.useCases.deleteRAGDocument,
			c.useCases.getRAGDocument,
			c.useCases.listRAGDocuments,
			c.useCases.linkAgentKnowledgeBases,
			c.useCases.getAgentKnowledgeBases,
			c.useCases.queryKnowledgeBase,
		),
		shortLink: shortlinkhttp.NewShortLinkHandler(
			c.useCases.createShortLink,
			c.useCases.updateShortLink,
			c.useCases.getShortLink,
			c.useCases.listShortLinks,
			c.useCases.deleteShortLink,
			c.useCases.shortLinkStats,
			c.useCases.resolveShortLink,
			c.useCases.unlockShortLink,
			c.useCases.publishShortLinkClick,
			c.useCases.shortLinkAnalytics,
			c.useCases.shortLinkRecentClicks,
			c.useCases.shortLinkQR,
			c.cfg.ShortLinkBaseURL,
		),
		export: exporthttp.NewExportHandler(
			c.useCases.exportEntries,
			c.useCases.getWCCampaign,
		),
		invoice: invoicehttp.NewInvoiceHandler(
			c.useCases.createInvoice,
			c.useCases.listInvoices,
			c.useCases.getInvoice,
		),
		callBilling: callbillinghttp.NewCallBillingHandler(
			c.useCases.listBillingRecords,
		),
		calls: handlers.NewCallsHandler(
			c.useCases.listCalls,
			c.useCases.getCall,
			c.useCases.billingQuery,
		),
		analytics: analyticshttp.NewAnalyticsHandler(
			c.useCases.getProfitReport,
			c.useCases.getCallAnalytics,
			c.useCases.getAdminOverview,
			c.useCases.getPlanContractions,
			c.useCases.getMetaServiceMessageCost,
		),
		supportInbox: supportinboxhttp.NewSupportInboxHandler(
			c.useCases.createSupportInbox,
			c.useCases.updateSupportInbox,
			c.useCases.deleteSupportInbox,
			c.useCases.getSupportInbox,
			c.useCases.listSupportInboxes,
			c.useCases.createSupportSession,
			c.useCases.reconnectSupportSession,
		),
		issue: issuehttp.NewIssueHandler(
			c.useCases.createIssue,
			c.useCases.listIssues,
			c.useCases.listAllIssues,
			c.useCases.getIssue,
			c.useCases.closeIssue,
			c.useCases.updateIssueStatus,
			c.useCases.createIssueResponse,
			c.useCases.listIssueResponses,
		),
		workflow: handlers.NewWorkflowHandler(
			c.useCases.createWorkflow,
			c.useCases.updateWorkflow,
			c.useCases.assignWorkflowDepartment,
			c.useCases.deleteWorkflow,
			c.useCases.getWorkflow,
			c.useCases.listWorkflows,
			c.useCases.activateWorkflow,
			c.useCases.pauseWorkflow,
			c.useCases.startWorkflowRun,
			c.useCases.cancelWorkflowRun,
			c.useCases.getWorkflowRun,
			c.useCases.listWorkflowRuns,
			c.useCases.testWorkflowNode,
			c.useCases.nodeCatalogFn,
		),
		workflowWebhook: workflowwebhookhttp.NewHandler(
			c.useCases.workflowWebhookConfig,
			c.useCases.handleWebhookTrigger,
		),
		wsWorkflowSimulator: wsdelivery.NewWSWorkflowSimulatorHandler(c.useCases.wsWorkflowSimulation, c.services.metrics),
		wsWorkflowAIBuilder: wsdelivery.NewWSWorkflowAIBuilderHandler(
			c.useCases.wsWorkflowAIBuilder,
			c.services.metrics,
			func(workspaceID, workflowID string) *workflow_usecase.BuilderSessionRecorder {
				return workflow_usecase.NewBuilderSessionRecorder(c.repositories.builderSession, workspaceID, workflowID)
			},
		),
		builderSession: buildersessionhttp.NewBuilderSessionHandler(workflow_usecase.NewBuilderSessionService(c.repositories.builderSession)),
		calendar: calendarhttp.NewCalendarHandler(
			c.useCases.createCalendarEvent,
			c.useCases.updateCalendarEvent,
			c.useCases.deleteCalendarEvent,
			c.useCases.getCalendarEvent,
			c.useCases.listCalendarEvents,
			c.useCases.connectGoogleCalendar,
			c.useCases.disconnectGoogleCalendar,
			c.useCases.getGoogleConnection,
			c.useCases.getGoogleAuthURL,
			c.useCases.startCalendarWatch,
			c.useCases.stopCalendarWatch,
			c.useCases.handleCalendarNotification,
			c.cfg.GoogleOAuthClientID != "" && c.cfg.GoogleOAuthClientSecret != "",
			c.cfg.AuthJWTSecret,
		),
		workspaceDepartment: workspacedepartmenthttp.NewWorkspaceDepartmentHandler(
			c.useCases.createWorkspaceDepartment,
			c.useCases.getWorkspaceDepartment,
			c.useCases.listWorkspaceDepartments,
			c.useCases.listWorkspaceDepartmentsByIDs,
			c.useCases.updateWorkspaceDepartment,
			c.useCases.deleteWorkspaceDepartment,
			c.useCases.addWorkspaceDepartmentMember,
			c.useCases.removeWorkspaceDepartmentMember,
			c.useCases.listWorkspaceDepartmentMembers,
		),
	}

	if c.services.requestCallPermission != nil {
		c.handlers.conversation.SetRequestCallPermission(c.services.requestCallPermission)
	}
}

func buildCallSessionWSHandler(c *Container) *wsdelivery.CallSessionWSHandler {
	base := wsdelivery.NewCallSessionWSHandler(
		c.services.startOutboundCall,
		c.services.endOutboundCall,
		c.services.callLifecycle,
		c.services.conversationAuth,
		log.Default(),
		c.services.metrics,
	)
	if c.services.crmTelemetryEmitter != nil {
		em := c.services.crmTelemetryEmitter
		base.WithPresenceTelemetry(func(workspaceID, userID, state, source string) {
			em.Presence(workspaceID, userID, state, source)
		})
	}
	if c.services.telephonyBoardSync != nil {
		base.WithLiveBoard(c.services.telephonyBoardSync, c.services.telephonyCapacity)
	}

	sessions := c.services.callSessions
	calls := c.services.calls
	usernameResolver := c.services.callSessionUsernameResolver
	inboundExecutor := wsdelivery.NewCallSessionInboundExecutor(
		calls,
		c.services.endOutboundCall,
		c.services.callLifecycle,
		c.recordingPool,
		log.Default(),
	)

	inboundBroker := callsession_usecase.NewInboundOfferBroker()

	if c.services.whatsappCallSignaling != nil && c.services.whatsappCallRegistry != nil {
		whatsappInboundUC := conversation_usecase.NewWhatsAppInboundCallUseCase(conversation_usecase.WhatsAppInboundConfig{
			Signaling:       c.services.whatsappCallSignaling,
			Registry:        c.services.whatsappCallRegistry,
			Phones:          c.repositories.businessPhone,
			Entries:         c.repositories.wcEntry,
			Assignment:      c.services.assignmentService,
			Assigner:        c.services.assignmentService,
			Departments:     c.services.campaignWorkspaceResolver,
			Eligible:        c.services.conversationHub,
			Sessions:        sessions,
			Admission:       c.services.callAdmission,
			Broker:          inboundBroker,
			Executor:        inboundExecutor,
			Messages:        c.repositories.conversation,
			Hub:             c.services.conversationHub,
			Users:           c.services.callSessionUsernameResolver,
			WorkspaceConfig: c.repositories.workspaceConfig,
			PublicIP:        c.services.whatsappPublicMediaIP,
			StunServers:     c.cfg.WhatsAppStunServers,
			Logger:          log.Default(),
		})
		if consumer, ok := c.services.whatsappCallWebhook.(*conversation_usecase.WhatsAppCallWebhookConsumer); ok {
			consumer.SetInboundHandler(whatsappInboundUC)
		}
	}

	return base.
		WithInboundCalls(inboundBroker).
		WithRegistries(sessions, calls).
		WithUserResolver(usernameResolver).
		WithRecording(c.recordingPool)
}

func (c *Container) buildWebhookHandler() *handlers.WebhookHandler {
	webhookAppSecrets := append([]string{c.cfg.MetaAppSecret}, c.cfg.MetaAppSecretsExtra...)
	h := handlers.NewWebhookHandler(c.useCases.publishWebhook, c.cfg.AsaasWebhookToken, c.cfg.WhatsAppWebhookVerifyToken, webhookAppSecrets...)
	if c.services.whatsappCallWebhook != nil {
		h.SetCallWebhookHandler(c.services.whatsappCallWebhook)
	}
	if c.services.whatsappCallPermissionWebhook != nil {
		h.SetCallPermissionWebhookHandler(c.services.whatsappCallPermissionWebhook)
	}
	if c.cfg.Dialog360WebhookSecret != "" {
		h.SetDialog360WebhookSecret(c.cfg.Dialog360WebhookSecret)
	} else {
		log.Println("[WARN] D360_WEBHOOK_SECRET is empty, 360dialog inbound webhook signature verification disabled")
	}
	return h
}

type callSessionUsernameResolver struct {
	repo  user.UserRepository
	ttl   time.Duration
	mu    sync.RWMutex
	cache map[string]cachedUsername
}

type cachedUsername struct {
	name string
	exp  time.Time
}

func newCallSessionUsernameResolver(repo user.UserRepository) *callSessionUsernameResolver {
	if repo == nil {
		return nil
	}
	return &callSessionUsernameResolver{repo: repo, ttl: 5 * time.Minute, cache: make(map[string]cachedUsername)}
}

func (r *callSessionUsernameResolver) ResolveUsernames(userIDs []string) map[string]string {
	if r == nil || r.repo == nil || len(userIDs) == 0 {
		return nil
	}
	now := time.Now()
	out := make(map[string]string, len(userIDs))
	var miss []string

	r.mu.RLock()
	for _, id := range userIDs {
		if c, ok := r.cache[id]; ok && now.Before(c.exp) {
			if c.name != "" {
				out[id] = c.name
			}
			continue
		}
		miss = append(miss, id)
	}
	r.mu.RUnlock()

	if len(miss) == 0 {
		return out
	}

	users, err := r.repo.FindByIDs(miss)
	if err != nil {
		return out
	}
	found := make(map[string]string, len(users))
	for _, u := range users {
		if u != nil {
			found[u.ID] = strings.TrimSpace(u.Username)
		}
	}
	r.mu.Lock()
	for _, id := range miss {
		name := found[id]
		r.cache[id] = cachedUsername{name: name, exp: now.Add(r.ttl)}
		if name != "" {
			out[id] = name
		}
	}
	r.mu.Unlock()
	return out
}

func (c *Container) newAuthHandler() *authhttp.AuthHandler {
	h := authhttp.NewAuthHandler(
		c.useCases.credentialsLogin,
		c.useCases.register,
		c.useCases.adminRegister,
		c.useCases.refreshToken,
		c.useCases.requestPasswordReset,
		c.useCases.resetPassword,
		c.useCases.sendEmailVerification,
		c.useCases.verifyEmailToken,
		c.useCases.changePassword,
		c.useCases.logout,
		c.useCases.logoutAll,
		c.useCases.listSessions,
		c.useCases.revokeSession,
	)
	if c.cfg.CookieDomain != "" {
		secure := c.cfg.AppEnv != "development"
		h.SetCookieConfig(authhttp.CookieConfig{
			Domain:        c.cfg.CookieDomain,
			Secure:        secure,
			AccessMaxAge:  c.cfg.AuthJWTAccessTTL,
			RefreshMaxAge: c.cfg.AuthJWTRefreshTTL,
		})
		log.Printf("Auth cookies enabled (domain=%s, secure=%v)", c.cfg.CookieDomain, secure)
	}

	return h
}

func (c *Container) buildMercadoPagoWebhookHandler() *mercadopagohttp.WebhookHandler {
	if c.cfg.PaymentProvider != payment_domain.ProviderMercadoPago {
		return nil
	}
	return mercadopagohttp.NewWebhookHandler(
		c.useCases.publishWebhook,
		c.cfg.MercadoPagoWebhookSecret,
		mercadopagohttp.WithSignatureTolerance(c.cfg.MercadoPagoSignatureTolerance),
	)
}

func withLeadInboxSeeding(c *Container, h *leadhttp.LeadHandler) *leadhttp.LeadHandler {
	h.SetAuthorizer(c.services.conversationAuth)
	if c.unofficialWhatsApp == nil || !c.unofficialWhatsApp.Enabled {
		return h
	}
	if c.unofficialWhatsApp.SeedInboxPublisher == nil {
		return h
	}
	h.SetInboxSeeder(c.unofficialWhatsApp.SeedInboxPublisher)
	return h
}
