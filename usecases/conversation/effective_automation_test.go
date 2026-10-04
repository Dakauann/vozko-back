package conversation_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type stubDelegations struct {
	delegation *conversation.Delegation
	err        error
}

func (s stubDelegations) Find(context.Context, string, shared.EntryType) (*conversation.Delegation, error) {
	return s.delegation, s.err
}

func (s stubDelegations) FindMany(context.Context, []shared.EntryRef) (map[shared.EntryRef]conversation.Automation, error) {
	return nil, s.err
}

func (s stubDelegations) Save(context.Context, conversation.Delegation) error { return s.err }

func (s stubDelegations) Delete(context.Context, string, shared.EntryType) error { return s.err }

func delegatedTo(kind conversation.AutomationKind, id string) stubDelegations {
	return stubDelegations{delegation: &conversation.Delegation{Automation: conversation.Automation{Kind: kind, ID: id}}}
}

func TestADelegatedAgentAnswersOnAChannelWithNoAutomation(t *testing.T) {
	ev, rep, sch := &recordingEvaluator{}, &recordingReplier{}, &recordingScheduler{}
	auto := NewInboundAutomation(ev, rep, sch, stubInboundCounter{count: 1}).
		WithDelegations(delegatedTo(conversation.AutomationAgent, "picked"))
	in := automationInput()
	in.Config = conversation.ChannelAutomation{}

	auto.Dispatch(context.Background(), in)

	if len(rep.reqs) != 1 || rep.reqs[0].AgentID != "picked" || !rep.reqs[0].AgentResponsesEnabled {
		t.Fatalf("agent requests = %+v", rep.reqs)
	}
	if len(ev.events) != 0 {
		t.Fatalf("no workflow may run beside the delegated agent: %+v", ev.events)
	}
}

func TestADelegatedWorkflowReplacesTheChannelAgentAndWorkflow(t *testing.T) {
	ev, rep, sch := &recordingEvaluator{}, &recordingReplier{}, &recordingScheduler{}
	auto := NewInboundAutomation(ev, rep, sch, stubInboundCounter{count: 3}).
		WithDelegations(delegatedTo(conversation.AutomationWorkflow, "picked-wf"))

	auto.Dispatch(context.Background(), automationInput())

	if len(rep.reqs) != 0 {
		t.Fatalf("the channel agent must stay silent: %+v", rep.reqs)
	}
	if len(ev.events) != 1 || ev.events[0].Data["account_workflow_id"] != "picked-wf" {
		t.Fatalf("workflow events = %+v", ev.events)
	}
}

func TestAnUnreadableDelegationRunsNoAutomation(t *testing.T) {
	ev, rep, sch := &recordingEvaluator{}, &recordingReplier{}, &recordingScheduler{}
	auto := NewInboundAutomation(ev, rep, sch, stubInboundCounter{count: 1}).
		WithDelegations(stubDelegations{err: errors.New("db down")})

	auto.Dispatch(context.Background(), automationInput())

	if len(rep.reqs) != 0 || len(ev.events) != 0 {
		t.Fatalf("an unknown delegation must not let the channel automation answer: replies %d, events %d", len(rep.reqs), len(ev.events))
	}
}

func TestEffectiveAutomationWithoutADelegationIsTheChannel(t *testing.T) {
	channel := fullyEnabled()
	got, err := EffectiveAutomation(context.Background(), stubDelegations{}, "e-1", shared.EntryTypeTelegram, channel)
	if err != nil || *got.AgentID != "agent-1" || got.WorkflowRef() != "wf-1" {
		t.Fatalf("EffectiveAutomation = %+v %v", got, err)
	}
}
