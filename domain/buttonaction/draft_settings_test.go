package buttonaction

import (
	"errors"
	"testing"
)

func TestDraftActionsBindToTheDraftsOwnButtons(t *testing.T) {
	draft := templateWith("Ofertas.", "", quickReplies("Quero", "Parar")...)

	settings, err := SettingsFor(draft, []Input{{ButtonIndex: 1, ButtonText: "Parar", Kind: KindOptOut}})

	if err != nil || len(settings.Actions) != 1 || settings.Actions[0].Kind != KindOptOut {
		t.Fatalf("settings = %+v, err = %v", settings, err)
	}
	if ExitVerdict(draft, settings, CampaignRoutes{}).State != VerdictOK {
		t.Fatal("an exit action on a draft button is an exit")
	}
}

func TestADraftActionOnAButtonTheDraftLacksIsRefused(t *testing.T) {
	draft := templateWith("Ofertas.", "", quickReplies("Quero")...)

	if _, err := SettingsFor(draft, []Input{{ButtonIndex: 0, ButtonText: "Parar", Kind: KindOptOut}}); err == nil {
		t.Fatal("an action must match the draft's button text")
	}
	if _, err := SettingsFor(draft, []Input{{ButtonIndex: 0, ButtonText: "Quero", Kind: KindOptOut}, {ButtonIndex: 0, ButtonText: "Quero", Kind: KindOptOut}}); !errors.Is(err, ErrDuplicateButton) {
		t.Fatalf("two actions on one button: %v", err)
	}
}
