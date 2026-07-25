package customfield

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	customfielddomain "vozko/domain/customfield"
	"vozko/infra/http/middleware"
	customfield_repository "vozko/infra/repositories/customfield"
	customfield_usecase "vozko/usecases/customfield"
)

type CustomFieldHandler struct {
	svc *customfield_usecase.Service
}

func NewCustomFieldHandler(svc *customfield_usecase.Service) *CustomFieldHandler {
	return &CustomFieldHandler{svc: svc}
}

// @Summary		Criar campo personalizado
// @Description	Cria uma definição de campo personalizado para um objeto do CRM (oportunidade, conversa ou lead). A chave, o rótulo e o tipo são obrigatórios.
// @Tags			Campos Personalizados
// @Accept			json
// @Produce		json
// @Param			request	body		CreateCustomFieldRequest	true	"Definição do campo a criar"
// @Success		201	{object}	customfield.Definition
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/custom-fields [post]
func (h *CustomFieldHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateCustomFieldRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"objectType": "string (required: 'opportunity' | 'conversation' | 'lead')",
			"key":        "string (required, stable machine key)",
			"label":      "string (required)",
			"type":       "string (required: text|number|date|boolean|select|multiselect)",
		})
		return
	}
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	created, err := h.svc.Create(wsID, customfield_usecase.CreateInput{
		ObjectType: strings.TrimSpace(req.ObjectType),
		Key:        strings.TrimSpace(req.Key),
		Label:      strings.TrimSpace(req.Label),
		Type:       customfielddomain.FieldType(strings.TrimSpace(req.Type)),
		Options:    req.Options,
		Required:   req.Required,
		Position:   req.Position,
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, created)
}

// @Summary		Atualizar campo personalizado
// @Description	Atualiza o rótulo, o tipo, as opções e os demais atributos de uma definição de campo personalizado existente. Todos os campos são opcionais.
// @Tags			Campos Personalizados
// @Accept			json
// @Produce		json
// @Param			id		path		string						true	"ID do campo personalizado"
// @Param			request	body		UpdateCustomFieldRequest	true	"Campos a atualizar"
// @Success		200	{object}	customfield.Definition
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/custom-fields/{id} [patch]
func (h *CustomFieldHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req UpdateCustomFieldRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"label": "string (optional)",
		})
		return
	}
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	in := customfield_usecase.UpdateInput{
		Label:    req.Label,
		Options:  req.Options,
		Required: req.Required,
		Position: req.Position,
	}
	if req.Type != nil {
		t := customfielddomain.FieldType(strings.TrimSpace(*req.Type))
		in.Type = &t
	}

	updated, err := h.svc.Update(wsID, id, in)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, updated)
}

// @Summary		Remover campo personalizado
// @Description	Exclui uma definição de campo personalizado do workspace.
// @Tags			Campos Personalizados
// @Produce		json
// @Param			id	path	string	true	"ID do campo personalizado"
// @Success		204	"Campo removido"
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/custom-fields/{id} [delete]
func (h *CustomFieldHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	if err := h.svc.Delete(wsID, id); err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusNoContent, nil)
}

// @Summary		Obter campo personalizado
// @Description	Retorna a definição de um campo personalizado do workspace pelo seu ID.
// @Tags			Campos Personalizados
// @Produce		json
// @Param			id	path		string	true	"ID do campo personalizado"
// @Success		200	{object}	customfield.Definition
// @Failure		401	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/custom-fields/{id} [get]
func (h *CustomFieldHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	d, err := h.svc.Get(wsID, id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, d)
}

// @Summary		Listar campos personalizados
// @Description	Retorna as definições de campos personalizados do workspace para o tipo de objeto informado (oportunidade por padrão).
// @Tags			Campos Personalizados
// @Produce		json
// @Param			objectType	query	string	false	"Tipo de objeto ('opportunity', 'conversation' ou 'lead')"
// @Success		200	{array}		customfield.Definition
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/custom-fields [get]
func (h *CustomFieldHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)
	objectType := strings.TrimSpace(r.URL.Query().Get("objectType"))
	if objectType == "" {
		objectType = "opportunity"
	}

	list, err := h.svc.ListByObject(wsID, objectType)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, list)
}

func (h *CustomFieldHandler) handleDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, customfield_repository.ErrNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, customfield_usecase.ErrKeyExists):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, customfielddomain.ErrWorkspaceRequired),
		errors.Is(err, customfielddomain.ErrKeyRequired),
		errors.Is(err, customfielddomain.ErrLabelRequired),
		errors.Is(err, customfielddomain.ErrInvalidType),
		errors.Is(err, customfielddomain.ErrOptionsRequired):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}
