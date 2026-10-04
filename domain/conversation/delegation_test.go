package conversation

import (
	"errors"
	"testing"
)

func TestNewAutomationAcceptsOnlyAgentsAndWorkflows(t *testing.T) {
	cases := map[string]struct {
		kind, id string
		want     error
	}{
		"agent":        {"agent", "a1", nil},
		"workflow":     {"workflow", "w1", nil},
		"empty id":     {"agent", "  ", ErrAutomationInvalid},
		"unknown kind": {"person", "u1", ErrAutomationInvalid},
		"no kind":      {"", "a1", ErrAutomationInvalid},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := NewAutomation(tc.kind, tc.id)
			if !errors.Is(err, tc.want) {
				t.Fatalf("NewAutomation = %v, want %v", err, tc.want)
			}
			if err == nil && (string(got.Kind) != tc.kind || got.ID != tc.id) {
				t.Fatalf("automation = %+v", got)
			}
		})
	}
}

func TestADelegateGovernsOverTheChannel(t *testing.T) {
	delegate := Automation{Kind: AutomationAgent, ID: "picked"}
	profile := AutomationProfile{WorkflowID: "channel-wf", WorkflowEnabled: true, Delegate: &delegate}

	got, ok := profile.Governing()
	if !ok || got != delegate {
		t.Fatalf("Governing() = %+v %v, want the delegate", got, ok)
	}
}

func TestADelegateGovernsOnAChannelWithNoAutomation(t *testing.T) {
	delegate := Automation{Kind: AutomationWorkflow, ID: "wf-1"}
	got, ok := AutomationProfile{Delegate: &delegate}.Governing()
	if !ok || got != delegate {
		t.Fatalf("Governing() = %+v %v", got, ok)
	}
}

func TestAPausedConversationIsNotGovernedEvenWhenDelegated(t *testing.T) {
	off := false
	delegate := Automation{Kind: AutomationAgent, ID: "picked"}
	if _, ok := (AutomationProfile{Delegate: &delegate, AutomationEnabled: &off}).Governing(); ok {
		t.Fatal("a paused conversation must not be governed")
	}
}

func TestDelegatedToReplacesTheChannelAutomationButKeepsHandling(t *testing.T) {
	channelAgent, channelWorkflow := "channel-agent", "channel-wf"
	channel := ChannelAutomation{
		AgentID: &channelAgent, EnableAgentResponses: true,
		WorkflowID: &channelWorkflow, EnableWorkflow: true,
		EnableAnalysis: true, Disclosure: "IA",
	}

	agent := channel.DelegatedTo(Automation{Kind: AutomationAgent, ID: "picked"})
	if !agent.HasAgent() || *agent.AgentID != "picked" || !agent.EnableAgentResponses || agent.RunsWorkflows() || agent.WorkflowRef() != "" {
		t.Fatalf("delegated to an agent = %+v", agent)
	}
	if !agent.EnableAnalysis || agent.Disclosure != "IA" {
		t.Fatal("handling settings come from the channel")
	}

	workflow := channel.DelegatedTo(Automation{Kind: AutomationWorkflow, ID: "picked-wf"})
	if workflow.HasAgent() || workflow.EnableAgentResponses || !workflow.RunsWorkflows() || workflow.WorkflowRef() != "picked-wf" {
		t.Fatalf("delegated to a workflow = %+v", workflow)
	}
	if *channel.AgentID != "channel-agent" || *channel.WorkflowID != "channel-wf" {
		t.Fatal("the channel configuration must not be mutated")
	}
}
