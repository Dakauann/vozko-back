package campaign

import (
	"errors"
	"testing"

	"vozko/domain/agent"
)

func TestRequireWorkflowVars(t *testing.T) {
	entries := []EntryMetadata{
		{Label: "5511999990001", Metadata: map[string]any{"escola": "Prisma", "turma": "A"}},
		{Label: "5511999990002", Metadata: map[string]any{"escola": "Prisma", "turma": " "}},
	}
	cases := []struct {
		name    string
		keys    []string
		entries []EntryMetadata
		want    error
	}{
		{name: "no variable asked", entries: entries},
		{name: "every entry carries the key", keys: []string{"escola"}, entries: entries},
		{name: "an entry left the key empty", keys: []string{"turma"}, entries: entries, want: ErrWorkflowVarsMissing},
		{name: "an entry has no metadata", keys: []string{"escola"}, entries: []EntryMetadata{{Label: "x"}}, want: ErrWorkflowVarsMissing},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := RequireWorkflowVars(tc.keys, tc.entries); !errors.Is(err, tc.want) {
				t.Fatalf("RequireWorkflowVars = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestRequireAgentVars(t *testing.T) {
	needsSchool := &agent.Agent{Variables: []agent.AgentVariable{{Name: "escola"}}}
	if err := RequireAgentVars(needsSchool, []EntryMetadata{{Metadata: map[string]any{"escola": "Prisma"}}}); err != nil {
		t.Fatalf("a filled variable was refused: %v", err)
	}
	if err := RequireAgentVars(needsSchool, []EntryMetadata{{Metadata: map[string]any{}}}); !errors.Is(err, ErrAgentVarsMissing) {
		t.Fatalf("a missing variable = %v", err)
	}
	if err := RequireAgentVars(nil, []EntryMetadata{{}}); !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("no agent = %v", err)
	}
}

func TestAutomationConfigured(t *testing.T) {
	cases := []struct {
		a    Automation
		want bool
	}{
		{a: Automation{}, want: false},
		{a: Automation{WorkflowID: "  "}, want: false},
		{a: Automation{WorkflowID: "wf-1"}, want: true},
		{a: Automation{AgentID: "agent-1"}, want: true},
	}
	for _, tc := range cases {
		if got := tc.a.Configured(); got != tc.want {
			t.Fatalf("Configured(%+v) = %v, want %v", tc.a, got, tc.want)
		}
	}
}

func TestEntryMetadataOfNamesTheLeadOrTheNumber(t *testing.T) {
	if got := EntryMetadataOf("lead-1", "5584", nil); got.Label != "lead lead-1" {
		t.Fatalf("label = %q", got.Label)
	}
	if got := EntryMetadataOf("", "5584", map[string]any{"escola": "A"}); got.Label != "number 5584" || got.Metadata["escola"] != "A" {
		t.Fatalf("entry = %+v", got)
	}
}
