package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"vozko/delivery/http/response"
	"vozko/domain/order"
	"vozko/domain/shared"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

type OrderHandler struct {
	checkoutUseCase   order.CheckoutUseCase
	getOrderUseCase   order.GetOrderUseCase
	listOrdersUseCase order.ListOrdersUseCase
}

func NewOrderHandler(checkoutUseCase order.CheckoutUseCase, getOrderUseCase order.GetOrderUseCase, listOrdersUseCase order.ListOrdersUseCase) *OrderHandler {
	return &OrderHandler{
		checkoutUseCase:   checkoutUseCase,
		getOrderUseCase:   getOrderUseCase,
		listOrdersUseCase: listOrdersUseCase,
	}
}

func (h *OrderHandler) Checkout(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		response.WriteError(w, http.StatusUnauthorized, "User not authenticated", nil)
		return
	}

	var payload struct {
		AddressID        string `json:"addressId"`
		CustomerName     string `json:"customerName"`
		CustomerDocument string `json:"customerDocument"`
		DirectItem *struct {
			ProductID string `json:"productId"`
			VariantID string `json:"variantId"`
			Quantity  int    `json:"quantity"`
			Options   []struct {
				OptionType  string `json:"optionType"`
				OptionValue string `json:"optionValue"`
			} `json:"options,omitempty"`
		} `json:"directItem,omitempty"`
		CartItemIDs []string `json:"cartItemIds,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"addressId":        "uuid (required)",
			"customerName":     "string (required)",
			"customerDocument": "string (required)",
			"directItem":       "object (optional) - {productId, variantId, quantity, options?}",
			"cartItemIds":      "[]string (optional) - specific cart item IDs to checkout",
		})
		return
	}

	expected := make(map[string]string)
	if payload.AddressID == "" {
		expected["addressId"] = "required"
	}
	if payload.CustomerName == "" {
		expected["customerName"] = "required"
	}
	if payload.CustomerDocument == "" {
		expected["customerDocument"] = "required"
	}

	if payload.DirectItem != nil {
		if payload.DirectItem.ProductID == "" {
			expected["directItem.productId"] = "required when directItem is provided"
		}
		if payload.DirectItem.VariantID == "" {
			expected["directItem.variantId"] = "required when directItem is provided"
		}

		if len(payload.CartItemIDs) > 0 {
			expected["directItem"] = "cannot use both directItem and cartItemIds"
		}
	}

	if len(expected) > 0 {
		response.WriteValidationError(w, expected)
		return
	}

	_, err := uuid.Parse(payload.AddressID)
	if err != nil {
		response.WriteValidationError(w, map[string]string{"addressId": "invalid format"})
		return
	}

	request := order.CheckoutRequest{
		AddressID:        payload.AddressID,
		CustomerName:     payload.CustomerName,
		CustomerDocument: payload.CustomerDocument,
		CartItemIDs:      payload.CartItemIDs,
	}

	if payload.DirectItem != nil {
		var options []order.OrderItemOption
		for _, opt := range payload.DirectItem.Options {
			options = append(options, order.OrderItemOption{
				OptionType:  opt.OptionType,
				OptionValue: opt.OptionValue,
			})
		}
		request.DirectItem = &order.DirectCheckoutItem{
			ProductID: payload.DirectItem.ProductID,
			VariantID: payload.DirectItem.VariantID,
			Quantity:  payload.DirectItem.Quantity,
			Options:   options,
		}
	}

	orderResult, err := h.checkoutUseCase.Execute(userID, &request)
	if err != nil {
		switch {
		case errors.Is(err, order.ErrCartEmpty):
			response.WriteError(w, http.StatusBadRequest, "Cart is empty", nil)
		case errors.Is(err, order.ErrAddressNotFound):
			response.WriteError(w, http.StatusNotFound, "Address not found", nil)
		case errors.Is(err, order.ErrProductNotFound):
			response.WriteError(w, http.StatusBadRequest, "Product not found in cart", nil)
		case errors.Is(err, order.ErrVariantNotFound):
			response.WriteError(w, http.StatusBadRequest, "Product variant not found in cart", nil)
		case errors.Is(err, order.ErrVariantNotAnnounced):
			response.WriteError(w, http.StatusBadRequest, "Product variant is not available for checkout", nil)
		case errors.Is(err, order.ErrProductOutOfStock):
			response.WriteError(w, http.StatusBadRequest, "Product is out of stock", nil)
		case errors.Is(err, order.ErrInsufficientStock):
			response.WriteError(w, http.StatusBadRequest, "Insufficient stock available", nil)
		case errors.Is(err, order.ErrInvalidCustomerDocument):
			response.WriteValidationError(w, map[string]string{"customerDocument": "invalid document"})
		default:
			response.WriteError(w, http.StatusInternalServerError, "Failed to process checkout: "+err.Error(), nil)
		}
		return
	}

	response.WriteSuccess(w, http.StatusCreated, orderResult)
}

func (h *OrderHandler) GetOrder(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		response.WriteError(w, http.StatusUnauthorized, "User not authenticated", nil)
		return
	}

	vars := mux.Vars(r)
	orderID := vars["id"]

	_, err := uuid.Parse(orderID)
	if err != nil {
		response.WriteValidationError(w, map[string]string{"id": "invalid format"})
		return
	}

	orderResult, err := h.getOrderUseCase.Execute(userID, orderID)
	if err != nil {
		switch {
		case errors.Is(err, order.ErrOrderNotFound):
			response.WriteError(w, http.StatusNotFound, "Order not found", nil)
		default:
			response.WriteError(w, http.StatusInternalServerError, "Failed to get order", nil)
		}
		return
	}

	response.WriteSuccess(w, http.StatusOK, orderResult)
}

func (h *OrderHandler) ListOrders(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		response.WriteError(w, http.StatusUnauthorized, "User not authenticated", nil)
		return
	}

	query := r.URL.Query()
	options := shared.QueryOptions{
		Pagination: parsePagination(query),
		Sorts:      parseSort(query, map[string]string{"createdat": "createdAt", "updatedat": "updatedAt", "status": "status", "expiresat": "expiresAt", "totalamount": "total_amount", "total_amount": "total_amount"}),
		Filters:    parseOrderFilters(query),
	}

	result, err := h.listOrdersUseCase.Execute(order.ListOrdersInput{
		UserID:  userID,
		Options: options,
	})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to get orders", nil)
		return
	}

	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

func parseOrderFilters(values url.Values) []shared.Filter {
	filters := make([]shared.Filter, 0)

	if statuses := splitListValues(values["status"]); len(statuses) > 0 {
		filters = append(filters, shared.Filter{Field: "status", Operator: shared.FilterOpIn, Values: statuses})
	}

	if search := strings.TrimSpace(values.Get("search")); search != "" {
		filters = append(filters, shared.Filter{Field: "search", Operator: shared.FilterOpLike, Values: []string{search}})
	} else if search := strings.TrimSpace(values.Get("q")); search != "" {
		filters = append(filters, shared.Filter{Field: "search", Operator: shared.FilterOpLike, Values: []string{search}})
	}

	if document := strings.TrimSpace(values.Get("customerDocument")); document != "" {
		filters = append(filters, shared.Filter{Field: "customerDocument", Operator: shared.FilterOpEquals, Values: []string{document}})
	}

	if name := strings.TrimSpace(values.Get("customerName")); name != "" {
		filters = append(filters, shared.Filter{Field: "customerName", Operator: shared.FilterOpLike, Values: []string{name}})
	}

	if paymentStatus := strings.TrimSpace(values.Get("paymentStatus")); paymentStatus != "" {
		filters = append(filters, shared.Filter{Field: "paymentStatus", Operator: shared.FilterOpEquals, Values: []string{paymentStatus}})
	}

	if totalMin := strings.TrimSpace(values.Get("totalMin")); totalMin != "" {
		filters = append(filters, shared.Filter{Field: "totalMin", Operator: shared.FilterOpGte, Values: []string{totalMin}})
	}

	if totalMax := strings.TrimSpace(values.Get("totalMax")); totalMax != "" {
		filters = append(filters, shared.Filter{Field: "totalMax", Operator: shared.FilterOpLte, Values: []string{totalMax}})
	}

	if createdFrom := strings.TrimSpace(values.Get("createdFrom")); createdFrom != "" {
		filters = append(filters, shared.Filter{Field: "createdAt", Operator: shared.FilterOpGte, Values: []string{createdFrom}})
	}

	if createdTo := strings.TrimSpace(values.Get("createdTo")); createdTo != "" {
		filters = append(filters, shared.Filter{Field: "createdAt", Operator: shared.FilterOpLte, Values: []string{createdTo}})
	}

	if updatedFrom := strings.TrimSpace(values.Get("updatedFrom")); updatedFrom != "" {
		filters = append(filters, shared.Filter{Field: "updatedAt", Operator: shared.FilterOpGte, Values: []string{updatedFrom}})
	}

	if updatedTo := strings.TrimSpace(values.Get("updatedTo")); updatedTo != "" {
		filters = append(filters, shared.Filter{Field: "updatedAt", Operator: shared.FilterOpLte, Values: []string{updatedTo}})
	}

	if expiresFrom := strings.TrimSpace(values.Get("expiresFrom")); expiresFrom != "" {
		filters = append(filters, shared.Filter{Field: "expiresAt", Operator: shared.FilterOpGte, Values: []string{expiresFrom}})
	}

	if expiresTo := strings.TrimSpace(values.Get("expiresTo")); expiresTo != "" {
		filters = append(filters, shared.Filter{Field: "expiresAt", Operator: shared.FilterOpLte, Values: []string{expiresTo}})
	}

	if addressID := strings.TrimSpace(values.Get("addressId")); addressID != "" {
		filters = append(filters, shared.Filter{Field: "addressId", Operator: shared.FilterOpEquals, Values: []string{addressID}})
	}

	return filters
}
