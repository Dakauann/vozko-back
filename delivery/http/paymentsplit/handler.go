package paymentsplit

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	paymentdomain "vozko/domain/payment"
)

type PaymentSplitHandler struct {
	createUseCase      paymentdomain.CreatePaymentSplitUseCase
	updateUseCase      paymentdomain.UpdatePaymentSplitUseCase
	deleteUseCase      paymentdomain.DeletePaymentSplitUseCase
	getUseCase         paymentdomain.GetPaymentSplitUseCase
	listUseCase        paymentdomain.ListPaymentSplitsUseCase
	getSuppiersUseCase paymentdomain.GetPaymentSplitSuppliersUseCase
}

func NewPaymentSplitHandler(
	createUC paymentdomain.CreatePaymentSplitUseCase,
	updateUC paymentdomain.UpdatePaymentSplitUseCase,
	deleteUC paymentdomain.DeletePaymentSplitUseCase,
	getUC paymentdomain.GetPaymentSplitUseCase,
	listUC paymentdomain.ListPaymentSplitsUseCase,
	getSuppliersUC paymentdomain.GetPaymentSplitSuppliersUseCase,
) *PaymentSplitHandler {
	return &PaymentSplitHandler{
		createUseCase:      createUC,
		updateUseCase:      updateUC,
		deleteUseCase:      deleteUC,
		getUseCase:         getUC,
		listUseCase:        listUC,
		getSuppiersUseCase: getSuppliersUC,
	}
}

func (h *PaymentSplitHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req paymentSplitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"name":             "string (required)",
			"type":             "string (required)",
			"provider":         "string (required)",
			"walletId":         "string (required)",
			"percentageSplit":  "number (optional)",
			"fixedAmountSplit": "number (optional)",
		})
		return
	}

	split := h.requestToDomain(req)
	created, err := h.createUseCase.Execute(split)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, toPaymentSplitResponse(created))
}

func (h *PaymentSplitHandler) GetSuppliers(w http.ResponseWriter, r *http.Request) {
	splits, err := h.getSuppiersUseCase.Execute()
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toPaymentSplitSuppliersResponse(splits))
}

func (h *PaymentSplitHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req paymentSplitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"name":             "string",
			"type":             "string",
			"provider":         "string",
			"walletId":         "string",
			"percentageSplit":  "number (optional)",
			"fixedAmountSplit": "number (optional)",
		})
		return
	}

	split := h.requestToDomain(req)
	updated, err := h.updateUseCase.Execute(id, split)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toPaymentSplitResponse(updated))
}

func (h *PaymentSplitHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	if err := h.deleteUseCase.Execute(id); err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusNoContent, nil)
}

func (h *PaymentSplitHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	split, err := h.getUseCase.Execute(id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toPaymentSplitResponse(split))
}

func (h *PaymentSplitHandler) List(w http.ResponseWriter, _ *http.Request) {
	splits, err := h.listUseCase.Execute()
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list payment splits", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, toPaymentSplitListResponse(splits))
}

func (h *PaymentSplitHandler) requestToDomain(req paymentSplitRequest) *paymentdomain.PaymentSplit {
	split := &paymentdomain.PaymentSplit{
		Name:     req.Name,
		Type:     paymentdomain.SplitType(req.Type),
		Provider: paymentdomain.SplitProvider(req.Provider),
		WalletID: req.WalletID,
	}

	if req.Percentage != nil {
		split.Percentage = *req.Percentage
	}

	if req.FixedAmount != nil {
		split.FixedAmount = *req.FixedAmount
	}

	return split
}

func (h *PaymentSplitHandler) handleDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, paymentdomain.ErrPaymentSplitNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, paymentdomain.ErrInvalidSplitType):
		response.WriteValidationError(w, map[string]string{"type": err.Error()})
	case errors.Is(err, paymentdomain.ErrAtLeastOneOfPercentageOrFixed):
		response.WriteValidationError(w, map[string]string{"percentageSplit": err.Error(), "fixedAmountSplit": err.Error()})
	case errors.Is(err, paymentdomain.ErrInvalidSplitProvider):
		response.WriteValidationError(w, map[string]string{"provider": err.Error()})
	case errors.Is(err, paymentdomain.ErrInvalidSplitPercentage):
		response.WriteValidationError(w, map[string]string{"percentageSplit": err.Error()})
	case errors.Is(err, paymentdomain.ErrInvalidSplitFixedAmount):
		response.WriteValidationError(w, map[string]string{"fixedAmountSplit": err.Error()})
	case errors.Is(err, paymentdomain.ErrPaymentSplitNameRequired):
		response.WriteValidationError(w, map[string]string{"name": "required"})
	case errors.Is(err, paymentdomain.ErrPaymentSplitWalletRequired):
		response.WriteValidationError(w, map[string]string{"walletId": "required"})
	default:
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
	}
}
