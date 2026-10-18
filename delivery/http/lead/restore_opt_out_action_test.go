package lead

import (
	"encoding/json"
	"net/http"
	"testing"

	"vozko/domain/leadaction"
	leadaction_usecase "vozko/usecases/leadaction"
)

func restoreBody() map[string]any {
	return map[string]any{
		"action":    "restore_opt_out",
		"params":    map[string]any{"note": "o botão SAIR estava ligado à opção errada", "confirmCount": 2},
		"selection": map[string]any{"mode": "ids", "ids": []string{actionRunID, "1b7c3f1e-2d4a-4c5b-9e8f-1a2b3c4d5e6f"}},
	}
}

func TestUndoingOptOutsCarriesTheNoteAndTheTypedCount(t *testing.T) {
	actions := &stubActions{outcome: leadaction_usecase.Outcome{Run: &leadaction.Run{ID: actionRunID, Action: leadaction.ActionRestoreOptOut, Status: leadaction.StatusQueued}}}
	rec := send(t, actionsHandler(actions), http.MethodPost, "/leads/actions", map[string]string{"Idempotency-Key": "k-restore"}, restoreBody())
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	req := actions.started[0]
	if req.Action != leadaction.ActionRestoreOptOut || req.Params.Note != "o botão SAIR estava ligado à opção errada" || req.Params.ConfirmCount != 2 {
		t.Fatalf("request = %+v", req)
	}
	preview := &stubActions{preview: &leadaction.Preview{ID: actionRunID, Action: leadaction.ActionRestoreOptOut, Status: leadaction.PreviewDone,
		Result: leadaction.PreviewResult{Selected: 2, Eligible: 1, Skipped: map[leadaction.SkipReason]int{leadaction.SkipOptOutNotRestorable: 1}}}}
	rec = send(t, actionsHandler(preview), http.MethodPost, "/leads/actions/preview", nil, map[string]any{
		"action": "restore_opt_out", "params": map[string]any{}, "selection": restoreBody()["selection"],
	})
	var out ActionPreviewResponse
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.Result.Skipped[leadaction.SkipOptOutNotRestorable] != 1 || out.Result.Eligible != 1 {
		t.Fatalf("preview = %d %s", rec.Code, rec.Body.String())
	}
}

func TestUndoingOptOutsRefusalsAreCoded(t *testing.T) {
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{leadaction.ErrNoteRequired, http.StatusBadRequest, "lead_action_note_required"},
		{leadaction.ErrNoteTooLong, http.StatusBadRequest, "lead_action_note_too_long"},
	}
	for _, tc := range cases {
		rec := send(t, actionsHandler(&stubActions{err: tc.err}), http.MethodPost, "/leads/actions", map[string]string{"Idempotency-Key": "k"}, restoreBody())
		var body struct {
			Code string `json:"code"`
		}
		if rec.Code != tc.status || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Code != tc.code {
			t.Errorf("%v = %d %s", tc.err, rec.Code, rec.Body.String())
		}
	}
}
