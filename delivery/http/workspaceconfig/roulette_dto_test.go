package workspaceconfig

import (
	"encoding/json"
	"strings"
	"testing"

	"vozko/domain/inbox_assignment"
	workspaceconfigdomain "vozko/domain/workspace_config"
)

func TestResponseCarriesTheRouletteStrategy(t *testing.T) {
	body, err := json.Marshal(toWorkspaceConfigResponse(&workspaceconfigdomain.WorkspaceConfig{
		RouletteStrategy:         inbox_assignment.StrategyWeighted,
		RouletteMaxOpenPerMember: 6,
		RouletteRecipients:       []inbox_assignment.Recipient{{UserID: "ana", Tier: 1, Weight: 3}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"rouletteStrategy":"weighted"`, `"rouletteMaxOpenPerMember":6`, `"rouletteRecipients":[{"userId":"ana","tier":1,"weight":3}]`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("response %s lacks %s", body, want)
		}
	}
}

func TestResponseDefaultsTheRouletteStrategyAndListsNoRecipientsAsAnEmptyArray(t *testing.T) {
	body, err := json.Marshal(toWorkspaceConfigResponse(&workspaceconfigdomain.WorkspaceConfig{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"rouletteStrategy":"round_robin"`, `"rouletteMaxOpenPerMember":0`, `"rouletteRecipients":[]`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("response %s lacks %s", body, want)
		}
	}
}
