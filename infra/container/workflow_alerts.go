package container

import (
	"log"

	workflow_domain "vozko/domain/workflow"
	workflow_repository "vozko/infra/repositories/workflow"
	workflow_usecase "vozko/usecases/workflow"
)

func (c *Container) runtimeAlertStore() *workflow_repository.RuntimeAlertRepository {
	return workflow_repository.NewRuntimeAlertRepository(c.db)
}

func (c *Container) wireWorkflowRuntimeAlerts(engine *workflow_usecase.RunEngine, steps *workflow_usecase.NodeExecutorRegistry) {
	store := c.runtimeAlertStore()
	engine.SetRuntimeAlerts(store)
	workflows, ok := c.repositories.workflow.(workflow_usecase.WorkflowsByID)
	if !ok {
		log.Fatalf("[workflow-alerts] the workflow repository cannot read workflows by id")
	}
	alerts, err := workflow_usecase.NewRuntimeAlerts(workflow_usecase.RuntimeAlertsDeps{
		Alerts:     store,
		Signals:    store,
		Workflows:  workflows,
		Agents:     c.repositories.agent,
		Notices:    c.notifications().Publisher,
		Steps:      steps,
		Visibility: c.conversationVisibility(),
	})
	if err != nil {
		log.Fatalf("[workflow-alerts] %v", err)
	}
	c.useCases.workflowRuntimeAlerts = alerts
}

var _ workflow_domain.RuntimeAlertRecorder = (*workflow_repository.RuntimeAlertRepository)(nil)
