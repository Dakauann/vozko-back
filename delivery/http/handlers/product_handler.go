package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"vozko/delivery/http/response"
	"vozko/domain/product"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"

	"github.com/gorilla/mux"
)

type ProductHandler struct {
	createUseCase      product.CreateProductUseCase
	updateUseCase      product.UpdateProductUseCase
	launchStockUseCase product.LaunchVariantStockUseCase
	getUseCase         product.GetProductUseCase
	listUseCase        product.ListProductsUseCase
	searchUseCase      product.SearchProductsUseCase
}

func NewProductHandler(createUseCase product.CreateProductUseCase, updateUseCase product.UpdateProductUseCase, launchStockUseCase product.LaunchVariantStockUseCase, getUseCase product.GetProductUseCase, listUseCase product.ListProductsUseCase, searchUseCase product.SearchProductsUseCase) *ProductHandler {
	return &ProductHandler{
		createUseCase:      createUseCase,
		updateUseCase:      updateUseCase,
		launchStockUseCase: launchStockUseCase,
		getUseCase:         getUseCase,
		listUseCase:        listUseCase,
		searchUseCase:      searchUseCase,
	}
}

func (h *ProductHandler) List(w http.ResponseWriter, r *http.Request) {
	queryValues := r.URL.Query()
	options := shared.QueryOptions{
		Pagination: parsePagination(queryValues),
		Sorts:      parseSort(queryValues, map[string]string{"createdat": "createdAt", "updatedat": "updatedAt", "name": "name", "price": "price"}),
		Filters:    parseProductFilters(queryValues),
	}

	search := strings.TrimSpace(queryValues.Get("search"))
	if search == "" {
		search = strings.TrimSpace(queryValues.Get("q"))
	}

	result, err := h.listUseCase.Execute(product.ListProductsInput{
		Search:  search,
		Options: options,
	})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch products", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

func (h *ProductHandler) Get(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	productID := vars["id"]

	p, err := h.getUseCase.Execute(productID)
	if err != nil {
		if errors.Is(err, product.ErrProductNotFound) {
			response.WriteError(w, http.StatusNotFound, "Product not found", nil)
		} else {
			response.WriteError(w, http.StatusInternalServerError, "Failed to fetch product", nil)
		}
		return
	}

	response.WriteSuccess(w, http.StatusOK, p)
}

func (h *ProductHandler) Create(w http.ResponseWriter, r *http.Request) {
	var p product.Product
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"name":        "string (required)",
			"description": "string (required)",
			"variants":    "array of product variants (required)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	if err := h.createUseCase.Execute(wsID, &p); err != nil {
		switch {
		case errors.Is(err, product.ErrProductNameRequired):
			response.WriteValidationError(w, map[string]string{"name": "required"})
		case errors.Is(err, product.ErrProductNameTooShort):
			response.WriteValidationError(w, map[string]string{"name": "must be at least 3 characters"})
		case errors.Is(err, product.ErrProductNameTooLong):
			response.WriteValidationError(w, map[string]string{"name": "must not exceed 255 characters"})
		case errors.Is(err, product.ErrProductDescriptionRequired):
			response.WriteValidationError(w, map[string]string{"description": "required"})
		case errors.Is(err, product.ErrProductDescriptionTooShort):
			response.WriteValidationError(w, map[string]string{"description": "must be at least 10 characters"})
		case errors.Is(err, product.ErrProductVariantsRequired):
			response.WriteValidationError(w, map[string]string{"variants": "at least one variant is required"})
		case errors.Is(err, product.ErrVariantSKURequired):
			response.WriteValidationError(w, map[string]string{"variant.sku": "required for all variants"})
		case errors.Is(err, product.ErrVariantNameRequired):
			response.WriteValidationError(w, map[string]string{"variant.name": "required for all variants"})
		case errors.Is(err, product.ErrVariantNameTooLong):
			response.WriteValidationError(w, map[string]string{"variant.name": "must not exceed 255 characters"})
		case errors.Is(err, product.ErrVariantSKUTooLong):
			response.WriteValidationError(w, map[string]string{"variant.sku": "must not exceed 50 characters"})
		case errors.Is(err, product.ErrVariantSKUDuplicate):
			response.WriteValidationError(w, map[string]string{"variant.sku": "SKU already exists, please use a unique SKU"})
		case errors.Is(err, product.ErrVariantRetailPriceInvalid):
			response.WriteValidationError(w, map[string]string{"variant.retailPrice": "must be greater than 0"})
		case errors.Is(err, product.ErrVariantWholesalePriceInvalid):
			response.WriteValidationError(w, map[string]string{"variant.wholesalePrice": "must be greater than 0"})
		case errors.Is(err, product.ErrVariantCostInvalid):
			response.WriteValidationError(w, map[string]string{"variant.cost": "cannot be negative"})
		case errors.Is(err, product.ErrVariantInventoryInvalid):
			response.WriteValidationError(w, map[string]string{"variant.inventory": "cannot be negative"})
		case errors.Is(err, product.ErrVariantMinQuantityInvalid):
			response.WriteValidationError(w, map[string]string{"variant.minQuantityForWholesale": "must be greater than 0"})
		case errors.Is(err, product.ErrVariantWeightInvalid):
			response.WriteValidationError(w, map[string]string{"variant.weight": "cannot be negative"})
		case errors.Is(err, product.ErrVariantDimensionsInvalid):
			response.WriteValidationError(w, map[string]string{"variant.dimensions": "cannot be negative"})
		case errors.Is(err, product.ErrVariantOptionTypeRequired):
			response.WriteValidationError(w, map[string]string{"variant.options.optionType": "required when creating new options"})
		case errors.Is(err, product.ErrVariantOptionValueRequired):
			response.WriteValidationError(w, map[string]string{"variant.options.optionValue": "required when creating new options"})
		case errors.Is(err, product.ErrVariantMediaRequired):
			response.WriteValidationError(w, map[string]string{"variant.mediaIDs": "at least one media is required"})
		case errors.Is(err, product.ErrMediaNotFound):
			response.WriteValidationError(w, map[string]string{"variant.mediaIDs": "one or more media IDs not found"})
		case errors.Is(err, product.ErrVariantCategoryRequired):
			response.WriteValidationError(w, map[string]string{"variant.categoryId": "required for all variants"})
		case errors.Is(err, product.ErrVariantCategoryNotFound):
			response.WriteValidationError(w, map[string]string{"variant.categoryId": "must reference an existing category"})
		case errors.Is(err, product.ErrVariantCategoryMustBeLeaf):
			response.WriteValidationError(w, map[string]string{"variant.categoryId": "must be a leaf category (category has subcategories)"})
		case errors.Is(err, product.ErrProductShopNotFound):
			response.WriteError(w, http.StatusNotFound, "Product shop not found", nil)
		case errors.Is(err, product.ErrProductShopUnauthorized):
			response.WriteError(w, http.StatusForbidden, "Unauthorized to manage products for this shop", nil)
		default:
			response.WriteError(w, http.StatusInternalServerError, "Failed to create product", nil)
		}
		return
	}

	response.WriteSuccess(w, http.StatusCreated, p)
}

func (h *ProductHandler) Update(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	productID := vars["id"]

	var input product.UpdateProductInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"name":        "string (optional)",
			"description": "string (optional)",
			"variants":    "array of variant updates (optional)",
		})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	if err := h.updateUseCase.Execute(wsID, productID, &input); err != nil {
		switch {
		case errors.Is(err, product.ErrProductNameRequired):
			response.WriteValidationError(w, map[string]string{"name": "required"})
		case errors.Is(err, product.ErrProductNameTooShort):
			response.WriteValidationError(w, map[string]string{"name": "must be at least 3 characters"})
		case errors.Is(err, product.ErrProductNameTooLong):
			response.WriteValidationError(w, map[string]string{"name": "must not exceed 255 characters"})
		case errors.Is(err, product.ErrProductDescriptionRequired):
			response.WriteValidationError(w, map[string]string{"description": "required"})
		case errors.Is(err, product.ErrProductDescriptionTooShort):
			response.WriteValidationError(w, map[string]string{"description": "must be at least 10 characters"})
		case errors.Is(err, product.ErrProductVariantsRequired):
			response.WriteValidationError(w, map[string]string{"variants": "at least one variant is required"})
		case errors.Is(err, product.ErrVariantSKURequired):
			response.WriteValidationError(w, map[string]string{"variant.sku": "required for all variants"})
		case errors.Is(err, product.ErrVariantSKUTooLong):
			response.WriteValidationError(w, map[string]string{"variant.sku": "must not exceed 50 characters"})
		case errors.Is(err, product.ErrVariantSKUDuplicate):
			response.WriteValidationError(w, map[string]string{"variant.sku": "SKU already exists, please use a unique SKU"})
		case errors.Is(err, product.ErrVariantRetailPriceInvalid):
			response.WriteValidationError(w, map[string]string{"variant.retailPrice": "must be greater than 0"})
		case errors.Is(err, product.ErrVariantWholesalePriceInvalid):
			response.WriteValidationError(w, map[string]string{"variant.wholesalePrice": "must be greater than 0"})
		case errors.Is(err, product.ErrVariantCostInvalid):
			response.WriteValidationError(w, map[string]string{"variant.cost": "cannot be negative"})
		case errors.Is(err, product.ErrVariantInventoryInvalid):
			response.WriteValidationError(w, map[string]string{"variant.inventory": "cannot be negative"})
		case errors.Is(err, product.ErrVariantMinQuantityInvalid):
			response.WriteValidationError(w, map[string]string{"variant.minQuantityForWholesale": "must be greater than 0"})
		case errors.Is(err, product.ErrVariantWeightInvalid):
			response.WriteValidationError(w, map[string]string{"variant.weight": "cannot be negative"})
		case errors.Is(err, product.ErrVariantDimensionsInvalid):
			response.WriteValidationError(w, map[string]string{"variant.dimensions": "cannot be negative"})
		case errors.Is(err, product.ErrVariantOptionTypeRequired):
			response.WriteValidationError(w, map[string]string{"variant.options.optionType": "required when creating new options"})
		case errors.Is(err, product.ErrVariantOptionValueRequired):
			response.WriteValidationError(w, map[string]string{"variant.options.optionValue": "required when creating new options"})
		case errors.Is(err, product.ErrVariantMediaRequired):
			response.WriteValidationError(w, map[string]string{"variant.mediaIDs": "at least one media is required"})
		case errors.Is(err, product.ErrMediaNotFound):
			response.WriteValidationError(w, map[string]string{"variant.mediaIDs": "one or more media IDs not found"})
		case errors.Is(err, product.ErrVariantCategoryRequired):
			response.WriteValidationError(w, map[string]string{"variant.categoryId": "required for all variants"})
		case errors.Is(err, product.ErrVariantCategoryNotFound):
			response.WriteValidationError(w, map[string]string{"variant.categoryId": "must reference an existing category"})
		default:
			response.WriteError(w, http.StatusInternalServerError, "Failed to update product", nil)
		}
		return
	}

	updatedProduct, err := h.getUseCase.Execute(productID)
	if err != nil {
		response.WriteSuccess(w, http.StatusOK, map[string]interface{}{"message": "Product updated successfully", "id": productID})
		return
	}

	response.WriteSuccess(w, http.StatusOK, updatedProduct)
}

func (h *ProductHandler) LaunchVariantStock(w http.ResponseWriter, r *http.Request) {
	type requestBody struct {
		Quantity int    `json:"quantity"`
		Note     string `json:"note"`
	}

	vars := mux.Vars(r)
	productID := vars["id"]
	variantID := vars["variantId"]

	var body requestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"quantity": "integer (required)",
			"note":     "string (optional)",
		})
		return
	}

	if body.Quantity <= 0 {
		response.WriteValidationError(w, map[string]string{"quantity": "must be greater than 0"})
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	err := h.launchStockUseCase.Execute(wsID, productID, variantID, body.Quantity, body.Note)
	if err != nil {
		switch {
		case errors.Is(err, product.ErrProductNotFound):
			response.WriteError(w, http.StatusNotFound, "Product not found", nil)
		case errors.Is(err, product.ErrVariantNotFound):
			response.WriteError(w, http.StatusNotFound, "Variant not found", nil)
		case errors.Is(err, product.ErrVariantStockAdjustmentInvalid):
			response.WriteValidationError(w, map[string]string{"quantity": "must be greater than 0"})
		default:
			response.WriteError(w, http.StatusInternalServerError, "Failed to add stock", nil)
		}
		return
	}

	updatedProduct, err := h.getUseCase.Execute(productID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch updated product", nil)
		return
	}

	var variant *product.Variant
	for i := range updatedProduct.Variants {
		if updatedProduct.Variants[i].ID == variantID {
			variant = &updatedProduct.Variants[i]
			break
		}
	}

	if variant == nil {
		response.WriteSuccess(w, http.StatusOK, map[string]string{"message": "Stock adjustment recorded"})
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"message":       "Stock adjustment recorded",
		"productId":     productID,
		"variantId":     variantID,
		"available":     variant.Inventory,
		"baseInventory": variant.BaseInventory,
		"launched":      variant.LaunchedStock,
		"reserved":      variant.ReservedStock,
		"sold":          variant.SoldStock,
	})
}

func (h *ProductHandler) Search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	values := r.URL.Query()
	pagination := parsePagination(values)
	if pagination.PageSize == shared.DefaultPageSize {
		if limitStr := values.Get("limit"); limitStr != "" {
			if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
				pagination.PageSize = parsedLimit
			}
		}
	}

	options := shared.QueryOptions{
		Pagination: pagination,
		Sorts:      parseSort(values, map[string]string{"createdat": "createdAt", "updatedat": "updatedAt", "name": "name", "price": "price"}),
		Filters:    parseProductFilters(values),
	}

	result, err := h.searchUseCase.Execute(product.SearchProductsInput{
		Query:   query,
		Options: options,
	})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to search products", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

func parseProductFilters(values url.Values) []shared.Filter {
	filters := make([]shared.Filter, 0)

	if shopId := strings.TrimSpace(firstQueryValue(values["shopId"])); shopId != "" {
		filters = append(filters, shared.Filter{Field: "shopId", Operator: shared.FilterOpEquals, Values: []string{shopId}})
	}

	if searchTagValues := values["tag"]; len(searchTagValues) > 0 {
		filters = append(filters, shared.Filter{Field: "tag", Operator: shared.FilterOpIn, Values: searchTagValues})
	}

	if sku := strings.TrimSpace(firstQueryValue(values["sku"])); sku != "" {
		filters = append(filters, shared.Filter{Field: "sku", Operator: shared.FilterOpLike, Values: []string{sku}})
	}

	if announced := strings.TrimSpace(firstQueryValue(values["announced"])); announced != "" {
		filters = append(filters, shared.Filter{Field: "announced", Operator: shared.FilterOpEquals, Values: []string{announced}})
	}

	if minPrice := strings.TrimSpace(firstQueryValue(values["minPrice"])); minPrice != "" {
		filters = append(filters, shared.Filter{Field: "minPrice", Operator: shared.FilterOpGte, Values: []string{minPrice}})
	}

	if maxPrice := strings.TrimSpace(firstQueryValue(values["maxPrice"])); maxPrice != "" {
		filters = append(filters, shared.Filter{Field: "maxPrice", Operator: shared.FilterOpLte, Values: []string{maxPrice}})
	}

	if createdFrom := strings.TrimSpace(firstQueryValue(values["createdFrom"])); createdFrom != "" {
		filters = append(filters, shared.Filter{Field: "createdAt", Operator: shared.FilterOpGte, Values: []string{createdFrom}})
	}

	if createdTo := strings.TrimSpace(firstQueryValue(values["createdTo"])); createdTo != "" {
		filters = append(filters, shared.Filter{Field: "createdAt", Operator: shared.FilterOpLte, Values: []string{createdTo}})
	}

	if categoryId := strings.TrimSpace(firstQueryValue(values["categoryId"])); categoryId != "" {
		filters = append(filters, shared.Filter{Field: "categoryId", Operator: shared.FilterOpEquals, Values: []string{categoryId}})
	}

	return filters
}
