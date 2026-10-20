package leadexit_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/buttonaction"
	"vozko/domain/conversation"
	"vozko/domain/leadexit"
	"vozko/domain/shared"
)

func delegatingFixture(t *testing.T, params buttonaction.Params) *fixture {
	t.Helper()
	f := newFixture(t, storedLead("lead-1", leadNumber))
	f.sentPromo("wamid.tmpl", leadNumber)
	f.promoButton("vz1:0", buttonaction.ButtonAction{ButtonIndex: 0, ButtonText: "Falar com a Elo", Kind: buttonaction.KindDelegate, Params: params})
	return f
}

func TestATapHandsTheConversationToTheChosenAgent(t *testing.T) {
	f := delegatingFixture(t, buttonaction.Params{AgentID: "agent-1"})

	result, err := f.step.Run(context.Background(), tapOn("wamid.in", "wamid.tmpl", "vz1:0", "Falar com a Elo"), &fakeReplier{})

	if err != nil || result.OptedOut {
		t.Fatalf("Run = %+v, %v", result, err)
	}
	want := conversation.Automation{Kind: conversation.AutomationAgent, ID: "agent-1"}
	if len(f.delegates.delegated) != 1 || f.delegates.delegated[0].Automation != want || f.delegates.delegated[0].EntryID != "entry-1" ||
		f.delegates.delegated[0].EntryType != shared.EntryTypeWhatsApp || f.delegates.delegated[0].WorkspaceID != workspaceA {
		t.Fatalf("delegated = %+v", f.delegates.delegated)
	}
	if run := f.runs.of("wamid.in"); run.Outcome != leadexit.OutcomeChanged || run.Kind != buttonaction.KindDelegate {
		t.Fatalf("run = %+v", run)
	}
}

func TestATapHandsTheConversationToTheChosenWorkflow(t *testing.T) {
	f := delegatingFixture(t, buttonaction.Params{WorkflowID: "wf-1"})

	if _, err := f.step.Run(context.Background(), tapOn("wamid.in", "wamid.tmpl", "vz1:0", "Falar com a Elo"), &fakeReplier{}); err != nil {
		t.Fatal(err)
	}

	if len(f.delegates.delegated) != 1 || f.delegates.delegated[0].Automation != (conversation.Automation{Kind: conversation.AutomationWorkflow, ID: "wf-1"}) {
		t.Fatalf("delegated = %+v", f.delegates.delegated)
	}
}

func TestAnAutomationThatCanNoLongerAnswerIsARefusalNotARetry(t *testing.T) {
	f := delegatingFixture(t, buttonaction.Params{AgentID: "agent-1"})
	f.delegates.err = conversation.ErrAutomationUnusable

	if _, err := f.step.Run(context.Background(), tapOn("wamid.in", "wamid.tmpl", "vz1:0", "Falar com a Elo"), &fakeReplier{}); err != nil {
		t.Fatalf("err = %v", err)
	}

	if run := f.runs.of("wamid.in"); run.Outcome != leadexit.OutcomeRefused || run.Reason != "automation_unusable" {
		t.Fatalf("run = %+v", run)
	}
}

func TestADelegationThatFailsToSaveIsRetried(t *testing.T) {
	f := delegatingFixture(t, buttonaction.Params{AgentID: "agent-1"})
	f.delegates.err = errors.New("db down")

	if _, err := f.step.Run(context.Background(), tapOn("wamid.in", "wamid.tmpl", "vz1:0", "Falar com a Elo"), &fakeReplier{}); err == nil {
		t.Fatal("a transient failure must surface so the inbound is retried")
	}

	if run := f.runs.of("wamid.in"); run.Outcome != leadexit.OutcomeFailed {
		t.Fatalf("run = %+v", run)
	}
}
