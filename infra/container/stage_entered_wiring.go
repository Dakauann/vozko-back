package container

import (
	workflow_domain "vozko/domain/workflow"
	stage_usecase "vozko/usecases/stage"
	workflow_usecase "vozko/usecases/workflow"
)

func (c *Container) announceStageEntries() {
	c.repositories.stage = stage_usecase.AnnounceEntries(c.repositories.stage, workflow_usecase.NewStageEnteredPublisher(c.services.workflowWakePub))
}

func (c *Container) consumeStageTriggers(evaluator workflow_domain.CheckedTriggerEvaluator) {
	c.useCases.consumeStageEntered = workflow_usecase.NewConsumeStageEnteredUseCase(c.services.workflowWakeSub, c.services.workflowWakePub, evaluator)
}
