package invoice

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	invoicedomain "vozko/domain/invoice"
	"vozko/infra/http/middleware"

	"github.com/gorilla/mux"
)

type InvoiceHandler struct {
	createUseCase   invoicedomain.CreateInvoiceUseCase
	listUseCase     invoicedomain.ListInvoicesUseCase
	getUseCase      invoicedomain.GetInvoiceUseCase
	renewPixUseCase invoicedomain.RenewPixUseCase
}

func NewInvoiceHandler(
	createUseCase invoicedomain.CreateInvoiceUseCase,
	listUseCase invoicedomain.ListInvoicesUseCase,
	getUseCase invoicedomain.GetInvoiceUseCase,
	renewPixUseCase invoicedomain.RenewPixUseCase,
) *InvoiceHandler {
	return &InvoiceHandler{
		createUseCase:   createUseCase,
		listUseCase:     listUseCase,
		getUseCase:      getUseCase,
		renewPixUseCase: renewPixUseCase,
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

	idempotencyKey, err := invoicedomain.ClientIdempotencyKey(workspaceID, req.IdempotencyKey)
	if err != nil {
		h.writeCreateInvoiceError(w, err)
		return
	}

	output, err := h.createUseCase.Execute(invoicedomain.CreateInvoiceInput{
		WorkspaceID:    workspaceID,
		UserID:         claims.UserID,
		Purpose:        invoicedomain.PurposeTopUp,
		AmountBRL:      req.AmountBRL,
		BillingType:    billingType,
		Description:    req.Description,
		IdempotencyKey: idempotencyKey,
		ReferralCode:   middleware.ExtractReferralCode(r),
	})
	if err != nil {
		log.Printf("[invoice] create failed: %v", err)
		h.writeCreateInvoiceError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, map[string]interface{}{"invoice": toInvoiceResponsePtr(output.Invoice, time.Now())})
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
		"invoices":   toInvoiceResponses(invoices, time.Now()),
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

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{"invoice": toInvoiceResponsePtr(inv, time.Now())})
}

// @Summary		Gerar um novo PIX para a fatura
// @Description	Emite uma nova cobrança PIX para a mesma fatura e o mesmo valor quando o PIX atual expirou ou quando a fatura venceu e ainda pode ser paga (a fatura mensal pode ser paga até o dia do corte). A cobrança anterior é cancelada no provedor antes, então cada fatura tem um único PIX pagável por vez. Se o PIX atual ainda é válido, ele é devolvido sem gerar outro. Faturas pagas, canceladas ou expiradas nunca recebem um novo PIX. A resposta traz em payment o estado do pagamento, a validade do código e o prazo final para pagar.
// @Tags			Faturas
// @Produce		json
// @Param			id	path		string	true	"Identificador da fatura"
// @Success		200	{object}	InvoiceEnvelope
// @Failure		400	{object}	response.ErrorResponse	"workspace_required"
// @Failure		404	{object}	response.ErrorResponse	"invoice_not_found"
// @Failure		409	{object}	response.ErrorResponse	"invoice_not_payable, invoice_already_paid ou invoice_pix_renew_in_progress"
// @Failure		422	{object}	response.ErrorResponse	"customer_document_required"
// @Failure		502	{object}	response.ErrorResponse	"invoice_pix_unavailable"
// @Failure		500	{object}	response.ErrorResponse	"invoice_pix_unavailable"
// @Security		BearerAuth
// @Router			/user/invoices/{id}/pix [post]
func (h *InvoiceHandler) RenewPix(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteErrorWithCode(w, http.StatusBadRequest, "workspace_required", "workspace ID required", nil)
		return
	}
	invoiceID := strings.TrimSpace(mux.Vars(r)["id"])
	if invoiceID == "" {
		response.WriteRefusal(w, refusalInvoiceNotFound)
		return
	}

	inv, err := h.renewPixUseCase.Execute(r.Context(), invoicedomain.RenewPixInput{WorkspaceID: workspaceID, InvoiceID: invoiceID})
	if err != nil {
		if !response.WriteErrorRefusal(w, err, renewPixRefusals) {
			log.Printf("[invoice] renew PIX of invoice %s failed: %v", invoiceID, err)
			response.WriteRefusal(w, refusalRenewPixFailed)
		}
		return
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{"invoice": toInvoiceResponsePtr(inv, time.Now())})
}

var (
	refusalInvoiceNotFound = response.Refusal{Status: http.StatusNotFound, Code: "invoice_not_found", Message: "Fatura não encontrada"}
	refusalRenewPixFailed  = response.Refusal{Status: http.StatusInternalServerError, Code: "invoice_pix_unavailable", Message: "Não foi possível gerar um novo PIX agora. Tente novamente em instantes."}
)

var renewPixRefusals = []response.ErrorRefusal{
	{Err: invoicedomain.ErrInvoiceNotFound, Refusal: refusalInvoiceNotFound},
	{Err: invoicedomain.ErrAlreadyPaid, Refusal: response.Refusal{Status: http.StatusConflict, Code: "invoice_already_paid", Message: "O pagamento desta fatura já foi recebido"}},
	{Err: invoicedomain.ErrNotPayable, Refusal: response.Refusal{Status: http.StatusConflict, Code: "invoice_not_payable", Message: "Esta fatura não pode mais ser paga"}},
	{Err: invoicedomain.ErrPixRenewInProgress, Refusal: response.Refusal{Status: http.StatusConflict, Code: "invoice_pix_renew_in_progress", Message: "Um novo PIX já está sendo gerado para esta fatura"}},
	{Err: invoicedomain.ErrCustomerDocumentRequired, Refusal: response.Refusal{Status: http.StatusUnprocessableEntity, Code: "customer_document_required", Message: invoicedomain.ErrCustomerDocumentRequired.Error()}},
	{Err: invoicedomain.ErrPixUnavailable, Refusal: response.Refusal{Status: http.StatusBadGateway, Code: "invoice_pix_unavailable", Message: refusalRenewPixFailed.Message}},
}

func RenewPixRefusalCodes() []string {
	return response.RefusalCodes(response.ErrorRefusalsOf(renewPixRefusals))
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
		Purpose:     invoicedomain.PurposeTopUp,
		AmountBRL:   req.AmountBRL,
		BillingType: billingType,
		Description: desc,
	})
	if err != nil {
		log.Printf("[invoice-admin] create failed for workspace %s: %v", workspaceID, err)
		h.writeCreateInvoiceError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, map[string]interface{}{"invoice": toInvoiceResponsePtr(output.Invoice, time.Now())})
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

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{"invoice": toInvoiceResponsePtr(inv, time.Now())})
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
		"invoices":   toInvoiceResponses(invoices, time.Now()),
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
	case errors.Is(err, invoicedomain.ErrBillingAddressRequired):
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, "billing_address_required", err.Error(), nil)
	case errors.Is(err, invoicedomain.ErrInvalidIdempotencyKey):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "invalid_idempotency_key", "Chave de idempotência inválida", nil)
	case errors.Is(err, invoicedomain.ErrInvalidAmount), errors.Is(err, invoicedomain.ErrInvalidPurpose), errors.Is(err, invoicedomain.ErrPlanDefinitionRequired):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case errors.Is(err, invoicedomain.ErrActiveSubscriptionRequired):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	default:
		response.WriteError(w, http.StatusInternalServerError, "failed to create invoice", nil)
	}
}
