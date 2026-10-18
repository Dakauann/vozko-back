package workflow

import (
	"errors"
	"testing"
)

func selectionBranch(variable, operator, value string) Node {
	return Node{ID: "branch", Type: NodeTypeConditionBranch, Config: map[string]interface{}{"variable": variable, "operator": operator, "value": value}}
}

func TestOnlyAnEqualityOnTheTappedOptionCountsAsAReply(t *testing.T) {
	cases := []struct {
		name   string
		branch Node
		reply  bool
	}{
		{"the button id", selectionBranch("{{selected_option_id}}", "eq", "template_button:1"), true},
		{"the button text ignoring case", selectionBranch("{{selected_option_title}}", "eq_ci", "Quero receber"), true},
		{"an interactive option id", selectionBranch("{{selected_option_id}}", "eq", "opt_sim"), true},
		{"the bare key", selectionBranch("selected_option_id", "eq", "template_button:0"), true},
		{"not equal to a button", selectionBranch("{{selected_option_id}}", "neq", "template_button:1"), false},
		{"not equal ignoring case", selectionBranch("{{selected_option_title}}", "neq_ci", "SAIR"), false},
		{"contains", selectionBranch("{{selected_option_title}}", "contains", "a"), false},
		{"starts with", selectionBranch("{{selected_option_id}}", "starts_with", "template_button"), false},
		{"an empty value", selectionBranch("{{selected_option_id}}", "eq", "  "), false},
		{"a variable as the value", selectionBranch("{{selected_option_id}}", "eq", "{{selected_option_id}}"), false},
		{"a tap that matched no button", selectionBranch("{{selected_option_id}}", "eq", OptionKindTemplateButton), false},
		{"the key inside a longer text", selectionBranch("{{message}} {{selected_option_id}}", "eq", "template_button:1"), false},
		{"no operator", selectionBranch("{{selected_option_id}}", "", "template_button:1"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateConsentGrantsFollowAReply(consentGraph(ConsentModeGrant, []Node{tc.branch},
				[]Edge{{Source: "t", Target: "branch"}, {Source: "branch", Target: "c", Label: "true"}}))
			if tc.reply && err != nil {
				t.Fatalf("err = %v", err)
			}
			if !tc.reply && !errors.Is(err, ErrConsentGrantNeedsReply) {
				t.Fatalf("err = %v, want %v", err, ErrConsentGrantNeedsReply)
			}
		})
	}
}
