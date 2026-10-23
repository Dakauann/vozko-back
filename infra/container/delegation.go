package container

import (
	"log"

	conversation_domain "vozko/domain/conversation"
	workflow_repository "vozko/infra/repositories/workflow"
	ia_usecase "vozko/usecases/inbox_assignment"
	workflow_usecase "vozko/usecases/workflow"
)

type delegationSetter interface {
	SetDelegations(conversation_domain.DelegationRepository)
}

func (c *Container) wireDelegations() {
	for name, target := range map[string]any{
		"official whatsapp":   c.useCases.handleWhatsAppMessage,
		"conversation status": c.services.conversationStatusService,
		"inbox history":       c.services.conversationHistory,
	} {
		setter, ok := target.(delegationSetter)
		if !ok {
			log.Fatalf("[delegation] %s cannot read conversation delegations", name)
		}
		setter.SetDelegations(c.services.delegations)
	}
}

func (c *Container) automationDirectory() ia_usecase.AutomationDirectory {
	return ia_usecase.AutomationDirectory{Agents: c.repositories.agent, Workflows: c.repositories.workflow}
}

func (c *Container) automationToggle() *ia_usecase.OperatorAutomationToggle {
	return ia_usecase.NewOperatorAutomationToggle(c.services.conversationAutomation, c.services.assignmentService, c.services.conversationAuth)
}

func (c *Container) automationDelegation(toggle *ia_usecase.OperatorAutomationToggle) *ia_usecase.AutomationDelegation {
	runs := workflow_usecase.NewConversationRunSwitch(workflow_repository.NewConversationRunCanceller(c.db), c.tapWorkflows(), c.repositories.workflowRunLog)
	return ia_usecase.NewAutomationDelegation(c.services.delegations, toggle, c.automationDirectory(), runs)
}

func (c *Container) buttonDelegations() *ia_usecase.AutomationDelegation {
	if c.services.conversationAutomation == nil || c.services.assignmentService == nil || c.services.delegations == nil {
		log.Fatalf("[lead-exit] handing a conversation to an automation from a button needs the conversation automation, the assignment service and the delegations")
	}
	return c.automationDelegation(c.automationToggle())
}
