package balance

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	balancedomain "vozko/domain/balance"
	"vozko/domain/shared"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
	"vozko/infra/http/middleware"
	balance_usecase "vozko/usecases/balance"
)

type BalanceHandler struct {
	createUseCase                 balancedomain.CreateBalanceUseCase
	getUseCase                    balancedomain.GetBalanceUseCase
	creditUseCase                 balancedomain.CreditBalanceUseCase
	debitUseCase                  balancedomain.DebitBalanceUseCase
	listTransactionsUseCase       balancedomain.ListTransactionsUseCase
	transactionsExporter          *balance_usecase.TransactionsExporter
	creditResourceUseCase         balancedomain.CreditResourceUseCase
	debitResourceUseCase          balancedomain.DebitResourceUseCase
	getFullSummaryUseCase         balancedomain.GetFullBalanceSummaryUseCase
	getOrCreateUseCase            balancedomain.GetOrCreateBalanceUseCase
	getOrCreateFullSummaryUseCase balancedomain.GetOrCreateFullBalanceSummaryUseCase
	getExchangeRateUseCase        workspace_pricing.GetExchangeRateUseCase
}

func NewBalanceHandler(
	createUC balancedomain.CreateBalanceUseCase,
	getUC balancedomain.GetBalanceUseCase,
	creditUC balancedomain.CreditBalanceUseCase,
	debitUC balancedomain.DebitBalanceUseCase,
	listTransactionsUC balancedomain.ListTransactionsUseCase,
	creditResourceUC balancedomain.CreditResourceUseCase,
	debitResourceUC balancedomain.DebitResourceUseCase,
	getFullSummaryUC balancedomain.GetFullBalanceSummaryUseCase,
	getOrCreateUC balancedomain.GetOrCreateBalanceUseCase,
	getOrCreateFullSummaryUC balancedomain.GetOrCreateFullBalanceSummaryUseCase,
	getExchangeRateUC workspace_pricing.GetExchangeRateUseCase,
	transactionsExporter *balance_usecase.TransactionsExporter,
) *BalanceHandler {
	return &BalanceHandler{
		createUseCase:                 createUC,
		getUseCase:                    getUC,
		creditUseCase:                 creditUC,
		debitUseCase:                  debitUC,
		listTransactionsUseCase:       listTransactionsUC,
		creditResourceUseCase:         creditResourceUC,
		debitResourceUseCase:          debitResourceUC,
		getFullSummaryUseCase:         getFullSummaryUC,
		getOrCreateUseCase:            getOrCreateUC,
		getOrCreateFullSummaryUseCase: getOrCreateFullSummaryUC,
		getExchangeRateUseCase:        getExchangeRateUC,
		transactionsExporter:          transactionsExporter,
	}
}

func (h *BalanceHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateBalanceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if errs := req.Validate(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}

	result, err := h.createUseCase.Execute(balancedomain.CreateBalanceInput{
		WorkspaceID:   req.WorkspaceID,
		InitialAmount: req.InitialAmount,
		Currency:      req.Currency,
	})
	if err != nil {
		if errors.Is(err, balancedomain.ErrWorkspaceAlreadyHasBalance) {
			response.WriteError(w, http.StatusConflict, "Workspace already has a balance", nil)
			return
		}
		if errors.Is(err, balancedomain.ErrInvalidAmount) {
			response.WriteValidationError(w, map[string]string{"initialAmount": "must be non-negative"})
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to create balance", nil)
		return
	}

	response.WriteSuccess(w, http.StatusCreated, mapBalanceToResponse(result))
}

func (h *BalanceHandler) GetByWorkspaceID(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspaceId"]

	result, err := h.getOrCreateUseCase.Execute(workspaceID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to get balance", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, mapBalanceToResponse(result))
}

// @Summary		Consultar saldo
// @Description	Retorna o saldo atual do workspace do usuário autenticado, com o total de créditos e débitos acumulados.
// @Tags			Saldo
// @Produce		json
// @Success		200	{object}	balance.FullBalanceSummaryResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/user/balance [get]
func (h *BalanceHandler) GetMyBalance(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	wsID := middleware.GetWorkspaceID(r)

	result, err := h.getOrCreateFullSummaryUseCase.Execute(wsID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to get balance", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, mapFullBalanceSummaryToResponse(result))
}

func (h *BalanceHandler) GetFullSummaryByWorkspaceID(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspaceId"]

	result, err := h.getOrCreateFullSummaryUseCase.Execute(workspaceID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to get balance summary", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, mapFullBalanceSummaryToResponse(result))
}

func (h *BalanceHandler) Credit(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspaceId"]

	var req CreditDebitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if errs := req.Validate(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}

	serviceType := balancedomain.ServiceType(req.ServiceType)

	if _, err := h.getOrCreateUseCase.Execute(workspaceID); err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to ensure balance exists", nil)
		return
	}

	result, err := h.creditUseCase.Execute(balancedomain.CreditBalanceInput{
		WorkspaceID: workspaceID,
		Amount:      req.Amount,
		ServiceType: serviceType,
		ReferenceID: req.ReferenceID,
		Description: req.Description,
	})
	if err != nil {
		if errors.Is(err, balancedomain.ErrInvalidAmount) {
			response.WriteValidationError(w, map[string]string{"amount": "must be positive"})
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to credit balance", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, mapTransactionToResponse(result))
}

func (h *BalanceHandler) Debit(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspaceId"]

	var req CreditDebitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if errs := req.Validate(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}

	serviceType := balancedomain.ServiceType(req.ServiceType)

	if _, err := h.getOrCreateUseCase.Execute(workspaceID); err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to ensure balance exists", nil)
		return
	}

	result, err := h.debitUseCase.Execute(balancedomain.DebitBalanceInput{
		WorkspaceID: workspaceID,
		Amount:      req.Amount,
		ServiceType: serviceType,
		ReferenceID: req.ReferenceID,
		Description: req.Description,
	})
	if err != nil {
		if errors.Is(err, balancedomain.ErrInsufficientBalance) {
			response.WriteError(w, http.StatusPaymentRequired, "Insufficient balance", nil)
			return
		}
		if errors.Is(err, balancedomain.ErrInvalidAmount) {
			response.WriteValidationError(w, map[string]string{"amount": "must be positive"})
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to debit balance", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, mapTransactionToResponse(result))
}

func (h *BalanceHandler) CreditResource(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspaceId"]

	var req CreditResourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if errs := req.Validate(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}

	serviceType := balancedomain.ServiceType(req.ServiceType)

	if _, err := h.getOrCreateUseCase.Execute(workspaceID); err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to ensure balance exists", nil)
		return
	}

	result, err := h.creditResourceUseCase.Execute(balancedomain.CreditBalanceInput{
		WorkspaceID: workspaceID,
		Amount:      req.Amount,
		ServiceType: serviceType,
		ReferenceID: req.ReferenceID,
		Description: req.Description,
	})
	if err != nil {
		if errors.Is(err, balancedomain.ErrInvalidAmount) {
			response.WriteValidationError(w, map[string]string{"amount": "must be positive"})
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to credit resource", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, mapTransactionToResponse(result))
}

func (h *BalanceHandler) DebitResource(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspaceId"]

	var req CreditResourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid request body", nil)
		return
	}

	if errs := req.Validate(); errs != nil {
		response.WriteValidationError(w, errs)
		return
	}

	serviceType := balancedomain.ServiceType(req.ServiceType)

	if _, err := h.getOrCreateUseCase.Execute(workspaceID); err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to ensure balance exists", nil)
		return
	}

	result, err := h.debitResourceUseCase.Execute(balancedomain.DebitBalanceInput{
		WorkspaceID: workspaceID,
		Amount:      req.Amount,
		ServiceType: serviceType,
		ReferenceID: req.ReferenceID,
		Description: req.Description,
	})
	if err != nil {
		if errors.Is(err, balancedomain.ErrInsufficientBalance) {
			response.WriteError(w, http.StatusPaymentRequired, "Insufficient balance", nil)
			return
		}
		if errors.Is(err, balancedomain.ErrInvalidAmount) {
			response.WriteValidationError(w, map[string]string{"amount": "must be positive"})
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to debit resource", nil)
		return
	}

	response.WriteSuccess(w, http.StatusOK, mapTransactionToResponse(result))
}

func (h *BalanceHandler) ListTransactions(w http.ResponseWriter, r *http.Request) {
	workspaceID := mux.Vars(r)["workspaceId"]
	values := r.URL.Query()

	input := balancedomain.ListTransactionsInput{
		WorkspaceID: workspaceID,
		QueryOptions: shared.QueryOptions{
			Pagination: httpx.ParsePagination(values),
		},
	}

	if serviceTypeStr := strings.TrimSpace(values.Get("serviceType")); serviceTypeStr != "" {
		st := balancedomain.ServiceType(serviceTypeStr)
		if !st.IsValid() {
			response.WriteValidationError(w, map[string]string{"serviceType": "invalid service type"})
			return
		}
		input.ServiceType = &st
	}

	if typeStr := strings.TrimSpace(values.Get("type")); typeStr != "" {
		tt := balancedomain.TransactionType(typeStr)
		if tt != balancedomain.TransactionTypeCredit && tt != balancedomain.TransactionTypeDebit {
			response.WriteValidationError(w, map[string]string{"type": "must be credit or debit"})
			return
		}
		input.Type = &tt
	}

	if resourceTypeStr := strings.TrimSpace(values.Get("resourceType")); resourceTypeStr != "" {
		rt := balancedomain.ResourceType(resourceTypeStr)
		if !rt.IsValid() {
			response.WriteValidationError(w, map[string]string{"resourceType": "must be money"})
			return
		}
		input.ResourceType = &rt
	}

	if sd := strings.TrimSpace(values.Get("startDate")); sd != "" {
		if parsed, err := time.Parse(time.RFC3339, sd); err == nil {
			input.StartDate = &parsed
		}
	}
	if ed := strings.TrimSpace(values.Get("endDate")); ed != "" {
		if parsed, err := time.Parse(time.RFC3339, ed); err == nil {
			input.EndDate = &parsed
		}
	}

	result, err := h.listTransactionsUseCase.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list transactions", nil)
		return
	}

	items := make([]TransactionResponse, len(result.Items))
	for i, t := range result.Items {
		items[i] = mapTransactionToResponse(t)
	}

	response.WriteSuccess(w, http.StatusOK, shared.PaginatedResult[TransactionResponse]{
		Items:      items,
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalItems: result.TotalItems,
		TotalPages: result.TotalPages,
	})
}

// @Summary		Listar transações do saldo
// @Description	Retorna, de forma paginada, as transações de crédito e débito do saldo do workspace do usuário autenticado. Aceita filtros por tipo de serviço, tipo de transação, tipo de recurso e intervalo de datas.
// @Tags			Saldo
// @Produce		json
// @Param			page			query	int		false	"Número da página (inicia em 1)"
// @Param			pageSize		query	int		false	"Quantidade de itens por página"
// @Param			serviceType		query	string	false	"Filtrar por tipo de serviço"
// @Param			type			query	string	false	"Filtrar por tipo de transação ('credit' ou 'debit')"
// @Param			resourceType	query	string	false	"Filtrar por tipo de recurso ('money')"
// @Param			startDate		query	string	false	"Data inicial (RFC3339)"
// @Param			endDate			query	string	false	"Data final (RFC3339)"
// @Success		200	{array}		balance.TransactionResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/user/balance/transactions [get]
func (h *BalanceHandler) ListMyTransactions(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}

	if _, err := h.getOrCreateUseCase.Execute(middleware.GetWorkspaceID(r)); err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to ensure balance exists", nil)
		return
	}

	values := r.URL.Query()

	input := balancedomain.ListTransactionsInput{
		WorkspaceID: middleware.GetWorkspaceID(r),
		QueryOptions: shared.QueryOptions{
			Pagination: httpx.ParsePagination(values),
		},
	}

	if serviceTypeStr := strings.TrimSpace(values.Get("serviceType")); serviceTypeStr != "" {
		st := balancedomain.ServiceType(serviceTypeStr)
		if !st.IsValid() {
			response.WriteValidationError(w, map[string]string{"serviceType": "invalid service type"})
			return
		}
		input.ServiceType = &st
	}

	if typeStr := strings.TrimSpace(values.Get("type")); typeStr != "" {
		tt := balancedomain.TransactionType(typeStr)
		if tt != balancedomain.TransactionTypeCredit && tt != balancedomain.TransactionTypeDebit {
			response.WriteValidationError(w, map[string]string{"type": "must be credit or debit"})
			return
		}
		input.Type = &tt
	}

	if resourceTypeStr := strings.TrimSpace(values.Get("resourceType")); resourceTypeStr != "" {
		rt := balancedomain.ResourceType(resourceTypeStr)
		if !rt.IsValid() {
			response.WriteValidationError(w, map[string]string{"resourceType": "must be money"})
			return
		}
		input.ResourceType = &rt
	}

	if sd := strings.TrimSpace(values.Get("startDate")); sd != "" {
		if parsed, err := time.Parse(time.RFC3339, sd); err == nil {
			input.StartDate = &parsed
		}
	}
	if ed := strings.TrimSpace(values.Get("endDate")); ed != "" {
		if parsed, err := time.Parse(time.RFC3339, ed); err == nil {
			input.EndDate = &parsed
		}
	}

	result, err := h.listTransactionsUseCase.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list transactions", nil)
		return
	}

	items := make([]TransactionResponse, len(result.Items))
	for i, t := range result.Items {
		items[i] = mapTransactionToResponse(t)
	}

	response.WriteSuccess(w, http.StatusOK, shared.PaginatedResult[TransactionResponse]{
		Items:      items,
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalItems: result.TotalItems,
		TotalPages: result.TotalPages,
	})
}

// @Summary		Listar tipos de serviço
// @Description	Retorna a lista de tipos de serviço aceitos para créditos e débitos de saldo.
// @Tags			Saldo
// @Produce		json
// @Success		200	{array}		string
// @Failure		401	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/user/balance/service-types [get]
func (h *BalanceHandler) ListServiceTypes(w http.ResponseWriter, r *http.Request) {
	response.WriteSuccess(w, http.StatusOK, balancedomain.AllServiceTypes())
}

// @Summary		Exportar transações do saldo
// @Description	Exporta as transações do saldo do workspace do usuário autenticado como arquivo CSV ou XLSX. Aceita filtros por tipo de serviço, tipo de transação e intervalo de datas.
// @Tags			Saldo
// @Produce		text/csv
// @Produce		application/vnd.openxmlformats-officedocument.spreadsheetml.sheet
// @Param			format		query	string	false	"Formato de exportação ('csv' ou 'xlsx')"
// @Param			serviceType	query	string	false	"Filtrar por tipo de serviço"
// @Param			type		query	string	false	"Filtrar por tipo de transação ('credit' ou 'debit')"
// @Param			startDate	query	string	false	"Data inicial (RFC3339)"
// @Param			endDate		query	string	false	"Data final (RFC3339)"
// @Success		200	{string}	string	"Arquivo CSV ou XLSX com as transações"
// @Failure		400	{object}	response.ErrorResponse
// @Failure		401	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/user/balance/transactions/export [get]
func (h *BalanceHandler) ExportMyTransactions(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return
	}
	if h.transactionsExporter == nil {
		response.WriteError(w, http.StatusServiceUnavailable, "Transaction export is not configured", nil)
		return
	}

	workspaceID := middleware.GetWorkspaceID(r)
	if _, err := h.getOrCreateUseCase.Execute(workspaceID); err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to ensure balance exists", nil)
		return
	}

	values := r.URL.Query()
	exportFormat := strings.ToLower(strings.TrimSpace(values.Get("format")))
	if exportFormat == "" {
		exportFormat = "csv"
	}
	if exportFormat != "csv" && exportFormat != "xlsx" {
		response.WriteValidationError(w, map[string]string{"format": "must be csv or xlsx"})
		return
	}

	input, err := balance_usecase.BuildTransactionsFilter(balance_usecase.TransactionsFilter{
		WorkspaceID: workspaceID,
		ServiceType: values.Get("serviceType"),
		Type:        values.Get("type"),
		StartDate:   values.Get("startDate"),
		EndDate:     values.Get("endDate"),
	})
	if err != nil {
		var fieldErr balance_usecase.FilterFieldError
		if errors.As(err, &fieldErr) {
			response.WriteValidationError(w, fieldErr.ValidationDetails())
			return
		}
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}

	rows, err := h.transactionsExporter.Rows(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list transactions", nil)
		return
	}

	if exportFormat == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=transactions.csv")
		if err := balance_usecase.WriteTransactionsCSV(rows, w); err != nil {
			return
		}
		return
	}

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=transactions.xlsx")
	_ = balance_usecase.WriteTransactionsXLSX(rows, w)
}
