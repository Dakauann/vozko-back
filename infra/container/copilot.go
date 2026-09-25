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
	attendanceDeps := copilottools.AttendanceDeps{
		Sections:    c.useCases.getOverview,
		Departments: c.useCases.listWorkspaceDepartments,
		Now:         now,
	}
	return copilot_usecase.NewService(
		agentloop.Engine{AI: c.services.ai},
		copilot_usecase.NewRegistry(
			copilottools.NewListAgentsTool(listAgents),
			copilottools.NewCountAgentsTool(listAgents),
			copilottools.NewGetAgentTool(getAgent),
			copilottools.NewCreateAgentTool(createAgent),
			copilottools.NewUpdateAgentTool(getAgent, updateAgent),
			copilottools.NewDeleteAgentTool(getAgent, deleteAgent),
			copilottools.NewListDepartmentsTool(c.useCases.listWorkspaceDepartments),
			copilottools.NewListModelsTool(c.services.ai),
			copilottools.NewListAgentToolsTool(c.services.toolRegistry),
			copilottools.NewAttendanceMetricsTool(attendanceDeps),
			copilottools.NewAttendanceTrendTool(attendanceDeps),
			copilottools.NewAttendanceTeamTool(attendanceDeps),
			copilottools.NewAttendanceBacklogTool(attendanceDeps),
			c.conversationInsightsTool(now),
			copilottools.NewCalculateTool(),
			copilottools.NewQueryDatasetTool(),
			copilottools.NewRenderChartTool(),
		),
		c.useCases.checkWsAccess,
		c.repositories.aichatThread,
		c.repositories.aichatMessage,
		copilot_usecase.NewInMemoryPendingStore(),
		func() string { return uuid.New().String() },
	)
}

func (c *Container) conversationInsightsTool(now copilottools.Clock) copilot.Tool {
	if c.audience == nil || !c.audience.Enabled {
		return nil
	}
	return copilottools.NewConversationInsightsTool(c.audience.Stats, c.audience.List, now)
}
