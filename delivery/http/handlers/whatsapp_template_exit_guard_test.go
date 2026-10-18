package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	whatsapptemplatehttp "vozko/delivery/http/whatsapptemplate"
	"vozko/domain/buttonaction"
	"vozko/domain/whatsapp/template"
	buttonaction_usecase "vozko/usecases/buttonaction"
)

type passThroughCreator struct {
	templates template.WorkspaceTemplatesUseCase
}

func (p passThroughCreator) CreateTemplate(_ context.Context, a buttonaction_usecase.Actor, req buttonaction_usecase.CreateTemplateRequest) (*template.CreateTemplateOutput, error) {
	return p.templates.Create(a.WorkspaceID, a.UserID, req.Template)
}

type refusingCreator struct {
	got buttonaction_usecase.CreateTemplateRequest
}

func (r *refusingCreator) CreateTemplate(_ context.Context, _ buttonaction_usecase.Actor, req buttonaction_usecase.CreateTemplateRequest) (*template.CreateTemplateOutput, error) {
	r.got = req
	return nil, buttonaction.ErrMarketingNeedsExit
}

func TestCreateForWorkspaceRefusesAMarketingTemplateWithoutAShownExit(t *testing.T) {
	h := newTemplateTestHandler()
	creator := &refusingCreator{}
	h.SetExitGuardedCreator(creator)

	req := requestWithClaims(http.MethodPost, "/whatsapp/templates", whatsapptemplatehttp.CreateTemplateRequest{
		BusinessPhoneID:  "phone-ws1",
		Name:             "promo",
		Category:         "MARKETING",
		ExitConfirmation: "Pronto.",
	}, userClaims("user-1"), "ws-1")
	rr := httptest.NewRecorder()
	h.CreateForWorkspace(rr, req)

	if rr.Code != http.StatusUnprocessableEntity || !strings.Contains(rr.Body.String(), "marketing_template_needs_exit") {
		t.Fatalf("got %d: %s", rr.Code, rr.Body.String())
	}
	if creator.got.ExitConfirmation != "Pronto." || creator.got.Template.Name != "promo" {
		t.Fatalf("request = %+v", creator.got)
	}
}

func TestCreateForWorkspaceWithoutTheExitGuardIsUnavailable(t *testing.T) {
	h := newTemplateTestHandler()
	h.SetExitGuardedCreator(nil)
	req := requestWithClaims(http.MethodPost, "/whatsapp/templates", whatsapptemplatehttp.CreateTemplateRequest{
		BusinessPhoneID: "phone-ws1", Name: "promo", Category: "MARKETING",
	}, userClaims("user-1"), "ws-1")
	rr := httptest.NewRecorder()
	h.CreateForWorkspace(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("got %d: %s", rr.Code, rr.Body.String())
	}
}
