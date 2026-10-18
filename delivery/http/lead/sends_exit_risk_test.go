package lead

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"vozko/domain/buttonaction"
)

func exitRiskSendBody(send map[string]any) map[string]any {
	return map[string]any{
		"action":    "send_template",
		"params":    map[string]any{"send": send},
		"selection": map[string]any{"mode": "ids", "ids": []string{actionRunID}},
	}
}

func TestATemplateSendCarriesTheRiskAcknowledgementToTheUseCase(t *testing.T) {
	cases := map[string]struct {
		send map[string]any
		want bool
	}{
		"ticked":     {map[string]any{"name": "Matrículas", "businessPhoneId": "bp-1", "templateId": "tpl-1", "exitRiskAcknowledged": true}, true},
		"not ticked": {map[string]any{"name": "Matrículas", "businessPhoneId": "bp-1", "templateId": "tpl-1"}, false},
	}
	for name, tc := range cases {
		actions := &stubActions{}
		rec := send(t, actionsHandler(actions), http.MethodPost, "/leads/actions", map[string]string{"Idempotency-Key": "k"}, exitRiskSendBody(tc.send))
		if rec.Code != http.StatusAccepted || len(actions.started) != 1 {
			t.Fatalf("%s = %d %s", name, rec.Code, rec.Body.String())
		}
		if got := actions.started[0].Params.Send.ExitRiskAcknowledged; got != tc.want {
			t.Errorf("%s: acknowledged = %v, want %v", name, got, tc.want)
		}
	}
}

func TestASendRefusedForTheMissingAcknowledgementAnswersItsCode(t *testing.T) {
	refused := fmt.Errorf("create part: %w", buttonaction.ErrExitRiskNotAcknowledged)
	rec := send(t, actionsHandler(&stubActions{err: refused}), http.MethodPost, "/leads/actions", map[string]string{"Idempotency-Key": "k"},
		exitRiskSendBody(map[string]any{"name": "Matrículas", "businessPhoneId": "bp-1", "templateId": "tpl-1"}))
	var body struct {
		Code string `json:"code"`
	}
	if rec.Code != http.StatusUnprocessableEntity || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Code != "exit_risk_not_acknowledged" {
		t.Fatalf("refusal = %d %s", rec.Code, rec.Body.String())
	}
}
