package conversation

import (
	"strings"
	"testing"
)

func options(pairs ...string) []InteractiveOption {
	out := make([]InteractiveOption, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, InteractiveOption{ID: pairs[i], Title: pairs[i+1]})
	}
	return out
}

func TestFitOptionsKeepsWhatTheChannelCanRender(t *testing.T) {
	kept, dropped := FitOptions(options("sim", "Sim", "", "Sem id", strings.Repeat("ç", 40), "Longo", "nao", "", "extra", "Extra"), 2, 64)

	if len(kept) != 2 || kept[0] != (InteractiveOption{ID: "sim", Title: "Sim"}) || kept[1] != (InteractiveOption{ID: "nao", Title: "nao"}) {
		t.Fatalf("kept = %+v", kept)
	}
	if len(dropped) != 3 {
		t.Fatalf("dropped = %+v", dropped)
	}
	if !strings.Contains(dropped[1].Reason, "64-byte") {
		t.Errorf("payload reason = %q, want the byte limit named", dropped[1].Reason)
	}
	if !strings.Contains(dropped[2].Reason, "2") {
		t.Errorf("cap reason = %q, want the cap named", dropped[2].Reason)
	}
}

func TestComposedBodyJoinsOnlyThePresentParts(t *testing.T) {
	full := SendInteractiveRequest{Header: " Atendimento ", Body: "Escolha", Footer: "Rodapé"}
	if got := full.ComposedBody(); got != "Atendimento\n\nEscolha\n\nRodapé" {
		t.Errorf("full = %q", got)
	}
	if got := (SendInteractiveRequest{Body: "Só o corpo"}).ComposedBody(); got != "Só o corpo" {
		t.Errorf("body only = %q", got)
	}
}
