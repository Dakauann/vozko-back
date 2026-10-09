package handlers

import (
	"encoding/json"
	"errors"

	"net/http"
	"vozko/delivery/http/response"
	"vozko/domain/address"
	"vozko/domain/cep"

	"github.com/gorilla/mux"
)

type AddressHandler struct {
	createUseCase address.CreateAddressUseCase
	getUseCase    address.GetAddressesUseCase
	updateUseCase address.UpdateAddressUseCase
	deleteUseCase address.DeleteAddressUseCase
}

func NewAddressHandler(
	createUseCase address.CreateAddressUseCase,
	getUseCase address.GetAddressesUseCase,
	updateUseCase address.UpdateAddressUseCase,
	deleteUseCase address.DeleteAddressUseCase,
) *AddressHandler {
	return &AddressHandler{
		createUseCase: createUseCase,
		getUseCase:    getUseCase,
		updateUseCase: updateUseCase,
		deleteUseCase: deleteUseCase,
	}
}

func (h *AddressHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		response.WriteError(w, http.StatusUnauthorized, "User not authenticated", nil)
		return
	}

	var addr address.Address
	if err := json.NewDecoder(r.Body).Decode(&addr); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"name":       "string (required)",
			"street":     "string (required)",
			"number":     "string (required)",
			"district":   "string (required)",
			"city":       "string (required)",
			"state":      "string (required)",
			"zipCode":    "string (required)",
			"complement": "string (optional)",
		})
		return
	}

	if expected := addressFieldErrors(addr); len(expected) > 0 {
		response.WriteValidationError(w, expected)
		return
	}

	result, err := h.createUseCase.Execute(userID, &addr)
	if err != nil {
		switch {
		case errors.Is(err, address.ErrMaxAddressesReached):
			response.WriteError(w, http.StatusBadRequest, "Maximum number of addresses reached", nil)
		default:
			response.WriteError(w, http.StatusInternalServerError, "Failed to create address", nil)
		}
		return
	}

	response.WriteSuccess(w, http.StatusCreated, result)
}

func (h *AddressHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		response.WriteError(w, http.StatusUnauthorized, "User not authenticated", nil)
		return
	}

	addresses, err := h.getUseCase.Execute(userID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to get addresses", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, addresses)
}

func (h *AddressHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		response.WriteError(w, http.StatusUnauthorized, "User not authenticated", nil)
		return
	}

	vars := mux.Vars(r)
	addressID := vars["id"]

	var addr address.Address
	if err := json.NewDecoder(r.Body).Decode(&addr); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"name":       "string",
			"street":     "string",
			"number":     "string",
			"district":   "string",
			"city":       "string",
			"state":      "string",
			"zipCode":    "string",
			"complement": "string (optional)",
		})
		return
	}

	if expected := addressFieldErrors(addr); len(expected) > 0 {
		response.WriteValidationError(w, expected)
		return
	}

	result, err := h.updateUseCase.Execute(userID, addressID, &addr)
	if err != nil {
		switch {
		case errors.Is(err, address.ErrAddressNotFound):
			response.WriteError(w, http.StatusNotFound, "Address not found", nil)
		default:
			response.WriteError(w, http.StatusInternalServerError, "Failed to update address", nil)
		}
		return
	}

	response.WriteSuccess(w, http.StatusOK, result)
}

func (h *AddressHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		response.WriteError(w, http.StatusUnauthorized, "User not authenticated", nil)
		return
	}

	vars := mux.Vars(r)
	addressID := vars["id"]

	err := h.deleteUseCase.Execute(userID, addressID)
	if err != nil {
		switch {
		case errors.Is(err, address.ErrAddressNotFound):
			response.WriteError(w, http.StatusNotFound, "Address not found", nil)
		default:
			response.WriteError(w, http.StatusInternalServerError, "Failed to delete address", nil)
		}
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]string{"message": "Address deleted successfully"})
}

func addressFieldErrors(addr address.Address) map[string]string {
	expected := make(map[string]string)
	for _, field := range addr.Missing() {
		expected[string(field)] = "required"
	}
	if _, missing := expected[string(address.FieldZipCode)]; !missing {
		if _, err := cep.Parse(addr.ZipCode); err != nil {
			expected[string(address.FieldZipCode)] = "must be a valid CEP format (8 digits)"
		}
	}
	return expected
}
