package container

import (
	"log"

	workflow_domain "vozko/domain/workflow"
	agent_usecase "vozko/usecases/agent"
	workflow_usecase "vozko/usecases/workflow"
)

func (c *Container) consentGuardedWorkflows() workflow_domain.ScopedWorkflowsUseCase {
	c.mustConsentGrantChecker()
	return workflow_usecase.NewScopedWorkflowsUseCase(workflow_usecase.ScopedWorkflowsDeps{
		Create:      c.useCases.createWorkflow,
		Get:         c.useCases.getWorkflow,
		Update:      c.useCases.updateWorkflow,
		Delete:      c.useCases.deleteWorkflow,
		Activate:    c.useCases.activateWorkflow,
		Pause:       c.useCases.pauseWorkflow,
		Permissions: c.services.conversationAuthImpl,
	})
}

func (c *Container) guardAgentConsentGrants() {
	c.mustConsentGrantChecker()
	for name, uc := range map[string]any{"create agent": c.useCases.createAgent, "update agent": c.useCases.updateAgent} {
		guarded, ok := uc.(agent_usecase.ConsentGrantGuarded)
		if !ok {
			log.Fatalf("[consent-grant] the %s use case cannot check the leads:consent permission", name)
		}
		guarded.GuardConsentGrants(c.services.conversationAuthImpl)
	}
}

func (c *Container) mustConsentGrantChecker() {
	if c.services.conversationAuthImpl == nil {
		log.Fatalf("[consent-grant] the permission checker must be wired before consent grants can be checked")
	}
}
