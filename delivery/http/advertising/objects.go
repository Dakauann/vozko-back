package advertisinghttp

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/advertising"
)

type SetBudgetRequest struct {
	Amount int64 `json:"amount"`
}

type EditObjectRequest struct {
	Edit advertising.ObjectEdit `json:"edit"`
}

type ObjectDetailResponse struct {
	Row        RowResponse                `json:"row"`
	Range      RangeResponse              `json:"range"`
	Budget     *advertising.Budget        `json:"budget"`
	Bid        advertising.Bid            `json:"bid"`
	Targeting  *advertising.Targeting     `json:"targeting"`
	Placements *advertising.Placements    `json:"placements"`
	Schedule   []advertising.DayPart      `json:"schedule"`
	Creative   *advertising.CreativeDraft `json:"creative"`
	Identity   advertising.Identity       `json:"identity"`
}

func (h *Handler) writeObject(w http.ResponseWriter, object *advertising.Object, err error, fallback string) {
	if err != nil {
		writeError(w, err, fallback)
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentObject(object, h.now()))
}

func (h *Handler) setStatus(w http.ResponseWriter, r *http.Request, on bool) {
	object, err := h.d.Manage.SetStatus(r.Context(), workspaceOf(r), mux.Vars(r)["metaId"], on)
	h.writeObject(w, object, err, "Failed to change the status")
}

// @Summary		Ligar campanha, conjunto ou anúncio
// @Description	Liga o item na Meta. Recusa contas sem meio de pagamento ou inativas.
// @Tags			Anúncios
// @Produce		json
// @Param			metaId	path		string	true	"ID do item na Meta"
// @Success		200		{object}	RowResponse
// @Failure		409		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/objects/{metaId}/activate [post]
func (h *Handler) Activate(w http.ResponseWriter, r *http.Request) { h.setStatus(w, r, true) }

// @Summary		Desligar campanha, conjunto ou anúncio
// @Tags			Anúncios
// @Produce		json
// @Param			metaId	path		string	true	"ID do item na Meta"
// @Success		200		{object}	RowResponse
// @Failure		409		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/objects/{metaId}/pause [post]
func (h *Handler) Pause(w http.ResponseWriter, r *http.Request) { h.setStatus(w, r, false) }

// @Summary		Alterar orçamento
// @Description	Altera o valor do orçamento atual (diário ou total), sem trocar o tipo. Valor em unidades mínimas da moeda da conta (centavos para BRL). A Meta permite 4 mudanças por hora.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			metaId	path		string				true	"ID da campanha ou do conjunto na Meta"
// @Param			body	body		SetBudgetRequest	true	"novo valor do orçamento"
// @Success		200		{object}	RowResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Failure		429		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/objects/{metaId}/budget [patch]
func (h *Handler) SetBudget(w http.ResponseWriter, r *http.Request) {
	var req SetBudgetRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	object, err := h.d.Manage.SetBudget(r.Context(), workspaceOf(r), mux.Vars(r)["metaId"], req.Amount)
	h.writeObject(w, object, err, "Failed to change the budget")
}

// @Summary		Detalhe de campanha, conjunto ou anúncio
// @Description	A linha do gerenciador com os resultados do período (sem período, os últimos 30 dias) e a configuração atual na Meta: orçamento, lance, público, posicionamentos, programação, criativo e identidade.
// @Tags			Anúncios
// @Produce		json
// @Param			metaId	path		string	true	"ID do item na Meta"
// @Param			since	query		string	false	"YYYY-MM-DD"
// @Param			until	query		string	false	"YYYY-MM-DD"
// @Success		200		{object}	ObjectDetailResponse
// @Failure		404		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/objects/{metaId} [get]
func (h *Handler) ObjectDetail(w http.ResponseWriter, r *http.Request) {
	dates, err := dateRangeQuery(r.URL.Query())
	if err != nil {
		writeError(w, err, "Failed to load the ad object")
		return
	}
	detail, err := h.d.Manage.Detail(r.Context(), workspaceOf(r), mux.Vars(r)["metaId"])
	if err != nil {
		writeError(w, err, "Failed to load the ad object")
		return
	}
	results, err := h.d.Report.ObjectRow(r.Context(), workspaceOf(r), detail.Object.MetaID, dates)
	if err != nil {
		writeError(w, err, "Failed to load the ad object")
		return
	}
	response.WriteSuccess(w, http.StatusOK, ObjectDetailResponse{
		Row: presentRow(results.Row, results.Account.Currency, h.now()), Range: presentRange(results.Range),
		Budget: detail.Budget, Bid: detail.Bid, Targeting: detail.Targeting,
		Placements: detail.Placements, Schedule: detail.Schedule, Creative: detail.Creative, Identity: detail.Identity,
	})
}

// @Summary		Editar campanha, conjunto ou anúncio
// @Description	Envia só os campos que mudam. Cada nível aceita campos próprios; o tipo de orçamento não muda depois de criado.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			metaId	path		string				true	"ID do item na Meta"
// @Param			body	body		EditObjectRequest	true	"alterações"
// @Success		200		{object}	RowResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/objects/{metaId} [patch]
func (h *Handler) EditObject(w http.ResponseWriter, r *http.Request) {
	var req EditObjectRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	object, err := h.d.Manage.Edit(r.Context(), workspaceOf(r), mux.Vars(r)["metaId"], req.Edit)
	h.writeObject(w, object, err, "Failed to edit the ad object")
}

// @Summary		Duplicar campanha, conjunto ou anúncio
// @Description	Cria uma cópia na Meta, opcionalmente dentro de outra campanha ou conjunto da mesma conta, com ou sem os itens filhos.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			metaId	path		string					true	"ID do item na Meta"
// @Param			body	body		advertising.CopyRequest	true	"opções da cópia"
// @Success		201		{object}	MetaIDResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/objects/{metaId}/copies [post]
func (h *Handler) CopyObject(w http.ResponseWriter, r *http.Request) {
	var req advertising.CopyRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	id, err := h.d.Manage.Copy(r.Context(), workspaceOf(r), mux.Vars(r)["metaId"], req)
	if err != nil {
		writeError(w, err, "Failed to copy the ad object")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, MetaIDResponse{MetaID: id})
}

// @Summary		Arquivar campanha, conjunto ou anúncio
// @Tags			Anúncios
// @Produce		json
// @Param			metaId	path		string	true	"ID do item na Meta"
// @Success		200		{object}	RowResponse
// @Failure		409		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/objects/{metaId}/archive [post]
func (h *Handler) ArchiveObject(w http.ResponseWriter, r *http.Request) {
	object, err := h.d.Manage.Lifecycle(r.Context(), workspaceOf(r), mux.Vars(r)["metaId"], advertising.LifecycleArchive)
	h.writeObject(w, object, err, "Failed to archive the ad object")
}

// @Summary		Excluir campanha, conjunto ou anúncio
// @Tags			Anúncios
// @Param			metaId	path	string	true	"ID do item na Meta"
// @Success		204
// @Failure		409	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/objects/{metaId} [delete]
func (h *Handler) DeleteObject(w http.ResponseWriter, r *http.Request) {
	if _, err := h.d.Manage.Lifecycle(r.Context(), workspaceOf(r), mux.Vars(r)["metaId"], advertising.LifecycleDelete); err != nil {
		writeError(w, err, "Failed to delete the ad object")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
