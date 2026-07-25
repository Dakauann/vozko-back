package buildersession

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/infra/http/middleware"
	workflow_usecase "vozko/usecases/workflow"
)

type BuilderSessionHandler struct {
	service *workflow_usecase.BuilderSessionService
}

func NewBuilderSessionHandler(service *workflow_usecase.BuilderSessionService) *BuilderSessionHandler {
	return &BuilderSessionHandler{service: service}
}

// @Summary		Listar conversas do copiloto de um fluxo
// @Description	Retorna as conversas anteriores com o copiloto de IA vinculadas a um fluxo específico, permitindo revisitar o histórico de construção.
// @Tags			Copiloto de fluxos
// @Produce		json
// @Param			id	path		string	true	"ID do fluxo"
// @Success		200	{array}		BuilderSessionResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/workflows/{id}/builder-sessions [get]
func (h *BuilderSessionHandler) ListByWorkflow(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.workspace(w, r)
	if !ok {
		return
	}
	req := ListByWorkflowRequest{WorkflowID: mux.Vars(r)["id"]}
	if errs := req.Validate(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}
	sessions, err := h.service.ListByWorkflow(r.Context(), workspaceID, req.WorkflowID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Erro ao listar conversas", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toBuilderSessionResponses(sessions))
}

// @Summary		Listar conversas do copiloto do workspace
// @Description	Retorna as conversas mais recentes com o copiloto de IA em todo o workspace, independentemente do fluxo.
// @Tags			Copiloto de fluxos
// @Produce		json
// @Success		200	{array}		BuilderSessionResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/builder-sessions [get]
func (h *BuilderSessionHandler) ListByWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.workspace(w, r)
	if !ok {
		return
	}
	sessions, err := h.service.ListByWorkspace(r.Context(), workspaceID, 50)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Erro ao listar conversas", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toBuilderSessionResponses(sessions))
}

// @Summary		Carregar uma conversa do copiloto
// @Description	Retorna uma conversa específica com o copiloto de IA, incluindo todas as mensagens e chamadas de ferramenta registradas.
// @Tags			Copiloto de fluxos
// @Produce		json
// @Param			sessionId	path		string	true	"ID da conversa"
// @Success		200			{object}	BuilderSessionResponse
// @Failure		401			{object}	response.ErrorResponse
// @Failure		404			{object}	response.ErrorResponse
// @Failure		500			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/builder-sessions/{sessionId} [get]
func (h *BuilderSessionHandler) Get(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := h.workspace(w, r)
	if !ok {
		return
	}
	req := GetSessionRequest{SessionID: mux.Vars(r)["sessionId"]}
	if errs := req.Validate(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}
	session, err := h.service.Get(r.Context(), workspaceID, req.SessionID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Erro ao carregar conversa", nil)
		return
	}
	if session == nil {
		response.WriteError(w, http.StatusNotFound, "Conversa não encontrada", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toBuilderSessionResponse(session))
}

func (h *BuilderSessionHandler) workspace(w http.ResponseWriter, r *http.Request) (string, bool) {
	if h == nil || h.service == nil {
		response.WriteError(w, http.StatusNotImplemented, "Histórico de conversas indisponível", nil)
		return "", false
	}
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusUnauthorized, "Workspace não informado", nil)
		return "", false
	}
	return workspaceID, true
}
