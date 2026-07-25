package handlers

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"vozko/delivery/http/response"
	"vozko/domain/property"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"

	"github.com/gorilla/mux"
)

type PropertyHandler struct {
	createUseCase property.CreatePropertyUseCase
	updateUseCase property.UpdatePropertyUseCase
	getUseCase    property.GetPropertyUseCase
	listUseCase   property.ListPropertiesUseCase
	searchUseCase property.SearchPropertiesUseCase
	deleteUseCase property.DeletePropertyUseCase
}

func NewPropertyHandler(
	createUseCase property.CreatePropertyUseCase,
	updateUseCase property.UpdatePropertyUseCase,
	getUseCase property.GetPropertyUseCase,
	listUseCase property.ListPropertiesUseCase,
	searchUseCase property.SearchPropertiesUseCase,
	deleteUseCase property.DeletePropertyUseCase,
) *PropertyHandler {
	return &PropertyHandler{
		createUseCase: createUseCase,
		updateUseCase: updateUseCase,
		getUseCase:    getUseCase,
		listUseCase:   listUseCase,
		searchUseCase: searchUseCase,
		deleteUseCase: deleteUseCase,
	}
}

func (h *PropertyHandler) List(w http.ResponseWriter, r *http.Request) {
	queryValues := r.URL.Query()

	var latitude, longitude *float64
	if latStr := strings.TrimSpace(queryValues.Get("latitude")); latStr != "" {
		if lat, err := strconv.ParseFloat(latStr, 64); err == nil {
			latitude = &lat
		}
	}
	if lonStr := strings.TrimSpace(queryValues.Get("longitude")); lonStr != "" {
		if lon, err := strconv.ParseFloat(lonStr, 64); err == nil {
			longitude = &lon
		}
	}

	options := shared.QueryOptions{
		Pagination: parsePagination(queryValues),
		Sorts: parseSort(queryValues, map[string]string{
			"createdat": "createdAt",
			"updatedat": "updatedAt",
			"name":      "name",
			"price":     "price",
			"totalarea": "totalArea",
			"bedrooms":  "bedrooms",
			"distance":  "distance",
		}),
		Filters: parsePropertyFilters(queryValues),
	}

	search := strings.TrimSpace(queryValues.Get("search"))
	if search == "" {
		search = strings.TrimSpace(queryValues.Get("q"))
	}

	result, err := h.listUseCase.Execute(property.ListPropertiesInput{
		Search:    search,
		Options:   options,
		Latitude:  latitude,
		Longitude: longitude,
	})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch properties", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

func (h *PropertyHandler) Get(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	propertyID := vars["id"]

	p, err := h.getUseCase.Execute(propertyID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			response.WriteError(w, http.StatusNotFound, "Property not found", nil)
		} else {
			response.WriteError(w, http.StatusInternalServerError, "Failed to fetch property", nil)
		}
		return
	}

	response.WriteSuccess(w, http.StatusOK, p)
}

func (h *PropertyHandler) Create(w http.ResponseWriter, r *http.Request) {
	var p property.Property
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"name":        "string (required)",
			"description": "string (required)",
			"type":        "HOUSE|APARTMENT|COMMERCIAL|LAND|CONDO|PENTHOUSE (required)",
			"location":    "object with address, city, latitude, longitude (required)",
			"price":       "number (required)",
			"totalArea":   "number (required)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	p.CreatedBy = claims.UserID

	if err := h.createUseCase.Execute(wsID, &p); err != nil {
		if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "must be") {
			response.WriteValidationError(w, map[string]string{"error": err.Error()})
		} else if strings.Contains(err.Error(), "shop") || strings.Contains(err.Error(), "unauthorized") {
			response.WriteError(w, http.StatusForbidden, err.Error(), nil)
		} else {
			response.WriteError(w, http.StatusInternalServerError, "Failed to create property", nil)
		}
		return
	}

	response.WriteSuccess(w, http.StatusCreated, p)
}

func (h *PropertyHandler) Update(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	propertyID := vars["id"]

	var p property.Property
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"name":        "string (required)",
			"description": "string (required)",
			"type":        "HOUSE|APARTMENT|COMMERCIAL|LAND|CONDO|PENTHOUSE (required)",
			"location":    "object with address, city, latitude, longitude (required)",
			"price":       "number (required)",
			"totalArea":   "number (required)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	if err := h.updateUseCase.Execute(wsID, propertyID, &p); err != nil {
		if strings.Contains(err.Error(), "not found") {
			response.WriteError(w, http.StatusNotFound, "Property not found", nil)
		} else if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "must be") {
			response.WriteValidationError(w, map[string]string{"error": err.Error()})
		} else if strings.Contains(err.Error(), "shop") || strings.Contains(err.Error(), "unauthorized") {
			response.WriteError(w, http.StatusForbidden, err.Error(), nil)
		} else {
			response.WriteError(w, http.StatusInternalServerError, "Failed to update property", nil)
		}
		return
	}

	response.WriteSuccess(w, http.StatusOK, p)
}

func (h *PropertyHandler) Delete(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	propertyID := vars["id"]

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if err := h.deleteUseCase.Execute(propertyID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			response.WriteError(w, http.StatusNotFound, "Property not found", nil)
		} else {
			response.WriteError(w, http.StatusInternalServerError, "Failed to delete property", nil)
		}
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]string{"message": "Property deleted successfully"})
}

func (h *PropertyHandler) Search(w http.ResponseWriter, r *http.Request) {
	queryValues := r.URL.Query()
	options := shared.QueryOptions{
		Pagination: parsePagination(queryValues),
		Sorts: parseSort(queryValues, map[string]string{
			"createdat": "createdAt",
			"updatedat": "updatedAt",
			"name":      "name",
			"price":     "price",
			"totalarea": "totalArea",
			"bedrooms":  "bedrooms",
		}),
		Filters: parsePropertyFilters(queryValues),
	}

	query := strings.TrimSpace(queryValues.Get("query"))
	if query == "" {
		query = strings.TrimSpace(queryValues.Get("q"))
	}

	result, err := h.searchUseCase.Execute(property.SearchPropertiesInput{
		Query:   query,
		Options: options,
	})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to search properties", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

func parsePropertyFilters(values url.Values) []shared.Filter {
	filters := make([]shared.Filter, 0)

	if propertyType := strings.TrimSpace(firstQueryValue(values["type"])); propertyType != "" {
		filters = append(filters, shared.Filter{Field: "type", Operator: shared.FilterOpEquals, Values: []string{propertyType}})
	}

	if status := strings.TrimSpace(firstQueryValue(values["status"])); status != "" {
		filters = append(filters, shared.Filter{Field: "status", Operator: shared.FilterOpEquals, Values: []string{status}})
	}

	if condition := strings.TrimSpace(firstQueryValue(values["condition"])); condition != "" {
		filters = append(filters, shared.Filter{Field: "condition", Operator: shared.FilterOpEquals, Values: []string{condition}})
	}

	if city := strings.TrimSpace(firstQueryValue(values["city"])); city != "" {
		filters = append(filters, shared.Filter{Field: "city", Operator: shared.FilterOpLike, Values: []string{city}})
	}

	if state := strings.TrimSpace(firstQueryValue(values["state"])); state != "" {
		filters = append(filters, shared.Filter{Field: "state", Operator: shared.FilterOpEquals, Values: []string{state}})
	}

	if neighborhood := strings.TrimSpace(firstQueryValue(values["neighborhood"])); neighborhood != "" {
		filters = append(filters, shared.Filter{Field: "neighborhood", Operator: shared.FilterOpLike, Values: []string{neighborhood}})
	}

	if minPrice := strings.TrimSpace(firstQueryValue(values["minPrice"])); minPrice != "" {
		filters = append(filters, shared.Filter{Field: "minPrice", Operator: shared.FilterOpGte, Values: []string{minPrice}})
	}

	if maxPrice := strings.TrimSpace(firstQueryValue(values["maxPrice"])); maxPrice != "" {
		filters = append(filters, shared.Filter{Field: "maxPrice", Operator: shared.FilterOpLte, Values: []string{maxPrice}})
	}

	if minBedrooms := strings.TrimSpace(firstQueryValue(values["minBedrooms"])); minBedrooms != "" {
		filters = append(filters, shared.Filter{Field: "minBedrooms", Operator: shared.FilterOpGte, Values: []string{minBedrooms}})
	}

	if minBathrooms := strings.TrimSpace(firstQueryValue(values["minBathrooms"])); minBathrooms != "" {
		filters = append(filters, shared.Filter{Field: "minBathrooms", Operator: shared.FilterOpGte, Values: []string{minBathrooms}})
	}

	if minArea := strings.TrimSpace(firstQueryValue(values["minArea"])); minArea != "" {
		filters = append(filters, shared.Filter{Field: "minArea", Operator: shared.FilterOpGte, Values: []string{minArea}})
	}

	if maxArea := strings.TrimSpace(firstQueryValue(values["maxArea"])); maxArea != "" {
		filters = append(filters, shared.Filter{Field: "maxArea", Operator: shared.FilterOpLte, Values: []string{maxArea}})
	}

	if categoryID := strings.TrimSpace(firstQueryValue(values["categoryId"])); categoryID != "" {
		filters = append(filters, shared.Filter{Field: "categoryId", Operator: shared.FilterOpEquals, Values: []string{categoryID}})
	}

	if category := strings.TrimSpace(firstQueryValue(values["category"])); category != "" {
		filters = append(filters, shared.Filter{Field: "category", Operator: shared.FilterOpEquals, Values: []string{category}})
	}

	if createdFrom := strings.TrimSpace(firstQueryValue(values["createdFrom"])); createdFrom != "" {
		filters = append(filters, shared.Filter{Field: "createdAt", Operator: shared.FilterOpGte, Values: []string{createdFrom}})
	}

	if createdTo := strings.TrimSpace(firstQueryValue(values["createdTo"])); createdTo != "" {
		filters = append(filters, shared.Filter{Field: "createdAt", Operator: shared.FilterOpLte, Values: []string{createdTo}})
	}

	return filters
}
