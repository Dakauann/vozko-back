package campaign

import "testing"

func ptr(b bool) *bool { return &b }

// The table is the rule. It is written out in full rather than generated,
// because the case that caused the incident — nothing configured, so nothing
// answers — reads as an unremarkable row until you know that the unofficial
// channel used to fall through it into the instance's workflow.
func TestAutomationMode(t *testing.T) {
	agent := Automation{AgentID: "a1", EnableAgentResponses: true}
	flow := Automation{WorkflowID: "w1", EnableWorkflow: true}
	both := Automation{AgentID: "a1", EnableAgentResponses: true, WorkflowID: "w1", EnableWorkflow: true}

	cases := []struct {
		name     string
		cfg      Automation
		override *bool
		want     AutomationMode
	}{
		{"nothing configured", Automation{}, nil, AutomationNone},
		{"agent only", agent, nil, AutomationAgent},
		{"workflow only", flow, nil, AutomationWorkflow},
		{"both configured: workflow wins", both, nil, AutomationWorkflow},

		// A flag with no id behind it is a half-filled form, not configuration.
		{"agent flag, no agent id", Automation{EnableAgentResponses: true}, nil, AutomationNone},
		{"workflow flag, no workflow id", Automation{EnableWorkflow: true}, nil, AutomationNone},
		{"agent id, flag off", Automation{AgentID: "a1"}, nil, AutomationNone},
		{"workflow id, flag off", Automation{WorkflowID: "w1"}, nil, AutomationNone},
		{"blank id is not an id", Automation{AgentID: "   ", EnableAgentResponses: true}, nil, AutomationNone},

		// A workflow campaign with the agent flag also on still runs only the
		// workflow — otherwise the customer gets two answers to one message.
		{"workflow wins even when agent id present", both, ptr(true), AutomationWorkflow},

		// The operator override, which they set while looking at the conversation.
		{"override off silences an agent", agent, ptr(false), AutomationNone},
		{"override off silences a workflow", flow, ptr(false), AutomationNone},
		{"override off silences both", both, ptr(false), AutomationNone},
		{"override on revives a disabled agent", Automation{AgentID: "a1"}, ptr(true), AutomationAgent},
		{"override on cannot invent an agent", Automation{}, ptr(true), AutomationNone},
		{"override on cannot invent a workflow", Automation{WorkflowID: "w1"}, ptr(true), AutomationNone},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.Mode(tc.override); got != tc.want {
				t.Fatalf("Mode = %q, want %q", got, tc.want)
			}
			if want := tc.want == AutomationWorkflow; tc.cfg.RunsWorkflow(tc.override) != want {
				t.Fatalf("RunsWorkflow = %v, want %v", !want, want)
			}
			if want := tc.want == AutomationAgent; tc.cfg.RunsAgent(tc.override) != want {
				t.Fatalf("RunsAgent = %v, want %v", !want, want)
			}
		})
	}
}

// This is the rule the official channel already enforces at
// handle_whatsapp_message_usecase.go:1986-2010. Pinning it here means the two
// channels cannot drift apart again without a red test.
func TestAutomationMatchesOfficialCampaignRule(t *testing.T) {
	official := func(enableAgent bool, agentID string, enableWf bool, wfID string, override *bool) AutomationMode {
		responsesEnabled := enableAgent
		if override != nil {
			responsesEnabled = *override
		}
		hasWorkflow := enableWf && wfID != ""
		hasAgent := responsesEnabled && agentID != ""

		if override != nil && !*override {
			return AutomationNone // fireWorkflowTriggers:199-203 blocks the workflow too
		}
		switch {
		case hasWorkflow:
			return AutomationWorkflow
		case hasAgent:
			return AutomationAgent
		default:
			return AutomationNone
		}
	}

	overrides := []*bool{nil, ptr(true), ptr(false)}
	ids := []string{"", "x"}
	flags := []bool{false, true}

	for _, ea := range flags {
		for _, aid := range ids {
			for _, ew := range flags {
				for _, wid := range ids {
					for _, ov := range overrides {
						cfg := Automation{AgentID: aid, WorkflowID: wid, EnableAgentResponses: ea, EnableWorkflow: ew}
						want := official(ea, aid, ew, wid, ov)
						if got := cfg.Mode(ov); got != want {
							t.Fatalf("cfg=%+v override=%v: Mode = %q, official rule = %q", cfg, ov, got, want)
						}
					}
				}
			}
		}
	}
}
