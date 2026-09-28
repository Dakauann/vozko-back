package workspace

type Screen string

var screenParams = make(map[Screen][]string)

func registerScreen(key string, params ...string) Screen {
	screen := Screen(key)
	screenParams[screen] = params
	return screen
}

var (
	ScreenLiveChat                  = registerScreen("live_chat")
	ScreenFunnels                   = registerScreen("funnels")
	ScreenStageGroups               = registerScreen("stage_groups")
	ScreenSales                     = registerScreen("sales")
	ScreenAttendance                = registerScreen("attendance")
	ScreenReports                   = registerScreen("reports")
	ScreenAudience                  = registerScreen("audience")
	ScreenAnalysisAlerts            = registerScreen("analysis_alerts")
	ScreenAIChat                    = registerScreen("ai_chat")
	ScreenAgents                    = registerScreen("agents")
	ScreenAgentsArchived            = registerScreen("agents_archived")
	ScreenAgentNew                  = registerScreen("agent_new")
	ScreenAgentDetail               = registerScreen("agent_detail", "agentId")
	ScreenAgentEdit                 = registerScreen("agent_edit", "agentId")
	ScreenAgentSimulator            = registerScreen("agent_simulator", "agentId")
	ScreenMCPServers                = registerScreen("mcp_servers")
	ScreenKnowledgeBases            = registerScreen("knowledge_bases")
	ScreenKnowledgeBaseNew          = registerScreen("knowledge_base_new")
	ScreenKnowledgeBaseDetail       = registerScreen("knowledge_base_detail", "id")
	ScreenKnowledgeBaseEdit         = registerScreen("knowledge_base_edit", "id")
	ScreenWorkflows                 = registerScreen("workflows")
	ScreenWorkflowNew               = registerScreen("workflow_new")
	ScreenWorkflowDetail            = registerScreen("workflow_detail", "id")
	ScreenWhatsAppCampaigns         = registerScreen("whatsapp_campaigns")
	ScreenWhatsAppCampaignsArchived = registerScreen("whatsapp_campaigns_archived")
	ScreenWhatsAppCampaignNew       = registerScreen("whatsapp_campaign_new")
	ScreenOrganicCampaigns          = registerScreen("organic_campaigns")
	ScreenOrganicCampaignNew        = registerScreen("organic_campaign_new")
	ScreenWhatsAppCampaignDetail    = registerScreen("whatsapp_campaign_detail", "campaignId")
	ScreenWhatsAppCampaignEdit      = registerScreen("whatsapp_campaign_edit", "campaignId")
	ScreenWhatsAppCampaignCRM       = registerScreen("whatsapp_campaign_crm", "campaignId")
	ScreenWhatsAppTemplates         = registerScreen("whatsapp_templates")
	ScreenWhatsAppTemplateNew       = registerScreen("whatsapp_template_new")
	ScreenWhatsAppTemplateDetail    = registerScreen("whatsapp_template_detail", "templateId")
	ScreenWhatsAppTemplateSend      = registerScreen("whatsapp_template_send", "templateId")
	ScreenBusinessPhones            = registerScreen("business_phones")
	ScreenBusinessPhoneConnect      = registerScreen("business_phone_connect")
	ScreenBusinessPhoneGuide        = registerScreen("business_phone_guide")
	ScreenBusinessPhoneDetail       = registerScreen("business_phone_detail", "phoneId")
	ScreenUnofficialNumbers         = registerScreen("unofficial_numbers")
	ScreenUnofficialNumberConnect   = registerScreen("unofficial_number_connect")
	ScreenUnofficialNumberDetail    = registerScreen("unofficial_number_detail", "instanceId")
	ScreenUnofficialCampaigns       = registerScreen("unofficial_campaigns")
	ScreenUnofficialCampaignsArch   = registerScreen("unofficial_campaigns_archived")
	ScreenUnofficialCampaignNew     = registerScreen("unofficial_campaign_new")
	ScreenUnofficialCampaignDetail  = registerScreen("unofficial_campaign_detail", "campaignId")
	ScreenUnofficialCampaignEdit    = registerScreen("unofficial_campaign_edit", "campaignId")
	ScreenInstagramAccounts         = registerScreen("instagram_accounts")
	ScreenInstagramConnect          = registerScreen("instagram_connect")
	ScreenInstagramAccount          = registerScreen("instagram_account", "accountId")
	ScreenFacebookPages             = registerScreen("facebook_pages")
	ScreenFacebookConnect           = registerScreen("facebook_connect")
	ScreenTelegramAccounts          = registerScreen("telegram_accounts")
	ScreenTelegramConnect           = registerScreen("telegram_connect")
	ScreenTelegramAccount           = registerScreen("telegram_account", "accountId")
	ScreenMessageShortcuts          = registerScreen("message_shortcuts")
	ScreenLeads                     = registerScreen("leads")
	ScreenLeadDetail                = registerScreen("lead_detail", "leadId")
	ScreenLinks                     = registerScreen("links")
	ScreenLinkNew                   = registerScreen("link_new")
	ScreenLinkDetail                = registerScreen("link_detail", "id")
	ScreenLinkEdit                  = registerScreen("link_edit", "id")
	ScreenCalendar                  = registerScreen("calendar")
	ScreenCalendarSettings          = registerScreen("calendar_settings")
	ScreenIntegrations              = registerScreen("integrations")
	ScreenIssues                    = registerScreen("issues")
	ScreenIssueNew                  = registerScreen("issue_new")
	ScreenIssueDetail               = registerScreen("issue_detail", "issueId")
	ScreenPlans                     = registerScreen("plans")
	ScreenAddons                    = registerScreen("addons")
	ScreenBalance                   = registerScreen("balance")
	ScreenInvoices                  = registerScreen("invoices")
	ScreenWorkspace                 = registerScreen("workspace")
	ScreenWorkspaceInvites          = registerScreen("workspace_invites")
)

func (s Screen) Valid() bool {
	_, ok := screenParams[s]
	return ok
}

func (s Screen) Params() []string {
	return screenParams[s]
}

func Screens() []Screen {
	out := make([]Screen, 0, len(screenParams))
	for s := range screenParams {
		out = append(out, s)
	}
	return out
}
