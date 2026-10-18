package whatsapptemplate

import (
	"context"
	"net/http"

	"vozko/delivery/http/httpx"
	"vozko/domain/buttonaction"
	whatsapptemplatedomain "vozko/domain/whatsapp/template"
	"vozko/infra/http/middleware"
	buttonaction_usecase "vozko/usecases/buttonaction"
)

type ExitGuardedCreator interface {
	CreateTemplate(ctx context.Context, a buttonaction_usecase.Actor, req buttonaction_usecase.CreateTemplateRequest) (*whatsapptemplatedomain.CreateTemplateOutput, error)
}

func (h *WhatsAppTemplateHandler) SetExitGuardedCreator(creator ExitGuardedCreator) {
	h.exitGuardedCreator = creator
}

func (h *WhatsAppTemplateHandler) createGuarded(r *http.Request, req CreateTemplateRequest, input whatsapptemplatedomain.CreateTemplateInput) (*whatsapptemplatedomain.CreateTemplateOutput, error) {
	if h.exitGuardedCreator == nil {
		return nil, buttonaction.ErrUnavailable
	}
	actor, ok := guardActor(r)
	if !ok {
		return nil, buttonaction.ErrActorRequired
	}
	return h.exitGuardedCreator.CreateTemplate(r.Context(), actor, buttonaction_usecase.CreateTemplateRequest{
		Template:         input,
		Actions:          httpx.ButtonActionInputs(req.ButtonActions),
		ExitConfirmation: req.ExitConfirmation,
	})
}

func guardActor(r *http.Request) (buttonaction_usecase.Actor, bool) {
	viewer, ok := httpx.WorkspaceActor(r)
	if !ok {
		return buttonaction_usecase.Actor{}, false
	}
	return buttonaction_usecase.Actor{
		WorkspaceID: viewer.WorkspaceID, UserID: viewer.UserID, IsAdmin: viewer.IsAdmin, Departments: middleware.GetDepartmentFilter(r),
	}, true
}
