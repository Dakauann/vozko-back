package conversation

import "testing"

func TestChannelAutomationRunsAnalysis(t *testing.T) {
	cases := []struct {
		name string
		cfg  ChannelAutomation
		want bool
	}{
		{"nothing enabled", ChannelAutomation{}, false},
		{"analysis", ChannelAutomation{EnableAnalysis: true}, true},
		{"auto staging", ChannelAutomation{EnableAutoStaging: true}, true},
		{"auto memory", ChannelAutomation{EnableAutoMemory: true}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.RunsAnalysis(); got != tc.want {
				t.Fatalf("got %t want %t", got, tc.want)
			}
		})
	}
}

func TestChannelAutomationWorkflowAndAgentGates(t *testing.T) {
	agent := "agent-1"
	cases := []struct {
		name         string
		cfg          ChannelAutomation
		wantWorkflow bool
		wantAgent    bool
	}{
		{"off", ChannelAutomation{}, false, false},
		{"workflow on", ChannelAutomation{EnableWorkflow: true}, true, false},
		{"agent configured", ChannelAutomation{AgentID: &agent}, false, true},
		{"blank agent id is not configured", ChannelAutomation{AgentID: new(string)}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.RunsWorkflows(); got != tc.wantWorkflow {
				t.Fatalf("workflows = %t", got)
			}
			if got := tc.cfg.HasAgent(); got != tc.wantAgent {
				t.Fatalf("agent = %t", got)
			}
		})
	}
}

func TestConversationOverrideSuppressesAutomation(t *testing.T) {
	off, on := false, true
	if AutomationAllowed(&off) {
		t.Fatal("explicit off must suppress")
	}
	if !AutomationAllowed(&on) || !AutomationAllowed(nil) {
		t.Fatal("on or unset must allow")
	}
}
