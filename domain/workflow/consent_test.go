package workflow

import (
	"errors"
	"testing"
)

func TestConsentNodesAreAnActionAndACondition(t *testing.T) {
	if NodeTypeActionConsent != "action_consent" || !NodeTypeActionConsent.Valid() || NodeTypeActionConsent.Category() != NodeCategoryAction {
		t.Fatalf("%q must be a valid action", NodeTypeActionConsent)
	}
	if NodeTypeConditionConsent != "condition_consent" || !NodeTypeConditionConsent.Valid() || NodeTypeConditionConsent.Category() != NodeCategoryCondition {
		t.Fatalf("%q must be a valid condition", NodeTypeConditionConsent)
	}
}

func TestTheConsentConditionBranchesOnTheLeadsState(t *testing.T) {
	cases := []struct {
		optedOut, granted bool
		want              string
	}{
		{false, true, ConsentBranchGranted},
		{true, false, ConsentBranchWithdrawn},
		{true, true, ConsentBranchWithdrawn},
		{false, false, ConsentBranchNone},
	}
	for _, tc := range cases {
		if got := ConsentBranch(tc.optedOut, tc.granted); got != tc.want {
			t.Errorf("ConsentBranch(optedOut=%v, granted=%v) = %q, want %q", tc.optedOut, tc.granted, got, tc.want)
		}
	}
}

func TestATemplateButtonTapIsASelectionFlowsCanBranchOn(t *testing.T) {
	data := map[string]interface{}{}
	ApplySelection(data, TemplateButtonSelection("SAIR", 1, true))
	if data[DataKeySelectedOptionID] != "template_button:1" || data[DataKeySelectedOptionTitle] != "SAIR" || data[DataKeySelectedOptionKind] != OptionKindTemplateButton {
		t.Fatalf("data = %+v", data)
	}
	if index, ok := TemplateButtonIndex("template_button:1"); !ok || index != 1 {
		t.Fatalf("index = %d, %v", index, ok)
	}

	unmatched := map[string]interface{}{}
	ApplySelection(unmatched, TemplateButtonSelection("Talvez", 0, false))
	if unmatched[DataKeySelectedOptionID] != OptionKindTemplateButton || unmatched[DataKeySelectedOptionTitle] != "Talvez" {
		t.Fatalf("an unmatched tap keeps its text and no index: %+v", unmatched)
	}
	for _, id := range []string{"template_button", "template_button:", "template_button:x", "button:1", "template_button:-1"} {
		if _, ok := TemplateButtonIndex(id); ok {
			t.Errorf("%q has no button index", id)
		}
	}
}

func TestTheTriggeringMessageTravelsWithTheRun(t *testing.T) {
	data := map[string]interface{}{}
	ApplyTriggerMessage(data, "  ")
	if len(data) != 0 {
		t.Fatalf("a blank id is not a message: %+v", data)
	}
	ApplyTriggerMessage(data, " msg-1 ")
	state := NewRunState()
	for k, v := range data {
		state.Set(k, v)
	}
	if got := TriggerMessageID(&state); got != "msg-1" {
		t.Fatalf("TriggerMessageID = %q", got)
	}
}

func consentGraph(mode string, nodes []Node, edges []Edge) *Graph {
	all := append([]Node{
		{ID: "t", Type: NodeTypeTriggerMessageReceived},
		{ID: "c", Type: NodeTypeActionConsent, Config: map[string]interface{}{ConsentModeKey: mode}},
		{ID: "end", Type: NodeTypeEnd},
	}, nodes...)
	return &Graph{Nodes: all, Edges: append(edges, Edge{Source: "c", Target: "end", Label: "sucesso"})}
}

func selectionCondition(variable string) Node {
	return Node{ID: "branch", Type: NodeTypeConditionBranch, Config: map[string]interface{}{"variable": variable, "operator": "eq", "value": "template_button:0"}}
}

func TestAConsentGrantMustFollowAReplyOnEveryPath(t *testing.T) {
	wait := Node{ID: "wait", Type: NodeTypeWaitForReply}
	ask := Node{ID: "ask", Type: NodeTypeActionSendInteractive}
	cases := []struct {
		name  string
		nodes []Node
		edges []Edge
		ok    bool
	}{
		{"straight from the trigger", nil, []Edge{{Source: "t", Target: "c"}}, false},
		{"after the lead replied", []Node{wait}, []Edge{{Source: "t", Target: "wait"}, {Source: "wait", Target: "c", Label: "replied"}}, true},
		{"after the wait timed out", []Node{wait}, []Edge{{Source: "t", Target: "wait"}, {Source: "wait", Target: "c", Label: "timeout"}}, false},
		{"after a reply on one path but not another", []Node{wait}, []Edge{
			{Source: "t", Target: "wait"}, {Source: "wait", Target: "c", Label: "replied"}, {Source: "t", Target: "c"},
		}, false},
		{"after an interactive option", []Node{ask}, []Edge{{Source: "t", Target: "ask"}, {Source: "ask", Target: "c", Label: "opt_sim"}}, true},
		{"after an unmatched interactive reply", []Node{ask}, []Edge{{Source: "t", Target: "ask"}, {Source: "ask", Target: "c", Label: InteractiveHandleNoMatch}}, true},
		{"after an interactive prompt nobody answered", []Node{ask}, []Edge{{Source: "t", Target: "ask"}, {Source: "ask", Target: "c", Label: InteractiveHandleNoReply}}, false},
		{"after an interactive prompt that failed to send", []Node{ask}, []Edge{{Source: "t", Target: "ask"}, {Source: "ask", Target: "c", Label: InteractiveHandleSendFailed}}, false},
		{"after a template button branch", []Node{selectionCondition("{{selected_option_id}}")}, []Edge{{Source: "t", Target: "branch"}, {Source: "branch", Target: "c", Label: "true"}}, true},
		{"after a template button text branch", []Node{selectionCondition("{{selected_option_title}}")}, []Edge{{Source: "t", Target: "branch"}, {Source: "branch", Target: "c", Label: "true"}}, true},
		{"on the false side of a template button branch", []Node{selectionCondition("{{selected_option_id}}")}, []Edge{{Source: "t", Target: "branch"}, {Source: "branch", Target: "c", Label: "false"}}, false},
		{"after a branch on the message text", []Node{selectionCondition("{{message}}")}, []Edge{{Source: "t", Target: "branch"}, {Source: "branch", Target: "c", Label: "true"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateConsentGrantsFollowAReply(consentGraph(ConsentModeGrant, tc.nodes, tc.edges))
			if tc.ok && err != nil {
				t.Fatalf("err = %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrConsentGrantNeedsReply) {
				t.Fatalf("err = %v, want %v", err, ErrConsentGrantNeedsReply)
			}
		})
	}
}

func TestAConsentWithdrawalIsAllowedAnywhere(t *testing.T) {
	g := consentGraph(ConsentModeWithdraw, nil, []Edge{{Source: "t", Target: "c"}})
	if err := RunPureGraphRules(g); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestActivationAndTheLinterRefuseAGrantWithoutAReply(t *testing.T) {
	g := consentGraph(ConsentModeGrant, nil, []Edge{{Source: "t", Target: "c"}})
	if err := RunPureGraphRules(g); !errors.Is(err, ErrConsentGrantNeedsReply) {
		t.Fatalf("activation err = %v", err)
	}
	report := LintGraph(g, WorkflowTypeMessages, nil, nil, nil)
	found := false
	for _, issue := range report.Issues {
		if issue.Code == LintConsentGrantNeedsReply && issue.NodeID == "c" && issue.Severity == SeverityBlocking {
			found = true
		}
	}
	if !found {
		t.Fatalf("lint issues = %+v", report.Issues)
	}
}

func TestAConsentNodeNeedsAKnownMode(t *testing.T) {
	for _, mode := range []string{"", "talvez", "grant"} {
		g := consentGraph(mode, nil, []Edge{{Source: "t", Target: "c"}})
		if err := ValidateConsentModes(g); !errors.Is(err, ErrNodeMissingRequiredField) {
			t.Errorf("mode %q: err = %v", mode, err)
		}
	}
	for _, mode := range []string{ConsentModeGrant, ConsentModeWithdraw} {
		if err := ValidateConsentModes(consentGraph(mode, nil, nil)); err != nil {
			t.Errorf("mode %q: err = %v", mode, err)
		}
	}
}
