package copilottools

import (
	"errors"
	"testing"

	"vozko/domain/buttonaction"
	"vozko/domain/copilot"
)

func TestOnlyATemplateWithoutAnExitAsksTheOperatorForTheAcknowledgement(t *testing.T) {
	none := ExitRiskChoices(buttonaction.Verdict{State: buttonaction.VerdictNone})
	if len(none) != 1 || none[0].Key != ExitRiskChoiceKey || none[0].Kind != ExitRiskChoiceKind || none[0].Default != "" {
		t.Fatalf("none = %+v; want one card choice with no default, so it never auto approves", none)
	}
	if ok := ExitRiskChoices(buttonaction.Verdict{State: buttonaction.VerdictOK}); len(ok) != 0 {
		t.Fatalf("ok = %+v", ok)
	}
}

func TestTheAcknowledgementNamesTheOperatorOnlyWhenTheCardWasTicked(t *testing.T) {
	cc := copilot.Context{UserID: "u-1", WorkspaceID: "ws-1"}
	cases := map[string]struct {
		args map[string]interface{}
		want string
	}{
		"ticked":       {map[string]interface{}{ExitRiskChoiceKey: ExitRiskAcknowledged}, "u-1"},
		"another word": {map[string]interface{}{ExitRiskChoiceKey: "yes"}, ""},
		"not a string": {map[string]interface{}{ExitRiskChoiceKey: true}, ""},
		"absent":       {map[string]interface{}{}, ""},
	}
	for name, tc := range cases {
		if got := ExitRiskAcknowledgedBy(cc, tc.args); got != tc.want {
			t.Errorf("%s = %q, want %q", name, got, tc.want)
		}
	}
}

func TestTheModelCannotSendTheAcknowledgementItself(t *testing.T) {
	modelArgs := map[string]interface{}{"name": "x", ExitRiskChoiceKey: ExitRiskAcknowledged}
	if err := RefuseUnaskedExitRisk(modelArgs, nil); !errors.Is(err, ErrInvalidArgs) {
		t.Fatalf("an acknowledgement the card did not ask for = %v, want refused", err)
	}
	asked := ExitRiskChoices(buttonaction.Verdict{State: buttonaction.VerdictNone})
	stripped := copilot.StripChoices(modelArgs, asked)
	if ExitRiskAcknowledgedBy(copilot.Context{UserID: "u-1"}, stripped) != "" {
		t.Fatal("the model's value survived the proposal")
	}
	approved, err := copilot.WithChoices(stripped, asked, map[string]string{ExitRiskChoiceKey: ExitRiskAcknowledged})
	if err != nil {
		t.Fatal(err)
	}
	if err := RefuseUnaskedExitRisk(approved, asked); err != nil {
		t.Fatalf("the operator's tick = %v", err)
	}
	if got := ExitRiskAcknowledgedBy(copilot.Context{UserID: "u-1"}, approved); got != "u-1" {
		t.Fatalf("acknowledged by %q", got)
	}
}

func TestTheVerdictFieldOfTheCard(t *testing.T) {
	none := ExitVerdictField(buttonaction.Verdict{State: buttonaction.VerdictNone})
	if none.Key != "risks" || none.Value == "" {
		t.Fatalf("none = %+v", none)
	}
	action := buttonaction.ButtonAction{Kind: buttonaction.KindOptOut}
	ok := ExitVerdictField(buttonaction.Verdict{State: buttonaction.VerdictOK, Buttons: []buttonaction.ButtonRoute{{Index: 1, Text: "SAIR", Action: &action}}, ExitWords: []string{"sair"}})
	if ok.Key != "leadExit" || ok.Value != "SAIR (opt_out), /sair" {
		t.Fatalf("ok = %+v", ok)
	}
}
