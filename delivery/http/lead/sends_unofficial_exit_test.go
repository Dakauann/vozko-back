package lead

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

func TestAnUnofficialSendWhoseMessageHidesTheExitAnswersItsCode(t *testing.T) {
	refused := fmt.Errorf("plan: %w", uwc.ErrCampaignNeedsExit)
	rec := send(t, actionsHandler(&stubActions{err: refused}), http.MethodPost, "/leads/actions", map[string]string{"Idempotency-Key": "k"},
		map[string]any{
			"action": "send_unofficial",
			"params": map[string]any{"send": map[string]any{
				"name": "Matrículas", "instanceId": "inst-1", "message": map[string]any{"kind": "text", "bodies": []string{"Olá"}},
			}},
			"selection": map[string]any{"mode": "ids", "ids": []string{actionRunID}},
		})
	var body struct {
		Code string `json:"code"`
	}
	if rec.Code != http.StatusUnprocessableEntity || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Code != "unofficial_campaign_needs_exit" {
		t.Fatalf("refusal = %d %s", rec.Code, rec.Body.String())
	}
}
