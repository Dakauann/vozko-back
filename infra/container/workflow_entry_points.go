package container

import (
	workflow_repository "vozko/infra/repositories/workflow"
	workflow_usecase "vozko/usecases/workflow"
)

func (c *Container) workflowEntryPoints() *workflow_usecase.EntryPoints {
	return workflow_usecase.NewEntryPoints(workflow_repository.NewEntryPointRepository(c.db))
}
