package container

import (
	address_repository "vozko/infra/repositories/address"
	affiliate_repository "vozko/infra/repositories/affiliate"
	agent_repository "vozko/infra/repositories/agent"
	ap_repository "vozko/infra/repositories/agent_presence"
	aa_repository "vozko/infra/repositories/ai_attendance"
	aichat_repository "vozko/infra/repositories/aichat"
	analytics_repository "vozko/infra/repositories/analytics"
	ah_repository "vozko/infra/repositories/assignment_history"
	attendance_repository "vozko/infra/repositories/attendance"
	attendance_target_repository "vozko/infra/repositories/attendance_target"
	ca_repository "vozko/infra/repositories/audience"
	auth_repository "vozko/infra/repositories/auth"
	balance_repository "vozko/infra/repositories/balance"
	calendar_repository "vozko/infra/repositories/calendar"
	call_billing_repository "vozko/infra/repositories/call_billing"
	call_cdr_repository "vozko/infra/repositories/call_cdr"
	call_recording_repository "vozko/infra/repositories/call_recording"
	cep_repository "vozko/infra/repositories/cep"
	config_repository "vozko/infra/repositories/config"
	conversation_repository "vozko/infra/repositories/conversation"
	ce_repository "vozko/infra/repositories/conversation_event"
	customer_repository "vozko/infra/repositories/customer"
	customfield_repository "vozko/infra/repositories/customfield"
	dealautomation_repository "vozko/infra/repositories/dealautomation"
	ia_repository "vozko/infra/repositories/inbox_assignment"
	invoice_repository "vozko/infra/repositories/invoice"
	issues_repository "vozko/infra/repositories/issues"
	label_repository "vozko/infra/repositories/label"
	lead_repository "vozko/infra/repositories/lead"
	lead_campaign_send_repository "vozko/infra/repositories/lead_campaign_send"
	lead_memory_repository "vozko/infra/repositories/lead_memory"
	lead_message_window_repository "vozko/infra/repositories/lead_message_window"
	media_repository "vozko/infra/repositories/media"
	msg_shortcut_repository "vozko/infra/repositories/message_shortcut"
	opportunity_repository "vozko/infra/repositories/opportunity"
	pipeline_repository "vozko/infra/repositories/pipeline"
	qe_repository "vozko/infra/repositories/queue_event"
	rag_repository "vozko/infra/repositories/rag"
	report_repository "vozko/infra/repositories/report"
	savedview_repository "vozko/infra/repositories/savedview"
	scheduled_message_repository "vozko/infra/repositories/scheduled_message"
	shortlink_repository "vozko/infra/repositories/shortlink"
	stage_repository "vozko/infra/repositories/stage"
	telephony_repository "vozko/infra/repositories/telephony"
	user_repository "vozko/infra/repositories/user"
	whatsapp_repository "vozko/infra/repositories/whatsapp"
	wc_repository "vozko/infra/repositories/whatsapp_campaign"
	wc_entry_repository "vozko/infra/repositories/whatsapp_campaign_entry"
	workflow_repository "vozko/infra/repositories/workflow"
	workspace_repository "vozko/infra/repositories/workspace"
	workspace_addon_repository "vozko/infra/repositories/workspace_addon"
	workspace_config_repository "vozko/infra/repositories/workspace_config"
	workspace_department_repository "vozko/infra/repositories/workspace_department"
	workspace_phone_access_repository "vozko/infra/repositories/workspace_phone_access"
	workspace_plan_repository "vozko/infra/repositories/workspace_plan"
	workspace_pricing_repository "vozko/infra/repositories/workspace_pricing"
	workspace_template_access_repository "vozko/infra/repositories/workspace_template_access"
)

func (c *Container) initRepositories() {
	monthlySendCaps := balance_repository.NewMonthlySendCapRepository(c.db)
	messageStatus := whatsapp_repository.NewMessageStatusRepository(c.db)
	qualityChanges := whatsapp_repository.NewQualityChangeRepository(c.db)
	wcCampaigns := wc_repository.NewCachedRepository(wc_repository.NewRepository(c.db), c.redisProvider.SharedState())
	c.repositories = &repositories{
		agent:                   agent_repository.NewCachedRepository(agent_repository.NewRepository(c.db), c.redisProvider.SharedState()),
		lead:                    lead_repository.NewCachedRepository(c.db, c.redisProvider.SharedState()),
		conversation:            conversation_repository.NewRepository(c.db),
		conversationAnalyses:    ca_repository.NewConversationReader(c.db),
		user:                    user_repository.NewUserRepository(c.db),
		media:                   media_repository.NewMediaRepository(c.db),
		address:                 address_repository.NewRepository(c.db),
		cep:                     cep_repository.NewRepository(c.db),
		whatsappTemplate:        whatsapp_repository.NewCachedTemplateRepository(whatsapp_repository.NewTemplateRepository(c.db), c.redisProvider.SharedState()),
		whatsappTemplateSend:    whatsapp_repository.NewTemplateSendRepository(c.db),
		whatsappTemplateNames:   whatsapp_repository.NewGrantedTemplateNames(c.db),
		passwordResetToken:      auth_repository.NewPasswordResetTokenRepository(c.db),
		emailVerification:       auth_repository.NewEmailVerificationRedisRepository(c.redisProvider.SharedState()),
		systemConfig:            config_repository.NewSystemConfigRepository(c.db),
		customer:                customer_repository.NewCustomerRepository(c.db),
		wcCampaign:              wcCampaigns,
		wcSchedules:             wcCampaigns,
		wcEntry:                 wc_entry_repository.NewRepository(c.db),
		wcDispatchReport:        wc_entry_repository.NewDispatchReportReader(c.db),
		businessPhone:           whatsapp_repository.NewCachedBusinessPhoneRepository(whatsapp_repository.NewBusinessPhoneRepository(c.db), c.redisProvider.SharedState()),
		ownerPhoneReader:        whatsapp_repository.NewOwnerPhoneReader(c.db),
		whatsappQuality:         qualityChanges,
		whatsappQualityHistory:  qualityChanges,
		whatsappQualityTimeline: qualityChanges,
		messageStatusRecorder:   messageStatus,
		messageStatusLedger:     messageStatus,
		messageStatusSending:    messageStatus,
		whatsappOptOuts:         whatsapp_repository.NewOptOutRepository(c.db),
		callRecording:           call_recording_repository.NewRepository(c.db),
		callCDR:                 call_cdr_repository.NewRepository(c.db),
		balance:                 balance_repository.NewCachedBalanceRepository(balance_repository.NewRepository(c.db), c.redisProvider.SharedState()),
		monthlySendCaps:         monthlySendCaps,
		monthlySendSlots:        monthlySendCaps,
		workspacePricing:        workspace_pricing_repository.NewRepository(c.db),
		workspaceTemplateAccess: workspace_template_access_repository.NewRepository(c.db),
		workspacePhoneAccess:    workspace_phone_access_repository.NewRepository(c.db),
		leadMessageWindow:       lead_message_window_repository.NewRepository(c.db),
		callPermission:          whatsapp_repository.NewCallPermissionRepository(c.db),
		leadCampaignSend:        lead_campaign_send_repository.NewRepository(c.db),
		conversationMedia:       conversation_repository.NewMediaRepository(c.db),
		stage:                   stage_repository.NewRepository(c.db),
		stageGroup:              stage_repository.NewStageGroupRepository(c.db),
		pipeline:                pipeline_repository.NewRepository(c.db),
		savedView:               savedview_repository.NewRepository(c.db),
		opportunity:             opportunity_repository.NewRepository(c.db),
		opportunityLink:         opportunity_repository.NewLinkRepository(c.db),
		opportunityOwners:       opportunity_repository.NewOwnerDirectory(c.db),
		customField:             customfield_repository.NewRepository(c.db),
		label:                   label_repository.NewRepository(c.db),
		messageShortcut:         msg_shortcut_repository.NewRepository(c.db),
		scheduledMessage:        scheduled_message_repository.NewRepository(c.db),
		leadMemory:              lead_memory_repository.NewRepository(c.db),
		workspace:               workspace_repository.NewCachedWorkspaceRepository(workspace_repository.NewRepository(c.db), c.redisProvider.SharedState()),
		customRole:              workspace_repository.NewCustomRoleRepository(c.db),
		attendance:              attendance_repository.New(c.db),
		attendanceTarget:        attendance_target_repository.New(c.db),
		telephony:               telephony_repository.New(c.db),
		conversationEvent:       ce_repository.New(c.db),
		assignmentHistory:       ah_repository.New(c.db),
		aiAttendance:            aa_repository.New(c.db),
		queueEvent:              qe_repository.New(c.db),
		agentPresence:           ap_repository.New(c.db),
		ragKnowledgeBase:        rag_repository.NewKnowledgeBaseRepository(c.db),
		ragDocument:             rag_repository.NewDocumentRepository(c.db),
		ragChunk:                rag_repository.NewChunkRepository(c.db),
		ragVector:               rag_repository.NewVectorRepository(c.db),
		ragAgentKB:              rag_repository.NewAgentKnowledgeBaseRepository(c.db),
		shortLink:               shortlink_repository.NewShortLinkRepository(c.db),
		shortLinkClick:          shortlink_repository.NewClickRepository(c.db),
		waba:                    whatsapp_repository.NewWABARepository(c.db),
		invoice:                 invoice_repository.NewRepository(c.db),
		callBilling:             call_billing_repository.NewRepository(c.db),
		analytics:               analytics_repository.NewRepository(c.db),
		workspacePlan:           workspace_plan_repository.NewPlanRepository(c.db),
		workspaceSubscription:   workspace_plan_repository.NewSubscriptionRepository(c.db),
		addonDefinition:         workspace_addon_repository.NewAddonDefinitionRepository(c.db),
		addonSubscription:       workspace_addon_repository.NewAddonSubscriptionRepository(c.db),
		aichatThread:            aichat_repository.NewThreadRepository(c.db),
		aichatMessage:           aichat_repository.NewMessageRepository(c.db),
		aichatQueue:             aichat_repository.NewQueueRepository(c.db),
		workspaceConfig:         workspace_config_repository.NewRepository(c.db),
		issue:                   issues_repository.NewIssuesRepository(c.db),
		issueResponse:           issues_repository.NewIssueResponseRepository(c.db),
		workflow:                workflow_repository.NewWorkflowRepository(c.db),
		workflowRun:             workflow_repository.NewWorkflowRunRepository(c.db),
		workflowRunLog:          workflow_repository.NewWorkflowRunLogRepository(c.db),
		workflowWebhook:         workflow_repository.NewWorkflowWebhookRepository(c.db),
		entryOwnership:          workflow_repository.NewEntryOwnershipRepository(c.db),
		entryResolver:           workflow_repository.NewEntryResolverRepository(c.db),
		builderSession:          workflow_repository.NewBuilderSessionRepository(c.db),
		calendar:                calendar_repository.NewRepository(c.db),
		inboxAssignment:         ia_repository.New(c.db),
		dealAutomation:          dealautomation_repository.New(c.db),
		workspaceDepartment:     workspace_department_repository.NewRepository(c.db),
		labelGroup:              label_repository.NewLabelGroupRepository(c.db),
		session:                 auth_repository.NewSessionRepository(c.db),
		affiliate:               affiliate_repository.NewRepository(c.db),
		report:                  report_repository.New(c.db),
	}
}
