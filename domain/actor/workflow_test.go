package actor

import "testing"

func TestWorkflowIsItsOwnKind(t *testing.T) {
	// A workflow is not an AI agent: it is identified, named and reported as a
	// workflow, never folded into ai:.
	cases := []struct {
		in   string
		want Kind
	}{
		{"workflow:wf-1", KindWorkflow},
		{"  workflow:wf-1 ", KindWorkflow},
		{"ai:agent-1", KindAI},
		{"wf-1", KindHuman},
	}
	for _, tc := range cases {
		if got := KindOf(tc.in); got != tc.want {
			t.Errorf("KindOf(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if !KindWorkflow.Valid() {
		t.Error("KindWorkflow must be valid")
	}
}

func TestFormatAndParseWorkflow(t *testing.T) {
	if got := FormatWorkflow(" wf-1 "); got != "workflow:wf-1" {
		t.Errorf("FormatWorkflow = %q", got)
	}
	if got := FormatWorkflow("workflow:wf-1"); got != "workflow:wf-1" {
		t.Errorf("FormatWorkflow must not double the prefix, got %q", got)
	}
	if got := FormatWorkflow(""); got != "" {
		t.Errorf("FormatWorkflow(\"\") = %q", got)
	}
	if got := ParseWorkflow("workflow:wf-1"); got != "wf-1" {
		t.Errorf("ParseWorkflow = %q", got)
	}
	if got := ParseWorkflow("ai:agent-1"); got != "" {
		t.Errorf("ParseWorkflow of an agent = %q, want empty", got)
	}
}

func TestNormalizeWorkflow(t *testing.T) {
	if k, id := Normalize(KindWorkflow, "wf-1"); k != KindWorkflow || id != "workflow:wf-1" {
		t.Fatalf("Normalize(workflow, bare) = %q %q", k, id)
	}
	if k, id := Normalize("", "workflow:wf-1"); k != KindWorkflow || id != "workflow:wf-1" {
		t.Fatalf("Normalize(inferred) = %q %q", k, id)
	}
}

func TestIsAutomation(t *testing.T) {
	cases := map[string]bool{
		"ai:agent-1":    true,
		"workflow:wf-1": true,
		"user-1":        false,
		"":              false,
		SystemID:        false,
	}
	for id, want := range cases {
		if got := IsAutomation(id); got != want {
			t.Errorf("IsAutomation(%q) = %v, want %v", id, got, want)
		}
	}
}
