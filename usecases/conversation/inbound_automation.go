package conversation_usecase

import (
	"context"
	"log"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type WorkflowEvaluator interface {
	Evaluate(event workflow.TriggerEvent)
}

type AgentReplier interface {
	Reply(ctx context.Context, req conversation.AIReplyRequest) (*conversation.Message, error)
}

type AnalysisRequester interface {
	ScheduleAnalysis(entryID string, entryType shared.EntryType)
}

type InboundCounter interface {
	CountInboundByEntry(entryID string, entryType shared.EntryType) (int64, error)
}

type InboundMessages interface {
	InboundCounter
	StoredInboundLookup
}

type InboundAutomationInput struct {
	WorkspaceID          string
	EntryID              string
	EntryType            shared.EntryType
	ContactRef           string
	ProviderMessageID    string
	ReceivedAt           time.Time
	LeadID               *string
	Text                 string
	Selection            *workflow.OptionSelection
	ConversationOverride *bool
	Config               conversation.ChannelAutomation
}

type InboundAutomation struct {
	workflows WorkflowEvaluator
	agents    AgentReplier
	analysis  AnalysisRequester
	inbound   InboundMessages
	router    *InboundRouter

	delegations conversation.DelegationRepository
}

func NewInboundAutomation(
	workflows WorkflowEvaluator,
	agents AgentReplier,
	analysis AnalysisRequester,
	inbound InboundMessages,
) *InboundAutomation {
	return &InboundAutomation{workflows: workflows, agents: agents, analysis: analysis, inbound: inbound, router: InboundRouterOf(workflows)}
}

func (a *InboundAutomation) WithDelegations(delegations conversation.DelegationRepository) *InboundAutomation {
	a.delegations = delegations
	return a
}

func (a *InboundAutomation) effective(ctx context.Context, in InboundAutomationInput) (InboundAutomationInput, bool) {
	config, err := EffectiveAutomation(ctx, a.delegations, in.EntryID, in.EntryType, in.Config)
	if err != nil {
		log.Printf("[inbound-automation] delegation of %s %s unreadable, no automation runs: %v", in.EntryType, in.EntryID, err)
		return in, false
	}
	in.Config = config
	return in, true
}

func (a *InboundAutomation) Dispatch(ctx context.Context, in InboundAutomationInput) error {
	a.Consume(ctx, in)
	a.ScheduleAnalysis(in)
	return nil
}

func (a *InboundAutomation) Consume(ctx context.Context, in InboundAutomationInput) {
	a.consume(ctx, in, true)
}

func (a *InboundAutomation) FireWorkflows(ctx context.Context, in InboundAutomationInput) {
	a.consume(ctx, in, false)
}

func (a *InboundAutomation) consume(ctx context.Context, in InboundAutomationInput, agentMayReply bool) {
	effective, ok := a.effective(ctx, in)
	if !ok {
		return
	}
	event := a.messageEvent(effective)
	route := a.router.Route(event, conversation.AutomationAllowed(effective.ConversationOverride), false)
	routed := a.router.Deliver(route, event)
	if route.Consumer.StartsRuns() {
		a.fireWorkflows(effective, routed)
	}
	if agentMayReply && route.Consumer.AgentReplies() {
		a.replyWithAgent(ctx, effective)
	}
}

func (a *InboundAutomation) messageEvent(in InboundAutomationInput) workflow.TriggerEvent {
	trigger := workflow.InboundTrigger{
		WorkspaceID:   in.WorkspaceID,
		EntryID:       in.EntryID,
		EntryType:     string(in.EntryType),
		Text:          in.Text,
		SentAt:        in.ReceivedAt,
		Selection:     in.Selection,
		ContactNumber: in.ContactRef,
		WorkflowID:    in.Config.WorkflowRef(),
	}
	trigger.StoredAs(StoredInbound(a.inbound, in.EntryType, in.EntryID, in.ProviderMessageID))
	return trigger.Event()
}

func (a *InboundAutomation) fireWorkflows(in InboundAutomationInput, event workflow.TriggerEvent) {
	if a.workflows == nil || !in.Config.RunsWorkflows() {
		return
	}
	if !conversation.AutomationAllowed(in.ConversationOverride) {
		log.Printf("[inbound-automation] automation disabled for %s %s, skipping workflow triggers", in.EntryType, in.EntryID)
		return
	}

	a.workflows.Evaluate(event)

	if IsFirstInbound(a.inbound, event) {
		a.workflows.Evaluate(event.AsFirstMessage())
	}
}

func (a *InboundAutomation) replyWithAgent(ctx context.Context, in InboundAutomationInput) {
	if a.agents == nil || !in.Config.HasAgent() {
		return
	}
	if _, err := a.agents.Reply(ctx, conversation.AIReplyRequest{
		WorkspaceID:           in.WorkspaceID,
		EntryID:               in.EntryID,
		EntryType:             in.EntryType,
		AgentID:               *in.Config.AgentID,
		AgentResponsesEnabled: in.Config.EnableAgentResponses,
		AutomationEnabled:     in.ConversationOverride,
		Text:                  in.Text,
		Disclosure:            in.Config.Disclosure,
		LeadID:                in.LeadID,
	}); err != nil {
		log.Printf("[inbound-automation] agent reply failed for %s %s: %v", in.EntryType, in.EntryID, err)
	}
}

func (a *InboundAutomation) ScheduleAnalysis(in InboundAutomationInput) {
	if a.analysis == nil || !in.Config.RunsAnalysis() {
		return
	}
	if !conversation.AutomationAllowed(in.ConversationOverride) {
		return
	}
	a.analysis.ScheduleAnalysis(in.EntryID, in.EntryType)
}
