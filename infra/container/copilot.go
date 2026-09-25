package container

import (
	"time"

	"github.com/google/uuid"

	"vozko/domain/agent"
	"vozko/domain/copilot"
	"vozko/usecases/agentloop"
	copilot_usecase "vozko/usecases/copilot"
	copilottools "vozko/usecases/copilot/copilottools"
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
	attendanceDeps := copilottools.AttendanceDeps{
		Sections:    c.useCases.getOverview,
		Departments: c.useCases.listWorkspaceDepartments,
		Now:         now,
	}
	toolset := append([]copilot.Tool{
		copilottools.NewListAgentsTool(listAgents),
		copilottools.NewCountAgentsTool(listAgents),
		copilottools.NewGetAgentTool(getAgent),
		copilottools.NewCreateAgentTool(createAgent),
		copilottools.NewUpdateAgentTool(getAgent, updateAgent),
		copilottools.NewDeleteAgentTool(getAgent, deleteAgent),
		copilottools.NewListDepartmentsTool(c.useCases.listWorkspaceDepartments),
		copilottools.NewOfferActionTool(state),
		copilottools.NewListModelsTool(c.services.ai),
		copilottools.NewListAgentToolsTool(c.services.toolRegistry),
		copilottools.NewAttendanceMetricsTool(attendanceDeps),
		copilottools.NewAttendanceTrendTool(attendanceDeps),
		copilottools.NewAttendanceTeamTool(attendanceDeps),
		copilottools.NewAttendanceBacklogTool(attendanceDeps),
		copilottools.NewAttendanceStagesTool(attendanceDeps),
		copilottools.NewAttendanceReworkTool(attendanceDeps),
		copilottools.NewAttendanceLiveTool(attendanceDeps),
		copilottools.NewCampaignDispatchTool(c.useCases.getWCDispatchReport, attendanceDeps),
		c.conversationInsightsTool(now),
		copilottools.NewCalculateTool(),
		copilottools.NewQueryDatasetTool(),
		copilottools.NewRenderChartTool(),
	}, c.operationTools()...)
	return copilot_usecase.NewService(
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
		{copilottools.NewCreateCalendarEventTool(c.useCases.createCalendarEvent)},
		{
			copilottools.NewPauseWorkflowTool(c.useCases.scopedWorkflows),
			copilottools.NewActivateWorkflowTool(c.useCases.scopedWorkflows),
		},
	} {
		out = append(out, group...)
	}
	return out
}

func (c *Container) templateCreateTools() []copilot.Tool {
	deps := copilottools.TemplateCreateDeps{
		Phones:    c.useCases.workspacePhones,
		Templates: c.useCases.workspaceTemplates,
		Media:     c.useCases.getMedia,
	}
	return []copilot.Tool{
		copilottools.NewListBusinessPhonesTool(deps),
		copilottools.NewCreateTemplateTool(deps),
	}
}

func (c *Container) campaignTools() []copilot.Tool {
	deps := copilottools.CampaignDeps{
		Preview:   c.useCases.wcImportPreview,
		Create:    c.useCases.createWCCampaign,
		Start:     c.useCases.startWCCampaign,
		Access:    c.useCases.wcCampaignAccess,
		Templates: c.useCases.workspaceTemplates,
		Phones:    c.useCases.workspacePhones,
		Costs:     c.useCases.consumeWhatsappTemplate,
		Balance:   c.services.cachedBalanceChecker,
	}
	return []copilot.Tool{
		copilottools.NewPreviewCampaignImportTool(deps),
		copilottools.NewCreateCampaignTool(deps),
		copilottools.NewStartCampaignTool(deps),
	}
}

func (c *Container) dealTools() []copilot.Tool {
	if c.services.conversationEntryLookup == nil || c.useCases.personDeals == nil {
		return nil
	}
	deps := copilottools.DealDeps{
		Deals:     c.useCases.personDeals,
		Pipelines: c.useCases.listPipelines,
		Stages:    c.useCases.listStages,
		Entries:   c.services.conversationEntryLookup,
	}
	return []copilot.Tool{
		copilottools.NewListDealPipelinesTool(deps),
		copilottools.NewListDealsTool(deps),
		copilottools.NewCreateDealTool(deps),
		copilottools.NewMoveDealTool(deps),
		copilottools.NewLinkDealTool(deps),
	}
}

func (c *Container) funnelTools() []copilot.Tool {
	funnels := c.funnelStages()
	if funnels == nil {
		return nil
	}
	deps := copilottools.FunnelDeps{
		CreatePipeline: c.useCases.createPipeline,
		CreateStage:    c.useCases.createStage,
		UpdateStage:    c.useCases.updateStage,
		ReorderStages:  c.useCases.reorderStages,
		SetInitial:     c.useCases.setInitialStage,
		Funnels:        funnels,
	}
	return []copilot.Tool{
		copilottools.NewCreatePipelineTool(deps),
		copilottools.NewCreateStageTool(deps),
		copilottools.NewRenameStageTool(deps),
		copilottools.NewReorderStagesTool(deps),
		copilottools.NewSetInitialStageTool(deps),
	}
}

func (c *Container) assignmentTools() []copilot.Tool {
	if c.services.conversationEntryLookup == nil || c.services.personAssign == nil {
		return nil
	}
	deps := copilottools.AssignmentDeps{
		Assign:      c.services.personAssign,
		Members:     c.useCases.listAssignableMembers,
		Departments: c.useCases.listWorkspaceDepartments,
		Entries:     c.services.conversationEntryLookup,
	}
	return []copilot.Tool{
		copilottools.NewListAssignableMembersTool(deps),
		copilottools.NewAssignConversationTool(deps),
		copilottools.NewTransferConversationTool(deps),
	}
}

func (c *Container) conversationActionTools() []copilot.Tool {
	if c.services.conversationEntryLookup == nil || c.services.personSend == nil {
		return nil
	}
	deps := copilottools.ConversationActionDeps{
		Send:      c.services.personSend,
		Scheduler: c.useCases.personScheduler,
		Entries:   c.services.conversationEntryLookup,
		Broadcast: c.services.conversationHub,
	}
	templates := copilottools.TemplateSendDeps{
		Send:      c.services.personTemplateSend,
		Templates: c.useCases.workspaceTemplates,
		Costs:     c.useCases.consumeWhatsappTemplate,
		Entries:   c.services.conversationEntryLookup,
	}
	return []copilot.Tool{
		copilottools.NewSendMessageTool(deps),
		copilottools.NewScheduleMessageTool(deps),
		copilottools.NewCancelScheduledMessageTool(deps),
		copilottools.NewSendTemplateTool(templates),
	}
}

func (c *Container) labelTools() []copilot.Tool {
	if c.services.conversationEntryLookup == nil {
		return nil
	}
	deps := copilottools.LabelDeps{
		EntryLabels: c.useCases.entryLabels,
		Create:      c.useCases.createLabel,
		List:        c.useCases.listLabels,
		Entries:     c.services.conversationEntryLookup,
		Broadcast:   c.services.conversationHub,
	}
	return []copilot.Tool{
		copilottools.NewApplyLabelTool(deps),
		copilottools.NewRemoveLabelTool(deps),
		copilottools.NewCreateLabelTool(deps),
	}
}

func (c *Container) stageMoveTools() []copilot.Tool {
	funnels := c.funnelStages()
	if funnels == nil || c.services.conversationEntryLookup == nil {
		return nil
	}
	deps := copilottools.StageMoveDeps{
		Move:      c.useCases.moveEntryStage,
		Funnels:   funnels,
		Entries:   c.services.conversationEntryLookup,
		Broadcast: c.services.conversationHub,
	}
	return []copilot.Tool{
		copilottools.NewMoveConversationStageTool(deps),
		copilottools.NewMoveConversationFunnelTool(deps),
	}
}

func (c *Container) catalogTools() []copilot.Tool {
	funnels := c.funnelStages()
	if funnels == nil {
		return nil
	}
	deps := copilottools.CatalogDeps{
		Templates: c.useCases.workspaceTemplates,
		Funnels:   funnels,
		Labels:    c.useCases.listLabels,
		Events:    c.useCases.listCalendarEvents,
		Workflows: c.useCases.listWorkflows,
		Now:       time.Now,
	}
	return []copilot.Tool{
		copilottools.NewListTemplatesTool(deps),
		copilottools.NewListPipelinesTool(deps),
		copilottools.NewListLabelsTool(deps),
		copilottools.NewListCalendarEventsTool(deps),
		copilottools.NewListWorkflowsTool(deps),
	}
}

func (c *Container) knowledgeTools() []copilot.Tool {
	deps := copilottools.KnowledgeDeps{List: c.useCases.listKnowledgeBases, Query: c.useCases.scopedKnowledgeQuery}
	return []copilot.Tool{
		copilottools.NewListKnowledgeBasesTool(deps),
		copilottools.NewSearchKnowledgeTool(deps),
	}
}

func (c *Container) knowledgeWriteTools() []copilot.Tool {
	deps := copilottools.KnowledgeWriteDeps{
		Create:    c.useCases.createKnowledgeBase,
		Access:    c.useCases.knowledgeBaseAccess,
		Documents: c.useCases.scopedKnowledgeDocuments,
		Media:     c.useCases.getMedia,
	}
	return []copilot.Tool{
		copilottools.NewCreateKnowledgeBaseTool(deps),
		copilottools.NewAddKnowledgeDocumentTool(deps),
	}
}

func (c *Container) conversationTools() []copilot.Tool {
	if c.services.inboxService == nil || c.services.conversationHistoryReader == nil {
		return nil
	}
	deps := copilottools.ConversationDeps{
		Inbox:   c.services.inboxService,
		History: c.services.conversationHistoryReader,
		Leads:   c.useCases.leadQueries,
	}
	return []copilot.Tool{
		copilottools.NewSearchConversationsTool(deps),
		copilottools.NewReadConversationTool(deps),
	}
}

func (c *Container) leadTools() []copilot.Tool {
	deps := copilottools.LeadDeps{Leads: c.useCases.leadQueries, Memories: c.useCases.listLeadMemories}
	memories := copilottools.LeadMemoryDeps{Leads: c.useCases.leadQueries, Create: c.useCases.createLeadMemory, Update: c.useCases.updateLeadMemory}
	return []copilot.Tool{
		copilottools.NewSearchLeadsTool(deps),
		copilottools.NewGetLeadTool(deps),
		copilottools.NewAddLeadMemoryTool(memories),
		copilottools.NewUpdateLeadMemoryTool(memories),
	}
}

func (c *Container) conversationInsightsTool(now copilottools.Clock) copilot.Tool {
	if c.audience == nil || !c.audience.Enabled {
		return nil
	}
	return copilottools.NewConversationInsightsTool(c.audience.Stats, c.audience.List, now)
}
