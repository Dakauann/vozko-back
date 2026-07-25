package workspacephoneaccess

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/shared"
	phoneaccessdomain "vozko/domain/workspace_phone_access"
	"vozko/infra/http/middleware"
)

type WorkspacePhoneAccessHandler struct {
	grantAccessUseCase         phoneaccessdomain.GrantAccessUseCase
	revokeAccessUseCase        phoneaccessdomain.RevokeAccessUseCase
	listWorkspaceAccessUseCase phoneaccessdomain.ListWorkspaceAccessUseCase
	listPhoneAccessUseCase     phoneaccessdomain.ListPhoneAccessUseCase
}

func NewWorkspacePhoneAccessHandler(
	grantUC phoneaccessdomain.GrantAccessUseCase,
	revokeUC phoneaccessdomain.RevokeAccessUseCase,
	listWorkspaceUC phoneaccessdomain.ListWorkspaceAccessUseCase,
	listPhoneUC phoneaccessdomain.ListPhoneAccessUseCase,
) *WorkspacePhoneAccessHandler {
	return &WorkspacePhoneAccessHandler{
		grantAccessUseCase:         grantUC,
		revokeAccessUseCase:        revokeUC,
		listWorkspaceAccessUseCase: listWorkspaceUC,
		listPhoneAccessUseCase:     listPhoneUC,
	}
}

func (h *WorkspacePhoneAccessHandler) GrantAccess(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var req GrantPhoneAccessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if errs := req.Validate(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}

	result, err := h.grantAccessUseCase.Execute(phoneaccessdomain.GrantAccessInput{
		WorkspaceID: req.WorkspaceID,
		PhoneID:     req.PhoneID,
		GrantedBy:   claims.UserID,
	})
	if err != nil {
		if errors.Is(err, phoneaccessdomain.ErrAccessAlreadyExists) {
			response.WriteError(w, http.StatusConflict, "Workspace already has access to this phone number", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to grant access", nil)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, mapPhoneAccessToResponse(result))
}

func (h *WorkspacePhoneAccessHandler) RevokeAccess(w http.ResponseWriter, r *http.Request) {
	var req RevokePhoneAccessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if errs := req.Validate(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}

	err := h.revokeAccessUseCase.Execute(phoneaccessdomain.RevokeAccessInput{
		WorkspaceID: req.WorkspaceID,
		PhoneID:     req.PhoneID,
	})
	if err != nil {
		if errors.Is(err, phoneaccessdomain.ErrAccessNotFound) {
			response.WriteError(w, http.StatusNotFound, "Access not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to revoke access", nil)
		return
	}

	response.WriteSuccess(w, http.StatusNoContent, nil)
}

func (h *WorkspacePhoneAccessHandler) ListByWorkspace(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspaceId"]
	values := r.URL.Query()

	input := phoneaccessdomain.ListWorkspaceAccessInput{
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

	items := make([]PhoneAccessResponse, len(result.Items))
	for i, a := range result.Items {
		items[i] = mapPhoneAccessToResponse(a)
	}

	response.WriteSuccess(w, http.StatusOK, shared.PaginatedResult[PhoneAccessResponse]{
		Items:      items,
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalItems: result.TotalItems,
		TotalPages: result.TotalPages,
	})
}

func (h *WorkspacePhoneAccessHandler) ListByPhone(w http.ResponseWriter, r *http.Request) {
	phoneID := mux.Vars(r)["phoneId"]
	values := r.URL.Query()

	input := phoneaccessdomain.ListPhoneAccessInput{
		PhoneID: phoneID,
		QueryOptions: shared.QueryOptions{
			Pagination: httpx.ParsePagination(values),
		},
	}

	result, err := h.listPhoneAccessUseCase.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list access", nil)
		return
	}

	items := make([]PhoneAccessResponse, len(result.Items))
	for i, a := range result.Items {
		items[i] = mapPhoneAccessToResponse(a)
	}

	response.WriteSuccess(w, http.StatusOK, shared.PaginatedResult[PhoneAccessResponse]{
		Items:      items,
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalItems: result.TotalItems,
		TotalPages: result.TotalPages,
	})
}
