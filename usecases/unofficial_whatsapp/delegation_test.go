package unofficial_whatsapp

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/campaign"
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

func delegated(kind conversation.AutomationKind, id string) stubDelegations {
	return stubDelegations{delegation: &conversation.Delegation{Automation: conversation.Automation{Kind: kind, ID: id}}}
}

func TestADelegatedAgentAnswersAManualCampaign(t *testing.T) {
	h := newAutoHarness(t, campaignAuto(campaign.Automation{}))
	h.uc.delegations = delegated(conversation.AutomationAgent, "picked")
	h.deliverInbound(t)

	if reqs := h.ai.all(); len(reqs) != 1 || reqs[0].AgentID != "picked" || !reqs[0].AgentResponsesEnabled {
		t.Fatalf("AI replies = %+v, want one from the delegated agent", reqs)
	}
	if events := h.workflows.all(); len(events) != 0 {
		t.Fatalf("fired %d workflow triggers beside the delegated agent", len(events))
	}
}

func TestADelegatedWorkflowReplacesTheInstanceAutomation(t *testing.T) {
	h := newAutoHarness(t, nil)
	h.uc.delegations = delegated(conversation.AutomationWorkflow, "picked-wf")
	h.deliverInbound(t)

	events := h.workflows.all()
	if len(events) != 1 || events[0].Data["account_workflow_id"] != "picked-wf" {
		t.Fatalf("workflow events = %+v, want only the delegated workflow", events)
	}
	if reqs := h.ai.all(); len(reqs) != 0 {
		t.Fatalf("the instance agent answered beside the delegated workflow: %+v", reqs)
	}
}

func TestAnUnreadableDelegationSilencesTheInstance(t *testing.T) {
	h := newAutoHarness(t, nil)
	h.uc.delegations = stubDelegations{err: errors.New("db down")}
	h.deliverInbound(t)

	if len(h.ai.all()) != 0 || len(h.workflows.all()) != 0 {
		t.Fatal("an unknown delegation must not let the instance automation answer")
	}
}
