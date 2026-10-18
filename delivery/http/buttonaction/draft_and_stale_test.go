package buttonactionhttp

import (
	"net/http"
	"strings"
	"testing"

	"vozko/domain/buttonaction"
	"vozko/domain/whatsapp/template"
)

func TestSavingAnActionPassesTheButtonTextThePersonSaw(t *testing.T) {
	svc := &fakeService{}
	body := `{"actions":[{"buttonIndex":0,"buttonText":"SAIR","kind":"opt_out","params":{}}]}`
	rec := serve(t, svc, http.MethodPut, "/whatsapp/templates/tmpl-1/button-actions", "3", body)
	if rec.Code != http.StatusOK || len(svc.savedChange.Actions) != 1 || svc.savedChange.Actions[0].ButtonText != "SAIR" {
		t.Fatalf("status = %d, saved = %+v", rec.Code, svc.savedChange)
	}
}

func TestAStaleSaveIsRefusedWithItsCode(t *testing.T) {
	rec := serve(t, &fakeService{saveErr: buttonaction.ErrButtonActionStale}, http.MethodPut, "/whatsapp/templates/tmpl-1/button-actions", "3", `{"actions":[]}`)
	if rec.Code != http.StatusUnprocessableEntity || codeOf(t, rec) != "button_action_stale" {
		t.Fatalf("status = %d, code = %q", rec.Code, codeOf(t, rec))
	}
}

func TestTheDraftVerdictReadsTheDraftAndStoresNothing(t *testing.T) {
	svc := &fakeService{}
	body := `{"category":"MARKETING","header":"Aviso","body":"Ofertas da semana","footer":"Responda /sair",` +
		`"buttons":[{"type":"QUICK_REPLY","text":"Parar"},{"type":"URL","text":"Loja"}],` +
		`"buttonActions":[{"buttonIndex":0,"buttonText":"Parar","kind":"opt_out","params":{}}]}`
	rec := serve(t, svc, http.MethodPost, "/whatsapp/templates/exit-verdict", "", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	for _, want := range []string{`"verdict":{"state":"none"`, `"templateExitRequired":true`, `"campaignAcknowledgementRequired":true`, `"allowedKinds":["opt_out"]`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("body misses %s: %s", want, rec.Body)
		}
	}
	draft := svc.draft
	if draft.Category != template.TemplateCategoryMarketing || len(draft.Buttons) != 2 || draft.Buttons[1].Type != "URL" ||
		len(draft.Actions) != 1 || draft.Actions[0].ButtonText != "Parar" ||
		draft.Header != "Aviso" || draft.Body != "Ofertas da semana" || draft.Footer != "Responda /sair" {
		t.Fatalf("draft = %+v", draft)
	}
	if svc.savedVersion != 0 {
		t.Fatalf("a draft verdict saved something")
	}
}

func TestTheDraftVerdictRefusesUnknownKeysAndCodesRefusals(t *testing.T) {
	for _, unknown := range []string{`{"category":"MARKETING","name":"x"}`, `{"category":"MARKETING","exitWords":["/sair"]}`} {
		if rec := serve(t, &fakeService{}, http.MethodPost, "/whatsapp/templates/exit-verdict", "", unknown); rec.Code != http.StatusBadRequest {
			t.Fatalf("unknown key %s = %d", unknown, rec.Code)
		}
	}
	rec := serve(t, &fakeService{draftErr: buttonaction.ErrKindUnknown}, http.MethodPost, "/whatsapp/templates/exit-verdict", "", `{"category":"MARKETING"}`)
	if rec.Code != http.StatusBadRequest || codeOf(t, rec) != "button_action_kind_unknown" {
		t.Fatalf("status = %d, code = %q", rec.Code, codeOf(t, rec))
	}
}
