package container

import (
	"log"

	aa "vozko/domain/ai_attendance"
	conversation_domain "vozko/domain/conversation"
	workflow_domain "vozko/domain/workflow"
	ai_attendance_repository "vozko/infra/repositories/ai_attendance"
	workflow_repository "vozko/infra/repositories/workflow"
	aa_usecase "vozko/usecases/ai_attendance"
	workflow_usecase "vozko/usecases/workflow"
)

func (c *Container) webhookDeliveries() workflow_domain.WebhookDeliveryRepository {
	return workflow_repository.NewWebhookDeliveryRepository(c.db)
}

func (c *Container) aiToolCallLog() aa.ToolCallLog {
	return ai_attendance_repository.NewToolCallLog(c.db)
}

func (c *Container) listAIToolCalls() *aa_usecase.ListToolCallsUseCase {
	return aa_usecase.NewListToolCallsUseCase(c.aiToolCallLog(), c.conversationVisibility())
}

func (c *Container) conversationVisibility() conversation_domain.Visibility {
	if c.services.conversationAuth == nil {
		log.Fatalf("[workflow-visibility] the conversation authorizer is read before it is built")
	}
	return conversation_domain.NewVisibility(c.services.conversationAuth)
}

func (c *Container) cancelWorkflowRun() workflow_domain.CancelRunUseCase {
	cancels, ok := c.repositories.workflowRun.(workflow_domain.RunCanceller)
	if !ok {
		log.Fatalf("[workflow-runs] the run store cannot cancel a run by its status alone")
	}
	return workflow_usecase.NewCancelRunUseCase(c.repositories.workflowRun, cancels, c.repositories.workflowRunLog, c.services.metrics, c.conversationVisibility())
}

func (c *Container) workflowRunPages() workflow_domain.RunDetailPages {
	pages, ok := c.useCases.getWorkflowRun.(workflow_domain.RunDetailPages)
	if !ok {
		log.Fatalf("[workflow-runs] the run detail cannot read its steps a page at a time")
	}
	return pages
}

func (c *Container) wireConversationWorkflowRunList(steps workflow_usecase.StepNamer) {
	c.useCases.listConversationRuns = workflow_usecase.NewListConversationRunsUseCase(
		workflow_repository.NewConversationRunsRepository(c.db),
		c.conversationVisibility(),
		steps,
	)
}

func (c *Container) workflowTraceRetention() *workflow_usecase.TraceRetention {
	return workflow_usecase.NewTraceRetention(
		workflow_repository.NewTraceRetentionRepository(c.db),
		c.webhookDeliveries(),
		c.aiToolCallLog(),
		c.runtimeAlertStore(),
	)
}
