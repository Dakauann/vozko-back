package workflow

import "testing"

func interactiveOptionResolver(n Node) ([]HandleDefinition, bool) {
	if !n.Type.IsInteractivePrompt() {
		return nil, false
	}
	return InteractiveOptionHandles(n.Config), true
}

var edgeHandleCatalog = []NodeDefinition{
	{Type: NodeTypeActionAssignLabel, Outputs: []HandleDefinition{{ID: "sucesso"}, {ID: "erro", Optional: true}}},
	{Type: NodeTypeActionSendText},
}

func badHandleEdges(report LintReport) map[string]LintSeverity {
	out := make(map[string]LintSeverity)
	for _, issue := range report.Issues {
		if issue.Code == LintBadHandleLabel {
			out[issue.EdgeRef] = issue.Severity
		}
	}
	return out
}

func TestAnEdgeFromAnOutputTheNodeNoLongerHasBlocksPublishing(t *testing.T) {
	g := &Graph{
		Nodes: []Node{
			{ID: "n27", Type: NodeTypeActionSendInteractive, Config: map[string]interface{}{"interactive_type": "list", "sections": realCapitalizedListSections}},
			{ID: "lbl", Type: NodeTypeActionAssignLabel, Config: map[string]interface{}{}},
			{ID: "txt", Type: NodeTypeActionSendText, Config: map[string]interface{}{}},
			{ID: "n44", Type: NodeTypeWaitDuration, Config: map[string]interface{}{}},
		},
		Edges: []Edge{
			{Source: "n27", Target: "n44", Label: "1"},
			{Source: "n27", Target: "lbl", Label: "2"},
			{Source: "n27", Target: "txt", Label: "3"},
			{Source: "n27", Target: "txt", Label: "default"},
			{Source: "lbl", Target: "txt", Label: "sucesso"},
			{Source: "lbl", Target: "n44", Label: "talvez"},
			{Source: "lbl", Target: "n44", Label: ""},
			{Source: "txt", Target: "n44", Label: ""},
			{Source: "txt", Target: "n44", Label: "default"},
			{Source: "txt", Target: "lbl", Label: "proximo"},
		},
	}

	got := badHandleEdges(LintGraph(g, WorkflowTypeMessages, edgeHandleCatalog, nil, interactiveOptionResolver))

	want := map[string]LintSeverity{
		"n27->txt": SeverityBlocking,
		"lbl->n44": SeverityBlocking,
		"txt->lbl": SeverityBlocking,
	}
	if len(got) != len(want) {
		t.Fatalf("bad handle edges = %v, want %v", got, want)
	}
	for ref, severity := range want {
		if got[ref] != severity {
			t.Fatalf("bad handle edges = %v, want %v", got, want)
		}
	}
}

func TestTheRealListWithCapitalizedKeysKeepsBothEdges(t *testing.T) {
	g := &Graph{
		Nodes: []Node{
			{ID: "n27", Type: NodeTypeActionSendInteractive, Config: map[string]interface{}{"interactive_type": "list", "sections": realCapitalizedListSections}},
			{ID: "n44", Type: NodeTypeWaitDuration, Config: map[string]interface{}{}},
			{ID: "n28", Type: NodeTypeWaitDuration, Config: map[string]interface{}{}},
		},
		Edges: []Edge{{Source: "n27", Target: "n44", Label: "1"}, {Source: "n27", Target: "n28", Label: "2"}},
	}

	if got := badHandleEdges(LintGraph(g, WorkflowTypeMessages, nil, nil, interactiveOptionResolver)); len(got) != 0 {
		t.Fatalf("bad handle edges = %v, want none", got)
	}
}

func TestEdgeOutputRuleMatchesTheEditor(t *testing.T) {
	branches := []HandleDefinition{{ID: "sucesso"}, {ID: "erro"}}
	for _, c := range []struct {
		name    string
		outputs []HandleDefinition
		label   string
		want    bool
	}{
		{"an unlabeled edge always has an output", branches, "", true},
		{"a declared output", branches, "erro", true},
		{"a label the node does not declare", branches, "default", false},
		{"a single unnamed output takes an unlabeled edge", nil, "", true},
		{"a single unnamed output takes default", nil, "default", true},
		{"a single unnamed output has no named outputs", nil, "sucesso", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := EdgeHasOutput(c.outputs, c.label); got != c.want {
				t.Fatalf("EdgeHasOutput(%q) = %v, want %v", c.label, got, c.want)
			}
		})
	}
}
