package container

import (
	workflowrunexporthttp "vozko/delivery/http/workflowrunexporthttp"
	workflow_repository "vozko/infra/repositories/workflow"
	workflow_usecase "vozko/usecases/workflow"
)

func (c *Container) workflowRunExportHandler() *workflowrunexporthttp.Handler {
	return workflowrunexporthttp.NewHandler(workflow_usecase.NewRunExportUseCase(
		c.useCases.getWorkflowRun,
		c.useCases.scopedWorkflows,
		workflow_repository.NewRunExportSource(c.db),
		workflow_repository.NewRunStepsReader(c.db),
		c.conversationVisibility(),
	))
}
