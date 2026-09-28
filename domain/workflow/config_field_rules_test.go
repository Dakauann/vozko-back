package workflow

import (
	"errors"
	"testing"
)

func dealNodeCatalog() []NodeDefinition {
	return []NodeDefinition{{
		Type: NodeTypeActionManageOpportunity,
		ConfigSchema: []ConfigField{
			{Key: "action", Required: true},
			{Key: "stage_id", RequiredWhen: &FieldRule{Field: "action", Values: []string{"move"}}},
			{Key: "title", VisibleWhen: &FieldRule{Field: "action", Values: []string{"create"}}, Required: true},
		},
	}}
}

func dealGraph(config map[string]interface{}) *Graph {
	return &Graph{Nodes: []Node{{ID: "n1", Type: NodeTypeActionManageOpportunity, Config: config}}}
}

func TestRequiredWhenAppliesOnlyToTheMatchingChoice(t *testing.T) {
	err := ValidateNodeConfigs(dealGraph(map[string]interface{}{"action": "move"}), dealNodeCatalog())
	if !errors.Is(err, ErrNodeMissingRequiredField) {
		t.Fatalf("moving without a stage must be refused, got %v", err)
	}
	if err := ValidateNodeConfigs(dealGraph(map[string]interface{}{"action": "lose"}), dealNodeCatalog()); err != nil {
		t.Fatalf("the stage is not required for other actions, got %v", err)
	}
}

func TestHiddenFieldsAreNeverRequired(t *testing.T) {
	if err := ValidateNodeConfigs(dealGraph(map[string]interface{}{"action": "lose"}), dealNodeCatalog()); err != nil {
		t.Fatalf("a hidden field cannot block saving, got %v", err)
	}
	err := ValidateNodeConfigs(dealGraph(map[string]interface{}{"action": "create"}), dealNodeCatalog())
	if !errors.Is(err, ErrNodeMissingRequiredField) {
		t.Fatalf("a visible required field must be filled, got %v", err)
	}
}

func TestFieldRuleMatchesOnlyListedValues(t *testing.T) {
	rule := &FieldRule{Field: "action", Values: []string{"move", "create"}}
	cases := map[string]bool{"move": true, "create": true, "lose": false, "": false}
	for value, want := range cases {
		if got := rule.Matches(map[string]interface{}{"action": value}); got != want {
			t.Errorf("%q: got %v", value, got)
		}
	}
	var none *FieldRule
	if none.Matches(map[string]interface{}{"action": "move"}) {
		t.Error("a nil rule never matches")
	}
}
