package copilot

import (
	"errors"
	"testing"

	"vozko/domain/workspace"
)

func TestNavigationCardPointsAtAScreenNotAURL(t *testing.T) {
	card, err := NewNavigationCard(workspace.ScreenFunnels, "")
	if err != nil {
		t.Fatal(err)
	}
	if card.Kind != ActionOpenScreen || card.Destination.Screen != workspace.ScreenFunnels || len(card.Destination.Params) != 0 {
		t.Fatalf("unexpected card %+v", card)
	}
}

func TestNavigationCardFillsTheScreensOnlyParam(t *testing.T) {
	card, err := NewNavigationCard(workspace.ScreenAgentDetail, "7b1c2f9e-0a51-4f7e-9f3a-2d5b8c1e4a10")
	if err != nil {
		t.Fatal(err)
	}
	if card.Destination.Params["agentId"] != "7b1c2f9e-0a51-4f7e-9f3a-2d5b8c1e4a10" {
		t.Fatalf("agentId not carried: %+v", card.Destination)
	}
}

func TestNavigationCardRefusesBadDestinations(t *testing.T) {
	cases := []struct {
		screen workspace.Screen
		id     string
	}{
		{"not_a_screen", ""},
		{workspace.ScreenAgentDetail, ""},
		{workspace.ScreenFunnels, "unexpected"},
		{workspace.ScreenAgentDetail, "../../admin"},
		{workspace.ScreenAgentDetail, "a b"},
	}
	for _, c := range cases {
		if _, err := NewNavigationCard(c.screen, c.id); !errors.Is(err, ErrInvalidDestination) {
			t.Errorf("%s with %q must be refused, got %v", c.screen, c.id, err)
		}
	}
}

func TestOpenScreenIsNotAnOfferableAction(t *testing.T) {
	for _, k := range ActionKinds() {
		if k == ActionOpenScreen {
			t.Fatal("navigation cards come from open_screen, not offer_action")
		}
	}
}
