package workspacepricing

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	pricingdomain "vozko/domain/workspace/workspace_pricing"
	"vozko/infra/http/middleware"
)

type WorkspacePricingHandler struct {
	getDefaults        pricingdomain.GetDefaultPricingItemsUseCase
	getResolved        pricingdomain.GetResolvedPricingUseCase
	updateItem         pricingdomain.UpdatePricingItemUseCase
	getAuditLog        pricingdomain.GetPricingAuditLogUseCase
	getExchangeRate    pricingdomain.GetExchangeRateUseCase
	updateExchangeRate pricingdomain.UpdateExchangeRateUseCase
}

func NewWorkspacePricingHandler(
	getDefaults pricingdomain.GetDefaultPricingItemsUseCase,
	getResolved pricingdomain.GetResolvedPricingUseCase,
	updateItem pricingdomain.UpdatePricingItemUseCase,
	getAuditLog pricingdomain.GetPricingAuditLogUseCase,
	getExchangeRate pricingdomain.GetExchangeRateUseCase,
	updateExchangeRate pricingdomain.UpdateExchangeRateUseCase,
) *WorkspacePricingHandler {
	return &WorkspacePricingHandler{
		getDefaults:        getDefaults,
		getResolved:        getResolved,
		updateItem:         updateItem,
		getAuditLog:        getAuditLog,
		getExchangeRate:    getExchangeRate,
		updateExchangeRate: updateExchangeRate,
	}
}

func (h *WorkspacePricingHandler) GetDefaults(w http.ResponseWriter, r *http.Request) {
	items, err := h.getDefaults.Execute()
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toPricingItemResponses(items))
}

func (h *WorkspacePricingHandler) GetResolved(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspaceId"]
	items, err := h.getResolved.Execute(workspaceID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toResolvedPricingItemResponses(items))
}

func (h *WorkspacePricingHandler) UpdateDefaultItem(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	var input pricingdomain.UpdatePricingItemInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	item, err := h.updateItem.Execute(input, claims.UserID)
	if err != nil {
		if errors.Is(err, pricingdomain.ErrCategoryNotConfigurable) {
			response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toPricingItemResponse(item))
}

func (h *WorkspacePricingHandler) GetAuditLog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var wsID *string
	if v := q.Get("workspaceId"); v != "" {
		wsID = &v
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))

	entries, err := h.getAuditLog.Execute(wsID, limit, offset)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toPricingAuditEntryResponses(entries))
}

func (h *WorkspacePricingHandler) GetExchangeRate(w http.ResponseWriter, r *http.Request) {
	item, err := h.getExchangeRate.Execute()
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toPricingItemResponse(item))
}

// @Summary		Consultar a cotação do dólar
// @Description	Retorna a cotação atual do dólar (USD) para o real (BRL) utilizada nos cálculos de preços e cobranças do workspace.
// @Tags			Preços
// @Produce		json
// @Success		200	{object}	PublicExchangeRateResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/pricing/exchange-rate [get]
func (h *WorkspacePricingHandler) GetPublicExchangeRate(w http.ResponseWriter, r *http.Request) {
	item, err := h.getExchangeRate.Execute()
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toPublicExchangeRateResponse(item))
}

func (h *WorkspacePricingHandler) UpdateExchangeRate(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	if claims.Email != pricingdomain.SuperAdminEmail {
		response.WriteError(w, http.StatusForbidden, pricingdomain.ErrSuperAdminRequired.Error(), nil)
		return
	}

	var body struct {
		PriceMicros int64 `json:"priceMicros"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.PriceMicros <= 0 {
		response.WriteError(w, http.StatusBadRequest, "priceMicros must be a positive integer (1,000,000 = 1 BRL per USD)", nil)
		return
	}

	rateFloat := float64(body.PriceMicros) / 1_000_000
	if rateFloat < 0.5 || rateFloat > 50.0 {
		response.WriteError(w, http.StatusBadRequest, "exchange rate must be between 0.5 and 50.0 BRL per USD", nil)
		return
	}

	item, err := h.updateExchangeRate.Execute(body.PriceMicros, claims.UserID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Internal server error", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toPricingItemResponse(item))
}
