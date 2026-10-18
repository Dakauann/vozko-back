package buttonaction

import (
	"errors"
	"testing"
)

func TestCampaignOverrideRefusesExitKinds(t *testing.T) {
	buttons := []string{"Falar com atendente", "SAIR"}
	tmpl := templateWith("Ofertas.", "", quickReplies(buttons...)...)
	cases := map[Kind]Params{
		KindOptOut:       {},
		KindConsentGrant: {Purpose: "ofertas"},
	}
	for kind, params := range cases {
		override := mustAction(t, buttons, 0, kind, params)
		if err := ValidateOverride(tmpl, settingsWith(), override); !errors.Is(err, ErrExitLocked) {
			t.Errorf("%s: got %v, want ErrExitLocked", kind, err)
		}
	}
}

func TestCampaignOverrideRefusesAButtonWhoseTemplateActionIsAnExit(t *testing.T) {
	buttons := []string{"Falar com atendente", "SAIR"}
	tmpl := templateWith("Ofertas.", "", quickReplies(buttons...)...)
	exit := mustAction(t, buttons, 1, KindOptOut, Params{})
	override := mustAction(t, buttons, 1, KindHandOff, Params{DepartmentID: "vendas"})
	if err := ValidateOverride(tmpl, settingsWith(exit), override); !errors.Is(err, ErrExitLocked) {
		t.Fatalf("got %v, want ErrExitLocked", err)
	}
}

func TestCampaignOverrideOfANonExitButton(t *testing.T) {
	buttons := []string{"Falar com atendente", "SAIR"}
	tmpl := templateWith("Ofertas.", "", quickReplies(buttons...)...)
	exit := mustAction(t, buttons, 1, KindOptOut, Params{})
	override := mustAction(t, buttons, 0, KindHandOff, Params{DepartmentID: "cobranca"})
	if err := ValidateOverride(tmpl, settingsWith(exit), override); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCampaignOverrideIsCheckedAgainstTheTemplate(t *testing.T) {
	buttons := []string{"Falar com atendente", "SAIR"}
	tmpl := templateWith("Ofertas.", "", quickReply("Falar com atendente"))
	override := mustAction(t, buttons, 1, KindHandOff, Params{DepartmentID: "vendas"})
	if err := ValidateOverride(tmpl, settingsWith(), override); !errors.Is(err, ErrButtonNotQuickReply) {
		t.Fatalf("got %v, want ErrButtonNotQuickReply", err)
	}
}

func TestOneActionPerButton(t *testing.T) {
	buttons := []string{"Falar com atendente", "SAIR"}
	first := mustAction(t, buttons, 0, KindHandOff, Params{DepartmentID: "vendas"})
	second := mustAction(t, buttons, 0, KindHandOff, Params{DepartmentID: "cobranca"})
	if err := EnsureOnePerButton([]ButtonAction{first, second}); !errors.Is(err, ErrDuplicateButton) {
		t.Fatalf("got %v, want ErrDuplicateButton", err)
	}
	if err := EnsureOnePerButton([]ButtonAction{first}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
