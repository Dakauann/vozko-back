package uazapi

import (
	"reflect"
	"testing"

	uw "vozko/domain/unofficial_whatsapp"
)

func TestAListMenuEncodesSectionsAndDescriptions(t *testing.T) {
	options := []uw.InteractiveOption{
		{ID: "a", Title: "Agendar", Description: "Escolha o dia", Section: "Atendimento"},
		{ID: "b", Title: "Benefícios", Section: "Atendimento"},
		{ID: "c|x", Title: "Pagar|já", Section: "[Financeiro]"},
	}

	got := encodeChoices(options, true)
	want := []string{"[Atendimento]", "Agendar|a|Escolha o dia", "Benefícios|b", "[(Financeiro)]", "Pagar/já|c/x"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("choices = %q, want %q", got, want)
	}
}

func TestButtonsCarryNeitherSectionsNorDescriptions(t *testing.T) {
	got := encodeChoices([]uw.InteractiveOption{{ID: "a", Title: "Sim", Description: "d", Section: "S"}}, false)
	if !reflect.DeepEqual(got, []string{"Sim|a"}) {
		t.Fatalf("choices = %q", got)
	}
}
