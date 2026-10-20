package container

import (
	"log"

	workflow_usecase "vozko/usecases/workflow"
)

func (c *Container) wireConversationRuns(engine *workflow_usecase.RunEngine) {
	shared := c.redisProvider.SharedState()
	starter, err := workflow_usecase.NewConversationRunStarter(
		c.repositories.workflow, c.repositories.workflowRun, workflow_usecase.NewEngineRunLauncher(engine, shared), shared)
	if err != nil {
		log.Fatalf("[lead-exit] starting a workflow from a tap: %v", err)
	}
	c.conversationRuns = starter
	c.useCases.startWorkflowRun = workflow_usecase.NewStartRunUseCase(starter, c.repositories.workflowRun, c.conversationVisibility())
}

func (c *Container) tapWorkflows() *workflow_usecase.ConversationRunStarter {
	if c.conversationRuns == nil {
		log.Fatalf("[lead-exit] the tap workflow starter is read before the workflow engine is built")
	}
	return c.conversationRuns
}
