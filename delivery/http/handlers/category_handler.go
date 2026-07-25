package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/category"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"
)

type CategoryHandler struct {
	createUseCase category.CreateCategoryUseCase
	updateUseCase category.UpdateCategoryUseCase
	deleteUseCase category.DeleteCategoryUseCase
	getUseCase    category.GetCategoryUseCase
	listUseCase   category.ListCategoriesUseCase
}

func NewCategoryHandler(
	createUC category.CreateCategoryUseCase,
	updateUC category.UpdateCategoryUseCase,
	deleteUC category.DeleteCategoryUseCase,
	getUC category.GetCategoryUseCase,
	listUC category.ListCategoriesUseCase,
) *CategoryHandler {
	return &CategoryHandler{
		createUseCase: createUC,
		updateUseCase: updateUC,
		deleteUseCase: deleteUC,
		getUseCase:    getUC,
		listUseCase:   listUC,
	}
}

type createCategoryRequest struct {
	Name        string  `json:"name"`
	Slug        string  `json:"slug,omitempty"`
	Description string  `json:"description,omitempty"`
	ParentID    *string `json:"parentId,omitempty"`
}

type updateCategoryRequest struct {
	Name        *string `json:"name,omitempty"`
	Slug        *string `json:"slug,omitempty"`
	Description *string `json:"description,omitempty"`
	ParentID    *string `json:"parentId,omitempty"`
	ClearParent bool    `json:"clearParent,omitempty"`
}

func (h *CategoryHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", map[string]string{
			"name":        "string (required)",
			"slug":        "string (optional)",
			"description": "string (optional)",
			"parentId":    "string (optional)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	input := category.CreateCategoryInput{
		Name:        strings.TrimSpace(req.Name),
		Slug:        strings.TrimSpace(req.Slug),
		Description: strings.TrimSpace(req.Description),
	}

	if req.ParentID != nil {
		trimmed := strings.TrimSpace(*req.ParentID)
		if trimmed != "" {
			input.ParentID = &trimmed
		}
	}

	created, err := h.createUseCase.Execute(input)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, created)
}

func (h *CategoryHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req updateCategoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"name":        "string (optional)",
			"slug":        "string (optional)",
			"description": "string (optional)",
			"parentId":    "string (optional)",
			"clearParent": "boolean (optional)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	input := category.UpdateCategoryInput{
		ClearParent: req.ClearParent,
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		input.Name = &name
	}

	if req.Slug != nil {
		slug := strings.TrimSpace(*req.Slug)
		input.Slug = &slug
	}

	if req.Description != nil {
		description := strings.TrimSpace(*req.Description)
		input.Description = &description
	}

	if req.ParentID != nil {
		parent := strings.TrimSpace(*req.ParentID)
		input.ParentID = &parent
	}

	updated, err := h.updateUseCase.Execute(id, input)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, updated)
}

func (h *CategoryHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if err := h.deleteUseCase.Execute(id); err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusNoContent, nil)
}

func (h *CategoryHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	categoryItem, err := h.getUseCase.Execute(id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, categoryItem)
}

func (h *CategoryHandler) List(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()

	options := shared.QueryOptions{
		Pagination: parsePagination(values),
		Sorts:      parseSort(values, map[string]string{"name": "name", "slug": "slug", "createdat": "createdAt", "updatedat": "updatedAt"}),
	}

	search := strings.TrimSpace(values.Get("search"))

	var parentID *string
	if raw := strings.TrimSpace(values.Get("parentId")); raw != "" {
		if strings.EqualFold(raw, "null") || strings.EqualFold(raw, "root") {
			empty := ""
			parentID = &empty
		} else {
			value := raw
			parentID = &value
		}
	}

	result, err := h.listUseCase.Execute(category.ListCategoriesInput{
		Search:   search,
		ParentID: parentID,
		Options:  options,
	})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list categories", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

func (h *CategoryHandler) handleDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, category.ErrCategoryNotFound):
		response.WriteError(w, http.StatusNotFound, "Category not found", nil)
	case errors.Is(err, category.ErrCategoryNameRequired):
		response.WriteValidationError(w, map[string]string{"name": "required"})
	case errors.Is(err, category.ErrCategorySlugRequired):
		response.WriteValidationError(w, map[string]string{"slug": "required"})
	case errors.Is(err, category.ErrCategorySlugInvalid):
		response.WriteValidationError(w, map[string]string{"slug": "must contain only letters, numbers, or dashes"})
	case errors.Is(err, category.ErrCategorySlugExists):
		response.WriteValidationError(w, map[string]string{"slug": "slug already exists"})
	case errors.Is(err, category.ErrCategoryParentNotFound):
		response.WriteValidationError(w, map[string]string{"parentId": "parent category not found"})
	case errors.Is(err, category.ErrCategoryCycle):
		response.WriteValidationError(w, map[string]string{"parentId": "cannot reference itself or its descendants"})
	case errors.Is(err, category.ErrCategoryHasChildren):
		response.WriteError(w, http.StatusBadRequest, "Category has child categories", nil)
	case errors.Is(err, category.ErrCategoryInUse):
		response.WriteError(w, http.StatusBadRequest, "Category is associated with existing products", nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}
