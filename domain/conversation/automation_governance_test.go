package conversation

import "testing"

func boolPtr(v bool) *bool { return &v }

func TestAutomationProfileGoverning(t *testing.T) {
	cases := []struct {
		name     string
		profile  AutomationProfile
		wantOK   bool
		wantKind AutomationKind
		wantID   string
	}{
		{
			// A brand-new conversation has no override yet; nil means "inherit", which is ON.
			name:     "agent with automation never touched governs",
			profile:  AutomationProfile{AgentID: "agent-1", AgentResponsesEnabled: true},
			wantOK:   true,
			wantKind: AutomationAgent,
			wantID:   "agent-1",
		},
		{
			name:     "agent with automation explicitly on governs",
			profile:  AutomationProfile{AgentID: "agent-1", AgentResponsesEnabled: true, AutomationEnabled: boolPtr(true)},
			wantOK:   true,
			wantKind: AutomationAgent,
			wantID:   "agent-1",
		},
		{
			// The operator paused the AI for this one conversation: nobody automated answers it.
			name:    "automation paused for the conversation means nothing governs",
			profile: AutomationProfile{AgentID: "agent-1", AgentResponsesEnabled: true, AutomationEnabled: boolPtr(false)},
			wantOK:  false,
		},
		{
			// An agent picked on the channel but with responses switched off never replies.
			name:    "agent configured but responses disabled does not govern",
			profile: AutomationProfile{AgentID: "agent-1"},
			wantOK:  false,
		},
		{
			name:    "responses enabled with no agent does not govern",
			profile: AutomationProfile{AgentResponsesEnabled: true},
			wantOK:  false,
		},
		{
			// Same precedence as the inbox ai_handler chip: the workflow drives the conversation.
			name: "workflow wins over agent",
			profile: AutomationProfile{
				AgentID: "agent-1", AgentResponsesEnabled: true,
				WorkflowID: "wf-1", WorkflowEnabled: true,
			},
			wantOK:   true,
			wantKind: AutomationWorkflow,
			wantID:   "wf-1",
		},
		{
			name:    "workflow paused for the conversation does not govern",
			profile: AutomationProfile{WorkflowID: "wf-1", WorkflowEnabled: true, AutomationEnabled: boolPtr(false)},
			wantOK:  false,
		},
		{
			name:     "disabled workflow falls back to the agent",
			profile:  AutomationProfile{AgentID: "agent-1", AgentResponsesEnabled: true, WorkflowID: "wf-1"},
			wantOK:   true,
			wantKind: AutomationAgent,
			wantID:   "agent-1",
		},
		{
			name:    "whitespace ids are not ids",
			profile: AutomationProfile{AgentID: "  ", AgentResponsesEnabled: true, WorkflowID: " ", WorkflowEnabled: true},
			wantOK:  false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.profile.Governing()
			if ok != tc.wantOK {
				t.Fatalf("governed = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if got.Kind != tc.wantKind || got.ID != tc.wantID {
				t.Fatalf("governor = %+v, want kind %q id %q", got, tc.wantKind, tc.wantID)
			}
		})
	}
}

func TestAutomationProfileConfiguredIgnoresThePause(t *testing.T) {
	// The inbox chip still names the paused AI ("IA pausada"), so the configured
	// automation must survive a pause even though Governing does not.
	p := AutomationProfile{AgentID: "agent-1", AgentResponsesEnabled: true, AutomationEnabled: boolPtr(false)}
	got, ok := p.Configured()
	if !ok || got.Kind != AutomationAgent || got.ID != "agent-1" {
		t.Fatalf("configured governor = %+v ok=%v, want the agent", got, ok)
	}
}

func TestAutomationActorID(t *testing.T) {
	// An agent and a workflow are different actors: history, events and the
	// inbox tell them apart by prefix, so a workflow must never become ai:.
	cases := map[Automation]string{
		{Kind: AutomationAgent, ID: "agent-1"}: "ai:agent-1",
		{Kind: AutomationWorkflow, ID: "wf-1"}: "workflow:wf-1",
	}
	for g, want := range cases {
		if got := g.ActorID(); got != want {
			t.Errorf("%s ActorID = %q, want %q", g.Kind, got, want)
		}
	}
}

func TestParseResponsibleKindAcceptsOnlyAutomationKinds(t *testing.T) {
	// The inbox filter narrows by who holds a conversation. "human" is already
	// served by responsible_user_id, and anything else is ignored, not trusted.
	cases := map[string]string{
		"ai":        "ai",
		" workflow": "workflow",
		"human":     "",
		"system":    "",
		"":          "",
		"AI":        "",
	}
	for in, want := range cases {
		if got := ParseResponsibleKind(in); string(got) != want {
			t.Errorf("ParseResponsibleKind(%q) = %q, want %q", in, got, want)
		}
	}
}
