package callbilling

import (
	"net/http"
	"time"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	billingdomain "vozko/domain/calls/billing"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"
)

type CallBillingHandler struct {
	listUseCase billingdomain.ListBillingRecordsUseCase
}

func NewCallBillingHandler(listUseCase billingdomain.ListBillingRecordsUseCase) *CallBillingHandler {
	return &CallBillingHandler{listUseCase: listUseCase}
}

// @Summary		Listar cobranças de chamadas
// @Description	Retorna o histórico paginado de cobranças de chamadas (transcrição, voz, IA e telefonia) do workspace, com filtros opcionais por status, origem e período.
// @Tags			Cobrança de chamadas
// @Produce		json
// @Param			page		query		int		false	"Número da página (inicia em 1)"
// @Param			pageSize	query		int		false	"Quantidade de itens por página"
// @Param			status		query		string	false	"Filtrar por status da cobrança"
// @Param			callSource	query		string	false	"Filtrar por origem da chamada"
// @Param			startDate	query		string	false	"Data inicial no formato RFC3339"
// @Param			endDate		query		string	false	"Data final no formato RFC3339"
// @Success		200	{object}	CallBillingListResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/call-billing [get]
func (h *CallBillingHandler) List(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "workspace ID required", nil)
		return
	}

	values := r.URL.Query()

	input := billingdomain.ListBillingInput{
		WorkspaceID: workspaceID,
		QueryOptions: shared.QueryOptions{
			Pagination: httpx.ParsePagination(values),
		},
	}

	if v := values.Get("status"); v != "" {
		s := billingdomain.BillingStatus(v)
		input.Status = &s
	}
	if v := values.Get("callSource"); v != "" {
		s := billingdomain.CallSource(v)
		input.CallSource = &s
	}
	if v := values.Get("startDate"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			input.StartDate = &t
		}
	}
	if v := values.Get("endDate"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			input.EndDate = &t
		}
	}

	result, err := h.listUseCase.Execute(input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to list billing records", nil)
		return
	}

	items := make([]CallBillingResponse, len(result.Items))
	for i, rec := range result.Items {
		items[i] = ToCallBillingResponse(rec)
	}

	response.WriteSuccess(w, http.StatusOK, shared.PaginatedResult[CallBillingResponse]{
		Items:      items,
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalItems: result.TotalItems,
		TotalPages: result.TotalPages,
	})
}
