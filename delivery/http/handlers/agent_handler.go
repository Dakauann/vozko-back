package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/agent"
	"vozko/domain/ai"
	"vozko/domain/auth"
	"vozko/domain/shared"
	"vozko/domain/tools"
	"vozko/domain/user"
	"vozko/domain/whatsapp/template"
	workspace_department "vozko/domain/workspace/workspace_department"
	"vozko/infra/http/middleware"
)

type AgentHandler struct {
	createUseCase           agent.CreateAgentUseCase
	updateUseCase           agent.UpdateAgentUseCase
	assignDepartmentUseCase agent.AssignDepartmentUseCase
	deleteUseCase           agent.DeleteAgentUseCase
	getUseCase              agent.GetAgentUseCase
	listUseCase             agent.ListAgentsUseCase
	toolRegistry            tools.Service
	aiService               ai.Service
	templateRepo            template.Repository
}

func NewAgentHandler(
	createUC agent.CreateAgentUseCase,
	updateUC agent.UpdateAgentUseCase,
	assignDepartmentUC agent.AssignDepartmentUseCase,
	deleteUC agent.DeleteAgentUseCase,
	getUC agent.GetAgentUseCase,
	listUC agent.ListAgentsUseCase,
	toolRegistry tools.Service,
	aiService ai.Service,
	templateRepo template.Repository,
) *AgentHandler {
	return &AgentHandler{
		createUseCase:           createUC,
		updateUseCase:           updateUC,
		assignDepartmentUseCase: assignDepartmentUC,
		deleteUseCase:           deleteUC,
		getUseCase:              getUC,
		listUseCase:             listUC,
		toolRegistry:            toolRegistry,
		aiService:               aiService,
		templateRepo:            templateRepo,
	}
}

type toolBindingRequest struct {
	Name       string                 `json:"name"`
	Visibility []string               `json:"visibility,omitempty"`
	Config     map[string]interface{} `json:"config,omitempty"`
}

type ragConfigRequest struct {
	MaxCharacters   *int     `json:"maxCharacters,omitempty"`
	MaxChunks       *int     `json:"maxChunks,omitempty"`
	MinScore        *float64 `json:"minScore,omitempty"`
	NumCandidates   *int     `json:"numCandidates,omitempty"`
	ContextWindow   *int     `json:"contextWindow,omitempty"`
	MaxChunksPerDoc *int     `json:"maxChunksPerDoc,omitempty"`
}

type agentVariableRequest struct {
	Name         string `json:"name"`
	Description  string `json:"description,omitempty"`
	DefaultValue string `json:"defaultValue,omitempty"`
}

type createAgentRequest struct {
	Name               string                 `json:"name"`
	Description        string                 `json:"description"`
	InitialMessage     string                 `json:"initialMessage"`
	UseInitialMessage  *bool                  `json:"useInitialMessage,omitempty"`
	MessagingPrompt    string                 `json:"messagingPrompt"`
	MessagingModel     string                 `json:"messagingModel"`
	AvatarURL          string                 `json:"avatarUrl"`
	Tags               []string               `json:"tags"`
	Metadata           map[string]string      `json:"metadata"`
	IsActive           *bool                  `json:"isActive"`
	Provider           string                 `json:"provider"`
	InternalTools      []toolBindingRequest   `json:"internalTools"`
	BusinessPhoneID    string                 `json:"businessPhoneId,omitempty"`
	WhatsAppTemplateID *string                `json:"whatsappTemplateId,omitempty"`
	MediaIDs           []string               `json:"mediaIds"`
	KnowledgeBaseIDs   []string               `json:"knowledgeBaseIds,omitempty"`
	MCPCollectionIDs   []string               `json:"mcpCollectionIds,omitempty"`
	DepartmentID       string                 `json:"departmentId,omitempty"`
	RAGEnabled         *bool                  `json:"ragEnabled,omitempty"`
	RAGConfig          *ragConfigRequest      `json:"ragConfig,omitempty"`
	Variables          []agentVariableRequest `json:"variables,omitempty"`
}

type updateAgentRequest struct {
	Name               string                 `json:"name"`
	Description        string                 `json:"description"`
	InitialMessage     string                 `json:"initialMessage"`
	UseInitialMessage  *bool                  `json:"useInitialMessage,omitempty"`
	MessagingPrompt    string                 `json:"messagingPrompt"`
	MessagingModel     string                 `json:"messagingModel"`
	AvatarURL          string                 `json:"avatarUrl"`
	Tags               []string               `json:"tags"`
	Metadata           map[string]string      `json:"metadata"`
	IsActive           *bool                  `json:"isActive"`
	Provider           string                 `json:"provider"`
	InternalTools      []toolBindingRequest   `json:"internalTools"`
	BusinessPhoneID    string                 `json:"businessPhoneId,omitempty"`
	WhatsAppTemplateID *string                `json:"whatsappTemplateId,omitempty"`
	MediaIDs           []string               `json:"mediaIds"`
	KnowledgeBaseIDs   []string               `json:"knowledgeBaseIds,omitempty"`
	MCPCollectionIDs   []string               `json:"mcpCollectionIds,omitempty"`
	RAGEnabled         *bool                  `json:"ragEnabled,omitempty"`
	RAGConfig          *ragConfigRequest      `json:"ragConfig,omitempty"`
	Variables          []agentVariableRequest `json:"variables,omitempty"`
}

func (h *AgentHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeInvalidBodyError(w)
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if req.Metadata == nil {
		req.Metadata = make(map[string]string)
	}
	if req.Tags == nil {
		req.Tags = make([]string, 0)
	}

	provider, ok := parseProvider(req.Provider)
	if !ok {
		response.WriteValidationError(w, map[string]string{"provider": "invalid provider"})
		return
	}

	if strings.TrimSpace(req.MessagingModel) == "" {
		response.WriteValidationError(w, map[string]string{"messagingModel": "required"})
		return
	}
	if !isSupportedMessagingModel(provider, req.MessagingModel) {
		response.WriteValidationError(w, map[string]string{"messagingModel": "unsupported model for provider"})
		return
	}

	if req.InternalTools == nil {
		response.WriteValidationError(w, map[string]string{"internalTools": "required"})
		return
	}

	r = withDepartmentCreationScope(r, req.DepartmentID)

	if containsToolBinding(req.InternalTools, agent.ToolSendWhatsAppMessage) {
		if req.WhatsAppTemplateID == nil || *req.WhatsAppTemplateID == "" {
			response.WriteValidationError(w, map[string]string{
				"whatsappTemplateId": "required when using send_whatsapp_message tool",
			})
			return
		}
	}

	if req.WhatsAppTemplateID != nil && *req.WhatsAppTemplateID != "" {
		tmpl, err := h.templateRepo.FindByID(*req.WhatsAppTemplateID)
		if err != nil {
			response.WriteValidationError(w, map[string]string{
				"whatsappTemplateId": "template not found",
			})
			return
		}
		if !tmpl.IsReadyToSend() {
			usabilityMsg := tmpl.GetUsabilityMessage()
			if usabilityMsg == "" {
				usabilityMsg = "template is not ready to send"
			}
			response.WriteValidationError(w, map[string]string{
				"whatsappTemplateId": usabilityMsg,
			})
			return
		}
	}

	created, err := h.createUseCase.Execute(r.Context(), agent.CreateAgentInput{
		WorkspaceID:        middleware.GetWorkspaceID(r),
		Name:               req.Name,
		Description:        req.Description,
		InitialMessage:     req.InitialMessage,
		UseInitialMessage:  req.UseInitialMessage,
		MessagingPrompt:    req.MessagingPrompt,
		MessagingModel:     req.MessagingModel,
		AvatarURL:          req.AvatarURL,
		Tags:               req.Tags,
		Metadata:           req.Metadata,
		Provider:           provider,
		IsActive:           req.IsActive,
		RAGEnabled:         req.RAGEnabled,
		RAGConfig:          convertRAGConfig(req.RAGConfig),
		InternalTools:      convertToolBindings(req.InternalTools),
		BusinessPhoneID:    req.BusinessPhoneID,
		WhatsAppTemplateID: req.WhatsAppTemplateID,
		MediaIDs:           req.MediaIDs,
		KnowledgeBaseIDs:   req.KnowledgeBaseIDs,
		MCPCollectionIDs:   req.MCPCollectionIDs,
		Variables:          convertVariables(req.Variables),
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, created)
}

func (h *AgentHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req updateAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeInvalidBodyError(w)
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if !h.verifyOwnership(w, id, claims, r) {
		return
	}

	if req.IsActive == nil {
		response.WriteValidationError(w, map[string]string{"isActive": "required"})
		return
	}

	if req.Metadata == nil {
		req.Metadata = make(map[string]string)
	}
	if req.Tags == nil {
		req.Tags = make([]string, 0)
	}

	provider, ok := parseProvider(req.Provider)
	if !ok {
		response.WriteValidationError(w, map[string]string{"provider": "invalid provider"})
		return
	}

	if strings.TrimSpace(req.MessagingModel) == "" {
		response.WriteValidationError(w, map[string]string{"messagingModel": "required"})
		return
	}
	if !isSupportedMessagingModel(provider, req.MessagingModel) {
		response.WriteValidationError(w, map[string]string{"messagingModel": "unsupported model for provider"})
		return
	}

	internalTools := req.InternalTools
	templateID := req.WhatsAppTemplateID
	hasWhatsAppTemplate := templateID != nil && *templateID != ""
	hasWhatsAppTool := containsToolBinding(internalTools, agent.ToolSendWhatsAppMessage)

	if hasWhatsAppTool && !hasWhatsAppTemplate {
		response.WriteValidationError(w, map[string]string{
			"whatsappTemplateId": "required when using send_whatsapp_message tool",
		})
		return
	}

	if !hasWhatsAppTemplate && hasWhatsAppTool {
		internalTools = removeToolBinding(internalTools, agent.ToolSendWhatsAppMessage)
	}

	if hasWhatsAppTemplate {
		tmpl, err := h.templateRepo.FindByID(*templateID)
		if err != nil {
			response.WriteValidationError(w, map[string]string{
				"whatsappTemplateId": "template not found",
			})
			return
		}
		if !tmpl.IsReadyToSend() {
			usabilityMsg := tmpl.GetUsabilityMessage()
			if usabilityMsg == "" {
				usabilityMsg = "template is not ready to send"
			}
			response.WriteValidationError(w, map[string]string{
				"whatsappTemplateId": usabilityMsg,
			})
			return
		}
	}

	updated, err := h.updateUseCase.Execute(r.Context(), id, agent.UpdateAgentInput{
		Name:                    &req.Name,
		Description:             &req.Description,
		InitialMessage:          &req.InitialMessage,
		UseInitialMessage:       ptrBool(boolPtrOrDefault(req.UseInitialMessage, true)),
		MessagingPrompt:         &req.MessagingPrompt,
		MessagingModel:          &req.MessagingModel,
		Provider:                &provider,
		AvatarURL:               &req.AvatarURL,
		Tags:                    req.Tags,
		Metadata:                req.Metadata,
		IsActive:                req.IsActive,
		RAGEnabled:              ptrBool(boolPtrOrDefault(req.RAGEnabled, false)),
		InternalTools:           convertToolBindings(internalTools),
		BusinessPhoneID:         &req.BusinessPhoneID,
		MediaIDs:                req.MediaIDs,
		KnowledgeBaseIDs:        req.KnowledgeBaseIDs,
		MCPCollectionIDs:        req.MCPCollectionIDs,
		Variables:               convertVariables(req.Variables),
		ReplaceWhatsAppTemplate: true,
		WhatsAppTemplateID:      req.WhatsAppTemplateID,
		ReplaceRAGConfig:        true,
		RAGConfig:               convertRAGConfig(req.RAGConfig),
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, updated)
}

func (h *AgentHandler) AssignDepartment(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if !requirePrivilegedDepartmentAssignment(w, r) {
		return
	}

	departmentID, ok := decodeDepartmentAssignmentRequest(w, r)
	if !ok {
		return
	}

	if !h.verifyOwnership(w, id, claims, r) {
		return
	}

	updated, err := h.assignDepartmentUseCase.Execute(withDepartmentCreationScope(r, departmentID).Context(), id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, updated)
}

func (h *AgentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if !h.verifyOwnership(w, id, claims, r) {
		return
	}

	if err := h.deleteUseCase.Execute(id); err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusNoContent, nil)
}

func (h *AgentHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	agentItem, err := h.getUseCase.Execute(id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	if agentItem.WorkspaceID != middleware.GetWorkspaceID(r) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this agent", nil)
		return
	}

	if !canAccessDepartment(r, agentItem.DepartmentID) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this agent", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, agentItem)
}

func (h *AgentHandler) RequiredVariables(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	agentItem, err := h.getUseCase.Execute(id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	if agentItem.WorkspaceID != middleware.GetWorkspaceID(r) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this agent", nil)
		return
	}

	if !canAccessDepartment(r, agentItem.DepartmentID) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this agent", nil)
		return
	}

	infos := agentItem.RequiredVariableInfos()
	response.WriteSuccess(w, http.StatusOK, infos)
}

func (h *AgentHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	values := r.URL.Query()

	options := shared.QueryOptions{
		Pagination: parsePagination(values),
		Sorts:      parseSort(values, map[string]string{"name": "name", "createdat": "createdAt", "updatedat": "updatedAt"}),
	}

	search := strings.TrimSpace(values.Get("search"))

	tagValues := append([]string{}, values["tag"]...)
	tagValues = append(tagValues, values["tags"]...)
	tags := splitListValues(tagValues)

	var isActive *bool
	if raw := strings.TrimSpace(values.Get("isActive")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			response.WriteValidationError(w, map[string]string{"isActive": "must be true or false"})
			return
		}
		isActive = &parsed
	}

	var createdAfter *time.Time
	if raw := strings.TrimSpace(values.Get("createdAfter")); raw != "" {
		parsed, err := parseTime(raw)
		if err != nil {
			response.WriteValidationError(w, map[string]string{"createdAfter": "must be a valid RFC3339 timestamp or YYYY-MM-DD"})
			return
		}
		createdAfter = &parsed
	}

	var createdBefore *time.Time
	if raw := strings.TrimSpace(values.Get("createdBefore")); raw != "" {
		parsed, err := parseTime(raw)
		if err != nil {
			response.WriteValidationError(w, map[string]string{"createdBefore": "must be a valid RFC3339 timestamp or YYYY-MM-DD"})
			return
		}
		createdBefore = &parsed
	}

	notArchived := false
	if shouldReturnEmptyDepartmentList(r) {
		response.WritePaginated(w, http.StatusOK, []*agent.AgentListItem{}, response.PaginationMeta{
			Page:       options.Pagination.Page,
			PageSize:   options.Pagination.PageSize,
			TotalPages: 0,
			TotalItems: 0,
		})
		return
	}
	result, err := h.listUseCase.Execute(agent.ListAgentsInput{
		WorkspaceID:   middleware.GetWorkspaceID(r),
		DepartmentIDs: departmentFilterIDs(r),
		Search:        search,
		Tags:          tags,
		IsActive:      isActive,
		Archived:      &notArchived,
		CreatedAfter:  createdAfter,
		CreatedBefore: createdBefore,
		Options:       options,
	})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list agents", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

func (h *AgentHandler) ListArchived(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	values := r.URL.Query()

	options := shared.QueryOptions{
		Pagination: parsePagination(values),
		Sorts:      parseSort(values, map[string]string{"name": "name", "createdat": "createdAt", "updatedat": "updatedAt"}),
	}

	isArchived := true
	if shouldReturnEmptyDepartmentList(r) {
		response.WritePaginated(w, http.StatusOK, []*agent.AgentListItem{}, response.PaginationMeta{
			Page:       options.Pagination.Page,
			PageSize:   options.Pagination.PageSize,
			TotalPages: 0,
			TotalItems: 0,
		})
		return
	}
	result, err := h.listUseCase.Execute(agent.ListAgentsInput{
		WorkspaceID:   middleware.GetWorkspaceID(r),
		DepartmentIDs: departmentFilterIDs(r),
		Search:        strings.TrimSpace(values.Get("search")),
		Archived:      &isArchived,
		Options:       options,
	})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list archived agents", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

func (h *AgentHandler) Archive(w http.ResponseWriter, r *http.Request) {
	agentID := mux.Vars(r)["id"]
	if agentID == "" {
		response.WriteError(w, http.StatusBadRequest, "Agent ID is required", nil)
		return
	}

	existing, err := h.getUseCase.Execute(agentID)
	if err != nil {
		if errors.Is(err, agent.ErrAgentNotFound) {
			response.WriteError(w, http.StatusNotFound, "Agent not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to get agent", nil)
		return
	}

	if existing.WorkspaceID != middleware.GetWorkspaceID(r) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this agent", nil)
		return
	}

	archived := true
	if _, err := h.updateUseCase.Execute(r.Context(), agentID, agent.UpdateAgentInput{Archived: &archived}); err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to archive agent", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{"archived": true})
}

func (h *AgentHandler) Unarchive(w http.ResponseWriter, r *http.Request) {
	agentID := mux.Vars(r)["id"]
	if agentID == "" {
		response.WriteError(w, http.StatusBadRequest, "Agent ID is required", nil)
		return
	}

	existing, err := h.getUseCase.Execute(agentID)
	if err != nil {
		if errors.Is(err, agent.ErrAgentNotFound) {
			response.WriteError(w, http.StatusNotFound, "Agent not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to get agent", nil)
		return
	}

	if existing.WorkspaceID != middleware.GetWorkspaceID(r) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this agent", nil)
		return
	}

	unarchived := false
	if _, err := h.updateUseCase.Execute(r.Context(), agentID, agent.UpdateAgentInput{Archived: &unarchived}); err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to unarchive agent", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{"archived": false})
}

func (h *AgentHandler) ListTools(w http.ResponseWriter, r *http.Request) {
	if h.toolRegistry == nil {
		response.WriteSuccess(w, http.StatusOK, []tools.Definition{})
		return
	}
	definitions := h.toolRegistry.Definitions()

	claims := middleware.GetClaims(r)
	isAdmin := claims != nil && claims.Role == string(user.RoleAdmin)
	if !isAdmin {
		filtered := make([]tools.Definition, 0, len(definitions))
		for _, def := range definitions {
			if !def.AdminOnly {
				filtered = append(filtered, def)
			}
		}
		definitions = filtered
	}

	sort.SliceStable(definitions, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(definitions[i].Name))
		right := strings.ToLower(strings.TrimSpace(definitions[j].Name))
		return left < right
	})

	type camelParam struct {
		Type               string `json:"type"`
		Description        string `json:"description"`
		DisplayName        string `json:"displayName"`
		DisplayDescription string `json:"displayDescription"`
	}

	type camelConfigParam struct {
		Type               string `json:"type"`
		Description        string `json:"description"`
		DisplayName        string `json:"displayName"`
		DisplayDescription string `json:"displayDescription"`
		DefaultValue       any    `json:"defaultValue,omitempty"`
		Options            []struct {
			Value string `json:"value"`
			Label string `json:"label"`
		} `json:"options,omitempty"`
		Required bool `json:"required"`
	}

	type camelTool struct {
		Name               string                      `json:"name"`
		DisplayName        string                      `json:"displayName"`
		Description        string                      `json:"description"`
		DisplayDescription string                      `json:"displayDescription"`
		Parameters         map[string]camelParam       `json:"parameters"`
		Required           []string                    `json:"required"`
		Visibility         []string                    `json:"visibility"`
		Category           string                      `json:"category"`
		ConfigSchema       map[string]camelConfigParam `json:"configSchema,omitempty"`
		RequiredConfig     []string                    `json:"requiredConfig,omitempty"`
		RequiresConfig     bool                        `json:"requiresConfig"`
	}

	camelDefs := make([]camelTool, len(definitions))
	for i, def := range definitions {
		params := make(map[string]camelParam, len(def.Parameters))
		for k, v := range def.Parameters {
			paramDisplayName := v.DisplayName
			if paramDisplayName == "" {
				paramDisplayName = k
			}
			paramDisplayDesc := v.DisplayDescription
			if paramDisplayDesc == "" {
				paramDisplayDesc = v.Description
			}
			params[toCamelCase(k)] = camelParam{
				Type:               v.Type,
				Description:        paramDisplayDesc,
				DisplayName:        paramDisplayName,
				DisplayDescription: paramDisplayDesc,
			}
		}
		configSchema := map[string]camelConfigParam{}
		if len(def.ConfigSchema) > 0 {
			for k, v := range def.ConfigSchema {
				displayName := v.DisplayName
				if displayName == "" {
					displayName = k
				}
				displayDescription := v.DisplayDescription
				if displayDescription == "" {
					displayDescription = v.Description
				}

				options := make([]struct {
					Value string `json:"value"`
					Label string `json:"label"`
				}, 0, len(v.Options))
				for _, option := range v.Options {
					options = append(options, struct {
						Value string `json:"value"`
						Label string `json:"label"`
					}{Value: option.Value, Label: option.Label})
				}

				configSchema[k] = camelConfigParam{
					Type:               v.Type,
					Description:        displayDescription,
					DisplayName:        displayName,
					DisplayDescription: displayDescription,
					DefaultValue:       v.Default,
					Options:            options,
					Required:           v.Required,
				}
			}
		}
		visibility := make([]string, len(def.Visibility))
		for j, v := range def.Visibility {
			visibility[j] = string(v)
		}

		displayName := def.DisplayName
		if displayName == "" {
			displayName = def.Name
		}
		displayDesc := def.DisplayDescription
		if displayDesc == "" {
			displayDesc = def.Description
		}

		camelDefs[i] = camelTool{
			Name:               def.Name,
			DisplayName:        displayName,
			Description:        displayDesc,
			DisplayDescription: displayDesc,
			Parameters:         params,
			Required:           def.Required,
			Visibility:         visibility,
			Category:           string(def.Category),
			ConfigSchema:       configSchema,
			RequiredConfig:     def.RequiredConfig,
			RequiresConfig:     def.RequiresConfig,
		}
	}

	response.WriteSuccess(w, http.StatusOK, camelDefs)
}

func toCamelCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func (h *AgentHandler) ListOptions(w http.ResponseWriter, r *http.Request) {
	// Models are loaded dynamically from the AI service below; this map starts
	// empty and is populated per-provider from the live, priced catalog.
	modelsByProvider := map[agent.AgentProvider]agentProviderModels{}
	var messagingModels []string
	var modelsWithPricing []ai.ModelInfo

	if h.aiService != nil {
		if priced, err := h.aiService.GetModelsWithPricing(r.Context()); err == nil && len(priced) > 0 {
			modelsWithPricing = priced
			ids := make([]string, len(priced))
			for i, m := range priced {
				ids[i] = m.ID
			}
			modelsByProvider[agent.AgentProviderAI] = agentProviderModels{LLM: ids}
			messagingModels = ids
		}
	}

	payload := struct {
		Providers    []agentProviderOption          `json:"providers"`
		Models       map[string]agentProviderModels `json:"models"`
		Messaging    []string                       `json:"messaging"`
		ModelPricing []ai.ModelInfo                 `json:"modelPricing"`
		Defaults     struct {
			MessagingModel string `json:"messagingModel"`
		} `json:"defaults"`
	}{
		Providers:    getSupportedAgentProviders(),
		Models:       make(map[string]agentProviderModels, len(modelsByProvider)),
		Messaging:    messagingModels,
		ModelPricing: modelsWithPricing,
	}

	for provider, models := range modelsByProvider {
		payload.Models[string(provider)] = models
	}

	payload.Defaults.MessagingModel = defaultMessagingModel

	response.WriteSuccess(w, http.StatusOK, payload)
}

func (h *AgentHandler) handleDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agent.ErrAgentNotFound):
		response.WriteErrorWithCode(w, http.StatusNotFound, "AGENT_NOT_FOUND", "Agent not found", nil)
	case errors.Is(err, agent.ErrAgentNameRequired):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "AGENT_NAME_REQUIRED", "Agent name is required", map[string]string{"name": "required"})
	case errors.Is(err, agent.ErrAgentNameTooLong):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "AGENT_NAME_TOO_LONG", "Agent name must not exceed 120 characters", map[string]string{"name": "must not exceed 120 characters"})
	case errors.Is(err, agent.ErrAgentInitialMessageRequired):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "AGENT_INITIAL_MESSAGE_REQUIRED", "Initial message is required", map[string]string{"initialMessage": "required"})
	case errors.Is(err, agent.ErrAgentMessagingPromptRequired):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "AGENT_MESSAGING_PROMPT_REQUIRED", "Messaging prompt is required", map[string]string{"messagingPrompt": "required"})
	case errors.Is(err, agent.ErrAgentProviderRequired):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "AGENT_PROVIDER_REQUIRED", "Provider is required", map[string]string{"provider": "required"})
	case errors.Is(err, agent.ErrAgentProviderInvalid), errors.Is(err, agent.ErrAgentProviderUnsupported):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "AGENT_PROVIDER_INVALID", "Unsupported provider", map[string]string{"provider": "unsupported provider"})
	case errors.Is(err, agent.ErrAgentProviderUnavailable):
		errMsg := "Provider temporarily unavailable"
		if unwrapped := errors.Unwrap(err); unwrapped != nil {
			errMsg = fmt.Sprintf("Provider error: %v", unwrapped)
		} else if detail := strings.TrimPrefix(err.Error(), agent.ErrAgentProviderUnavailable.Error()); detail != "" {
			errMsg = fmt.Sprintf("Provider error: %s", strings.TrimPrefix(detail, ": "))
		}
		response.WriteErrorWithCode(w, http.StatusFailedDependency, "AGENT_PROVIDER_UNAVAILABLE", errMsg, nil)
	case errors.Is(err, agent.ErrAgentMessagingModelRequired):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "AGENT_MESSAGING_MODEL_REQUIRED", "Messaging model is required", map[string]string{"messagingModel": "required"})
	case errors.Is(err, agent.ErrAgentMessagingModelTooLong):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "AGENT_MESSAGING_MODEL_TOO_LONG", "Messaging model must not exceed 64 characters", map[string]string{"messagingModel": "must not exceed 64 characters"})
	case errors.Is(err, agent.ErrAgentMetadataInvalid):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "AGENT_METADATA_INVALID", "Metadata must be a valid JSON object of string values", map[string]string{"metadata": "must be a valid JSON object of string values"})
	case errors.Is(err, agent.ErrAgentInternalToolInvalid):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "AGENT_TOOL_INVALID", err.Error(), map[string]string{"internalTools": err.Error()})
	case errors.Is(err, agent.ErrAgentInternalToolsRequired):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "AGENT_TOOLS_REQUIRED", "At least one tool is required", map[string]string{"internalTools": "required"})
	case errors.Is(err, agent.ErrAgentVariableNameInvalid):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "AGENT_VARIABLE_NAME_INVALID", "Variable names must contain only letters, digits, and underscores", map[string]string{"variables": "names must match ^\\w+$"})
	case errors.Is(err, agent.ErrAgentVariableDuplicate):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "AGENT_VARIABLE_DUPLICATE", "Variable names must be unique", map[string]string{"variables": "duplicate variable name"})
	case errors.Is(err, agent.ErrAgentVariableTooMany):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "AGENT_VARIABLE_TOO_MANY", "Agent must not have more than 50 variables", map[string]string{"variables": "maximum 50 variables"})
	case errors.Is(err, agent.ErrAgentBusinessPhoneNotFound):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "AGENT_PHONE_NOT_FOUND", "Business phone not found", map[string]string{"businessPhoneId": "business phone not found"})
	case errors.Is(err, agent.ErrAgentBusinessPhoneNoAccess):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "AGENT_PHONE_NO_ACCESS", "Phone does not belong to this workspace", map[string]string{"businessPhoneId": "phone does not belong to this workspace"})
	case errors.Is(err, workspace_department.ErrDepartmentRequired):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "AGENT_DEPARTMENT_REQUIRED", "Department is required when you belong to multiple departments", map[string]string{"departmentId": "required when you belong to multiple departments"})
	case errors.Is(err, workspace_department.ErrDepartmentAccessDenied), errors.Is(err, workspace_department.ErrDepartmentNotFound):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "AGENT_DEPARTMENT_INVALID", "Department is invalid or not accessible", map[string]string{"departmentId": "invalid or not accessible"})
	default:
		response.WriteErrorWithCode(w, http.StatusInternalServerError, "AGENT_INTERNAL_ERROR", "Internal server error", nil)
	}
}

func (h *AgentHandler) writeInvalidBodyError(w http.ResponseWriter) {
	response.WriteInvalidBodyError(w, map[string]string{
		"name":               "string (required)",
		"description":        "string (optional)",
		"initialMessage":     "string (required)",
		"messagingPrompt":    "string (required)",
		"language":           "string (required)",
		"messagingModel":     "string (required)",
		"avatarUrl":          "string (optional)",
		"tags":               "string array (optional)",
		"metadata":           "object map[string]string (optional)",
		"provider":           "string (required, supported: \"platform\")",
		"isActive":           "boolean (optional)",
		"internalTools":      "array of {name: string, visibility?: string[], config?: object} (required; use [] for none)",
		"departmentId":       "string (optional; required for members who belong to multiple departments)",
		"whatsappTemplateId": "string (required when using send_whatsapp_message tool)",
		"mediaIds":           "string array (optional; IDs of media linked to agent for send_whatsapp_media)",
	})
}

func parseTime(value string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}
	return time.Parse("2006-01-02", value)
}

func boolPtrOrDefault(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

func ptrBool(v bool) *bool {
	return &v
}

func parseProvider(value string) (agent.AgentProvider, bool) {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return "", false
	}

	provider := agent.AgentProvider(trimmed)
	if !provider.IsValid() {
		return provider, false
	}

	return provider, true
}

func containsToolBinding(tools []toolBindingRequest, toolName string) bool {
	for _, t := range tools {
		if t.Name == toolName {
			return true
		}
	}
	return false
}

func removeToolBinding(tools []toolBindingRequest, toolName string) []toolBindingRequest {
	result := make([]toolBindingRequest, 0, len(tools))
	for _, t := range tools {
		if t.Name != toolName {
			result = append(result, t)
		}
	}
	return result
}

func convertToolBindings(reqTools []toolBindingRequest) []agent.ToolBinding {
	if reqTools == nil {
		return nil
	}
	result := make([]agent.ToolBinding, 0, len(reqTools))
	for _, t := range reqTools {
		tb := agent.ToolBinding{Name: t.Name, Config: t.Config}
		if len(t.Visibility) > 0 {
			tb.Visibility = make([]agent.ToolVisibility, 0, len(t.Visibility))
			for _, v := range t.Visibility {
				tb.Visibility = append(tb.Visibility, agent.ToolVisibility(v))
			}
		}
		result = append(result, tb)
	}
	return result
}

func convertVariables(reqVars []agentVariableRequest) []agent.AgentVariable {
	if reqVars == nil {
		return nil
	}
	result := make([]agent.AgentVariable, 0, len(reqVars))
	for _, v := range reqVars {
		result = append(result, agent.AgentVariable{
			Name:         v.Name,
			Description:  v.Description,
			DefaultValue: v.DefaultValue,
		})
	}
	return result
}

func convertRAGConfig(req *ragConfigRequest) *agent.RAGConfig {
	if req == nil {
		return nil
	}
	cfg := &agent.RAGConfig{}
	if req.MaxCharacters != nil {
		cfg.MaxCharacters = *req.MaxCharacters
	}
	if req.MaxChunks != nil {
		cfg.MaxChunks = *req.MaxChunks
	}
	if req.MinScore != nil {
		cfg.MinScore = *req.MinScore
	}
	if req.NumCandidates != nil {
		cfg.NumCandidates = *req.NumCandidates
	}
	if req.ContextWindow != nil {
		cfg.ContextWindow = *req.ContextWindow
	}
	if req.MaxChunksPerDoc != nil {
		cfg.MaxChunksPerDoc = *req.MaxChunksPerDoc
	}
	return cfg
}

func (h *AgentHandler) verifyOwnership(w http.ResponseWriter, agentID string, claims *auth.Claims, r *http.Request) bool {
	agentItem, err := h.getUseCase.Execute(agentID)
	if err != nil {
		h.handleDomainError(w, err)
		return false
	}

	if agentItem.WorkspaceID != middleware.GetWorkspaceID(r) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this agent", nil)
		return false
	}

	return true
}
