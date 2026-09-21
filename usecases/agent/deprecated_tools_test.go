package agent_usecase

import (
	"errors"
	"testing"

	"vozko/domain/agent"
	toolsdomain "vozko/domain/tools"
)

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
