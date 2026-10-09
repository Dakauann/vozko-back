package copilot_usecase

import "testing"

func TestTheRegistryListsToolsInTheSameOrderEveryTime(t *testing.T) {
	r := NewRegistry(&fakeTool{name: "zeta"}, &fakeTool{name: "alfa"}, &fakeTool{name: "meio"})
	want := []string{"alfa", "meio", "zeta"}
	for round := 0; round < 20; round++ {
		defs := r.Definitions()
		tools := r.Tools()
		for i, name := range want {
			if defs[i].Name != name || tools[i].Definition().Name != name {
				t.Fatalf("round %d: tools must be sorted by name so the prompt prefix never changes, got %v", round, defs)
			}
		}
	}
}
