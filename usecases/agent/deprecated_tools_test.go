package agent_usecase

import (
	"errors"
	"testing"

	"vozko/domain/agent"
	toolsdomain "vozko/domain/tools"
)

// A saved agent outlives the registry that defined its tools.
//
// conversation_analysis was superseded when the analysis job stopped asking the
// model for a verdict — wantAnalysis in analysis_debounce_job.go is a hardcoded
// false — so no handler defines it any more. But 54 agents still carried the
// binding, and the editor resubmits whatever it loaded. Picking a knowledge
// base therefore failed with "agent internal tool is invalid:
// conversation_analysis", naming a binding the operator had not touched, and
// the agent could not be saved at all.

// stubDef is the minimum a registry entry needs for these tests.
type stubDef struct{ name string }

func (s stubDef) Definition() toolsdomain.Definition {
	return toolsdomain.Definition{Name: s.name}
}

func indexWith(names ...string) *toolRegistryIndex {
	idx := &toolRegistryIndex{defs: map[string]toolsdomain.Definition{}, all: []string{}}
	for _, n := range names {
		idx.defs[n] = toolsdomain.Definition{Name: n}
		idx.all = append(idx.all, n)
	}
	return idx
}

func TestRetiredToolIsDroppedNotRejected(t *testing.T) {
	idx := indexWith("manage_entry_stage", "search_knowledge_base")

	// The exact shape found in production: the dead tool sits alongside live ones.
	got, err := validateRequestedTools(idx, []string{
		"conversation_analysis", "manage_entry_stage", "search_knowledge_base",
	})
	if err != nil {
		t.Fatalf("a retired tool must not fail the save: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("kept %v, want only the two live tools", got)
	}
	for _, n := range got {
		if n == "conversation_analysis" {
			t.Error("the retired tool survived into the saved set")
		}
	}
}

// Bindings are the path the agent editor actually submits, and they carry
// per-tool config, so they validate separately from bare names.
func TestRetiredBindingIsDroppedNotRejected(t *testing.T) {
	idx := indexWith("manage_entry_stage")

	got, err := validateRequestedToolBindings(idx, []agent.ToolBinding{
		{Name: "conversation_analysis", Visibility: []agent.ToolVisibility{agent.ToolVisibility("post_conversation")}},
		{Name: "manage_entry_stage", Visibility: []agent.ToolVisibility{agent.ToolVisibilityMessaging}},
	})
	if err != nil {
		t.Fatalf("a retired binding must not fail the save: %v", err)
	}
	if len(got) != 1 || got[0].Name != "manage_entry_stage" {
		t.Errorf("kept %+v, want only manage_entry_stage", got)
	}
}

// Dropping the retired name must not soften the check. A tool that is simply
// misspelled or genuinely unknown still names nothing and must still fail,
// otherwise a typo silently disables a capability the operator wanted.
func TestAnUnknownToolStillFails(t *testing.T) {
	idx := indexWith("manage_entry_stage")

	_, err := validateRequestedTools(idx, []string{"manage_entry_stagee"})
	if !errors.Is(err, agent.ErrAgentInternalToolInvalid) {
		t.Errorf("err = %v, an unknown tool must still be rejected", err)
	}

	_, err = validateRequestedToolBindings(idx, []agent.ToolBinding{{Name: "not_a_tool"}})
	if !errors.Is(err, agent.ErrAgentInternalToolInvalid) {
		t.Errorf("err = %v, an unknown binding must still be rejected", err)
	}
}

// Both retired names must behave the same way; query_knowledge_base was the
// first and is what established the mechanism.
func TestBothRetiredNamesAreDropped(t *testing.T) {
	idx := indexWith("search_knowledge_base")

	got, err := validateRequestedTools(idx, []string{
		"query_knowledge_base", "conversation_analysis", "search_knowledge_base",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != "search_knowledge_base" {
		t.Errorf("kept %v, want only search_knowledge_base", got)
	}
}
