package conversation_usecase

import (
	"context"
	"log"

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

type InboundAutomationInput struct {
	WorkspaceID          string
	EntryID              string
	EntryType            shared.EntryType
	ContactRef           string
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
	inbound   InboundCounter
}

func NewInboundAutomation(
	workflows WorkflowEvaluator,
	agents AgentReplier,
	analysis AnalysisRequester,
	inbound InboundCounter,
) *InboundAutomation {
	return &InboundAutomation{workflows: workflows, agents: agents, analysis: analysis, inbound: inbound}
}

func (a *InboundAutomation) Dispatch(ctx context.Context, in InboundAutomationInput) {
	a.FireWorkflows(ctx, in)
	a.ReplyWithAgent(ctx, in)
	a.ScheduleAnalysis(in)
}

func (a *InboundAutomation) FireWorkflows(ctx context.Context, in InboundAutomationInput) {
	if a.workflows == nil || !in.Config.RunsWorkflows() {
		return
	}
	if !conversation.AutomationAllowed(in.ConversationOverride) {
		log.Printf("[inbound-automation] automation disabled for %s %s, skipping workflow triggers", in.EntryType, in.EntryID)
		return
	}

	data := map[string]interface{}{
		"message":      in.Text,
		"channel":      string(in.EntryType),
		"workspace_id": in.WorkspaceID,
	}
	if ref := in.Config.WorkflowRef(); ref != "" {
		data["account_workflow_id"] = ref
	}
	workflow.ApplySelection(data, in.Selection)
	workflow.ApplyContactNumber(data, in.ContactRef)

	a.workflows.Evaluate(workflow.TriggerEvent{
		WorkspaceID: in.WorkspaceID,
		EntryID:     in.EntryID,
		EntryType:   string(in.EntryType),
		TriggerType: workflow.TriggerMessageReceived,
		Data:        data,
	})

	if a.isFirstInbound(in) {
		a.workflows.Evaluate(workflow.TriggerEvent{
			WorkspaceID: in.WorkspaceID,
			EntryID:     in.EntryID,
			EntryType:   string(in.EntryType),
			TriggerType: workflow.TriggerFirstMessage,
			Data:        data,
		})
	}
}

func (a *InboundAutomation) ReplyWithAgent(ctx context.Context, in InboundAutomationInput) {
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

func (a *InboundAutomation) isFirstInbound(in InboundAutomationInput) bool {
	if a.inbound == nil {
		return false
	}
	count, err := a.inbound.CountInboundByEntry(in.EntryID, in.EntryType)
	if err != nil {
		log.Printf("[inbound-automation] could not count inbound messages for %s %s: %v", in.EntryType, in.EntryID, err)
		return false
	}
	return count == 1
}
