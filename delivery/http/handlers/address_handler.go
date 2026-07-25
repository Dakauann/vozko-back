package handlers

import (
	"encoding/json"
	"errors"
	"regexp"

	"net/http"
	"vozko/delivery/http/response"
	"vozko/domain/address"

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

	expected := make(map[string]string)
	if addr.Name == "" {
		expected["name"] = "required"
	}
	if addr.Street == "" {
		expected["street"] = "required"
	}
	if addr.Number == "" {
		expected["number"] = "required"
	}
	if addr.District == "" {
		expected["district"] = "required"
	}
	if addr.City == "" {
		expected["city"] = "required"
	}
	if addr.State == "" {
		expected["state"] = "required"
	}
	if addr.ZipCode == "" {
		expected["zipCode"] = "required"
	} else if !h.isValidCEP(addr.ZipCode) {
		expected["zipCode"] = "must be a valid CEP format (8 digits)"
	}

	if len(expected) > 0 {
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

	expected := make(map[string]string)
	if addr.Name == "" {
		expected["name"] = "required"
	}
	if addr.Street == "" {
		expected["street"] = "required"
	}
	if addr.Number == "" {
		expected["number"] = "required"
	}
	if addr.District == "" {
		expected["district"] = "required"
	}
	if addr.City == "" {
		expected["city"] = "required"
	}
	if addr.State == "" {
		expected["state"] = "required"
	}
	if addr.ZipCode == "" {
		expected["zipCode"] = "required"
	} else if !h.isValidCEP(addr.ZipCode) {
		expected["zipCode"] = "must be a valid CEP format (8 digits)"
	}

	if len(expected) > 0 {
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

func (h *AddressHandler) isValidCEP(cep string) bool {
	cleanedCEP := h.cleanCEP(cep)
	validCEP := regexp.MustCompile(`^\d{8}$`)
	return validCEP.MatchString(cleanedCEP)
}

func (h *AddressHandler) cleanCEP(cepCode string) string {
	cleaned := regexp.MustCompile(`\D`).ReplaceAllString(cepCode, "")
	return cleaned
}
