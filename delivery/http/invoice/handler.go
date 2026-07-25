package invoice

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	invoicedomain "vozko/domain/invoice"
	"vozko/infra/http/middleware"

	"github.com/gorilla/mux"
)

type InvoiceHandler struct {
	createUseCase invoicedomain.CreateInvoiceUseCase
	listUseCase   invoicedomain.ListInvoicesUseCase
	getUseCase    invoicedomain.GetInvoiceUseCase
}

func NewInvoiceHandler(
	createUseCase invoicedomain.CreateInvoiceUseCase,
	listUseCase invoicedomain.ListInvoicesUseCase,
	getUseCase invoicedomain.GetInvoiceUseCase,
) *InvoiceHandler {
	return &InvoiceHandler{
		createUseCase: createUseCase,
		listUseCase:   listUseCase,
		getUseCase:    getUseCase,
	}
}

// @Summary		Criar uma fatura
// @Description	Cria uma fatura de recarga de saldo para o workspace e gera a cobrança correspondente via PIX ou boleto.
// @Tags			Faturas
// @Accept			json
// @Produce		json
// @Param			request	body		CreateInvoiceRequest	true	"Dados da fatura a ser criada"
// @Success		201	{object}	InvoiceEnvelope
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		409	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/user/invoices [post]
func (h *InvoiceHandler) Create(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace ID required", nil)
		return
	}

	var req CreateInvoiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid request body", nil)
		return
	}

	if errs := req.Validate(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}

	billingType := strings.ToUpper(strings.TrimSpace(req.BillingType))

	output, err := h.createUseCase.Execute(invoicedomain.CreateInvoiceInput{
		WorkspaceID:  workspaceID,
		UserID:       claims.UserID,
		AmountBRL:    req.AmountBRL,
		BillingType:  billingType,
		Description:  req.Description,
		ReferralCode: middleware.ExtractReferralCode(r),
	})
	if err != nil {
		log.Printf("[invoice] create failed: %v", err)
		h.writeCreateInvoiceError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, map[string]interface{}{"invoice": toInvoiceResponsePtr(output.Invoice)})
}

// @Summary		Listar faturas
// @Description	Retorna a lista paginada de faturas do workspace.
// @Tags			Faturas
// @Produce		json
// @Param			page		query		int	false	"Número da página (inicia em 1)"
// @Param			pageSize	query		int	false	"Quantidade de itens por página"
// @Success		200	{object}	InvoiceListResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/user/invoices [get]
func (h *InvoiceHandler) List(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace ID required", nil)
		return
	}

	pagination := httpx.ParsePagination(r.URL.Query())
	page := pagination.Page
	pageSize := pagination.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	invoices, total, err := h.listUseCase.Execute(workspaceID, page, pageSize)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	totalPages := (int(total) + pageSize - 1) / pageSize

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"invoices":   toInvoiceResponses(invoices),
		"page":       page,
		"pageSize":   pageSize,
		"total":      total,
		"totalPages": totalPages,
	})
}

// @Summary		Consultar uma fatura
// @Description	Retorna os detalhes de uma fatura do workspace a partir do seu identificador.
// @Tags			Faturas
// @Produce		json
// @Param			id	path	string	true	"Identificador da fatura"
// @Success		200	{object}	InvoiceEnvelope
// @Failure		400	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/user/invoices/{id} [get]
func (h *InvoiceHandler) Get(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace ID required", nil)
		return
	}

	vars := mux.Vars(r)
	id := vars["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "invoice ID required", nil)
		return
	}

	inv, err := h.getUseCase.Execute(id)
	if err != nil {
		if err == invoicedomain.ErrInvoiceNotFound {
			response.WriteError(w, http.StatusNotFound, "invoice not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	if inv.WorkspaceID != workspaceID {
		response.WriteError(w, http.StatusNotFound, "invoice not found", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{"invoice": toInvoiceResponsePtr(inv)})
}

func (h *InvoiceHandler) AdminCreate(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspaceId"]
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace ID required", nil)
		return
	}

	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "unauthorized", nil)
		return
	}

	var req CreateInvoiceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid request body", nil)
		return
	}

	if errs := req.ValidateAdmin(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}

	billingType := strings.ToUpper(strings.TrimSpace(req.BillingType))
	if billingType != "PIX" && billingType != "BOLETO" {
		billingType = "PIX"
	}

	desc := strings.TrimSpace(req.Description)
	if desc == "" {
		desc = "Recarga de saldo por administrador"
	}

	output, err := h.createUseCase.Execute(invoicedomain.CreateInvoiceInput{
		WorkspaceID: workspaceID,
		UserID:      claims.UserID,
		AmountBRL:   req.AmountBRL,
		BillingType: billingType,
		Description: desc,
	})
	if err != nil {
		log.Printf("[invoice-admin] create failed for workspace %s: %v", workspaceID, err)
		h.writeCreateInvoiceError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, map[string]interface{}{"invoice": toInvoiceResponsePtr(output.Invoice)})
}

func (h *InvoiceHandler) AdminGet(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspaceId"]
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace ID required", nil)
		return
	}

	id := mux.Vars(r)["id"]
	if id == "" {
		response.WriteError(w, http.StatusBadRequest, "invoice ID required", nil)
		return
	}

	inv, err := h.getUseCase.Execute(id)
	if err != nil {
		if err == invoicedomain.ErrInvoiceNotFound {
			response.WriteError(w, http.StatusNotFound, "invoice not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	if inv.WorkspaceID != workspaceID {
		response.WriteError(w, http.StatusNotFound, "invoice not found", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{"invoice": toInvoiceResponsePtr(inv)})
}

func (h *InvoiceHandler) AdminList(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspaceId"]
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace ID required", nil)
		return
	}

	pagination := httpx.ParsePagination(r.URL.Query())
	page := pagination.Page
	pageSize := pagination.PageSize
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	invoices, total, err := h.listUseCase.Execute(workspaceID, page, pageSize)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, err.Error(), nil)
		return
	}

	totalPages := (int(total) + pageSize - 1) / pageSize

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"invoices":   toInvoiceResponses(invoices),
		"page":       page,
		"pageSize":   pageSize,
		"total":      total,
		"totalPages": totalPages,
	})
}

func (h *InvoiceHandler) writeCreateInvoiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, invoicedomain.ErrCustomerDocumentRequired):
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "customer_document_required", err.Error(), nil)
	case errors.Is(err, invoicedomain.ErrInvalidAmount), errors.Is(err, invoicedomain.ErrInvalidPurpose), errors.Is(err, invoicedomain.ErrPlanDefinitionRequired):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, invoicedomain.ErrActiveSubscriptionRequired):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "failed to create invoice", nil)
	}
}
