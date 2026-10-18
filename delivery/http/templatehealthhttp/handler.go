package templatehealthhttp

import (
	"errors"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	domain "vozko/domain/whatsapp/template"
	"vozko/infra/http/middleware"
	template_usecase "vozko/usecases/whatsapp/template"
)

type Reader interface {
	Read(workspaceID, templateID string) (template_usecase.TemplateHealth, error)
}

type Handler struct {
	reader Reader
}

func NewHandler(reader Reader) *Handler {
	return &Handler{reader: reader}
}

// @Summary		Saúde do modelo de mensagem
// @Description	Mostra a qualidade que a Meta deu ao modelo (UNKNOWN enquanto a Meta ainda não avaliou), se ele está sinalizado e as mudanças recentes de qualidade, status e categoria. Exige acesso do workspace ao modelo.
// @Tags			Modelos de WhatsApp
// @Produce		json
// @Param			id	path	string	true	"ID do modelo"
// @Success		200	{object}	templatehealthhttp.Response
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/templates/{id}/health [get]
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	health, err := h.reader.Read(middleware.GetWorkspaceID(r), mux.Vars(r)["id"])
	switch {
	case errors.Is(err, domain.ErrTemplateAccessDenied):
		response.WriteError(w, http.StatusForbidden, "You don't have access to this template", nil)
	case errors.Is(err, domain.ErrTemplateNotFound):
		response.WriteError(w, http.StatusNotFound, "Template not found", nil)
	case err != nil:
		response.WriteError(w, http.StatusInternalServerError, "Failed to read the template health", nil)
	default:
		response.WriteSuccess(w, http.StatusOK, toResponse(health))
	}
}
