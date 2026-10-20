package conversation_usecase

import (
	"log/slog"

	"vozko/domain/conversation"
	"vozko/domain/workflow"
)

type InboundRouter struct {
	waiters workflow.ReplyWaiters
}

func NewInboundRouter(waiters workflow.ReplyWaiters) *InboundRouter {
	return &InboundRouter{waiters: waiters}
}

func InboundRouterOf(evaluator any) *InboundRouter {
	waiters, ok := evaluator.(workflow.ReplyWaiters)
	if !ok || waiters == nil {
		return nil
	}
	return NewInboundRouter(waiters)
}

type InboundRoute struct {
	Consumer conversation.InboundConsumer
	waiting  *workflow.WorkflowRun
}

func (r *InboundRouter) Route(event workflow.TriggerEvent, automationAllowed, resumedByForm bool) InboundRoute {
	facts := conversation.InboundFacts{AutomationAllowed: automationAllowed, ResumedByForm: resumedByForm}
	var waiting *workflow.WorkflowRun
	if automationAllowed && !resumedByForm && r != nil && r.waiters != nil && event.EntryID != "" {
		run, err := r.waiters.WaitingForReply(event)
		facts.WaitingRunUnreadable = err != nil
		facts.WaitingRun = err == nil && run != nil
		if facts.WaitingRun {
			waiting = run
		}
	}
	route := InboundRoute{Consumer: conversation.DecideInboundConsumer(facts), waiting: waiting}
	if facts.WaitingRunUnreadable {
		slog.Warn("conversation.inbound.no_automated_reply",
			slog.String("workspace_id", event.WorkspaceID), slog.String("entry_id", event.EntryID),
			slog.String("entry_type", event.EntryType), slog.String("reason", "waiting_run_unreadable"))
	}
	return route
}

func (r *InboundRouter) Deliver(route InboundRoute, event workflow.TriggerEvent) workflow.TriggerEvent {
	event.Reply = &workflow.ReplyRouting{}
	if route.Consumer != conversation.InboundConsumerRun || route.waiting == nil || r == nil || r.waiters == nil {
		return event
	}
	if r.waiters.WakeForReply(route.waiting, event) {
		event.Reply = &workflow.ReplyRouting{RunID: route.waiting.ID, WorkflowID: route.waiting.WorkflowID}
	}
	return event
}
