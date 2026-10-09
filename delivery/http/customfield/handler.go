package customfield

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/conversation"
	customfielddomain "vozko/domain/customfield"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
	customfield_usecase "vozko/usecases/customfield"
)

type Permissions interface {
	HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool
}

type CustomFieldHandler struct {
	svc         *customfield_usecase.Service
	permissions Permissions
}

func NewCustomFieldHandler(svc *customfield_usecase.Service, permissions Permissions) *CustomFieldHandler {
	return &CustomFieldHandler{svc: svc, permissions: permissions}
}

func (h *CustomFieldHandler) viewerOf(userID, workspaceID string, isAdmin bool) customfielddomain.Viewer {
	if h.permissions == nil || strings.TrimSpace(userID) == "" {
		return customfielddomain.Viewer{}
	}
	return customfielddomain.Viewer{ReadsSensitive: h.permissions.HasWorkspacePermission(
		userID, workspaceID,
		string(workspace_domain.ResourceLeads), string(workspace_domain.ActionReadSensitive),
		isAdmin,
	)}
}

// @Summary		Criar campo personalizado
// @Description	Cria uma definição de campo personalizado para um objeto do CRM (oportunidade ou lead). A chave, o rótulo e o tipo são obrigatórios. Um campo de lead exige a escolha explícita de 'sensitive'; só campos de lead podem ser sensíveis (400 para outro objeto); um campo sensível exige 'legalBasis' (base legal da LGPD) e só é lido por quem pode ver dados sensíveis. 'optionTones' associa cada opção de um campo de seleção a um tom (chart-1 a chart-5 ou neutral). 'role' = classification marca o campo de classificação, no máximo um por objeto e sempre do tipo select. Responde 409 quando a chave ou o papel já existem no objeto. A chave de um campo sensível removido continua protegida: criar outro campo com ela, sensível ou não, pede `leads:read_sensitive`, porque os valores antigos continuam guardados nos leads. Toda recusa traz 'code' (custom_field_*), por exemplo custom_field_sensitivity_choice_missing, custom_field_legal_basis_required, custom_field_key_exists ou custom_field_role_taken. A permissão vem do objeto do campo: campos de lead pedem `leads:read` para ler e `leads:configure` para criar, editar e remover, e um campo sensível (antes ou depois da mudança) pede também `leads:read_sensitive` para ser criado, editado, desmarcado ou removido; campos de oportunidade seguem `conversations:read`, `conversations:create`, `conversations:update` e `conversations:delete`. Sem a permissão responde 403 (`custom_field_forbidden`).
// @Tags			Campos Personalizados
// @Accept			json
// @Produce		json
// @Param			request	body		CreateCustomFieldRequest	true	"Definição do campo a criar"
// @Success		201	{object}	customfield.Definition
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/custom-fields [post]
func (h *CustomFieldHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateCustomFieldRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"objectType": "string (required: 'opportunity' | 'lead')",
			"key":        "string (required, stable machine key)",
			"label":      "string (required)",
			"type":       "string (required: text|number|date|boolean|select|multiselect)",
		})
		return
	}
	a, ok := h.requestActor(w, r)
	if !ok {
		return
	}
	created, err := h.svc.Create(a, createInputFrom(req))
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, created)
}

// @Summary		Atualizar campo personalizado
// @Description	Atualiza o rótulo, o tipo, as opções e os demais atributos de uma definição de campo personalizado existente. Todos os campos são opcionais. Ao trocar as opções sem enviar 'optionTones', os tons das opções removidas são descartados; um objeto vazio em 'optionTones' remove todos os tons e 'role' vazio remove o papel. Desligar 'sensitive' apaga a base legal; só campos de lead podem ser sensíveis. Responde 409 quando o papel já pertence a outro campo do objeto. Toda recusa traz 'code' (custom_field_*), por exemplo custom_field_not_found ou custom_field_role_taken. A permissão vem do objeto do campo: campos de lead pedem `leads:read` para ler e `leads:configure` para criar, editar e remover, e um campo sensível (antes ou depois da mudança) pede também `leads:read_sensitive` para ser criado, editado, desmarcado ou removido; campos de oportunidade seguem `conversations:read`, `conversations:create`, `conversations:update` e `conversations:delete`. Sem a permissão responde 403 (`custom_field_forbidden`).
// @Tags			Campos Personalizados
// @Accept			json
// @Produce		json
// @Param			id		path		string						true	"ID do campo personalizado"
// @Param			request	body		UpdateCustomFieldRequest	true	"Campos a atualizar"
// @Success		200	{object}	customfield.Definition
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
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
	a, ok := h.requestActor(w, r)
	if !ok {
		return
	}
	updated, err := h.svc.Update(a, id, updateInputFrom(req))
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, updated)
}

// @Summary		Remover campo personalizado
// @Description	Exclui uma definição de campo personalizado do workspace. Os valores guardados nos leads ficam ocultos; a chave de um campo sensível removido só volta a ser usada por quem tem `leads:read_sensitive`. A permissão vem do objeto do campo: campos de lead pedem `leads:read` para ler e `leads:configure` para criar, editar e remover, e um campo sensível (antes ou depois da mudança) pede também `leads:read_sensitive` para ser criado, editado, desmarcado ou removido; campos de oportunidade seguem `conversations:read`, `conversations:create`, `conversations:update` e `conversations:delete`. Sem a permissão responde 403 (`custom_field_forbidden`).
// @Tags			Campos Personalizados
// @Produce		json
// @Param			id	path	string	true	"ID do campo personalizado"
// @Success		204	"Campo removido"
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/custom-fields/{id} [delete]
func (h *CustomFieldHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	a, ok := h.requestActor(w, r)
	if !ok {
		return
	}
	if err := h.svc.Delete(a, id); err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusNoContent, nil)
}

// @Summary		Obter campo personalizado
// @Description	Retorna a definição de um campo personalizado do workspace pelo seu ID. A permissão vem do objeto do campo: campos de lead pedem `leads:read` para ler e `leads:configure` para criar, editar e remover, e um campo sensível (antes ou depois da mudança) pede também `leads:read_sensitive` para ser criado, editado, desmarcado ou removido; campos de oportunidade seguem `conversations:read`, `conversations:create`, `conversations:update` e `conversations:delete`. Sem a permissão responde 403 (`custom_field_forbidden`).
// @Tags			Campos Personalizados
// @Produce		json
// @Param			id	path		string	true	"ID do campo personalizado"
// @Success		200	{object}	customfield.Definition
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/custom-fields/{id} [get]
func (h *CustomFieldHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	a, ok := h.requestActor(w, r)
	if !ok {
		return
	}
	d, err := h.svc.Get(a, id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, d)
}

// @Summary		Listar campos personalizados
// @Description	Retorna as definições de campos personalizados do workspace para o tipo de objeto informado (oportunidade por padrão). Cada definição traz `readable`, o veredito do servidor sobre quem pede: um campo sensível só é legível com `leads:read_sensitive`. Os valores de um campo não legível nunca saem do servidor; a definição continua listada para que a tela conte quantos campos estão ocultos. A permissão vem do objeto do campo: campos de lead pedem `leads:read` para ler e `leads:configure` para criar, editar e remover, e um campo sensível (antes ou depois da mudança) pede também `leads:read_sensitive` para ser criado, editado, desmarcado ou removido; campos de oportunidade seguem `conversations:read`, `conversations:create`, `conversations:update` e `conversations:delete`. Sem a permissão responde 403 (`custom_field_forbidden`).
// @Tags			Campos Personalizados
// @Produce		json
// @Param			objectType	query	string	false	"Tipo de objeto ('opportunity' ou 'lead')"	Enums(opportunity, lead)
// @Success		200	{array}		customfield.ViewerDefinition
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/custom-fields [get]
func (h *CustomFieldHandler) List(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requestActor(w, r)
	if !ok {
		return
	}
	objectType := strings.TrimSpace(r.URL.Query().Get("objectType"))
	if objectType == "" {
		objectType = "opportunity"
	}

	list, err := h.svc.ListByObject(a, customfielddomain.ObjectType(strings.ToLower(objectType)))
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	viewer := h.viewerOf(a.UserID, a.WorkspaceID, a.IsAdmin)
	response.WriteSuccess(w, http.StatusOK, customfielddomain.ProjectFor(list, viewer))
}

func (h *CustomFieldHandler) handleDomainError(w http.ResponseWriter, err error) {
	code := customfielddomain.ErrorCode(err)
	switch {
	case errors.Is(err, customfielddomain.ErrForbidden):
		response.WriteErrorWithCode(w, http.StatusForbidden, code, err.Error(), nil)
	case errors.Is(err, customfielddomain.ErrNotFound):
		response.WriteErrorWithCode(w, http.StatusNotFound, code, err.Error(), nil)
	case errors.Is(err, customfielddomain.ErrKeyExists), errors.Is(err, customfielddomain.ErrRoleTaken):
		response.WriteErrorWithCode(w, http.StatusConflict, code, err.Error(), nil)
	case errors.Is(err, customfielddomain.ErrInvalidDefinition):
		response.WriteErrorWithCode(w, http.StatusBadRequest, code, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}

func createInputFrom(req CreateCustomFieldRequest) customfield_usecase.CreateInput {
	return customfield_usecase.CreateInput{
		ObjectType:  customfielddomain.ObjectType(strings.ToLower(strings.TrimSpace(req.ObjectType))),
		Key:         strings.TrimSpace(req.Key),
		Label:       strings.TrimSpace(req.Label),
		Type:        customfielddomain.FieldType(strings.TrimSpace(req.Type)),
		Options:     req.Options,
		OptionTones: tonesFrom(req.OptionTones),
		Required:    req.Required,
		Sensitive:   req.Sensitive,
		LegalBasis:  req.LegalBasis,
		Role:        customfielddomain.Role(strings.TrimSpace(req.Role)),
		Position:    req.Position,
	}
}

func updateInputFrom(req UpdateCustomFieldRequest) customfield_usecase.UpdateInput {
	in := customfield_usecase.UpdateInput{
		Label:       req.Label,
		Options:     req.Options,
		OptionTones: tonesFrom(req.OptionTones),
		Required:    req.Required,
		Sensitive:   req.Sensitive,
		LegalBasis:  req.LegalBasis,
		Position:    req.Position,
	}
	if req.Type != nil {
		t := customfielddomain.FieldType(strings.TrimSpace(*req.Type))
		in.Type = &t
	}
	if req.Role != nil {
		role := customfielddomain.Role(strings.TrimSpace(*req.Role))
		in.Role = &role
	}
	return in
}

func tonesFrom(raw map[string]string) map[string]customfielddomain.Tone {
	if raw == nil {
		return nil
	}
	tones := make(map[string]customfielddomain.Tone, len(raw))
	for option, tone := range raw {
		tones[option] = customfielddomain.Tone(strings.TrimSpace(tone))
	}
	return tones
}

func (h *CustomFieldHandler) requestActor(w http.ResponseWriter, r *http.Request) (conversation.Viewer, bool) {
	claims := middleware.GetClaims(r)
	if claims == nil || strings.TrimSpace(claims.UserID) == "" {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return conversation.Viewer{}, false
	}
	a, ok := httpx.WorkspaceActor(r)
	if !ok {
		h.handleDomainError(w, customfielddomain.ErrWorkspaceRequired)
	}
	return a, ok
}
