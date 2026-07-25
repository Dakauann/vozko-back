package savedview

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/crmfilter"
	savedviewdomain "vozko/domain/savedview"
	"vozko/infra/http/middleware"
)

type SavedViewHandler struct {
	createUseCase     savedviewdomain.CreateSavedViewUseCase
	updateUseCase     savedviewdomain.UpdateSavedViewUseCase
	deleteUseCase     savedviewdomain.DeleteSavedViewUseCase
	listUseCase       savedviewdomain.ListSavedViewsUseCase
	setDefaultUseCase savedviewdomain.SetDefaultSavedViewUseCase
}

func NewSavedViewHandler(
	createUC savedviewdomain.CreateSavedViewUseCase,
	updateUC savedviewdomain.UpdateSavedViewUseCase,
	deleteUC savedviewdomain.DeleteSavedViewUseCase,
	listUC savedviewdomain.ListSavedViewsUseCase,
	setDefaultUC savedviewdomain.SetDefaultSavedViewUseCase,
) *SavedViewHandler {
	return &SavedViewHandler{
		createUseCase:     createUC,
		updateUseCase:     updateUC,
		deleteUseCase:     deleteUC,
		listUseCase:       listUC,
		setDefaultUseCase: setDefaultUC,
	}
}

// @Summary		Listar visões salvas
// @Description	Retorna as visões salvas do usuário para o tipo de objeto informado (conversa por padrão), usadas para configurar o quadro do CRM.
// @Tags			Visões Salvas
// @Produce		json
// @Param			objectType	query	string	false	"Tipo de objeto ('conversation' ou 'opportunity')"
// @Success		200	{array}		savedview.SavedView
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/saved-views [get]
func (h *SavedViewHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)
	objectType := savedviewdomain.ObjectType(strings.TrimSpace(r.URL.Query().Get("objectType")))
	if objectType == "" {
		objectType = savedviewdomain.ObjectConversation
	}

	views, err := h.listUseCase.Execute(wsID, claims.UserID, objectType)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, views)
}

// @Summary		Criar uma visão salva
// @Description	Cria uma nova visão salva com filtros, agrupamento e ordenação para o quadro do CRM. O nome e o tipo de objeto são obrigatórios.
// @Tags			Visões Salvas
// @Accept			json
// @Produce		json
// @Param			request	body		SavedViewRequest	true	"Dados da visão a criar"
// @Success		201	{object}	savedview.SavedView
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/saved-views [post]
func (h *SavedViewHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req SavedViewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"name":       "string (required)",
			"objectType": "string (required: 'conversation' | 'opportunity')",
			"filter":     "object (crmfilter)",
		})
		return
	}
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	created, err := h.createUseCase.Execute(wsID, claims.UserID, req.toEntity())
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, created)
}

// @Summary		Atualizar uma visão salva
// @Description	Atualiza os filtros, o agrupamento, a ordenação e os demais atributos de uma visão salva existente.
// @Tags			Visões Salvas
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"ID da visão salva"
// @Param			request	body		SavedViewRequest	true	"Campos da visão a atualizar"
// @Success		200	{object}	savedview.SavedView
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/saved-views/{id} [put]
func (h *SavedViewHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req SavedViewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"name": "string (required)",
		})
		return
	}
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	updated, err := h.updateUseCase.Execute(wsID, claims.UserID, id, req.toEntity())
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, updated)
}

// @Summary		Remover uma visão salva
// @Description	Exclui uma visão salva do usuário.
// @Tags			Visões Salvas
// @Produce		json
// @Param			id	path	string	true	"ID da visão salva"
// @Success		204	"Visão removida"
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/saved-views/{id} [delete]
func (h *SavedViewHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	if err := h.deleteUseCase.Execute(wsID, claims.UserID, id); err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusNoContent, nil)
}

// @Summary		Definir visão padrão
// @Description	Marca a visão salva informada como a visão padrão do usuário para aquele tipo de objeto.
// @Tags			Visões Salvas
// @Produce		json
// @Param			id	path		string	true	"ID da visão salva"
// @Success		200	{object}	savedview.SavedView
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/saved-views/{id}/default [put]
func (h *SavedViewHandler) SetDefault(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	updated, err := h.setDefaultUseCase.Execute(wsID, claims.UserID, id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, updated)
}

func (h *SavedViewHandler) handleDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, savedviewdomain.ErrNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, savedviewdomain.ErrUnauthorized):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, savedviewdomain.ErrWorkspaceRequired),
		errors.Is(err, savedviewdomain.ErrNameRequired),
		errors.Is(err, savedviewdomain.ErrInvalidObject),
		errors.Is(err, savedviewdomain.ErrInvalidGroupBy),
		errors.Is(err, savedviewdomain.ErrGroupByKeyMissing),
		errors.Is(err, savedviewdomain.ErrInvalidVisibility),
		errors.Is(err, savedviewdomain.ErrInvalidSortDir),
		errors.Is(err, crmfilter.ErrUnknownField),
		errors.Is(err, crmfilter.ErrUnsupportedOp),
		errors.Is(err, crmfilter.ErrMissingValue),
		errors.Is(err, crmfilter.ErrBetweenValues),
		errors.Is(err, crmfilter.ErrInvalidNumber),
		errors.Is(err, crmfilter.ErrInvalidDate),
		errors.Is(err, crmfilter.ErrMissingCustomKey):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}
