package workspace_config

import (
	"testing"

	ia "vozko/domain/inbox_assignment"
)

func TestEffectiveRouletteStrategy(t *testing.T) {
	var none *WorkspaceConfig
	if got := none.EffectiveRouletteStrategy(); got != ia.StrategyRoundRobin {
		t.Fatalf("an unconfigured workspace keeps round robin, got %q", got)
	}
	if got := (&WorkspaceConfig{RouletteStrategy: "bogus"}).EffectiveRouletteStrategy(); got != ia.StrategyRoundRobin {
		t.Fatalf("got %q", got)
	}
	if got := (&WorkspaceConfig{RouletteStrategy: ia.StrategyPriority}).EffectiveRouletteStrategy(); got != ia.StrategyPriority {
		t.Fatalf("got %q", got)
	}
}

func TestEffectiveRouletteMaxOpenPerMember(t *testing.T) {
	var none *WorkspaceConfig
	if got := none.EffectiveRouletteMaxOpenPerMember(); got != 0 {
		t.Fatalf("got %d", got)
	}
	cases := map[int]int{-3: 0, 0: 0, 12: 12, ia.MaxOpenPerMember + 40: ia.MaxOpenPerMember}
	for stored, want := range cases {
		if got := (&WorkspaceConfig{RouletteMaxOpenPerMember: stored}).EffectiveRouletteMaxOpenPerMember(); got != want {
			t.Fatalf("stored %d: got %d want %d", stored, got, want)
		}
	}
}

func TestEffectiveRouletteRecipients(t *testing.T) {
	list := []ia.Recipient{{UserID: "ana", Tier: 1, Weight: 1}}
	var none *WorkspaceConfig
	if got := none.EffectiveRouletteRecipients(); got != nil {
		t.Fatalf("got %v", got)
	}
	if got := (&WorkspaceConfig{RouletteRecipients: list}).EffectiveRouletteRecipients(); len(got) != 1 {
		t.Fatalf("got %v", got)
	}
}
