package whatsapptemplate

import (
	"context"
	"net/http"

	"vozko/delivery/http/response"
	whatsapptemplatedomain "vozko/domain/whatsapp/template"
	"vozko/infra/http/middleware"
)

const codeTrackingUnavailable = "template_tracking_unavailable"

type ButtonTracking interface {
	Apply(ctx context.Context, workspaceID, userID string, components []whatsapptemplatedomain.TemplateComponent, tracked []int) ([]whatsapptemplatedomain.TemplateComponent, error)
}

func (h *WhatsAppTemplateHandler) SetClickTracking(tracking ButtonTracking, linkBase string) {
	h.tracking = tracking
	h.linkBase = linkBase
}

func (h *WhatsAppTemplateHandler) trackButtons(w http.ResponseWriter, r *http.Request, userID string, components []whatsapptemplatedomain.TemplateComponent, tracked []int) ([]whatsapptemplatedomain.TemplateComponent, bool) {
	if tracked == nil {
		return components, true
	}
	if h.tracking == nil {
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, codeTrackingUnavailable, "O rastreio de cliques não está disponível agora", nil)
		return nil, false
	}
	out, err := h.tracking.Apply(r.Context(), middleware.GetWorkspaceID(r), userID, components, tracked)
	if err != nil {
		writeTemplateError(w, err)
		return nil, false
	}
	return out, true
}
