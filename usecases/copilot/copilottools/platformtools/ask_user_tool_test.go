package platformtools

import (
	"context"
	"testing"

	"vozko/domain/copilot"
	"vozko/usecases/copilot/copilottools/tooltest"
)

func creativePaths() map[string]interface{} {
	return map[string]interface{}{"questions": []interface{}{map[string]interface{}{
		"question": "Como você quer mexer no criativo?",
		"options": []interface{}{
			map[string]interface{}{"label": "Gerar e ajustar por descrição", "detail": "cada pedido vira uma versão nova"},
			map[string]interface{}{"label": "Editar no Estúdio", "detail": "você mexe à mão e eu edito junto"},
		},
	}}}
}

func TestAskUserShowsTheQuestionCardOutsideTheStudioAndWaits(t *testing.T) {
	res := NewAskUserTool().Execute(context.Background(), tooltest.OwnerOn(copilot.View{}), creativePaths())
	if res.Status != copilot.StatusOK || res.Card == nil || res.Card.Kind != copilot.ActionAsk || !res.EndTurn {
		t.Fatalf("got %+v", res)
	}
	if len(res.Card.Questions) != 1 || len(res.Card.Questions[0].Options) != 2 {
		t.Fatalf("card %+v", res.Card)
	}
}

func TestAskUserRefusesACardThatCannotBeAnsweredWithATap(t *testing.T) {
	one := map[string]interface{}{"questions": []interface{}{map[string]interface{}{"question": "Ok?", "options": []interface{}{map[string]interface{}{"label": "Sim"}}}}}
	for _, args := range []map[string]interface{}{{}, one} {
		if res := NewAskUserTool().Execute(context.Background(), tooltest.OwnerOn(copilot.View{}), args); res.Status != copilot.StatusError || res.Card != nil {
			t.Errorf("%v must be refused, got %+v", args, res)
		}
	}
}

func TestAskUserAndStudioAskNeverMeet(t *testing.T) {
	tool := NewAskUserTool()
	studio := copilot.View{Surface: copilot.SurfaceStudio, ProjectID: "p1", ProjectKind: copilot.StudioImage}
	if studio.Offers(tool) || !(copilot.View{}).Offers(tool) || tool.Meta().Mutating {
		t.Fatal("the studio keeps studio_ask; everywhere else gets ask_user, a read")
	}
	if tool.Definition().Parameters["questions"].Items == nil {
		t.Fatal("the questions schema must describe its items")
	}
}
