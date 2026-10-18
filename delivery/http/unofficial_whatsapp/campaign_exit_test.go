package unofficial_whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/auth"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/infra/http/middleware"
)

type refusingCreate struct{ err error }

func (c refusingCreate) Execute(context.Context, *uwc.Campaign, uw.DepartmentScope) (*uwc.Campaign, error) {
	return nil, c.err
}

func TestACampaignWhoseMessageHidesTheExitIsRefusedWithItsCode(t *testing.T) {
	h := NewCampaignHandler(CampaignHandlerDeps{Create: refusingCreate{err: fmt.Errorf("validate: %w", uwc.ErrCampaignNeedsExit)}, Departments: openScope{}})
	r := httptest.NewRequest(http.MethodPost, "/unofficial-whatsapp/campaigns", bytes.NewBufferString(
		`{"name":"demo","instanceId":"inst-1","message":{"kind":"text","bodies":["oi"]},"targets":[{"number":"5584999990001"}]}`))
	ctx := context.WithValue(r.Context(), middleware.WorkspaceIDContextKey, "ws-1")
	ctx = context.WithValue(ctx, middleware.ClaimsContextKey, &auth.Claims{UserID: "u-1", Role: "user"})

	w := httptest.NewRecorder()
	h.Create(w, r.WithContext(ctx))

	var body struct {
		Code string `json:"code"`
	}
	if w.Code != http.StatusUnprocessableEntity || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Code != "unofficial_campaign_needs_exit" {
		t.Fatalf("refusal = %d %s", w.Code, w.Body.String())
	}
}

func TestTheAudioExitTextTravelsBothWays(t *testing.T) {
	var dto MessageSpecDTO
	if err := json.Unmarshal([]byte(`{"kind":"audio","bodies":[],"mediaId":"m-1","exitText":"Responda /sair"}`), &dto); err != nil {
		t.Fatal(err)
	}
	spec := dto.ToDomain()
	if spec.ExitText != "Responda /sair" {
		t.Fatalf("domain exit text = %q", spec.ExitText)
	}
	blob, _ := json.Marshal(MessageSpecToDTO(spec))
	var back map[string]any
	if err := json.Unmarshal(blob, &back); err != nil || back["exitText"] != "Responda /sair" {
		t.Fatalf("dto = %s, %v", blob, err)
	}
}
