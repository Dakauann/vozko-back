package conversation_usecase

import (
	"vozko/domain/conversation"
	workflow_domain "vozko/domain/workflow"
)

func (a *agentContext) automationRuns() bool {
	return a != nil && a.automationResolved && (a.wcEntry == nil || a.wcEntry.IsAutomationEnabled())
}

func (uc *handleWhatsAppMessageUseCase) routeInbound(agentCtx *agentContext, event workflow_domain.TriggerEvent, resumedByForm bool) InboundRoute {
	if event.EntryID == "" || event.WorkspaceID == "" {
		return InboundRoute{Consumer: conversation.InboundConsumerAgent}
	}
	return uc.inboundRouter.Route(event, agentCtx.automationRuns(), resumedByForm)
}

func (uc *handleWhatsAppMessageUseCase) deliverInbound(agentCtx *agentContext, route InboundRoute, event workflow_domain.TriggerEvent, history []*conversation.Message, startsTriggers bool) {
	starts := startsTriggers && route.Consumer.StartsRuns()
	if route.Consumer != conversation.InboundConsumerRun && !starts {
		return
	}
	go func() {
		routed := uc.inboundRouter.Deliver(route, event)
		if starts {
			uc.fireWorkflowTriggers(agentCtx, routed, history)
		}
	}()
}
