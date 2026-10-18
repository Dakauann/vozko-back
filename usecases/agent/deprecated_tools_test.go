package agent_usecase

import (
	"errors"
	"testing"

	"vozko/domain/agent"
	toolsdomain "vozko/domain/tools"
)

func indexWith(names ...string) *toolRegistryIndex {
	idx := &toolRegistryIndex{defs: map[string]toolsdomain.Definition{}, all: []string{}}
	for _, n := range names {
		idx.defs[n] = toolsdomain.Definition{Name: n}
		idx.all = append(idx.all, n)
	}
	return idx
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

	_, err := validateRequestedToolBindings(idx, []agent.ToolBinding{{Name: "not_a_tool"}})
	if !errors.Is(err, agent.ErrAgentInternalToolInvalid) {
		t.Errorf("err = %v, an unknown binding must still be rejected", err)
	}
}
