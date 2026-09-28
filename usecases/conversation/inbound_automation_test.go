package conversation_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type recordingEvaluator struct{ events []workflow.TriggerEvent }

func (r *recordingEvaluator) Evaluate(e workflow.TriggerEvent) { r.events = append(r.events, e) }

type recordingReplier struct {
	reqs []conversation.AIReplyRequest
	err  error
}

func (r *recordingReplier) Reply(_ context.Context, req conversation.AIReplyRequest) (*conversation.Message, error) {
	r.reqs = append(r.reqs, req)
	return nil, r.err
}

type recordingScheduler struct{ scheduled []string }

func (r *recordingScheduler) ScheduleAnalysis(entryID string, _ shared.EntryType) {
	r.scheduled = append(r.scheduled, entryID)
}

type stubInboundCounter struct {
	count int64
	err   error
}

func (s stubInboundCounter) CountInboundByEntry(string, shared.EntryType) (int64, error) {
	return s.count, s.err
}

func newAutomationUnderTest(count int64) (*InboundAutomation, *recordingEvaluator, *recordingReplier, *recordingScheduler) {
	ev, rep, sch := &recordingEvaluator{}, &recordingReplier{}, &recordingScheduler{}
	return NewInboundAutomation(ev, rep, sch, stubInboundCounter{count: count}), ev, rep, sch
}

func fullyEnabled() conversation.ChannelAutomation {
	agent, wf := "agent-1", "wf-1"
	return conversation.ChannelAutomation{
		AgentID: &agent, WorkflowID: &wf,
		EnableAgentResponses: true, EnableWorkflow: true, EnableAnalysis: true,
	}
}

func automationInput() InboundAutomationInput {
	return InboundAutomationInput{
		WorkspaceID: "ws-1", EntryID: "e-1", EntryType: shared.EntryTypeTelegram,
		ContactRef: "psid-1", Text: "oi", Config: fullyEnabled(),
	}
}

func TestDispatchRunsEveryConfiguredAutomation(t *testing.T) {
	auto, ev, rep, sch := newAutomationUnderTest(1)
	auto.Dispatch(context.Background(), automationInput())

	if len(ev.events) != 2 || ev.events[0].TriggerType != workflow.TriggerMessageReceived || ev.events[1].TriggerType != workflow.TriggerFirstMessage {
		t.Fatalf("workflow events = %+v", ev.events)
	}
	data := ev.events[0].Data
	if data["message"] != "oi" || data["channel"] != "telegram" || data["workspace_id"] != "ws-1" || data["account_workflow_id"] != "wf-1" {
		t.Fatalf("trigger data = %+v", data)
	}
	if len(rep.reqs) != 1 || rep.reqs[0].AgentID != "agent-1" || !rep.reqs[0].AgentResponsesEnabled || rep.reqs[0].EntryType != shared.EntryTypeTelegram {
		t.Fatalf("agent requests = %+v", rep.reqs)
	}
	if len(sch.scheduled) != 1 || sch.scheduled[0] != "e-1" {
		t.Fatalf("analysis = %+v", sch.scheduled)
	}
}

func TestDispatchSkipsFirstMessageTriggerAfterTheFirst(t *testing.T) {
	auto, ev, _, _ := newAutomationUnderTest(3)
	auto.Dispatch(context.Background(), automationInput())
	if len(ev.events) != 1 {
		t.Fatalf("workflow events = %d, want 1", len(ev.events))
	}
}

func TestFirstMessageTriggerIsSkippedWhenCountFails(t *testing.T) {
	ev, rep, sch := &recordingEvaluator{}, &recordingReplier{}, &recordingScheduler{}
	auto := NewInboundAutomation(ev, rep, sch, stubInboundCounter{err: errors.New("db down")})
	auto.Dispatch(context.Background(), automationInput())
	if len(ev.events) != 1 {
		t.Fatalf("workflow events = %d, want only message_received", len(ev.events))
	}
}

func TestConversationOverrideSilencesWorkflowsAndAnalysisButAgentDecides(t *testing.T) {
	auto, ev, rep, sch := newAutomationUnderTest(1)
	off := false
	in := automationInput()
	in.ConversationOverride = &off
	auto.Dispatch(context.Background(), in)
	if len(ev.events) != 0 || len(sch.scheduled) != 0 {
		t.Fatalf("workflows=%d analysis=%d, want none", len(ev.events), len(sch.scheduled))
	}
	if len(rep.reqs) != 1 || rep.reqs[0].AutomationEnabled == nil || *rep.reqs[0].AutomationEnabled {
		t.Fatalf("agent must receive the override to decide: %+v", rep.reqs)
	}
}

func TestDisabledConfigRunsNothing(t *testing.T) {
	auto, ev, rep, sch := newAutomationUnderTest(1)
	in := automationInput()
	in.Config = conversation.ChannelAutomation{}
	auto.Dispatch(context.Background(), in)
	if len(ev.events)+len(rep.reqs)+len(sch.scheduled) != 0 {
		t.Fatal("nothing should run")
	}
}

func TestSelectionIsAppliedToTriggerData(t *testing.T) {
	auto, ev, _, _ := newAutomationUnderTest(2)
	in := automationInput()
	in.Selection = &workflow.OptionSelection{ID: "opt:1", Title: "Vendas"}
	auto.FireWorkflows(context.Background(), in)
	if ev.events[0].Data[workflow.DataKeySelectedOptionID] != "opt:1" {
		t.Fatalf("selection lost: %+v", ev.events[0].Data)
	}
}

func TestAgentReplyCarriesTheChannelDisclosure(t *testing.T) {
	auto, _, rep, _ := newAutomationUnderTest(1)
	in := automationInput()
	in.Config.Disclosure = "Assistente virtual"

	auto.ReplyWithAgent(context.Background(), in)

	if len(rep.reqs) != 1 || rep.reqs[0].Disclosure != "Assistente virtual" {
		t.Fatalf("agent requests = %+v", rep.reqs)
	}
}
