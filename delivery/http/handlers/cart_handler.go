package handlers

import (
	"encoding/json"
	"errors"

	"net/http"
	"vozko/delivery/http/response"
	"vozko/domain/cart"

	"github.com/gorilla/mux"
)

type CartHandler struct {
	addToCartUseCase         cart.AddToCartUseCase
	removeFromCartUseCase    cart.RemoveFromCartUseCase
	updateCartItemUseCase    cart.UpdateCartItemUseCase
	decrementCartItemUseCase cart.DecrementCartItemUseCase
	getCartUseCase           cart.GetCartUseCase
	clearCartUseCase         cart.ClearCartUseCase
}

func NewCartHandler(
	addToCartUseCase cart.AddToCartUseCase,
	removeFromCartUseCase cart.RemoveFromCartUseCase,
	updateCartItemUseCase cart.UpdateCartItemUseCase,
	decrementCartItemUseCase cart.DecrementCartItemUseCase,
	getCartUseCase cart.GetCartUseCase,
	clearCartUseCase cart.ClearCartUseCase,
) *CartHandler {
	return &CartHandler{
		addToCartUseCase:         addToCartUseCase,
		removeFromCartUseCase:    removeFromCartUseCase,
		updateCartItemUseCase:    updateCartItemUseCase,
		decrementCartItemUseCase: decrementCartItemUseCase,
		getCartUseCase:           getCartUseCase,
		clearCartUseCase:         clearCartUseCase,
	}
}

func (h *CartHandler) GetCart(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		response.WriteError(w, http.StatusUnauthorized, "User not authenticated", nil)
		return
	}

	cartResult, err := h.getCartUseCase.Execute(userID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to get cart", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, cartResult)
}

func (h *CartHandler) AddToCart(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		response.WriteError(w, http.StatusUnauthorized, "User not authenticated", nil)
		return
	}

	var payload struct {
		ProductID       string                `json:"productId"`
		VariantID       string                `json:"variantId"`
		Quantity        int                   `json:"quantity"`
		SelectedOptions []cart.SelectedOption `json:"options"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"productId": "string (required)",
			"variantId": "string (required)",
			"quantity":  "integer (required)",
			"options":   "array of selected options (optional)",
		})
		return
	}

	if payload.ProductID == "" || payload.VariantID == "" {
		expected := make(map[string]string)
		if payload.ProductID == "" {
			expected["productId"] = "required"
		}
		if payload.VariantID == "" {
			expected["variantId"] = "required"
		}
		response.WriteValidationError(w, expected)
		return
	}

	cartResult, err := h.addToCartUseCase.Execute(userID, payload.ProductID, payload.VariantID, payload.Quantity, payload.SelectedOptions)
	if err != nil {
		switch {
		case errors.Is(err, cart.ErrInvalidQuantity):
			response.WriteValidationError(w, map[string]string{"quantity": "must be greater than 0"})
		case errors.Is(err, cart.ErrMaxQuantityExceeded):
			response.WriteValidationError(w, map[string]string{"quantity": "exceeds maximum allowed quantity"})
		case errors.Is(err, cart.ErrInsufficientStock):
			response.WriteValidationError(w, map[string]string{"quantity": "insufficient stock available"})

		case errors.Is(err, cart.ErrProductNotFound):
			response.WriteValidationError(w, map[string]string{"productId": "product not found"})
		case errors.Is(err, cart.ErrVariantNotFound):
			response.WriteValidationError(w, map[string]string{"variantId": "variant not found"})
		case errors.Is(err, cart.ErrVariantNotAnnounced):
			response.WriteValidationError(w, map[string]string{"variantId": "variant is not available"})
		case errors.Is(err, cart.ErrOptionSelectionRequired):
			response.WriteValidationError(w, map[string]string{"options": "option selection is required"})
		case errors.Is(err, cart.ErrInvalidOptionSelection):
			response.WriteValidationError(w, map[string]string{"options": "invalid option selection"})
		default:
			response.WriteError(w, http.StatusInternalServerError, "Failed to add item to cart", nil)
		}
		return
	}

	response.WriteSuccess(w, http.StatusOK, cartResult)
}

func (h *CartHandler) UpdateCartItem(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		response.WriteError(w, http.StatusUnauthorized, "User not authenticated", nil)
		return
	}

	vars := mux.Vars(r)
	itemID := vars["itemId"]

	var payload struct {
		Quantity int `json:"quantity"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"quantity": "integer (required)",
		})
		return
	}

	cartResult, err := h.updateCartItemUseCase.Execute(userID, itemID, payload.Quantity)
	if err != nil {
		switch {
		case errors.Is(err, cart.ErrInvalidQuantity):
			response.WriteValidationError(w, map[string]string{"quantity": "must be greater than 0"})
		case errors.Is(err, cart.ErrMaxQuantityExceeded):
			response.WriteValidationError(w, map[string]string{"quantity": "exceeds maximum allowed quantity"})
		case errors.Is(err, cart.ErrInsufficientStock):
			response.WriteValidationError(w, map[string]string{"quantity": "insufficient stock available"})
		case errors.Is(err, cart.ErrCartItemNotFound):
			response.WriteValidationError(w, map[string]string{"itemId": "cart item not found"})
		case errors.Is(err, cart.ErrVariantNotFound):
			response.WriteValidationError(w, map[string]string{"variantId": "variant not found"})
		case errors.Is(err, cart.ErrVariantNotAnnounced):
			response.WriteValidationError(w, map[string]string{"variantId": "variant is not available"})
		default:
			response.WriteError(w, http.StatusInternalServerError, "Failed to update cart item", nil)
		}
		return
	}

	response.WriteSuccess(w, http.StatusOK, cartResult)
}

func (h *CartHandler) RemoveFromCart(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		response.WriteError(w, http.StatusUnauthorized, "User not authenticated", nil)
		return
	}

	vars := mux.Vars(r)
	itemID := vars["itemId"]

	cartResult, err := h.removeFromCartUseCase.Execute(userID, itemID)
	if err != nil {
		switch {
		case errors.Is(err, cart.ErrCartItemNotFound):
			response.WriteValidationError(w, map[string]string{"itemId": "cart item not found"})
		default:
			response.WriteError(w, http.StatusInternalServerError, "Failed to remove item from cart", nil)
		}
		return
	}

	response.WriteSuccess(w, http.StatusOK, cartResult)
}

func (h *CartHandler) DecrementCartItem(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		response.WriteError(w, http.StatusUnauthorized, "User not authenticated", nil)
		return
	}

	vars := mux.Vars(r)
	itemID := vars["itemId"]

	cartResult, err := h.decrementCartItemUseCase.Execute(userID, itemID)
	if err != nil {
		switch {
		case errors.Is(err, cart.ErrCartItemNotFound):
			response.WriteValidationError(w, map[string]string{"itemId": "cart item not found"})
		default:
			response.WriteError(w, http.StatusInternalServerError, "Failed to decrement cart item", nil)
		}
		return
	}

	response.WriteSuccess(w, http.StatusOK, cartResult)
}

func (h *CartHandler) ClearCart(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		response.WriteError(w, http.StatusUnauthorized, "User not authenticated", nil)
		return
	}

	err := h.clearCartUseCase.Execute(userID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to clear cart", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]string{"message": "Cart cleared successfully"})
}
