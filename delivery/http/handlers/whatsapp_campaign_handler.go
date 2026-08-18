package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/agent"
	"vozko/domain/auth"
	"vozko/domain/shared"
	"vozko/domain/stage"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	workflow_domain "vozko/domain/workflow"
	workspace_department "vozko/domain/workspace/workspace_department"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	"vozko/infra/http/middleware"
	whatsapp_campaign_usecase "vozko/usecases/whatsapp_campaign"
)

type WhatsAppCampaignHandler struct {
	createUseCase           wc.CreateCampaignUseCase
	updateUseCase           wc.UpdateCampaignUseCase
	assignDepartmentUseCase wc.AssignDepartmentUseCase
	deleteUseCase           wc.DeleteCampaignUseCase
	getUseCase              wc.GetCampaignUseCase
	listUseCase             wc.ListCampaignsUseCase
	listEntriesUseCase      wc.ListEntriesUseCase
	dispatchUseCase         wc.DispatchCampaignUseCase
	messageConsumerUseCase  wc.MessageConsumerUseCase
	resetUseCase            wc.ResetCampaignUseCase
	clearHistoryUseCase     wc.ClearHistoryUseCase
	deleteEntryUseCase      wc.DeleteEntryUseCase
	updateEntryUseCase      wc.UpdateEntryUseCase
	addEntriesUseCase       wc.AddEntriesUseCase
	quickSendUseCase        wc.QuickSendUseCase
	conversationHub         EntryUpdateBroadcaster
	cloneStagesUseCase      stage.CloneStagesFromGroupUseCase
	getWorkflowUseCase      workflow_domain.GetWorkflowUseCase
	activeSubscription      workspace_plan.EnsureActiveWorkspaceSubscriptionUseCase
	getAgentUseCase         agent.GetAgentUseCase
	summaryUseCase          wc.GetSummaryUseCase
}

func NewWhatsAppCampaignHandler(
	createUC wc.CreateCampaignUseCase,
	updateUC wc.UpdateCampaignUseCase,
	assignDepartmentUC wc.AssignDepartmentUseCase,
	deleteUC wc.DeleteCampaignUseCase,
	getUC wc.GetCampaignUseCase,
	listUC wc.ListCampaignsUseCase,
	listEntriesUC wc.ListEntriesUseCase,
	dispatchUC wc.DispatchCampaignUseCase,
	messageConsumerUC wc.MessageConsumerUseCase,
	resetUC wc.ResetCampaignUseCase,
	clearHistoryUC wc.ClearHistoryUseCase,
	deleteEntryUC wc.DeleteEntryUseCase,
	updateEntryUC wc.UpdateEntryUseCase,
	addEntriesUC wc.AddEntriesUseCase,
	quickSendUC wc.QuickSendUseCase,
	conversationHub EntryUpdateBroadcaster,
	cloneStagesUC stage.CloneStagesFromGroupUseCase,
	getWorkflowUC workflow_domain.GetWorkflowUseCase,
	activeSubscription workspace_plan.EnsureActiveWorkspaceSubscriptionUseCase,
	getAgentUC agent.GetAgentUseCase,
	summaryUC wc.GetSummaryUseCase,
) *WhatsAppCampaignHandler {
	return &WhatsAppCampaignHandler{
		createUseCase:           createUC,
		updateUseCase:           updateUC,
		assignDepartmentUseCase: assignDepartmentUC,
		deleteUseCase:           deleteUC,
		getUseCase:              getUC,
		listUseCase:             listUC,
		listEntriesUseCase:      listEntriesUC,
		dispatchUseCase:         dispatchUC,
		messageConsumerUseCase:  messageConsumerUC,
		resetUseCase:            resetUC,
		clearHistoryUseCase:     clearHistoryUC,
		deleteEntryUseCase:      deleteEntryUC,
		updateEntryUseCase:      updateEntryUC,
		addEntriesUseCase:       addEntriesUC,
		quickSendUseCase:        quickSendUC,
		conversationHub:         conversationHub,
		cloneStagesUseCase:      cloneStagesUC,
		getWorkflowUseCase:      getWorkflowUC,
		activeSubscription:      activeSubscription,
		getAgentUseCase:         getAgentUC,
		summaryUseCase:          summaryUC,
	}
}

func (h *WhatsAppCampaignHandler) validateWhatsAppWorkflow(
	w http.ResponseWriter,
	workspaceID string,
	workflowID string,
) ([]string, bool) {
	workflowID = strings.TrimSpace(workflowID)
	if workflowID == "" {
		return nil, true
	}
	if h.getWorkflowUseCase == nil {
		response.WriteError(w, http.StatusInternalServerError, "Workflow validation unavailable", nil)
		return nil, false
	}
	wf, err := h.getWorkflowUseCase.Execute(workflowID)
	if err != nil {
		response.WriteValidationError(w, map[string]string{"workflowId": "invalid or not found"})
		return nil, false
	}
	if wf.WorkspaceID != workspaceID {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this workflow", nil)
		return nil, false
	}
	requiredVars := workflow_domain.ExtractCampaignVars(wf.Graph)
	return requiredVars, true
}

type whatsappCampaignRequest struct {
	Name                 string                             `json:"name"`
	Type                 string                             `json:"type,omitempty"`
	TemplateID           string                             `json:"templateId"`
	DepartmentID         string                             `json:"departmentId,omitempty"`
	BusinessPhoneID      string                             `json:"businessPhoneId,omitempty"`
	AgentID              string                             `json:"agentId,omitempty"`
	WorkflowID           string                             `json:"workflowId,omitempty"`
	PipelineID           string                             `json:"pipelineId,omitempty"`
	EnableAgentResponses bool                               `json:"enableAgentResponses"`
	EnableWorkflow       bool                               `json:"enableWorkflow"`
	EnableAnalysis       bool                               `json:"enableAnalysis"`
	EnableAutoStaging    bool                               `json:"EnableAutoStaging"`
	EnableAutoMemory     bool                               `json:"enableAutoMemory"`
	PreferAudio          bool                               `json:"preferAudio"`
	ShowTemplateInCrm    bool                               `json:"showTemplateInCrm"`
	AiModel              string                             `json:"aiModel,omitempty"`
	ScheduledStart       *string                            `json:"scheduledStart,omitempty"`
	StageGroupID         string                             `json:"StageGroupID,omitempty"`
	PhoneNumbers         []whatsappCampaignPhoneNumberInput `json:"phoneNumbers"`
}

type whatsappCampaignPhoneNumberInput struct {
	Number    string                 `json:"number"`
	Name      string                 `json:"name,omitempty"`
	Variables []string               `json:"variables,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

func (req whatsappCampaignRequest) toDomainPhoneInputs() []wc.PhoneInput {
	if len(req.PhoneNumbers) == 0 {
		return nil
	}
	inputs := make([]wc.PhoneInput, 0, len(req.PhoneNumbers))
	for _, item := range req.PhoneNumbers {
		inputs = append(inputs, wc.PhoneInput{
			Number:    item.Number,
			Name:      item.Name,
			Variables: item.Variables,
			Metadata:  item.Metadata,
		})
	}
	return inputs
}

func (h *WhatsAppCampaignHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req whatsappCampaignRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeInvalidBodyError(w)
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	workspaceID := middleware.GetWorkspaceID(r)
	requiredCampVars, ok := h.validateWhatsAppWorkflow(w, workspaceID, req.WorkflowID)
	if !ok {
		return
	}

	campaign := &wc.Campaign{
		WorkspaceID:          workspaceID,
		BusinessPhoneID:      req.BusinessPhoneID,
		Name:                 req.Name,
		Type:                 wc.CampaignType(req.Type),
		TemplateID:           req.TemplateID,
		AgentID:              req.AgentID,
		WorkflowID:           req.WorkflowID,
		PipelineID:           req.PipelineID,
		EnableAgentResponses: req.EnableAgentResponses,
		EnableWorkflow:       req.EnableWorkflow,
		EnableAnalysis:       req.EnableAnalysis && !req.EnableAgentResponses,
		EnableAutoStaging:    req.EnableAutoStaging && !req.EnableAgentResponses,
		EnableAutoMemory:     req.EnableAutoMemory && !req.EnableAgentResponses,
		PreferAudio:          req.PreferAudio && req.AgentID != "",
		ShowTemplateInCrm:    req.ShowTemplateInCrm,
		AiModel:              req.AiModel,
		PhoneInputs:          req.toDomainPhoneInputs(),
	}

	if req.ScheduledStart != nil && *req.ScheduledStart != "" {
		parsed, parseErr := time.Parse(time.RFC3339, *req.ScheduledStart)
		if parseErr != nil {
			response.WriteError(w, http.StatusBadRequest, "Invalid scheduledStart format, expected RFC3339 (e.g. 2026-02-23T10:00:00Z)", nil)
			return
		}
		campaign.ScheduledStart = parsed
	}

	r = withDepartmentCreationScope(r, req.DepartmentID)

	if len(requiredCampVars) > 0 {
		if err := campaign.ValidateWorkflowVars(requiredCampVars); err != nil {
			h.handleDomainError(w, err)
			return
		}
	}

	if req.AgentID != "" && h.getAgentUseCase != nil {
		agentRecord, agentErr := h.getAgentUseCase.Execute(req.AgentID)
		if agentErr == nil {
			for _, p := range req.PhoneNumbers {
				if err := agent.ValidateEntryMetadata(agentRecord, p.Metadata); err != nil {
					response.WriteErrorWithCode(w, http.StatusBadRequest, "AGENT_REQUIRED_VARIABLE_MISSING", err.Error(), map[string]string{"phoneNumbers": err.Error()})
					return
				}
			}
		}
	}

	created, err := h.createUseCase.Execute(r.Context(), campaign)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	if req.StageGroupID != "" && h.cloneStagesUseCase != nil {
		if err := h.cloneStagesUseCase.Execute(middleware.GetWorkspaceID(r), created.ID, "whatsapp", req.StageGroupID); err != nil {
			log.Printf("[WhatsAppCampaignHandler] clone stages from group %s: %v", req.StageGroupID, err)
		}
	}

	response.WriteSuccess(w, http.StatusCreated, created)
}

func (h *WhatsAppCampaignHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req whatsappCampaignRequest
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

	requiredCampVars, ok := h.validateWhatsAppWorkflow(w, middleware.GetWorkspaceID(r), req.WorkflowID)
	if !ok {
		return
	}

	updateCampaign := &wc.Campaign{
		BusinessPhoneID:      req.BusinessPhoneID,
		Name:                 req.Name,
		Type:                 wc.CampaignType(req.Type),
		TemplateID:           req.TemplateID,
		AgentID:              req.AgentID,
		WorkflowID:           req.WorkflowID,
		PipelineID:           req.PipelineID,
		EnableAgentResponses: req.EnableAgentResponses,
		EnableWorkflow:       req.EnableWorkflow,
		EnableAnalysis:       req.EnableAnalysis && !req.EnableAgentResponses,
		EnableAutoStaging:    req.EnableAutoStaging && !req.EnableAgentResponses,
		EnableAutoMemory:     req.EnableAutoMemory && !req.EnableAgentResponses,
		PreferAudio:          req.PreferAudio && req.AgentID != "",
		ShowTemplateInCrm:    req.ShowTemplateInCrm,
		AiModel:              req.AiModel,
		PhoneInputs:          req.toDomainPhoneInputs(),
	}

	if req.ScheduledStart != nil && *req.ScheduledStart != "" {
		parsed, parseErr := time.Parse(time.RFC3339, *req.ScheduledStart)
		if parseErr != nil {
			response.WriteError(w, http.StatusBadRequest, "Invalid scheduledStart format, expected RFC3339 (e.g. 2026-02-23T10:00:00Z)", nil)
			return
		}
		updateCampaign.ScheduledStart = parsed
	}

	if len(requiredCampVars) > 0 {
		if err := updateCampaign.ValidateWorkflowVars(requiredCampVars); err != nil {
			h.handleDomainError(w, err)
			return
		}
	}

	if req.AgentID != "" && h.getAgentUseCase != nil {
		agentRecord, agentErr := h.getAgentUseCase.Execute(req.AgentID)
		if agentErr == nil {
			for _, p := range req.PhoneNumbers {
				if err := agent.ValidateEntryMetadata(agentRecord, p.Metadata); err != nil {
					response.WriteErrorWithCode(w, http.StatusBadRequest, "AGENT_REQUIRED_VARIABLE_MISSING", err.Error(), map[string]string{"phoneNumbers": err.Error()})
					return
				}
			}
		}
	}

	updated, err := h.updateUseCase.Execute(id, updateCampaign)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, updated)
}

func (h *WhatsAppCampaignHandler) AssignDepartment(w http.ResponseWriter, r *http.Request) {
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

func (h *WhatsAppCampaignHandler) Delete(w http.ResponseWriter, r *http.Request) {
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

func (h *WhatsAppCampaignHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	campaignItem, err := h.getUseCase.Execute(id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	if campaignItem.WorkspaceID != middleware.GetWorkspaceID(r) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this campaign", nil)
		return
	}

	if !canAccessDepartment(r, campaignItem.DepartmentID) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this campaign", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, campaignItem)
}

func (h *WhatsAppCampaignHandler) List(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	values := r.URL.Query()

	opts := shared.QueryOptions{
		Pagination: parsePagination(values),
		Sorts:      parseSort(values, map[string]string{"name": "name", "createdat": "createdAt", "updatedat": "updatedAt"}),
	}

	templateIDs := splitListValues(values["templateId"])
	templateIDs = append(templateIDs, splitListValues(values["templateIds"])...)

	scheduledStartFilter := shared.Filter{}
	from := strings.TrimSpace(values.Get("scheduledStartFrom"))
	if from == "" {
		from = strings.TrimSpace(values.Get("scheduledStartGte"))
	}
	to := strings.TrimSpace(values.Get("scheduledStartTo"))
	if to == "" {
		to = strings.TrimSpace(values.Get("scheduledStartLte"))
	}

	switch {
	case from != "" && to != "":
		scheduledStartFilter = shared.Filter{Operator: shared.FilterOpBetween, Values: []string{from, to}}
	case from != "":
		scheduledStartFilter = shared.Filter{Operator: shared.FilterOpGte, Values: []string{from}}
	case to != "":
		scheduledStartFilter = shared.Filter{Operator: shared.FilterOpLte, Values: []string{to}}
	}

	notArchived := false
	input := wc.ListCampaignsInput{
		Search:         strings.TrimSpace(values.Get("search")),
		Type:           wc.CampaignType(strings.TrimSpace(values.Get("type"))),
		TemplateIDs:    templateIDs,
		Archived:       &notArchived,
		Options:        opts,
		WorkspaceID:    middleware.GetWorkspaceID(r),
		DepartmentIDs:  departmentFilterIDs(r),
		ScheduledStart: scheduledStartFilter,
	}
	if shouldReturnEmptyDepartmentList(r) {
		response.WritePaginated(w, http.StatusOK, []*wc.Campaign{}, response.PaginationMeta{
			Page:       opts.Pagination.Page,
			PageSize:   opts.Pagination.PageSize,
			TotalPages: 0,
			TotalItems: 0,
		})
		return
	}

	result, err := h.listUseCase.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list whatsapp campaigns", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

func (h *WhatsAppCampaignHandler) ListArchived(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	values := r.URL.Query()

	opts := shared.QueryOptions{
		Pagination: parsePagination(values),
		Sorts:      parseSort(values, map[string]string{"name": "name", "createdat": "createdAt", "updatedat": "updatedAt"}),
	}

	isArchived := true
	input := wc.ListCampaignsInput{
		Search:        strings.TrimSpace(values.Get("search")),
		Archived:      &isArchived,
		Options:       opts,
		WorkspaceID:   middleware.GetWorkspaceID(r),
		DepartmentIDs: departmentFilterIDs(r),
	}
	if shouldReturnEmptyDepartmentList(r) {
		response.WritePaginated(w, http.StatusOK, []*wc.Campaign{}, response.PaginationMeta{
			Page:       opts.Pagination.Page,
			PageSize:   opts.Pagination.PageSize,
			TotalPages: 0,
			TotalItems: 0,
		})
		return
	}

	result, err := h.listUseCase.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list archived whatsapp campaigns", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

func (h *WhatsAppCampaignHandler) Archive(w http.ResponseWriter, r *http.Request) {
	campaignID := mux.Vars(r)["id"]
	if campaignID == "" {
		response.WriteError(w, http.StatusBadRequest, "Campaign ID is required", nil)
		return
	}

	existing, err := h.getUseCase.Execute(campaignID)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	if existing.WorkspaceID != middleware.GetWorkspaceID(r) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this campaign", nil)
		return
	}

	existing.Archived = true
	if _, err := h.updateUseCase.Execute(campaignID, existing); err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to archive campaign", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{"archived": true})
}

func (h *WhatsAppCampaignHandler) Unarchive(w http.ResponseWriter, r *http.Request) {
	campaignID := mux.Vars(r)["id"]
	if campaignID == "" {
		response.WriteError(w, http.StatusBadRequest, "Campaign ID is required", nil)
		return
	}

	existing, err := h.getUseCase.Execute(campaignID)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	if existing.WorkspaceID != middleware.GetWorkspaceID(r) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this campaign", nil)
		return
	}

	existing.Archived = false
	if _, err := h.updateUseCase.Execute(campaignID, existing); err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to unarchive campaign", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{"archived": false})
}

func (h *WhatsAppCampaignHandler) ListEntries(w http.ResponseWriter, r *http.Request) {
	campaignID := mux.Vars(r)["id"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if !h.verifyOwnership(w, campaignID, claims, r) {
		return
	}

	values := r.URL.Query()

	opts := shared.QueryOptions{
		Pagination: parsePagination(values),
		Sorts:      parseSort(values, map[string]string{"number": "number", "name": "name", "status": "status", "createdat": "createdAt", "updatedat": "updatedAt"}),
	}

	var statusFilter wce.SendStatus
	if statusParam := values.Get("status"); statusParam != "" {
		statusFilter = wce.SendStatus(strings.TrimSpace(strings.ToUpper(statusParam)))
	}

	conversationFilter := parseWCConversationFilter(values)

	var errorCodeFilter int
	if ecStr := values.Get("errorCode"); ecStr != "" {
		if ec, err := strconv.Atoi(ecStr); err == nil {
			errorCodeFilter = ec
		}
	}

	result, err := h.listEntriesUseCase.Execute(wce.ListEntriesInput{
		CampaignID:         campaignID,
		Number:             strings.TrimSpace(values.Get("search")),
		Status:             statusFilter,
		StageID:            strings.TrimSpace(values.Get("StageID")),
		ErrorCode:          errorCodeFilter,
		ConversationFilter: conversationFilter,
		Options:            opts,
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

func parseWCConversationFilter(values url.Values) wce.ConversationFilter {
	filter := wce.ConversationFilter{
		ToolName:    strings.TrimSpace(values.Get("toolName")),
		MessageType: strings.TrimSpace(values.Get("messageType")),
	}

	if hasTools := values.Get("hasToolCalls"); hasTools != "" {
		val := strings.ToLower(hasTools) == "true"
		filter.HasToolCalls = &val
	}

	if minStr := values.Get("minMessageCount"); minStr != "" {
		if min, err := strconv.Atoi(minStr); err == nil {
			filter.MinMessageCount = &min
		}
	}
	if maxStr := values.Get("maxMessageCount"); maxStr != "" {
		if max, err := strconv.Atoi(maxStr); err == nil {
			filter.MaxMessageCount = &max
		}
	}

	return filter
}

func (h *WhatsAppCampaignHandler) StartCampaign(w http.ResponseWriter, r *http.Request) {
	campaignID := mux.Vars(r)["id"]
	if campaignID == "" {
		response.WriteValidationError(w, map[string]string{"id": "required"})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	campaign, err := h.getUseCase.Execute(campaignID)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	if campaign.WorkspaceID != middleware.GetWorkspaceID(r) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this campaign", nil)
		return
	}

	if h.activeSubscription != nil {
		if _, subErr := h.activeSubscription.Execute(campaign.WorkspaceID); subErr != nil {
			log.Printf("[SUBSCRIPTION] blocked whatsapp campaign start: workspace=%s user=%s campaign=%s err=%v", campaign.WorkspaceID, claims.UserID, campaignID, subErr)
			response.WriteCodedError(w, http.StatusPaymentRequired, "NO_ACTIVE_SUBSCRIPTION", "This workspace has no active subscription.")
			return
		}
	}

	if campaign.Metrics == nil || campaign.Metrics.TotalNumbers == 0 {
		response.WriteValidationError(w, map[string]string{"phoneNumbers": "campaign has no phone numbers to process"})
		return
	}

	activeCount := campaign.Metrics.Pending
	if activeCount == 0 && campaign.Metrics.Processed == campaign.Metrics.TotalNumbers {
		response.WriteValidationError(w, map[string]string{"phoneNumbers": "all phone numbers have already been processed"})
		return
	}

	if err := h.dispatchUseCase.Dispatch(wc.DispatchCampaignInput{
		CampaignID: campaign.ID,
		Action:     wc.CampaignActionStart,
	}); err != nil {
		h.handleDispatchError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusAccepted, map[string]string{"message": "WhatsApp campaign started"})
}

func (h *WhatsAppCampaignHandler) PauseCampaign(w http.ResponseWriter, r *http.Request) {
	h.dispatchCampaignAction(w, r, wc.CampaignActionPause, "WhatsApp campaign paused")
}

func (h *WhatsAppCampaignHandler) StopCampaign(w http.ResponseWriter, r *http.Request) {
	h.dispatchCampaignAction(w, r, wc.CampaignActionStop, "WhatsApp campaign stopped")
}

func (h *WhatsAppCampaignHandler) dispatchCampaignAction(w http.ResponseWriter, r *http.Request, action wc.CampaignAction, successMessage string) {
	campaignID := mux.Vars(r)["id"]
	if campaignID == "" {
		response.WriteValidationError(w, map[string]string{"id": "required"})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if !h.verifyOwnership(w, campaignID, claims, r) {
		return
	}

	if err := h.dispatchUseCase.Dispatch(wc.DispatchCampaignInput{
		CampaignID: campaignID,
		Action:     action,
	}); err != nil {
		h.handleDispatchError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusAccepted, map[string]string{"message": successMessage})
}

func (h *WhatsAppCampaignHandler) PrepareReset(w http.ResponseWriter, r *http.Request) {
	campaignID := mux.Vars(r)["id"]
	if campaignID == "" {
		response.WriteValidationError(w, map[string]string{"id": "required"})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if !h.verifyOwnership(w, campaignID, claims, r) {
		return
	}

	if h.resetUseCase == nil {
		response.WriteError(w, http.StatusInternalServerError, "Reset functionality not configured", nil)
		return
	}

	output, err := h.resetUseCase.PrepareReset(campaignID)
	if err != nil {
		h.handleResetError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, output)
}

func (h *WhatsAppCampaignHandler) ConfirmReset(w http.ResponseWriter, r *http.Request) {
	campaignID := mux.Vars(r)["id"]
	if campaignID == "" {
		response.WriteValidationError(w, map[string]string{"id": "required"})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if !h.verifyOwnership(w, campaignID, claims, r) {
		return
	}

	if h.resetUseCase == nil {
		response.WriteError(w, http.StatusInternalServerError, "Reset functionality not configured", nil)
		return
	}

	var body struct {
		ResetCode string `json:"resetCode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteValidationError(w, map[string]string{"resetCode": "required"})
		return
	}

	if body.ResetCode == "" {
		response.WriteValidationError(w, map[string]string{"resetCode": "required"})
		return
	}

	output, err := h.resetUseCase.ConfirmReset(wc.ResetCampaignInput{
		CampaignID: campaignID,
		ResetCode:  body.ResetCode,
	})
	if err != nil {
		h.handleResetError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, output)
}

func (h *WhatsAppCampaignHandler) PrepareClearHistory(w http.ResponseWriter, r *http.Request) {
	campaignID := mux.Vars(r)["id"]
	if campaignID == "" {
		response.WriteValidationError(w, map[string]string{"id": "required"})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if !h.verifyOwnership(w, campaignID, claims, r) {
		return
	}

	if h.clearHistoryUseCase == nil {
		response.WriteError(w, http.StatusInternalServerError, "Clear history functionality not configured", nil)
		return
	}

	output, err := h.clearHistoryUseCase.PrepareClearHistory(campaignID)
	if err != nil {
		h.handleClearHistoryError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, output)
}

func (h *WhatsAppCampaignHandler) ConfirmClearHistory(w http.ResponseWriter, r *http.Request) {
	campaignID := mux.Vars(r)["id"]
	if campaignID == "" {
		response.WriteValidationError(w, map[string]string{"id": "required"})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if !h.verifyOwnership(w, campaignID, claims, r) {
		return
	}

	if h.clearHistoryUseCase == nil {
		response.WriteError(w, http.StatusInternalServerError, "Clear history functionality not configured", nil)
		return
	}

	var body struct {
		ClearCode string `json:"clearCode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteValidationError(w, map[string]string{"clearCode": "required"})
		return
	}

	if body.ClearCode == "" {
		response.WriteValidationError(w, map[string]string{"clearCode": "required"})
		return
	}

	output, err := h.clearHistoryUseCase.ConfirmClearHistory(wc.ClearHistoryInput{
		CampaignID: campaignID,
		ClearCode:  body.ClearCode,
	})
	if err != nil {
		h.handleClearHistoryError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, output)
}

func (h *WhatsAppCampaignHandler) handleDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, wc.ErrCampaignNotFound):
		response.WriteError(w, http.StatusNotFound, "WhatsApp campaign not found", nil)
	case errors.Is(err, wc.ErrCampaignNameRequired):
		response.WriteValidationError(w, map[string]string{"name": "required"})
	case errors.Is(err, wc.ErrCampaignTemplateIDRequired):
		response.WriteValidationError(w, map[string]string{"templateId": "required"})
	case errors.Is(err, wc.ErrCampaignBusinessPhoneIDRequired):
		response.WriteValidationError(w, map[string]string{"businessPhoneId": "required"})
	case errors.Is(err, wc.ErrCampaignBusinessPhoneNotFound):
		response.WriteError(w, http.StatusNotFound, "Business phone number not found", nil)
	case errors.Is(err, wc.ErrCampaignBusinessPhoneNoAccess):
		response.WriteError(w, http.StatusForbidden, "You don't have access to this business phone number", nil)
	case errors.Is(err, wc.ErrCampaignPhoneNumbersRequired):
		response.WriteValidationError(w, map[string]string{"phoneNumbers": "at least one phone number is required"})
	case errors.Is(err, wc.ErrCampaignPhoneNumbersTooMany):
		response.WriteValidationError(w, map[string]string{"phoneNumbers": "exceeds maximum allowed"})
	case errors.Is(err, wc.ErrCampaignPhoneNumberInvalid):
		response.WriteValidationError(w, map[string]string{"phoneNumbers": "contains invalid phone numbers, correct format is E164, e.g: 5584999999999"})
	case errors.Is(err, workspace_department.ErrDepartmentRequired):
		response.WriteValidationError(w, map[string]string{"departmentId": "required when you belong to multiple departments"})
	case errors.Is(err, workspace_department.ErrDepartmentAccessDenied), errors.Is(err, workspace_department.ErrDepartmentNotFound):
		response.WriteValidationError(w, map[string]string{"departmentId": "invalid or not accessible"})
	case errors.Is(err, wc.ErrCampaignTemplateVariablesMismatch):
		response.WriteValidationError(w, map[string]string{"variables": "some phone numbers have fewer variables than required by the template"})
	case errors.Is(err, wc.ErrCampaignTemplateVariableEmpty):
		response.WriteValidationError(w, map[string]string{"variables": "template variables cannot be empty"})
	case errors.Is(err, wc.ErrCampaignTemplateNamedParameters):
		response.WriteValidationError(w, map[string]string{"templateId": "WhatsApp campaigns only support templates with positional parameters {{1}}, {{2}}, etc. - templates with named parameters like {{name}} are not allowed"})
	case errors.Is(err, wc.ErrCampaignWorkflowVarsMissing):
		response.WriteValidationError(w, map[string]string{"metadata": err.Error()})
	case errors.Is(err, agent.ErrAgentRequiredVariableMissing):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "AGENT_REQUIRED_VARIABLE_MISSING", err.Error(), nil)
	default:
		log.Printf("[WhatsAppCampaignHandler] Unhandled error: %v", err)
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}

func (h *WhatsAppCampaignHandler) handleDispatchError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, wc.ErrDispatchCampaignIDRequired):
		response.WriteValidationError(w, map[string]string{"id": "required"})
	case errors.Is(err, wc.ErrDispatchPhoneNumbersRequired):
		response.WriteValidationError(w, map[string]string{"phoneNumbers": "at least one phone number is required"})
	case errors.Is(err, wc.ErrDispatchActionRequired):
		response.WriteValidationError(w, map[string]string{"action": "invalid"})
	case errors.Is(err, wc.ErrDispatchCampaignAlreadyRunning):
		response.WriteValidationError(w, map[string]string{"status": "campaign is already running"})
	case errors.Is(err, wc.ErrDispatchCampaignNotRunning):
		response.WriteValidationError(w, map[string]string{"status": "campaign is not running"})
	case errors.Is(err, wc.ErrDispatchCampaignAlreadyStopped):
		response.WriteValidationError(w, map[string]string{"status": "campaign is already stopped"})
	default:
		response.WriteError(w, http.StatusInternalServerError, "Failed to process campaign action", nil)
	}
}

func (h *WhatsAppCampaignHandler) handleResetError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, wc.ErrCampaignNotFound):
		response.WriteError(w, http.StatusNotFound, "WhatsApp campaign not found", nil)
	case errors.Is(err, wc.ErrCampaignResetCodeInvalid):
		response.WriteValidationError(w, map[string]string{"resetCode": "invalid or expired reset code"})
	case errors.Is(err, wc.ErrCampaignResetNotAllowed):
		response.WriteValidationError(w, map[string]string{"status": "cannot reset a running campaign, stop it first"})
	default:
		response.WriteError(w, http.StatusInternalServerError, "Failed to reset campaign", nil)
	}
}

func (h *WhatsAppCampaignHandler) handleClearHistoryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, wc.ErrCampaignNotFound):
		response.WriteError(w, http.StatusNotFound, "WhatsApp campaign not found", nil)
	case errors.Is(err, wc.ErrCampaignClearCodeInvalid):
		response.WriteValidationError(w, map[string]string{"clearCode": "invalid or expired clear code"})
	case errors.Is(err, wc.ErrCampaignClearNotAllowed):
		response.WriteValidationError(w, map[string]string{"status": "cannot clear history of a running campaign, stop it first"})
	default:
		response.WriteError(w, http.StatusInternalServerError, "Failed to clear conversation history", nil)
	}
}

func (h *WhatsAppCampaignHandler) writeInvalidBodyError(w http.ResponseWriter) {
	response.WriteInvalidBodyError(w, map[string]string{
		"name":         "string (required)",
		"templateId":   "string (required)",
		"agentId":      "string (optional)",
		"departmentId": "string (optional; required for members who belong to multiple departments)",
		"phoneNumbers": "array of objects (required, each with number, optional name/variables/metadata). The 'variables' field is an array of strings used to fill template placeholders like {{1}}, {{2}}, etc., in order.",
	})
}

func (h *WhatsAppCampaignHandler) handleEntryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, wc.ErrCampaignNotFound):
		response.WriteError(w, http.StatusNotFound, "WhatsApp campaign not found", nil)
	case errors.Is(err, wc.ErrEntryNotFound):
		response.WriteError(w, http.StatusNotFound, "Campaign entry not found", nil)
	case errors.Is(err, wc.ErrCampaignRunning):
		response.WriteValidationError(w, map[string]string{"status": "operation not allowed while campaign is running, stop it first"})
	case errors.Is(err, wc.ErrEntryDuplicate):
		response.WriteValidationError(w, map[string]string{"number": "phone number already exists in this campaign"})
	case errors.Is(err, wc.ErrCampaignPhoneNumbersRequired):
		response.WriteValidationError(w, map[string]string{"phoneNumbers": "at least one phone number is required"})
	case errors.Is(err, wc.ErrCampaignPhoneNumbersTooMany):
		response.WriteValidationError(w, map[string]string{"phoneNumbers": "exceeds maximum allowed"})
	case errors.Is(err, wc.ErrCampaignPhoneNumberInvalid):
		response.WriteValidationError(w, map[string]string{"number": "invalid phone number format"})
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}

func (h *WhatsAppCampaignHandler) DeleteEntry(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	campaignID := vars["id"]
	entryID := vars["entryId"]

	if campaignID == "" {
		response.WriteValidationError(w, map[string]string{"id": "required"})
		return
	}
	if entryID == "" {
		response.WriteValidationError(w, map[string]string{"entryId": "required"})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if !h.verifyOwnership(w, campaignID, claims, r) {
		return
	}

	if h.deleteEntryUseCase == nil {
		response.WriteError(w, http.StatusInternalServerError, "Delete entry functionality not configured", nil)
		return
	}

	err := h.deleteEntryUseCase.Execute(wc.DeleteEntryInput{
		CampaignID: campaignID,
		EntryID:    entryID,
	})
	if err != nil {
		h.handleEntryError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *WhatsAppCampaignHandler) UpdateEntry(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	campaignID := vars["id"]
	entryID := vars["entryId"]

	if campaignID == "" {
		response.WriteValidationError(w, map[string]string{"id": "required"})
		return
	}
	if entryID == "" {
		response.WriteValidationError(w, map[string]string{"entryId": "required"})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if !h.verifyOwnership(w, campaignID, claims, r) {
		return
	}

	if h.updateEntryUseCase == nil {
		response.WriteError(w, http.StatusInternalServerError, "Update entry functionality not configured", nil)
		return
	}

	var body struct {
		Number            *string                `json:"number,omitempty"`
		Name              *string                `json:"name,omitempty"`
		Variables         []string               `json:"variables,omitempty"`
		Metadata          map[string]interface{} `json:"metadata,omitempty"`
		AutomationEnabled *bool                  `json:"automationEnabled,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	output, err := h.updateEntryUseCase.Execute(wc.UpdateEntryInput{
		CampaignID:        campaignID,
		EntryID:           entryID,
		Number:            body.Number,
		Name:              body.Name,
		Variables:         body.Variables,
		Metadata:          body.Metadata,
		AutomationEnabled: body.AutomationEnabled,
	})
	if err != nil {
		h.handleEntryError(w, err)
		return
	}

	if h.conversationHub != nil {
		go h.conversationHub.BroadcastEntryUpdate(entryID, "whatsapp", nil)
	}

	response.WriteSuccess(w, http.StatusOK, output)
}

func (h *WhatsAppCampaignHandler) ToggleEntryAI(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	campaignID := vars["id"]
	entryID := vars["entryId"]

	if campaignID == "" {
		response.WriteValidationError(w, map[string]string{"id": "required"})
		return
	}
	if entryID == "" {
		response.WriteValidationError(w, map[string]string{"entryId": "required"})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if !h.verifyOwnership(w, campaignID, claims, r) {
		return
	}

	if h.updateEntryUseCase == nil {
		response.WriteError(w, http.StatusInternalServerError, "Update entry functionality not configured", nil)
		return
	}

	var body struct {
		AutomationEnabled *bool `json:"automationEnabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}
	if body.AutomationEnabled == nil {
		response.WriteValidationError(w, map[string]string{"automationEnabled": "required"})
		return
	}

	output, err := h.updateEntryUseCase.Execute(wc.UpdateEntryInput{
		CampaignID:        campaignID,
		EntryID:           entryID,
		AutomationEnabled: body.AutomationEnabled,
	})
	if err != nil {
		h.handleEntryError(w, err)
		return
	}

	if h.conversationHub != nil {
		go h.conversationHub.BroadcastEntryUpdate(entryID, "whatsapp", nil)
	}

	response.WriteSuccess(w, http.StatusOK, output)
}

func (h *WhatsAppCampaignHandler) AddEntries(w http.ResponseWriter, r *http.Request) {
	campaignID := mux.Vars(r)["id"]
	if campaignID == "" {
		response.WriteValidationError(w, map[string]string{"id": "required"})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if !h.verifyOwnership(w, campaignID, claims, r) {
		return
	}

	if h.addEntriesUseCase == nil {
		response.WriteError(w, http.StatusInternalServerError, "Add entries functionality not configured", nil)
		return
	}

	var body struct {
		PhoneNumbers []struct {
			Number    string                 `json:"number"`
			Name      string                 `json:"name,omitempty"`
			Variables []string               `json:"variables,omitempty"`
			Metadata  map[string]interface{} `json:"metadata,omitempty"`
		} `json:"phoneNumbers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if len(body.PhoneNumbers) == 0 {
		response.WriteValidationError(w, map[string]string{"phoneNumbers": "at least one phone number is required"})
		return
	}

	if h.getUseCase != nil && h.getWorkflowUseCase != nil {
		campaign, campErr := h.getUseCase.Execute(campaignID)
		if campErr == nil && campaign != nil && campaign.WorkflowID != "" {
			requiredVars, wfOk := h.validateWhatsAppWorkflow(w, campaign.WorkspaceID, campaign.WorkflowID)
			if !wfOk {
				return
			}
			if len(requiredVars) > 0 {

				tmpInputs := make([]wc.PhoneInput, 0, len(body.PhoneNumbers))
				for _, p := range body.PhoneNumbers {
					tmpInputs = append(tmpInputs, wc.PhoneInput{
						Number:   p.Number,
						Metadata: p.Metadata,
					})
				}
				tmp := &wc.Campaign{PhoneInputs: tmpInputs}
				if err := tmp.ValidateWorkflowVars(requiredVars); err != nil {
					h.handleDomainError(w, err)
					return
				}
			}
		}
	}

	if len(body.PhoneNumbers) > 0 && h.getUseCase != nil && h.getAgentUseCase != nil {
		campaignForAgent, campErr := h.getUseCase.Execute(campaignID)
		if campErr == nil && campaignForAgent.AgentID != "" {
			agentRecord, agentErr := h.getAgentUseCase.Execute(campaignForAgent.AgentID)
			if agentErr == nil {
				for _, p := range body.PhoneNumbers {
					if err := agent.ValidateEntryMetadata(agentRecord, p.Metadata); err != nil {
						response.WriteErrorWithCode(w, http.StatusBadRequest, "AGENT_REQUIRED_VARIABLE_MISSING", err.Error(), map[string]string{"phoneNumbers": err.Error()})
						return
					}
				}
			}
		}
	}

	phoneInputs := make([]wc.EntryInput, 0, len(body.PhoneNumbers))
	for _, p := range body.PhoneNumbers {
		phoneInputs = append(phoneInputs, wc.EntryInput{
			Number:    p.Number,
			Name:      p.Name,
			Variables: p.Variables,
			Metadata:  p.Metadata,
		})
	}

	output, err := h.addEntriesUseCase.Execute(wc.AddEntriesInput{
		CampaignID:   campaignID,
		PhoneNumbers: phoneInputs,
	})
	if err != nil {
		h.handleEntryError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, output)
}

func (h *WhatsAppCampaignHandler) QuickSend(w http.ResponseWriter, r *http.Request) {
	campaignID := mux.Vars(r)["id"]
	if campaignID == "" {
		response.WriteValidationError(w, map[string]string{"id": "required"})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if h.quickSendUseCase == nil {
		response.WriteError(w, http.StatusInternalServerError, "Quick send functionality not configured", nil)
		return
	}

	campaign, err := h.getUseCase.Execute(campaignID)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	if campaign.WorkspaceID != middleware.GetWorkspaceID(r) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this campaign", nil)
		return
	}

	if h.activeSubscription != nil {
		if _, subErr := h.activeSubscription.Execute(campaign.WorkspaceID); subErr != nil {
			log.Printf("[SUBSCRIPTION] blocked whatsapp quick send: workspace=%s user=%s campaign=%s err=%v", campaign.WorkspaceID, claims.UserID, campaignID, subErr)
			response.WriteCodedError(w, http.StatusPaymentRequired, "NO_ACTIVE_SUBSCRIPTION", "This workspace has no active subscription.")
			return
		}
	}

	var body struct {
		PhoneNumbers []struct {
			Number    string                 `json:"number"`
			Name      string                 `json:"name,omitempty"`
			Variables []string               `json:"variables,omitempty"`
			Metadata  map[string]interface{} `json:"metadata,omitempty"`
		} `json:"phoneNumbers"`
	}
	if r.Body != nil {

		decErr := json.NewDecoder(r.Body).Decode(&body)
		if decErr != nil && !errors.Is(decErr, io.EOF) {
			response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
			return
		}
	}

	if len(body.PhoneNumbers) > 0 && h.getWorkflowUseCase != nil && campaign.WorkflowID != "" {
		requiredVars, wfOk := h.validateWhatsAppWorkflow(w, campaign.WorkspaceID, campaign.WorkflowID)
		if !wfOk {
			return
		}
		if len(requiredVars) > 0 {
			tmpInputs := make([]wc.PhoneInput, 0, len(body.PhoneNumbers))
			for _, p := range body.PhoneNumbers {
				tmpInputs = append(tmpInputs, wc.PhoneInput{
					Number:   p.Number,
					Metadata: p.Metadata,
				})
			}
			tmp := &wc.Campaign{PhoneInputs: tmpInputs}
			if err := tmp.ValidateWorkflowVars(requiredVars); err != nil {
				h.handleDomainError(w, err)
				return
			}
		}
	}

	if len(body.PhoneNumbers) > 0 && campaign.AgentID != "" && h.getAgentUseCase != nil {
		agentRecord, agentErr := h.getAgentUseCase.Execute(campaign.AgentID)
		if agentErr == nil {
			for _, p := range body.PhoneNumbers {
				if err := agent.ValidateEntryMetadata(agentRecord, p.Metadata); err != nil {
					response.WriteErrorWithCode(w, http.StatusBadRequest, "AGENT_REQUIRED_VARIABLE_MISSING", err.Error(), map[string]string{"phoneNumbers": err.Error()})
					return
				}
			}
		}
	}

	phoneInputs := make([]wc.EntryInput, 0, len(body.PhoneNumbers))
	for _, p := range body.PhoneNumbers {
		phoneInputs = append(phoneInputs, wc.EntryInput{
			Number:    p.Number,
			Name:      p.Name,
			Variables: p.Variables,
			Metadata:  p.Metadata,
		})
	}

	output, err := h.quickSendUseCase.Execute(wc.QuickSendInput{
		CampaignID:   campaignID,
		PhoneNumbers: phoneInputs,
	})
	if err != nil {

		switch {
		case errors.Is(err, whatsapp_campaign_usecase.ErrQuickSendBusy):
			response.WriteCodedError(w, http.StatusConflict, "QUICK_SEND_BUSY", "Another quick send is already in progress for this campaign. Please retry shortly.")
		case errors.Is(err, wc.ErrDispatchCampaignIDRequired),
			errors.Is(err, wc.ErrCampaignPhoneNumberInvalid),
			errors.Is(err, wc.ErrCampaignPhoneNumbersTooMany),
			errors.Is(err, wc.ErrCampaignWorkflowVarsMissing):
			h.handleEntryError(w, err)
		case errors.Is(err, wc.ErrCampaignStatusInvalid):
			h.handleDispatchError(w, err)
		default:
			h.handleEntryError(w, err)
		}
		return
	}

	response.WriteSuccess(w, http.StatusAccepted, output)
}

func (h *WhatsAppCampaignHandler) verifyOwnership(w http.ResponseWriter, campaignID string, claims *auth.Claims, r *http.Request) bool {
	campaign, err := h.getUseCase.Execute(campaignID)
	if err != nil {
		h.handleDomainError(w, err)
		return false
	}

	if campaign.WorkspaceID != middleware.GetWorkspaceID(r) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this campaign", nil)
		return false
	}

	return true
}
