package whatsapptemplate

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	whatsapptemplatedomain "vozko/domain/whatsapp/template"
	"vozko/domain/workspace_template_access"
	"vozko/infra/http/middleware"
)

type WhatsAppTemplateHandler struct {
	listUseCase                 whatsapptemplatedomain.ListUseCase
	getUseCase                  whatsapptemplatedomain.GetUseCase
	syncUseCase                 whatsapptemplatedomain.SyncTemplatesUseCase
	syncOneUseCase              whatsapptemplatedomain.SyncTemplateUseCase
	createUseCase               whatsapptemplatedomain.CreateTemplateUseCase
	replicateUseCase            whatsapptemplatedomain.ReplicateTemplateUseCase
	setHeaderMediaUC            whatsapptemplatedomain.SetTemplateHeaderMediaUseCase
	deleteUseCase               whatsapptemplatedomain.DeleteTemplateUseCase
	clientFactory               whatsapptemplatedomain.WhatsAppClientFactory
	workspaceTemplateAccessRepo workspace_template_access.Repository
	phoneRepo                   businessphone.Repository
}

func NewWhatsAppTemplateHandler(
	listUC whatsapptemplatedomain.ListUseCase,
	getUC whatsapptemplatedomain.GetUseCase,
	syncUC whatsapptemplatedomain.SyncTemplatesUseCase,
	syncOneUC whatsapptemplatedomain.SyncTemplateUseCase,
	createUC whatsapptemplatedomain.CreateTemplateUseCase,
	replicateUC whatsapptemplatedomain.ReplicateTemplateUseCase,
	setHeaderMediaUC whatsapptemplatedomain.SetTemplateHeaderMediaUseCase,
	deleteUC whatsapptemplatedomain.DeleteTemplateUseCase,
	clientFactory whatsapptemplatedomain.WhatsAppClientFactory,
	workspaceTemplateAccessRepo workspace_template_access.Repository,
	phoneRepo businessphone.Repository,
) *WhatsAppTemplateHandler {
	return &WhatsAppTemplateHandler{
		listUseCase:                 listUC,
		getUseCase:                  getUC,
		syncUseCase:                 syncUC,
		syncOneUseCase:              syncOneUC,
		createUseCase:               createUC,
		replicateUseCase:            replicateUC,
		setHeaderMediaUC:            setHeaderMediaUC,
		deleteUseCase:               deleteUC,
		clientFactory:               clientFactory,
		workspaceTemplateAccessRepo: workspaceTemplateAccessRepo,
		phoneRepo:                   phoneRepo,
	}
}

func (h *WhatsAppTemplateHandler) parseTemplateListInput(r *http.Request) (whatsapptemplatedomain.ListInput, error) {
	var req ListTemplatesRequest
	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return whatsapptemplatedomain.ListInput{}, err
		}
	} else {
		req.Status = r.URL.Query().Get("status")
		req.Category = r.URL.Query().Get("category")
		req.Language = r.URL.Query().Get("language")
		req.BusinessPhoneID = r.URL.Query().Get("businessPhoneId")
		req.WABAId = r.URL.Query().Get("wabaId")
		if v := r.URL.Query().Get("page"); v != "" {
			req.Page, _ = strconv.Atoi(v)
		}
		if v := r.URL.Query().Get("pageSize"); v != "" {
			req.PageSize, _ = strconv.Atoi(v)
		}
	}

	wabaID := req.WABAId
	if wabaID == "" && req.BusinessPhoneID != "" {
		resolved, err := h.clientFactory.WABAIdForPhone(req.BusinessPhoneID)
		if err != nil {
			return whatsapptemplatedomain.ListInput{}, err
		}
		wabaID = resolved
	}

	return whatsapptemplatedomain.ListInput{
		Status:   whatsapptemplatedomain.TemplateStatus(req.Status),
		Category: whatsapptemplatedomain.TemplateCategory(req.Category),
		Language: req.Language,
		Search:   r.URL.Query().Get("search"),
		WABAId:   wabaID,
		Options: shared.QueryOptions{
			Pagination: shared.Pagination{
				Page:     req.Page,
				PageSize: req.PageSize,
			},
		},
	}, nil
}

// @Summary		Listar modelos de mensagem
// @Description	Retorna, de forma paginada, os modelos (templates) de mensagem do WhatsApp aos quais o workspace tem acesso. Aceita filtros por status, categoria, idioma e telefone comercial.
// @Tags			Modelos de WhatsApp
// @Produce		json
// @Param			status			query	string	false	"Status do modelo (ex.: APPROVED, PENDING, REJECTED)"
// @Param			category		query	string	false	"Categoria do modelo (ex.: MARKETING, UTILITY, AUTHENTICATION)"
// @Param			language		query	string	false	"Idioma do modelo (ex.: pt_BR)"
// @Param			businessPhoneId	query	string	false	"ID do telefone comercial"
// @Param			wabaId			query	string	false	"ID da conta do WhatsApp Business (WABA)"
// @Param			search			query	string	false	"Termo de busca por nome"
// @Param			page			query	int		false	"Número da página (inicia em 1)"
// @Param			pageSize		query	int		false	"Quantidade de itens por página"
// @Success		200	{array}		template.Template
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/templates [get]
func (h *WhatsAppTemplateHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	input, err := h.parseTemplateListInput(r)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request: "+err.Error(), nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)
	templateIDs, err := h.workspaceTemplateAccessRepo.GetTemplateIDsForWorkspace(wsID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to check template access", nil)
		return
	}
	if len(templateIDs) == 0 {
		response.WritePaginated(w, http.StatusOK, []templateResponse{}, response.PaginationMeta{
			Page:       1,
			PageSize:   20,
			TotalPages: 0,
			TotalItems: 0,
		})
		return
	}
	input.TemplateIDs = templateIDs

	result, err := h.listUseCase.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list templates", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, toTemplateResponses(result.Items), response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

func (h *WhatsAppTemplateHandler) ListAll(w http.ResponseWriter, r *http.Request) {
	input, err := h.parseTemplateListInput(r)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request: "+err.Error(), nil)
		return
	}

	result, err := h.listUseCase.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list templates", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, toTemplateResponses(result.Items), response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

// @Summary		Consultar modelo de mensagem
// @Description	Retorna os detalhes de um modelo (template) de mensagem do WhatsApp, desde que o workspace tenha acesso a ele.
// @Tags			Modelos de WhatsApp
// @Produce		json
// @Param			id	path	string	true	"ID do modelo"
// @Success		200	{object}	template.Template
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/templates/{id} [get]
func (h *WhatsAppTemplateHandler) Get(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Template ID is required", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)
	hasAccess, err := h.workspaceTemplateAccessRepo.HasAccess(wsID, id)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to verify template access", nil)
		return
	}
	if !hasAccess {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this template", nil)
		return
	}

	tmpl, err := h.getUseCase.Execute(id)
	if err != nil {
		if errors.Is(err, whatsapptemplatedomain.ErrTemplateNotFound) {
			response.WriteError(w, http.StatusNotFound, "Template not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to get template", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toTemplateResponse(tmpl))
}

func (h *WhatsAppTemplateHandler) GetAdmin(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "Template ID is required", nil)
		return
	}

	tmpl, err := h.getUseCase.Execute(id)
	if err != nil {
		if errors.Is(err, whatsapptemplatedomain.ErrTemplateNotFound) {
			response.WriteError(w, http.StatusNotFound, "Template not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to get template", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toTemplateResponse(tmpl))
}

func (h *WhatsAppTemplateHandler) Sync(w http.ResponseWriter, r *http.Request) {
	businessPhoneID := r.URL.Query().Get("businessPhoneId")
	if businessPhoneID == "" {
		response.WriteError(w, http.StatusBadRequest, "businessPhoneId query parameter is required", nil)
		return
	}

	templates, err := h.syncUseCase.Execute(whatsapptemplatedomain.SyncTemplatesInput{
		BusinessPhoneID: businessPhoneID,
	})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to sync templates: "+err.Error(), nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message":   "Templates synchronized successfully",
		"count":     len(templates),
		"templates": toTemplateResponses(templates),
	})
}

func (h *WhatsAppTemplateHandler) SyncOne(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	templateID := vars["id"]
	if templateID == "" {
		response.WriteError(w, http.StatusBadRequest, "Template ID is required", nil)
		return
	}

	tmpl, err := h.syncOneUseCase.Execute(whatsapptemplatedomain.SyncTemplateInput{
		TemplateID: templateID,
	})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to sync template: "+err.Error(), nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message":  "Template synchronized successfully",
		"template": toTemplateResponse(tmpl),
	})
}

func (h *WhatsAppTemplateHandler) UpdateHeaderMediaURL(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	templateID := vars["id"]
	if templateID == "" {
		response.WriteError(w, http.StatusBadRequest, "Template ID is required", nil)
		return
	}

	var req UpdateHeaderMediaURLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	err := h.setHeaderMediaUC.Execute(whatsapptemplatedomain.SetTemplateHeaderMediaInput{
		TemplateID:     templateID,
		HeaderMediaURL: req.HeaderMediaURL,
	})
	if err != nil {
		if errors.Is(err, whatsapptemplatedomain.ErrTemplateNotFound) {
			response.WriteError(w, http.StatusNotFound, "Template not found", nil)
			return
		}
		if errors.Is(err, whatsapptemplatedomain.ErrHeaderMediaURLNotApplicable) {
			response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to update header media URL: "+err.Error(), nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message": "Header media URL updated successfully",
	})
}

func (h *WhatsAppTemplateHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if req.BusinessPhoneID == "" {
		response.WriteError(w, http.StatusBadRequest, "businessPhoneId is required", nil)
		return
	}
	if req.Name == "" {
		response.WriteError(w, http.StatusBadRequest, "Template name is required", nil)
		return
	}
	if req.Category == "" {
		response.WriteError(w, http.StatusBadRequest, "Template category is required", nil)
		return
	}

	components := make([]whatsapptemplatedomain.TemplateComponent, 0, len(req.Components))
	for _, c := range req.Components {
		comp := whatsapptemplatedomain.TemplateComponent{
			Type:   c.Type,
			Format: c.Format,
			Text:   c.Text,
		}

		for _, b := range c.Buttons {
			comp.Buttons = append(comp.Buttons, whatsapptemplatedomain.TemplateButton{
				Type:        b.Type,
				Text:        b.Text,
				URL:         b.URL,
				PhoneNumber: b.PhoneNumber,
				Example:     b.Example,
			})
		}

		if c.Example != nil {
			comp.Example = &whatsapptemplatedomain.TemplateExample{
				HeaderText:   c.Example.HeaderText,
				HeaderHandle: c.Example.HeaderHandle,
				BodyText:     c.Example.BodyText,
			}

			for _, np := range c.Example.BodyTextNamed {
				comp.Example.BodyTextNamed = append(comp.Example.BodyTextNamed, whatsapptemplatedomain.NamedParamExample{
					ParamName: np.ParamName,
					Example:   np.Example,
				})
			}

			for _, np := range c.Example.HeaderTextNamed {
				comp.Example.HeaderTextNamed = append(comp.Example.HeaderTextNamed, whatsapptemplatedomain.NamedParamExample{
					ParamName: np.ParamName,
					Example:   np.Example,
				})
			}
		}
		components = append(components, comp)
	}

	input := whatsapptemplatedomain.CreateTemplateInput{
		BusinessPhoneID: req.BusinessPhoneID,
		Name:            req.Name,
		Language:        req.Language,
		Category:        whatsapptemplatedomain.TemplateCategory(req.Category),
		ParameterFormat: req.ParameterFormat,
		Components:      components,
		HeaderMediaURL:  req.HeaderMediaURL,
	}

	result, err := h.createUseCase.Execute(input)
	if err != nil {
		if isTemplateValidationError(err) {
			response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to create template: "+err.Error(), nil)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, result)
}

func isTemplateValidationError(err error) bool {
	validationErrors := []error{
		whatsapptemplatedomain.ErrTemplateNameRequired,
		whatsapptemplatedomain.ErrTemplateNameInvalidChars,
		whatsapptemplatedomain.ErrTemplateNameMustStartLetter,
		whatsapptemplatedomain.ErrTemplateNameTooLong,
		whatsapptemplatedomain.ErrHeaderTextTooLong,
		whatsapptemplatedomain.ErrHeaderTextTooManyVariables,
		whatsapptemplatedomain.ErrHeaderFormatRequired,
		whatsapptemplatedomain.ErrHeaderMediaNeedsHandle,
		whatsapptemplatedomain.ErrBodyTextTooLong,
		whatsapptemplatedomain.ErrBodyVariableAtStart,
		whatsapptemplatedomain.ErrBodyVariableAtEnd,
		whatsapptemplatedomain.ErrBodyConsecutiveVariables,
		whatsapptemplatedomain.ErrBodyNeedsExample,
		whatsapptemplatedomain.ErrFooterTextTooLong,
		whatsapptemplatedomain.ErrFooterHasVariables,
		whatsapptemplatedomain.ErrTooManyButtons,
		whatsapptemplatedomain.ErrButtonTextTooLong,
		whatsapptemplatedomain.ErrButtonTextRequired,
		whatsapptemplatedomain.ErrButtonURLRequired,
		whatsapptemplatedomain.ErrButtonPhoneRequired,
		whatsapptemplatedomain.ErrButtonsNotGrouped,
		whatsapptemplatedomain.ErrURLButtonVariableNotEnd,
		whatsapptemplatedomain.ErrURLButtonTooManyVars,
		whatsapptemplatedomain.ErrCopyCodeNeedsExample,
		whatsapptemplatedomain.ErrInvalidComponentType,
		whatsapptemplatedomain.ErrInvalidHeaderFormat,
		whatsapptemplatedomain.ErrInvalidButtonType,
		whatsapptemplatedomain.ErrInvalidCategory,
		whatsapptemplatedomain.ErrCallPermissionWithButtons,
		whatsapptemplatedomain.ErrMultipleCallPermissionRequests,
		whatsapptemplatedomain.ErrMixedParameterStyles,
	}

	for _, validationErr := range validationErrors {
		if errors.Is(err, validationErr) {
			return true
		}
	}

	errMsg := err.Error()
	validationPatterns := []string{
		"template must have a BODY",
		"requires example",
		"header text with variables",
		"URL button with variable",
	}
	for _, pattern := range validationPatterns {
		if contains(errMsg, pattern) {
			return true
		}
	}

	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func (h *WhatsAppTemplateHandler) Replicate(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	templateID := vars["id"]
	if templateID == "" {
		response.WriteError(w, http.StatusBadRequest, "Template ID is required", nil)
		return
	}

	var req ReplicateTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if req.TargetBusinessPhoneID == "" {
		response.WriteError(w, http.StatusBadRequest, "targetBusinessPhoneId is required", nil)
		return
	}

	result, err := h.replicateUseCase.Execute(whatsapptemplatedomain.ReplicateTemplateInput{
		TemplateID:            templateID,
		TargetBusinessPhoneID: req.TargetBusinessPhoneID,
	})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to replicate template: "+err.Error(), nil)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, result)
}

func (h *WhatsAppTemplateHandler) verifyPhoneOwnership(w http.ResponseWriter, phoneID, wsID string) *businessphone.WhatsAppBusinessPhoneNumber {
	phone, err := h.phoneRepo.FindByID(phoneID)
	if err != nil {
		if errors.Is(err, businessphone.ErrPhoneNumberNotFound) {
			response.WriteError(w, http.StatusNotFound, "Business phone not found", nil)
			return nil
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to look up business phone", nil)
		return nil
	}
	if !phone.BelongsToWorkspace(wsID) {
		response.WriteError(w, http.StatusForbidden, "This phone does not belong to your workspace", nil)
		return nil
	}
	return phone
}

func (h *WhatsAppTemplateHandler) verifyTemplateOwnership(w http.ResponseWriter, templateID, wsID string) *whatsapptemplatedomain.Template {
	hasAccess, err := h.workspaceTemplateAccessRepo.HasAccess(wsID, templateID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to verify template access", nil)
		return nil
	}
	if !hasAccess {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this template", nil)
		return nil
	}

	tmpl, err := h.getUseCase.Execute(templateID)
	if err != nil {
		if errors.Is(err, whatsapptemplatedomain.ErrTemplateNotFound) {
			response.WriteError(w, http.StatusNotFound, "Template not found", nil)
			return nil
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to get template", nil)
		return nil
	}

	phones, err := h.phoneRepo.FindByWABAId(tmpl.WABAId)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to verify phone ownership", nil)
		return nil
	}
	owned := false
	for _, p := range phones {
		if p.BelongsToWorkspace(wsID) {
			owned = true
			break
		}
	}
	if !owned {
		response.WriteError(w, http.StatusForbidden, "The phone associated with this template does not belong to your workspace", nil)
		return nil
	}

	return tmpl
}

// @Summary		Criar modelo de mensagem
// @Description	Cria um novo modelo (template) de mensagem do WhatsApp para um telefone comercial do workspace e concede acesso ao workspace. O telefone precisa pertencer ao workspace.
// @Tags			Modelos de WhatsApp
// @Accept			json
// @Produce		json
// @Param			request	body		CreateTemplateRequest	true	"Dados do modelo a criar"
// @Success		201	{object}	template.Template
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/templates [post]
func (h *WhatsAppTemplateHandler) CreateForWorkspace(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	var req CreateTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if req.BusinessPhoneID == "" {
		response.WriteError(w, http.StatusBadRequest, "businessPhoneId is required", nil)
		return
	}
	if req.Name == "" {
		response.WriteError(w, http.StatusBadRequest, "Template name is required", nil)
		return
	}
	if req.Category == "" {
		response.WriteError(w, http.StatusBadRequest, "Template category is required", nil)
		return
	}

	if h.verifyPhoneOwnership(w, req.BusinessPhoneID, wsID) == nil {
		return
	}

	components := make([]whatsapptemplatedomain.TemplateComponent, 0, len(req.Components))
	for _, c := range req.Components {
		comp := whatsapptemplatedomain.TemplateComponent{
			Type:   c.Type,
			Format: c.Format,
			Text:   c.Text,
		}
		for _, b := range c.Buttons {
			comp.Buttons = append(comp.Buttons, whatsapptemplatedomain.TemplateButton{
				Type:        b.Type,
				Text:        b.Text,
				URL:         b.URL,
				PhoneNumber: b.PhoneNumber,
				Example:     b.Example,
			})
		}
		if c.Example != nil {
			comp.Example = &whatsapptemplatedomain.TemplateExample{
				HeaderText:   c.Example.HeaderText,
				HeaderHandle: c.Example.HeaderHandle,
				BodyText:     c.Example.BodyText,
			}
			for _, np := range c.Example.BodyTextNamed {
				comp.Example.BodyTextNamed = append(comp.Example.BodyTextNamed, whatsapptemplatedomain.NamedParamExample{
					ParamName: np.ParamName,
					Example:   np.Example,
				})
			}
			for _, np := range c.Example.HeaderTextNamed {
				comp.Example.HeaderTextNamed = append(comp.Example.HeaderTextNamed, whatsapptemplatedomain.NamedParamExample{
					ParamName: np.ParamName,
					Example:   np.Example,
				})
			}
		}
		components = append(components, comp)
	}

	input := whatsapptemplatedomain.CreateTemplateInput{
		BusinessPhoneID: req.BusinessPhoneID,
		Name:            req.Name,
		Language:        req.Language,
		Category:        whatsapptemplatedomain.TemplateCategory(req.Category),
		ParameterFormat: req.ParameterFormat,
		Components:      components,
		HeaderMediaURL:  req.HeaderMediaURL,
	}

	result, err := h.createUseCase.Execute(input)
	if err != nil {
		if isTemplateValidationError(err) {
			response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to create template: "+err.Error(), nil)
		return
	}

	access := workspace_template_access.NewWorkspaceTemplateAccess(
		"", wsID, result.ID, claims.UserID,
	)
	err = h.workspaceTemplateAccessRepo.Create(access)

	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to grant workspace access to template: "+err.Error(), nil)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, result)
}

// @Summary		Remover modelo de mensagem
// @Description	Exclui um modelo (template) de mensagem do WhatsApp pertencente ao workspace. Modelos gerenciados pela plataforma não podem ser removidos.
// @Tags			Modelos de WhatsApp
// @Produce		json
// @Param			id	path	string	true	"ID do modelo"
// @Success		200	{object}	MessageResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/templates/{id} [delete]
func (h *WhatsAppTemplateHandler) DeleteForWorkspace(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	templateID := mux.Vars(r)["id"]
	if templateID == "" {
		response.WriteError(w, http.StatusBadRequest, "Template ID is required", nil)
		return
	}

	if h.verifyTemplateOwnership(w, templateID, wsID) == nil {
		return
	}

	err := h.deleteUseCase.Execute(whatsapptemplatedomain.DeleteTemplateInput{
		TemplateID: templateID,
	})
	if err != nil {
		if errors.Is(err, whatsapptemplatedomain.ErrTemplateNotFound) {
			response.WriteError(w, http.StatusNotFound, "Template not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to delete template: "+err.Error(), nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message": "Template deleted successfully",
	})
}

// @Summary		Sincronizar modelos de mensagem
// @Description	Sincroniza, com a Meta, os modelos (templates) de mensagem do telefone comercial informado. O telefone precisa pertencer ao workspace.
// @Tags			Modelos de WhatsApp
// @Produce		json
// @Param			businessPhoneId	query	string	true	"ID do telefone comercial"
// @Success		200	{object}	MessageResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/templates/sync [post]
func (h *WhatsAppTemplateHandler) SyncForWorkspace(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	businessPhoneID := r.URL.Query().Get("businessPhoneId")
	if businessPhoneID == "" {
		response.WriteError(w, http.StatusBadRequest, "businessPhoneId query parameter is required", nil)
		return
	}

	if h.verifyPhoneOwnership(w, businessPhoneID, wsID) == nil {
		return
	}

	templates, err := h.syncUseCase.Execute(whatsapptemplatedomain.SyncTemplatesInput{
		BusinessPhoneID: businessPhoneID,
	})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to sync templates: "+err.Error(), nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message":   "Templates synchronized successfully",
		"count":     len(templates),
		"templates": toTemplateResponses(templates),
	})
}

// @Summary		Sincronizar um modelo de mensagem
// @Description	Sincroniza, com a Meta, um único modelo (template) de mensagem do WhatsApp pertencente ao workspace.
// @Tags			Modelos de WhatsApp
// @Produce		json
// @Param			id	path	string	true	"ID do modelo"
// @Success		200	{object}	MessageResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/templates/{id}/sync [post]
func (h *WhatsAppTemplateHandler) SyncOneForWorkspace(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	templateID := mux.Vars(r)["id"]
	if templateID == "" {
		response.WriteError(w, http.StatusBadRequest, "Template ID is required", nil)
		return
	}

	if h.verifyTemplateOwnership(w, templateID, wsID) == nil {
		return
	}

	tmpl, err := h.syncOneUseCase.Execute(whatsapptemplatedomain.SyncTemplateInput{
		TemplateID: templateID,
	})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to sync template: "+err.Error(), nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message":  "Template synchronized successfully",
		"template": toTemplateResponse(tmpl),
	})
}

// @Summary		Atualizar mídia do cabeçalho do modelo
// @Description	Atualiza a URL da mídia do cabeçalho de um modelo (template) de mensagem do WhatsApp pertencente ao workspace.
// @Tags			Modelos de WhatsApp
// @Accept			json
// @Produce		json
// @Param			id		path		string						true	"ID do modelo"
// @Param			request	body		UpdateHeaderMediaURLRequest	true	"Nova URL da mídia do cabeçalho"
// @Success		200	{object}	MessageResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/whatsapp/templates/{id}/header-media [patch]
func (h *WhatsAppTemplateHandler) UpdateHeaderMediaForWorkspace(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	wsID := middleware.GetWorkspaceID(r)

	templateID := mux.Vars(r)["id"]
	if templateID == "" {
		response.WriteError(w, http.StatusBadRequest, "Template ID is required", nil)
		return
	}

	if h.verifyTemplateOwnership(w, templateID, wsID) == nil {
		return
	}

	var req UpdateHeaderMediaURLRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	err := h.setHeaderMediaUC.Execute(whatsapptemplatedomain.SetTemplateHeaderMediaInput{
		TemplateID:     templateID,
		HeaderMediaURL: req.HeaderMediaURL,
	})
	if err != nil {
		if errors.Is(err, whatsapptemplatedomain.ErrTemplateNotFound) {
			response.WriteError(w, http.StatusNotFound, "Template not found", nil)
			return
		}
		if errors.Is(err, whatsapptemplatedomain.ErrHeaderMediaURLNotApplicable) {
			response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to update header media URL: "+err.Error(), nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message": "Header media URL updated successfully",
	})
}
