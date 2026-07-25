package pipeline

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	pipelinedomain "vozko/domain/pipeline"
	"vozko/infra/http/middleware"
)

type PipelineHandler struct {
	createUseCase pipelinedomain.CreatePipelineUseCase
	updateUseCase pipelinedomain.UpdatePipelineUseCase
	deleteUseCase pipelinedomain.DeletePipelineUseCase
	listUseCase   pipelinedomain.ListPipelinesUseCase
	getUseCase    pipelinedomain.GetPipelineUseCase
}

func NewPipelineHandler(
	createUC pipelinedomain.CreatePipelineUseCase,
	updateUC pipelinedomain.UpdatePipelineUseCase,
	deleteUC pipelinedomain.DeletePipelineUseCase,
	listUC pipelinedomain.ListPipelinesUseCase,
	getUC pipelinedomain.GetPipelineUseCase,
) *PipelineHandler {
	return &PipelineHandler{
		createUseCase: createUC,
		updateUseCase: updateUC,
		deleteUseCase: deleteUC,
		listUseCase:   listUC,
		getUseCase:    getUC,
	}
}

// @Summary		Listar funis
// @Description	Retorna os funis do workspace, opcionalmente filtrados pelo tipo de objeto (conversa ou oportunidade).
// @Tags			Funis
// @Produce		json
// @Param			objectType	query		string	false	"Tipo de objeto do funil ('conversation' | 'opportunity')"
// @Success		200	{array}		PipelineResponse
// @Failure		401	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/pipelines [get]
func (h *PipelineHandler) List(w http.ResponseWriter, r *http.Request) {
	if middleware.GetClaims(r) == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)
	objectType := r.URL.Query().Get("objectType")

	pipelines, err := h.listUseCase.Execute(wsID, objectType)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toPipelineResponses(pipelines))
}

// @Summary		Consultar um funil
// @Description	Retorna os detalhes de um funil do workspace a partir do seu identificador.
// @Tags			Funis
// @Produce		json
// @Param			id	path	string	true	"Identificador do funil"
// @Success		200	{object}	PipelineResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/pipelines/{id} [get]
func (h *PipelineHandler) Get(w http.ResponseWriter, r *http.Request) {
	if middleware.GetClaims(r) == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)
	id := mux.Vars(r)["id"]

	p, err := h.getUseCase.Execute(wsID, id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toPipelineResponse(p))
}

// @Summary		Criar um funil
// @Description	Cria um novo funil no workspace para organizar conversas ou oportunidades.
// @Tags			Funis
// @Accept			json
// @Produce		json
// @Param			body	body		CreatePipelineRequest	true	"Dados do funil"
// @Success		201	{object}	PipelineResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/pipelines [post]
func (h *PipelineHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreatePipelineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"name":       "string (required)",
			"objectType": "string (optional: 'conversation' | 'opportunity')",
		})
		return
	}
	if middleware.GetClaims(r) == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	created, err := h.createUseCase.Execute(wsID, pipelinedomain.CreatePipelineInput{
		Name:         strings.TrimSpace(req.Name),
		ObjectType:   strings.TrimSpace(req.ObjectType),
		DepartmentID: strings.TrimSpace(req.DepartmentID),
		Position:     req.Position,
		IsDefault:    req.IsDefault,
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, toPipelineResponse(created))
}

// @Summary		Atualizar um funil
// @Description	Atualiza os dados de um funil existente. Apenas os campos enviados são alterados.
// @Tags			Funis
// @Accept			json
// @Produce		json
// @Param			id		path		string					true	"Identificador do funil"
// @Param			body	body		UpdatePipelineRequest	true	"Campos a atualizar"
// @Success		200	{object}	PipelineResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/pipelines/{id} [put]
func (h *PipelineHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req UpdatePipelineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"name": "string (optional)",
		})
		return
	}
	if middleware.GetClaims(r) == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	updated, err := h.updateUseCase.Execute(wsID, id, pipelinedomain.UpdatePipelineInput{
		Name:         req.Name,
		DepartmentID: req.DepartmentID,
		Position:     req.Position,
		IsDefault:    req.IsDefault,
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toPipelineResponse(updated))
}

// @Summary		Excluir um funil
// @Description	Remove um funil do workspace a partir do seu identificador.
// @Tags			Funis
// @Param			id	path	string	true	"Identificador do funil"
// @Success		204	"Funil excluído com sucesso"
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/pipelines/{id} [delete]
func (h *PipelineHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	if middleware.GetClaims(r) == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	if err := h.deleteUseCase.Execute(wsID, id); err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusNoContent, nil)
}

func (h *PipelineHandler) handleDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pipelinedomain.ErrNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, pipelinedomain.ErrNameRequired):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, pipelinedomain.ErrWorkspaceRequired):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, pipelinedomain.ErrInvalidObject):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, pipelinedomain.ErrUnauthorized):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}
