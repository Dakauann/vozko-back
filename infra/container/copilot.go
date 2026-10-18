package container

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

	"vozko/domain/agent"
	"vozko/domain/copilot"
	"vozko/infra/copilotscreen"
	aichat_repository "vozko/infra/repositories/aichat"
	"vozko/usecases/agentloop"
	copilot_usecase "vozko/usecases/copilot"
	copilottools "vozko/usecases/copilot/copilottools"
	"vozko/usecases/copilot/copilottools/attendancetools"
	"vozko/usecases/copilot/copilottools/crmtools"
	"vozko/usecases/copilot/copilottools/leadtools"
	"vozko/usecases/copilot/copilottools/mediatools"
	"vozko/usecases/copilot/copilottools/platformtools"
	"vozko/usecases/copilot/copilottools/skilltools"
	"vozko/usecases/copilot/copilottools/telephonytools"
	"vozko/usecases/copilot/skills"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

func (c *Container) buildCopilot(
	listAgents agent.ListAgentsUseCase,
	getAgent agent.GetAgentUseCase,
	createAgent agent.CreateAgentUseCase,
	updateAgent agent.UpdateAgentUseCase,
	deleteAgent agent.DeleteAgentUseCase,
) *copilot_usecase.Service {
	now := time.Now
	state := c.workspaceReadiness()
	attendanceDeps := attendancetools.AttendanceDeps{
		Sections:    c.useCases.getOverview,
		Departments: c.useCases.listWorkspaceDepartments,
		Now:         now,
	}
	skillLibrary, err := skills.Load()
	if err != nil {
		log.Fatalf("[container] copilot skills: %v", err)
	}
	agentDeps := platformtools.AgentDeps{KnowledgeBases: c.useCases.listKnowledgeBases, Collections: c.mcpCollection, Catalog: c.services.toolRegistry}
	toolset := append([]copilot.Tool{
		platformtools.NewListAgentsTool(listAgents),
		platformtools.NewCountAgentsTool(listAgents),
		platformtools.NewGetAgentTool(getAgent, c.services.toolRegistry),
		platformtools.NewCreateAgentTool(createAgent, agentDeps),
		platformtools.NewUpdateAgentTool(getAgent, updateAgent, agentDeps),
		platformtools.NewDeleteAgentTool(getAgent, deleteAgent),
		platformtools.NewListDepartmentsTool(c.useCases.listWorkspaceDepartments),
		platformtools.NewOfferActionTool(state),
		platformtools.NewAskUserTool(),
		platformtools.NewListModelsTool(c.services.ai),
		platformtools.NewListAgentToolsTool(c.services.toolRegistry),
		attendancetools.NewAttendanceMetricsTool(attendanceDeps),
		attendancetools.NewAttendanceTrendTool(attendanceDeps),
		attendancetools.NewAttendanceTeamTool(attendanceDeps),
		attendancetools.NewMemberActivityTool(c.memberActivityUseCase()),
		attendancetools.NewAttendanceBacklogTool(attendanceDeps),
		attendancetools.NewAttendanceStagesTool(attendanceDeps),
		attendancetools.NewAttendanceReworkTool(attendanceDeps),
		attendancetools.NewAttendanceLiveTool(attendanceDeps),
		attendancetools.NewCampaignDispatchTool(c.useCases.getWCDispatchReport, attendanceDeps),
		c.conversationInsightsTool(now),
		attendancetools.NewCalculateTool(),
		attendancetools.NewQueryDatasetTool(),
		attendancetools.NewRenderChartTool(),
		skilltools.NewLoadSkillTool(skillLibrary),
	}, append(c.operationTools(), c.numberAutomationTools(getAgent)...)...)
	svc := copilot_usecase.NewService(
		agentloop.Engine{AI: c.services.ai},
		copilot_usecase.NewRegistry(toolset...),
		c.useCases.checkWsAccess,
		c.useCases.chatFunds,
		c.repositories.aichatThread,
		c.repositories.aichatMessage,
		c.useCases.getMedia,
		state,
		func() string { return uuid.New().String() },
	)
	screens := copilotscreen.NewBroker(c.redisProvider.SharedState())
	screens.Start(context.Background())
	svc.SetScreenMailbox(screens)
	svc.SetQueue(c.repositories.aichatQueue)
	svc.SetProjectAdopter(aichat_repository.NewProjectAdopter(c.db))
	return svc
}

func (c *Container) operationTools() []copilot.Tool {
	var out []copilot.Tool
	for _, group := range [][]copilot.Tool{
		c.conversationTools(),
		c.leadTools(),
		c.knowledgeTools(),
		c.knowledgeWriteTools(),
		c.catalogTools(),
		c.stageMoveTools(),
		c.labelTools(),
		c.conversationActionTools(),
		c.assignmentTools(),
		c.funnelTools(),
		c.dealTools(),
		c.templateCreateTools(),
		c.campaignTools(),
		c.unofficialCampaignTools(),
		c.unofficialGroupTools(),
		c.workspaceAdminTools(),
		c.accessTools(),
		c.callTools(),
		c.telegramTools(),
		c.adsTools(),
		{
			mediatools.NewGenerateImageTool(c.mediaGeneration().Service),
			mediatools.NewGenerateMusicTool(c.mediaGeneration().Service),
			mediatools.NewGenerateVoiceoverTool(c.mediaGeneration().Service),
			mediatools.NewRenderVideoTool(c.mediaGeneration().Service),
		},
		c.studioTools(),
		{crmtools.NewCreateCalendarEventTool(c.useCases.createCalendarEvent)},
		{
			crmtools.NewPauseWorkflowTool(c.useCases.scopedWorkflows),
			crmtools.NewActivateWorkflowTool(c.useCases.scopedWorkflows),
		},
	} {
		out = append(out, group...)
	}
	return out
}

func (c *Container) templateCreateTools() []copilot.Tool {
	deps := crmtools.TemplateCreateDeps{
		Phones:  c.useCases.workspacePhones,
		Creator: c.buttonActions(),
		Media:   c.useCases.getMedia,
	}
	return []copilot.Tool{
		crmtools.NewListBusinessPhonesTool(deps),
		crmtools.NewCreateTemplateTool(deps),
		crmtools.NewEditTemplateTool(crmtools.TemplateEditDeps{
			Templates: c.useCases.workspaceTemplates,
			Editor:    c.buttonActions(),
			Media:     c.useCases.getMedia,
		}),
	}
}

func (c *Container) numberAutomationTools(getAgent agent.GetAgentUseCase) []copilot.Tool {
	funnels := c.funnelStages()
	if funnels == nil {
		return nil
	}
	deps := platformtools.NumberAutomationDeps{
		Numbers:   c.numberReceptive(),
		Agents:    getAgent,
		Workflows: c.useCases.scopedWorkflows,
		Funnels:   funnels,
	}
	return []copilot.Tool{
		platformtools.NewNumberAutomationTool(deps),
		platformtools.NewConfigureNumberAutomationTool(deps),
	}
}

func (c *Container) campaignTools() []copilot.Tool {
	deps := crmtools.CampaignDeps{
		Preview:   c.useCases.wcImportPreview,
		Create:    c.useCases.createWCCampaign,
		Start:     c.useCases.startWCCampaign,
		Access:    c.useCases.wcCampaignAccess,
		Templates: c.useCases.workspaceTemplates,
		Phones:    c.useCases.workspacePhones,
		Costs:     c.useCases.consumeWhatsappTemplate,
		Balance:   c.services.cachedBalanceChecker,
		Exit:      c.buttonActions(),
	}
	return []copilot.Tool{
		crmtools.NewPreviewCampaignImportTool(deps),
		crmtools.NewCreateCampaignTool(deps),
		crmtools.NewStartCampaignTool(deps),
	}
}

func (c *Container) workspaceAdminTools() []copilot.Tool {
	deps := platformtools.WorkspaceAdminDeps{
		Members:           c.useCases.listWsMembers,
		Invite:            c.useCases.inviteMember,
		Invites:           c.useCases.listWorkspaceInvites,
		CancelInvite:      c.useCases.cancelInvite,
		RemoveMember:      c.useCases.removeMember,
		ChangeRole:        c.useCases.updateMemberRole,
		Permissions:       c.useCases.getMemberPermissions,
		SetPermissions:    c.useCases.setMemberPermissions,
		Catalog:           c.useCases.listResourcePermissions,
		Roles:             c.useCases.listCustomRoles,
		CreateRole:        c.useCases.createCustomRole,
		UpdateRole:        c.useCases.updateCustomRole,
		DeleteRole:        c.useCases.deleteCustomRole,
		AssignRole:        c.useCases.assignCustomRole,
		CreateDepartment:  c.useCases.createWorkspaceDepartment,
		ScopedDepartments: c.useCases.scopedDepartments,
	}
	return []copilot.Tool{
		platformtools.NewListWorkspaceMembersTool(deps),
		platformtools.NewListWorkspaceInvitesTool(deps),
		platformtools.NewGetMemberPermissionsTool(deps),
		platformtools.NewListPermissionCatalogTool(deps),
		platformtools.NewInviteMemberTool(deps),
		platformtools.NewCancelInviteTool(deps),
		platformtools.NewRemoveMemberTool(deps),
		platformtools.NewChangeMemberRoleTool(deps),
		platformtools.NewUpdateMemberPermissionsTool(deps),
		platformtools.NewListRolesTool(deps),
		platformtools.NewCreateRoleTool(deps),
		platformtools.NewUpdateRoleTool(deps),
		platformtools.NewDeleteRoleTool(deps),
		platformtools.NewListDepartmentMembersTool(deps),
		platformtools.NewCreateDepartmentTool(deps),
		platformtools.NewUpdateDepartmentTool(deps),
		platformtools.NewDeleteDepartmentTool(deps),
		platformtools.NewAddDepartmentMemberTool(deps),
		platformtools.NewRemoveDepartmentMemberTool(deps),
	}
}

func (c *Container) accessTools() []copilot.Tool {
	deps := platformtools.AccessDeps{Diagnose: c.useCases.diagnoseAccess, Projects: c.studioService()}
	return []copilot.Tool{
		platformtools.NewExplainPermissionTool(),
		platformtools.NewDiagnoseAccessTool(deps),
		platformtools.NewOpenScreenTool(deps),
	}
}

func (c *Container) callTools() []copilot.Tool {
	if c.sipTrunks == nil || c.callRouting == nil {
		return nil
	}
	history := telephonytools.CallHistoryDeps{History: c.callHistory(), Permissions: c.useCases.checkWsAccess}
	lines := telephonytools.PhoneLineDeps{
		List:   c.sipTrunks.Lines.List,
		Get:    c.sipTrunks.Lines.Get,
		Create: c.sipTrunks.Lines.Create,
		Update: c.sipTrunks.Lines.Update,
		Delete: c.sipTrunks.Lines.Delete,
	}
	queues := telephonytools.CallQueueDeps{
		Queues:      c.callRouting.Catalog,
		Monitor:     c.callRouting.Monitor,
		Music:       c.callRouting.Music,
		Names:       c.services.callSessionUsernameResolver,
		Departments: c.useCases.scopedDepartments,
		Now:         time.Now,
	}
	return []copilot.Tool{
		telephonytools.NewPlaceCallTool(telephonytools.CallDeps{Planner: c.sipTrunks.Planner}),
		telephonytools.NewListCallsTool(history),
		telephonytools.NewGetCallTool(history),
		telephonytools.NewListPhoneLinesTool(lines),
		telephonytools.NewCreatePhoneLineTool(lines),
		telephonytools.NewUpdatePhoneLineTool(lines),
		telephonytools.NewChangePhoneLinePasswordTool(lines),
		telephonytools.NewDeletePhoneLineTool(lines),
		telephonytools.NewListCallQueuesTool(queues),
		telephonytools.NewCreateCallQueueTool(queues),
		telephonytools.NewUpdateCallQueueTool(queues),
		telephonytools.NewDeleteCallQueueTool(queues),
	}
}

func (c *Container) telegramTools() []copilot.Tool {
	if c.telegram == nil || c.telegram.Connect == nil {
		return nil
	}
	deps := crmtools.TelegramDeps{Connect: c.telegram.Connect, List: c.telegram.List, Departments: c.useCases.scopedDepartments}
	return []copilot.Tool{
		crmtools.NewListTelegramBotsTool(deps),
		crmtools.NewConnectTelegramBotTool(deps),
	}
}

func (c *Container) unofficialCampaignTools() []copilot.Tool {
	campaigns := c.unofficialWhatsAppCampaigns
	if c.unofficialWhatsApp == nil || !c.unofficialWhatsApp.Enabled || campaigns == nil || !campaigns.Enabled {
		return nil
	}
	deps := c.unofficialCampaignToolDeps()
	return []copilot.Tool{
		crmtools.NewListUnofficialNumbersTool(deps),
		crmtools.NewPreviewUnofficialImportTool(deps),
		crmtools.NewCreateUnofficialCampaignTool(deps),
		crmtools.NewStartUnofficialCampaignTool(deps),
	}
}

func (c *Container) unofficialCampaignToolDeps() crmtools.UnofficialCampaignDeps {
	campaigns := c.unofficialWhatsAppCampaigns
	return crmtools.UnofficialCampaignDeps{
		Scopes:  c.services.conversationAuthImpl,
		Numbers: uwuc.NewListInstancesUseCase(c.unofficialWhatsApp.Instances),
		Usable:  campaigns.Instances,
		Preview: campaigns.Preview,
		Create:  campaigns.Create,
		Access:  campaigns.Access,
		Actions: campaigns.Actions,
		Media:   c.useCases.getMedia,
	}
}

func (c *Container) unofficialGroupTools() []copilot.Tool {
	campaigns := c.unofficialWhatsAppCampaigns
	if c.unofficialWhatsApp == nil || c.unofficialWhatsApp.WorkspaceGroups == nil || campaigns == nil || !campaigns.Enabled {
		return nil
	}
	deps := crmtools.UnofficialGroupDeps{
		Campaigns:   c.unofficialCampaignToolDeps(),
		Groups:      c.unofficialWhatsApp.WorkspaceGroups,
		Permissions: c.useCases.checkWsAccess,
	}
	return []copilot.Tool{
		crmtools.NewUnofficialGroupsDashboardTool(deps),
		crmtools.NewListUnofficialGroupsTool(deps),
		crmtools.NewUnofficialGroupOverviewTool(deps),
		crmtools.NewUnofficialGroupPeopleTool(deps),
		crmtools.NewCreateUnofficialGroupCampaignTool(deps),
		crmtools.NewCreateUnofficialGroupAnalysisTool(deps),
	}
}

func (c *Container) dealTools() []copilot.Tool {
	if c.services.conversationEntryLookup == nil || c.useCases.personDeals == nil {
		return nil
	}
	deps := crmtools.DealDeps{
		Deals:     c.useCases.personDeals,
		Pipelines: c.useCases.listPipelines,
		Stages:    c.useCases.listStages,
		Entries:   c.services.conversationEntryLookup,
	}
	return []copilot.Tool{
		crmtools.NewListDealPipelinesTool(deps),
		crmtools.NewListDealsTool(deps),
		crmtools.NewCreateDealTool(deps),
		crmtools.NewMoveDealTool(deps),
		crmtools.NewLinkDealTool(deps),
	}
}

func (c *Container) funnelTools() []copilot.Tool {
	funnels := c.funnelStages()
	if funnels == nil {
		return nil
	}
	deps := crmtools.FunnelDeps{
		CreatePipeline: c.useCases.createPipeline,
		CreateStage:    c.useCases.createStage,
		UpdateStage:    c.useCases.updateStage,
		ReorderStages:  c.useCases.reorderStages,
		SetInitial:     c.useCases.setInitialStage,
		Funnels:        funnels,
	}
	return []copilot.Tool{
		crmtools.NewCreatePipelineTool(deps),
		crmtools.NewCreateStageTool(deps),
		crmtools.NewRenameStageTool(deps),
		crmtools.NewReorderStagesTool(deps),
		crmtools.NewSetInitialStageTool(deps),
	}
}

func (c *Container) assignmentTools() []copilot.Tool {
	if c.services.conversationEntryLookup == nil || c.services.personAssign == nil {
		return nil
	}
	deps := crmtools.AssignmentDeps{
		Assign:      c.services.personAssign,
		Members:     c.useCases.listAssignableMembers,
		Departments: c.useCases.listWorkspaceDepartments,
		Entries:     c.services.conversationEntryLookup,
	}
	return []copilot.Tool{
		crmtools.NewListAssignableMembersTool(deps),
		crmtools.NewAssignConversationTool(deps),
		crmtools.NewTransferConversationTool(deps),
	}
}

func (c *Container) conversationActionTools() []copilot.Tool {
	if c.services.conversationEntryLookup == nil || c.services.personSend == nil {
		return nil
	}
	deps := crmtools.ConversationActionDeps{
		Send:      c.services.personSend,
		Scheduler: c.useCases.personScheduler,
		Entries:   c.services.conversationEntryLookup,
		Broadcast: c.services.conversationHub,
	}
	templates := crmtools.TemplateSendDeps{
		Scheduler: c.useCases.personScheduler,
		Send:      c.services.personTemplateSend,
		Templates: c.useCases.workspaceTemplates,
		Costs:     c.useCases.consumeWhatsappTemplate,
		Balance:   c.services.cachedBalanceChecker,
		Entries:   c.services.conversationEntryLookup,
	}
	return []copilot.Tool{
		crmtools.NewSendMessageTool(deps),
		crmtools.NewScheduleMessageTool(deps),
		crmtools.NewCancelScheduledMessageTool(deps),
		crmtools.NewSendTemplateTool(templates),
	}
}

func (c *Container) labelTools() []copilot.Tool {
	if c.services.conversationEntryLookup == nil {
		return nil
	}
	deps := crmtools.LabelDeps{
		EntryLabels: c.useCases.entryLabels,
		Create:      c.useCases.createLabel,
		List:        c.useCases.listLabels,
		Entries:     c.services.conversationEntryLookup,
		Broadcast:   c.services.conversationHub,
	}
	return []copilot.Tool{
		crmtools.NewApplyLabelTool(deps),
		crmtools.NewRemoveLabelTool(deps),
		crmtools.NewCreateLabelTool(deps),
	}
}

func (c *Container) stageMoveTools() []copilot.Tool {
	funnels := c.funnelStages()
	if funnels == nil || c.services.conversationEntryLookup == nil {
		return nil
	}
	deps := crmtools.StageMoveDeps{
		Move:      c.useCases.moveEntryStage,
		Funnels:   funnels,
		Entries:   c.services.conversationEntryLookup,
		Broadcast: c.services.conversationHub,
	}
	return []copilot.Tool{
		crmtools.NewMoveConversationStageTool(deps),
		crmtools.NewMoveConversationFunnelTool(deps),
	}
}

func (c *Container) catalogTools() []copilot.Tool {
	funnels := c.funnelStages()
	if funnels == nil {
		return nil
	}
	deps := crmtools.CatalogDeps{
		Templates: c.useCases.workspaceTemplates,
		Funnels:   funnels,
		Labels:    c.useCases.listLabels,
		Events:    c.useCases.listCalendarEvents,
		Workflows: c.useCases.listWorkflows,
		Now:       time.Now,
	}
	return []copilot.Tool{
		crmtools.NewListTemplatesTool(deps),
		crmtools.NewListPipelinesTool(deps),
		crmtools.NewListLabelsTool(deps),
		crmtools.NewListCalendarEventsTool(deps),
		crmtools.NewListWorkflowsTool(deps),
	}
}

func (c *Container) knowledgeTools() []copilot.Tool {
	deps := crmtools.KnowledgeDeps{List: c.useCases.listKnowledgeBases, Query: c.useCases.scopedKnowledgeQuery}
	return []copilot.Tool{
		crmtools.NewListKnowledgeBasesTool(deps),
		crmtools.NewSearchKnowledgeTool(deps),
	}
}

func (c *Container) knowledgeWriteTools() []copilot.Tool {
	deps := crmtools.KnowledgeWriteDeps{
		Create:    c.useCases.createKnowledgeBase,
		Access:    c.useCases.knowledgeBaseAccess,
		Documents: c.useCases.scopedKnowledgeDocuments,
		Media:     c.useCases.getMedia,
	}
	return []copilot.Tool{
		crmtools.NewCreateKnowledgeBaseTool(deps),
		crmtools.NewAddKnowledgeDocumentTool(deps),
	}
}

func (c *Container) conversationTools() []copilot.Tool {
	if c.services.inboxService == nil || c.services.conversationHistoryReader == nil {
		return nil
	}
	deps := crmtools.ConversationDeps{
		Inbox:   c.services.inboxService,
		History: c.services.conversationHistoryReader,
		Leads:   c.useCases.leadQueries,
	}
	return []copilot.Tool{
		crmtools.NewSearchConversationsTool(deps),
		crmtools.NewReadConversationTool(deps),
	}
}

func (c *Container) leadTools() []copilot.Tool {
	deps := leadtools.LeadDeps{
		Leads:       c.useCases.leadQueries,
		Memories:    c.useCases.listLeadMemories,
		Pages:       c.leadPages(),
		Sections:    c.leadSections(),
		Areas:       c.leadAreas(),
		Definitions: c.repositories.customField,
	}
	memories := leadtools.LeadMemoryDeps{Leads: c.useCases.leadQueries, Create: c.useCases.createLeadMemory, Update: c.useCases.updateLeadMemory}
	actions := leadtools.LeadActionDeps{
		Leads:     deps,
		Actions:   c.leadActions(),
		Sends:     c.leadSends(),
		Proposals: c.redisProvider.SharedState(),
		Templates: c.useCases.workspaceTemplates,
		Names:     c.actorNames(),
		Exit:      c.buttonActions(),
	}
	return []copilot.Tool{
		leadtools.NewSearchLeadsTool(deps),
		leadtools.NewGetLeadTool(deps),
		leadtools.NewLeadGeoSummaryTool(deps),
		leadtools.NewAddLeadMemoryTool(memories),
		leadtools.NewUpdateLeadMemoryTool(memories),
		leadtools.NewPrepareLeadActionTool(actions),
		leadtools.NewStartLeadSendTool(actions),
		leadtools.NewCancelLeadSendTool(actions),
	}
}

func (c *Container) conversationInsightsTool(now copilottools.Clock) copilot.Tool {
	if c.audience == nil || !c.audience.Enabled {
		return nil
	}
	return attendancetools.NewConversationInsightsTool(c.audience.Stats, c.audience.List, now)
}
