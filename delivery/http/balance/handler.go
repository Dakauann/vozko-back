package balance

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/xuri/excelize/v2"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	balancedomain "vozko/domain/balance"
	"vozko/domain/shared"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
	"vozko/infra/http/middleware"
)

type BalanceHandler struct {
	createUseCase                 balancedomain.CreateBalanceUseCase
	getUseCase                    balancedomain.GetBalanceUseCase
	creditUseCase                 balancedomain.CreditBalanceUseCase
	debitUseCase                  balancedomain.DebitBalanceUseCase
	listTransactionsUseCase       balancedomain.ListTransactionsUseCase
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
// @Success		200	{object}	balance.FullBalanceSummary
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

	items := make([]transactionResponse, len(result.Items))
	for i, t := range result.Items {
		items[i] = mapTransactionToResponse(t)
	}

	response.WriteSuccess(w, http.StatusOK, shared.PaginatedResult[transactionResponse]{
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
// @Success		200	{array}		balance.Transaction
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

	items := make([]transactionResponse, len(result.Items))
	for i, t := range result.Items {
		items[i] = mapTransactionToResponse(t)
	}

	response.WriteSuccess(w, http.StatusOK, shared.PaginatedResult[transactionResponse]{
		Items:      items,
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalItems: result.TotalItems,
		TotalPages: result.TotalPages,
	})
}

func formatBRLCurrency(value float64) string {
	rounded := math.Round(value*1000) / 1000
	if rounded == 0 {
		rounded = 0
	}

	sign := ""
	if rounded < 0 {
		sign = "-"
		rounded = -rounded
	}

	whole := int64(rounded)
	fractional := int(math.Round((rounded - float64(whole)) * 1000))
	if fractional == 1000 {
		whole++
		fractional = 0
	}

	fractionalDigits := strings.TrimRight(fmt.Sprintf("%03d", fractional), "0")
	if len(fractionalDigits) < 2 {
		fractionalDigits += strings.Repeat("0", 2-len(fractionalDigits))
	}

	return fmt.Sprintf("%sR$ %s,%s", sign, formatBrazilianInteger(whole), fractionalDigits)
}

func formatBrazilianInteger(value int64) string {
	if value == 0 {
		return "0"
	}

	parts := make([]string, 0, 4)
	for value > 0 {
		chunk := value % 1000
		value /= 1000
		if value > 0 {
			parts = append(parts, fmt.Sprintf("%03d", chunk))
		} else {
			parts = append(parts, fmt.Sprintf("%d", chunk))
		}
	}

	for left, right := 0, len(parts)-1; left < right; left, right = left+1, right-1 {
		parts[left], parts[right] = parts[right], parts[left]
	}

	return strings.Join(parts, ".")
}

func formatMicrosToBRL(micros int64, exchangeRate float64) string {
	if exchangeRate <= 0 {
		return "-"
	}
	return formatBRLCurrency(float64(micros) / 1_000_000 * exchangeRate)
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

	if _, err := h.getOrCreateUseCase.Execute(middleware.GetWorkspaceID(r)); err != nil {
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

	input := balancedomain.ListTransactionsInput{
		WorkspaceID: middleware.GetWorkspaceID(r),
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

	var allTransactions []*balancedomain.Transaction
	const exportPageSize = shared.MaxPageSize
	for page := 1; ; page++ {
		input.QueryOptions = shared.QueryOptions{
			Pagination: shared.Pagination{Page: page, PageSize: exportPageSize},
		}
		result, err := h.listTransactionsUseCase.Execute(input)
		if err != nil {
			response.WriteError(w, http.StatusInternalServerError, "Failed to list transactions", nil)
			return
		}
		if len(result.Items) == 0 {
			break
		}
		allTransactions = append(allTransactions, result.Items...)
		if int64(len(allTransactions)) >= result.TotalItems {
			break
		}
		if len(result.Items) < result.PageSize {
			break
		}
	}

	var currentExchangeRate float64
	if item, err := h.getExchangeRateUseCase.Execute(); err == nil && item != nil && item.PriceMicros > 0 {
		currentExchangeRate = float64(item.PriceMicros) / 1_000_000
	}

	microsToUSD := func(micros int64) string {
		return fmt.Sprintf("%.6f", float64(micros)/1_000_000)
	}

	headers := []string{
		"Date", "Type", "Service", "Description",
		"Amount (USD)", "Amount (BRL)",
		"Balance Before (USD)", "Balance Before (BRL)",
		"Balance After (USD)", "Balance After (BRL)",
		"Reference ID",
	}

	rows := make([][]string, 0, len(allTransactions))
	for _, t := range allTransactions {
		refID := ""
		if t.ReferenceID != nil {
			refID = *t.ReferenceID
		}
		signedAmount := t.Amount
		if t.Type == balancedomain.TransactionTypeDebit {
			signedAmount = -signedAmount
		}

		txRate := float64(t.ExchangeRateMicros) / 1_000_000
		if t.ExchangeRateMicros <= 0 {
			txRate = currentExchangeRate
		}
		rows = append(rows, []string{
			t.CreatedAt.Format("2006-01-02 15:04:05"),
			string(t.Type),
			string(t.ServiceType),
			t.Description,
			microsToUSD(signedAmount),
			formatMicrosToBRL(signedAmount, txRate),
			microsToUSD(t.BalanceBefore),
			formatMicrosToBRL(t.BalanceBefore, currentExchangeRate),
			microsToUSD(t.BalanceAfter),
			formatMicrosToBRL(t.BalanceAfter, currentExchangeRate),
			refID,
		})
	}

	if exportFormat == "csv" {
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=transactions.csv")
		writer := csv.NewWriter(w)
		_ = writer.Write(headers)
		for _, row := range rows {
			_ = writer.Write(row)
		}
		writer.Flush()
		return
	}

	f := excelize.NewFile()
	sheet := "Transactions"
	idx, _ := f.NewSheet(sheet)
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")

	for i, hdr := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(sheet, cell, hdr)
	}
	for rowIdx, row := range rows {
		for colIdx, val := range row {
			cell, _ := excelize.CoordinatesToCellName(colIdx+1, rowIdx+2)
			_ = f.SetCellValue(sheet, cell, val)
		}
	}

	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename=transactions.xlsx")
	_ = f.Write(w)
}
