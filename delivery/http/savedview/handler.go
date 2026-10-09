package savedview

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/conversation"
	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	"vozko/domain/lead"
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
// @Description	Retorna as visões salvas do usuário para o tipo de objeto informado (conversa por padrão), usadas para configurar o quadro do CRM. Uma visão de lead compartilhada por outra pessoa só aparece quando o filtro dela vale para quem pede: se ele usa um campo sensível sem `leads:read_sensitive`, um campo de endereço completo sem `leads:read_addresses` ou um campo que não existe mais, a visão fica de fora da lista, para que os valores do filtro nunca cheguem a quem não pode lê-los. A permissão vem do objeto da visão: visões de lead pedem `leads:read`; visões de conversa e de oportunidade seguem `conversations:read`, `conversations:create`, `conversations:update` e `conversations:delete`. Sem a permissão responde 403 (`saved_view_forbidden`).
// @Tags			Visões Salvas
// @Produce		json
// @Param			objectType	query	string	false	"Tipo de objeto ('conversation', 'opportunity' ou 'lead')"
// @Success		200	{array}		savedview.SavedView
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/saved-views [get]
func (h *SavedViewHandler) List(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requestActor(w, r)
	if !ok {
		return
	}
	objectType := savedviewdomain.ObjectType(strings.TrimSpace(r.URL.Query().Get("objectType")))
	if objectType == "" {
		objectType = savedviewdomain.ObjectConversation
	}

	views, err := h.listUseCase.Execute(a, objectType)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, views)
}

// @Summary		Criar uma visão salva
// @Description	Cria uma nova visão salva com filtros, agrupamento e ordenação para o quadro do CRM. O nome e o tipo de objeto são obrigatórios. A permissão vem do objeto da visão: visões de lead pedem `leads:read`; visões de conversa e de oportunidade seguem `conversations:read`, `conversations:create`, `conversations:update` e `conversations:delete`. Sem a permissão responde 403 (`saved_view_forbidden`). O filtro de uma visão de lead é validado para quem salva: um campo desconhecido ou um valor impossível responde 400 (`lead_filter_invalid` ou `custom_field_filter_*`) e um campo sensível sem `leads:read_sensitive` responde 403 (`custom_field_filter_sensitive_forbidden`) e um filtro por CEP, precisão ou status do mapa ou área sem `leads:read_addresses` responde 403 (`lead_filter_address_forbidden`). As colunas escolhidas (`columns`) ficam salvas na visão.
// @Tags			Visões Salvas
// @Accept			json
// @Produce		json
// @Param			request	body		SavedViewRequest	true	"Dados da visão a criar"
// @Success		201	{object}	savedview.SavedView
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/saved-views [post]
func (h *SavedViewHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req SavedViewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"name":       "string (required)",
			"objectType": "string (required: 'conversation' | 'opportunity' | 'lead')",
			"filter":     "object (crmfilter)",
		})
		return
	}
	a, ok := h.requestActor(w, r)
	if !ok {
		return
	}
	created, err := h.createUseCase.Execute(a, req.toEntity())
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, created)
}

// @Summary		Atualizar uma visão salva
// @Description	Atualiza os filtros, o agrupamento, a ordenação e os demais atributos de uma visão salva existente. A permissão vem do objeto da visão: visões de lead pedem `leads:read`; visões de conversa e de oportunidade seguem `conversations:read`, `conversations:create`, `conversations:update` e `conversations:delete`. Sem a permissão responde 403 (`saved_view_forbidden`). O filtro de uma visão de lead é validado para quem salva: um campo desconhecido ou um valor impossível responde 400 (`lead_filter_invalid` ou `custom_field_filter_*`) e um campo sensível sem `leads:read_sensitive` responde 403 (`custom_field_filter_sensitive_forbidden`) e um filtro por CEP, precisão ou status do mapa ou área sem `leads:read_addresses` responde 403 (`lead_filter_address_forbidden`). As colunas escolhidas (`columns`) ficam salvas na visão.
// @Tags			Visões Salvas
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"ID da visão salva"
// @Param			request	body		SavedViewRequest	true	"Campos da visão a atualizar"
// @Success		200	{object}	savedview.SavedView
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
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
	a, ok := h.requestActor(w, r)
	if !ok {
		return
	}
	updated, err := h.updateUseCase.Execute(a, id, req.toEntity())
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, updated)
}

// @Summary		Remover uma visão salva
// @Description	Exclui uma visão salva do usuário. A permissão vem do objeto da visão: visões de lead pedem `leads:read`; visões de conversa e de oportunidade seguem `conversations:read`, `conversations:create`, `conversations:update` e `conversations:delete`. Sem a permissão responde 403 (`saved_view_forbidden`).
// @Tags			Visões Salvas
// @Produce		json
// @Param			id	path	string	true	"ID da visão salva"
// @Success		204	"Visão removida"
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/saved-views/{id} [delete]
func (h *SavedViewHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	a, ok := h.requestActor(w, r)
	if !ok {
		return
	}
	if err := h.deleteUseCase.Execute(a, id); err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusNoContent, nil)
}

// @Summary		Definir visão padrão
// @Description	Marca a visão salva informada como a visão padrão do usuário para aquele tipo de objeto. A permissão vem do objeto da visão: visões de lead pedem `leads:read`; visões de conversa e de oportunidade seguem `conversations:read`, `conversations:create`, `conversations:update` e `conversations:delete`. Sem a permissão responde 403 (`saved_view_forbidden`).
// @Tags			Visões Salvas
// @Produce		json
// @Param			id	path		string	true	"ID da visão salva"
// @Success		200	{object}	savedview.SavedView
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/saved-views/{id}/default [put]
func (h *SavedViewHandler) SetDefault(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	a, ok := h.requestActor(w, r)
	if !ok {
		return
	}
	updated, err := h.setDefaultUseCase.Execute(a, id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, updated)
}

func (h *SavedViewHandler) handleDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, savedviewdomain.ErrForbidden):
		response.WriteErrorWithCode(w, http.StatusForbidden, CodeForbidden, err.Error(), nil)
	case errors.Is(err, customfield.ErrFilterSensitive), errors.Is(err, lead.ErrLeadForbidden), errors.Is(err, lead.ErrLeadFilterAddressForbidden):
		response.WriteErrorWithCode(w, http.StatusForbidden, lead.ErrorCode(err), err.Error(), nil)
	case errors.Is(err, lead.ErrLeadFilterInvalid), errors.Is(err, customfield.ErrFilterUnknownKey), errors.Is(err, customfield.ErrFilterOperator), errors.Is(err, customfield.ErrFilterValue):
		response.WriteErrorWithCode(w, http.StatusBadRequest, lead.ErrorCode(err), err.Error(), nil)
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
		errors.Is(err, crmfilter.ErrMissingCustomKey),
		errors.Is(err, crmfilter.ErrInvalidValue),
		errors.Is(err, crmfilter.ErrTooManyValues):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}

const CodeForbidden = "saved_view_forbidden"

func (h *SavedViewHandler) requestActor(w http.ResponseWriter, r *http.Request) (conversation.Viewer, bool) {
	claims := middleware.GetClaims(r)
	if claims == nil || strings.TrimSpace(claims.UserID) == "" {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return conversation.Viewer{}, false
	}
	a, ok := httpx.WorkspaceActor(r)
	if !ok {
		h.handleDomainError(w, savedviewdomain.ErrWorkspaceRequired)
	}
	return a, ok
}
