package container

import (
	"log"

	conversation_domain "vozko/domain/conversation"
	ia_usecase "vozko/usecases/inbox_assignment"
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

func (c *Container) automationDelegation(toggle *ia_usecase.OperatorAutomationToggle) *ia_usecase.AutomationDelegation {
	return ia_usecase.NewAutomationDelegation(c.services.delegations, toggle, ia_usecase.AutomationDirectory{
		Agents:    c.repositories.agent,
		Workflows: c.repositories.workflow,
	})
}
