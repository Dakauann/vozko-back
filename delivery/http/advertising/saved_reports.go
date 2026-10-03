package advertisinghttp

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/advertising"
	adsuc "vozko/usecases/advertising"
)

type SavedReportRequest struct {
	Name        string                       `json:"name"`
	AdAccountID string                       `json:"adAccountId"`
	Definition  advertising.ReportDefinition `json:"definition"`
}

func (r SavedReportRequest) input() adsuc.SavedReportInput {
	return adsuc.SavedReportInput{Name: r.Name, AdAccountID: r.AdAccountID, Definition: r.Definition}
}

type ReportTemplateResponse struct {
	Key        string                       `json:"key"`
	Definition advertising.ReportDefinition `json:"definition"`
}

type ReportOptionsResponse struct {
	Templates       []ReportTemplateResponse                            `json:"templates"`
	Views           []advertising.ReportView                            `json:"views"`
	Levels          []advertising.Level                                 `json:"levels"`
	Breakdowns      []advertising.Breakdown                             `json:"breakdowns"`
	Metrics         []advertising.ReportMetric                          `json:"metrics"`
	TrendMetrics    []advertising.ReportMetric                          `json:"trendMetrics"`
	MetricKinds     map[advertising.ReportMetric]advertising.MetricKind `json:"metricKinds"`
	BreakdownGroups [][]advertising.Breakdown                           `json:"breakdownGroups"`
}

type SavedReportResponse struct {
	ID           string                       `json:"id"`
	Name         string                       `json:"name"`
	AdAccountID  string                       `json:"adAccountId"`
	Definition   advertising.ReportDefinition `json:"definition"`
	CreatedBy    string                       `json:"createdBy"`
	CreatedAt    time.Time                    `json:"createdAt"`
	UpdatedAt    time.Time                    `json:"updatedAt"`
	LastOpenedAt *time.Time                   `json:"lastOpenedAt,omitempty"`
}

// @Summary		Opções de Relatórios de Anúncios
// @Description	Tudo o que o editor de relatórios pode oferecer, vindo das mesmas regras que validam o relatório salvo: modelos sugeridos (roi_snapshot, reach_frequency, overall_performance, signups_summary, age_gender, engagement) com a definição pronta, visualizações, níveis, quebras aceitas pela Meta, métricas, as métricas diárias que a tendência mostra e as combinações de quebras que a Meta aceita.
// @Tags			Anúncios
// @Produce		json
// @Success		200	{object}	ReportOptionsResponse
// @Security		BearerAuth
// @Router			/ads/report-options [get]
func (h *Handler) ReportOptions(w http.ResponseWriter, _ *http.Request) {
	o := h.d.Reports.Options()
	response.WriteSuccess(w, http.StatusOK, ReportOptionsResponse{
		Templates: presentAll(o.Templates, func(t advertising.ReportTemplate) ReportTemplateResponse {
			return ReportTemplateResponse{Key: t.Key, Definition: presentDefinition(t.Definition)}
		}),
		Views: o.Views, Levels: o.Levels, Breakdowns: o.Breakdowns, Metrics: o.Metrics, TrendMetrics: o.TrendMetrics,
		BreakdownGroups: o.BreakdownGroups,
	})
}

// @Summary		Listar relatórios salvos
// @Description	Relatórios do workspace, abertos por último primeiro. Os dados vêm de POST /ads/accounts/{id}/report-runs.
// @Tags			Anúncios
// @Produce		json
// @Success		200	{array}	SavedReportResponse
// @Security		BearerAuth
// @Router			/ads/reports [get]
func (h *Handler) SavedReports(w http.ResponseWriter, r *http.Request) {
	reports, err := h.d.Reports.List(r.Context(), workspaceOf(r))
	if err != nil {
		writeError(w, err, "Failed to list the saved reports")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentAll(reports, presentSavedReport))
}

// @Summary		Abrir relatório salvo
// @Description	Devolve o relatório e marca o último acesso.
// @Tags			Anúncios
// @Produce		json
// @Param			id	path		string	true	"ID do relatório"
// @Success		200	{object}	SavedReportResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/reports/{id} [get]
func (h *Handler) OpenSavedReport(w http.ResponseWriter, r *http.Request) {
	report, err := h.d.Reports.Open(r.Context(), workspaceOf(r), mux.Vars(r)["id"])
	h.writeSavedReport(w, report, err, http.StatusOK, "Failed to open the saved report")
}

// @Summary		Salvar novo relatório
// @Description	Guarda nome, conta e definição (view pivot, trend ou bars; nível; quebras aceitas pela Meta; métricas; período). A tendência aceita só métricas diárias e nenhuma quebra.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			body	body		SavedReportRequest	true	"relatório"
// @Success		201		{object}	SavedReportResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/reports [post]
func (h *Handler) CreateSavedReport(w http.ResponseWriter, r *http.Request) {
	var req SavedReportRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	report, err := h.d.Reports.Create(r.Context(), workspaceOf(r), personOf(r).UserID, req.input())
	h.writeSavedReport(w, report, err, http.StatusCreated, "Failed to save the report")
}

// @Summary		Atualizar relatório salvo
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			id		path		string					true	"ID do relatório"
// @Param			body	body		SavedReportRequest	true	"relatório"
// @Success		200		{object}	SavedReportResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/reports/{id} [put]
func (h *Handler) UpdateSavedReport(w http.ResponseWriter, r *http.Request) {
	var req SavedReportRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	report, err := h.d.Reports.Update(r.Context(), workspaceOf(r), mux.Vars(r)["id"], req.input())
	h.writeSavedReport(w, report, err, http.StatusOK, "Failed to update the report")
}

// @Summary		Excluir relatório salvo
// @Tags			Anúncios
// @Param			id	path	string	true	"ID do relatório"
// @Success		204
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/reports/{id} [delete]
func (h *Handler) DeleteSavedReport(w http.ResponseWriter, r *http.Request) {
	if err := h.d.Reports.Delete(r.Context(), workspaceOf(r), mux.Vars(r)["id"]); err != nil {
		writeError(w, err, "Failed to delete the report")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) writeSavedReport(w http.ResponseWriter, report *advertising.SavedReport, err error, status int, fallback string) {
	if err != nil {
		writeError(w, err, fallback)
		return
	}
	response.WriteSuccess(w, status, presentSavedReport(report))
}

func presentSavedReport(r *advertising.SavedReport) SavedReportResponse {
	return SavedReportResponse{
		ID: r.ID, Name: r.Name, AdAccountID: r.AdAccountID, Definition: presentDefinition(r.Definition), CreatedBy: r.CreatedBy,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt, LastOpenedAt: r.LastOpenedAt,
	}
}

func presentDefinition(d advertising.ReportDefinition) advertising.ReportDefinition {
	d.Breakdowns = append([]advertising.Breakdown{}, d.Breakdowns...)
	d.Metrics = append([]advertising.ReportMetric{}, d.Metrics...)
	return d
}
