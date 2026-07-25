package workspacetemplateaccess

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/shared"
	workspacetemplateaccessdomain "vozko/domain/workspace_template_access"
	"vozko/infra/http/middleware"
)

type WorkspaceTemplateAccessHandler struct {
	grantAccessUseCase         workspacetemplateaccessdomain.GrantAccessUseCase
	revokeAccessUseCase        workspacetemplateaccessdomain.RevokeAccessUseCase
	listWorkspaceAccessUseCase workspacetemplateaccessdomain.ListWorkspaceAccessUseCase
	listTemplateAccessUseCase  workspacetemplateaccessdomain.ListTemplateAccessUseCase
}

func NewWorkspaceTemplateAccessHandler(
	grantUC workspacetemplateaccessdomain.GrantAccessUseCase,
	revokeUC workspacetemplateaccessdomain.RevokeAccessUseCase,
	listWorkspaceUC workspacetemplateaccessdomain.ListWorkspaceAccessUseCase,
	listTemplateUC workspacetemplateaccessdomain.ListTemplateAccessUseCase,
) *WorkspaceTemplateAccessHandler {
	return &WorkspaceTemplateAccessHandler{
		grantAccessUseCase:         grantUC,
		revokeAccessUseCase:        revokeUC,
		listWorkspaceAccessUseCase: listWorkspaceUC,
		listTemplateAccessUseCase:  listTemplateUC,
	}
}

func (h *WorkspaceTemplateAccessHandler) GrantAccess(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var req GrantAccessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if errs := req.Validate(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}

	result, err := h.grantAccessUseCase.Execute(workspacetemplateaccessdomain.GrantAccessInput{
		WorkspaceID: req.WorkspaceID,
		TemplateID:  req.TemplateID,
		GrantedBy:   claims.UserID,
	})
	if err != nil {
		if errors.Is(err, workspacetemplateaccessdomain.ErrAccessAlreadyExists) {
			response.WriteError(w, http.StatusConflict, "Workspace already has access to this template", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to grant access", nil)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, toAccessResponse(result))
}

func (h *WorkspaceTemplateAccessHandler) RevokeAccess(w http.ResponseWriter, r *http.Request) {
	var req RevokeAccessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if errs := req.Validate(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}

	err := h.revokeAccessUseCase.Execute(workspacetemplateaccessdomain.RevokeAccessInput{
		WorkspaceID: req.WorkspaceID,
		TemplateID:  req.TemplateID,
	})
	if err != nil {
		if errors.Is(err, workspacetemplateaccessdomain.ErrAccessNotFound) {
			response.WriteError(w, http.StatusNotFound, "Access not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to revoke access", nil)
		return
	}

	response.WriteSuccess(w, http.StatusNoContent, nil)
}

func (h *WorkspaceTemplateAccessHandler) ListByWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspaceId"]
	values := r.URL.Query()

	input := workspacetemplateaccessdomain.ListWorkspaceAccessInput{
		WorkspaceID: workspaceID,
		QueryOptions: shared.QueryOptions{
			Pagination: httpx.ParsePagination(values),
		},
	}

	result, err := h.listWorkspaceAccessUseCase.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list access", nil)
		return
	}

	items := make([]AccessResponse, len(result.Items))
	for i, a := range result.Items {
		items[i] = toAccessResponse(a)
	}

	response.WriteSuccess(w, http.StatusOK, shared.PaginatedResult[AccessResponse]{
		Items:      items,
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalItems: result.TotalItems,
		TotalPages: result.TotalPages,
	})
}

func (h *WorkspaceTemplateAccessHandler) ListByTemplate(w http.ResponseWriter, r *http.Request) {
	templateID := mux.Vars(r)["templateId"]
	values := r.URL.Query()

	input := workspacetemplateaccessdomain.ListTemplateAccessInput{
		TemplateID: templateID,
		QueryOptions: shared.QueryOptions{
			Pagination: httpx.ParsePagination(values),
		},
	}

	result, err := h.listTemplateAccessUseCase.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list access", nil)
		return
	}

	items := make([]AccessResponse, len(result.Items))
	for i, a := range result.Items {
		items[i] = toAccessResponse(a)
	}

	response.WriteSuccess(w, http.StatusOK, shared.PaginatedResult[AccessResponse]{
		Items:      items,
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalItems: result.TotalItems,
		TotalPages: result.TotalPages,
	})
}
