package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/stage"
	workspace_department "vozko/domain/workspace/workspace_department"
	"vozko/infra/http/middleware"

	"github.com/gorilla/mux"
)

type StageGroupHandler struct {
	createUseCase stage.CreateStageGroupUseCase
	updateUseCase stage.UpdateStageGroupUseCase
	deleteUseCase stage.DeleteStageGroupUseCase
	listUseCase   stage.ListStageGroupsUseCase
	getUseCase    stage.GetStageGroupUseCase
}

func NewStageGroupHandler(
	createUseCase stage.CreateStageGroupUseCase,
	updateUseCase stage.UpdateStageGroupUseCase,
	deleteUseCase stage.DeleteStageGroupUseCase,
	listUseCase stage.ListStageGroupsUseCase,
	getUseCase stage.GetStageGroupUseCase,
) *StageGroupHandler {
	return &StageGroupHandler{
		createUseCase: createUseCase,
		updateUseCase: updateUseCase,
		deleteUseCase: deleteUseCase,
		listUseCase:   listUseCase,
		getUseCase:    getUseCase,
	}
}

func (h *StageGroupHandler) List(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if shouldReturnEmptyDepartmentList(r) {
		response.WriteSuccess(w, http.StatusOK, []*stage.StageGroup{})
		return
	}

	groups, err := h.listUseCase.Execute(workspaceID, departmentFilterIDs(r))
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list tag groups", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, groups)
}

func (h *StageGroupHandler) Get(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	groupID := mux.Vars(r)["id"]

	group, err := h.getUseCase.Execute(workspaceID, groupID)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	if !canAccessDepartment(r, group.DepartmentID) {
		response.WriteError(w, http.StatusForbidden, "access denied", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, group)
}

func (h *StageGroupHandler) Create(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)

	var input stage.CreateStageGroupInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	r = withDepartmentCreationScope(r, input.DepartmentID)

	group, err := h.createUseCase.Execute(r.Context(), workspaceID, input)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, group)
}

func (h *StageGroupHandler) Update(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	groupID := mux.Vars(r)["id"]

	existing, err := h.getUseCase.Execute(workspaceID, groupID)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	if !canAccessDepartment(r, existing.DepartmentID) {
		response.WriteError(w, http.StatusForbidden, "access denied", nil)
		return
	}

	var input stage.UpdateStageGroupInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	group, err := h.updateUseCase.Execute(workspaceID, groupID, input)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, group)
}

func (h *StageGroupHandler) Delete(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	groupID := mux.Vars(r)["id"]

	existing, err := h.getUseCase.Execute(workspaceID, groupID)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	if !canAccessDepartment(r, existing.DepartmentID) {
		response.WriteError(w, http.StatusForbidden, "access denied", nil)
		return
	}

	if err := h.deleteUseCase.Execute(workspaceID, groupID); err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (h *StageGroupHandler) handleDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, stage.ErrTagGroupNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, stage.ErrTagGroupNameRequired):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, stage.ErrUnauthorized):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	case errors.Is(err, workspace_department.ErrDepartmentAccessDenied):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}
