package advertisinghttp

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/advertising"
	"vozko/domain/report"
	adsuc "vozko/usecases/advertising"
)

type ReportRunRequest struct {
	Definition advertising.ReportDefinition    `json:"definition"`
	Range      RangeResponse                   `json:"range"`
	ObjectIDs  []string                        `json:"objectIds,omitempty"`
	Windows    []advertising.AttributionWindow `json:"windows,omitempty"`
}

type ReportExportRequest struct {
	ReportRunRequest
	Name        string                   `json:"name"`
	AdAccountID string                   `json:"adAccountId"`
	ReportID    string                   `json:"reportId,omitempty"`
	Labels      advertising.ExportLabels `json:"labels"`
}

type ReportRunRowResponse struct {
	Key        string                  `json:"key"`
	ObjectID   string                  `json:"objectId,omitempty"`
	Name       string                  `json:"name,omitempty"`
	Dimensions []string                `json:"dimensions"`
	Share      float64                 `json:"share"`
	Values     advertising.ReportCells `json:"values"`
}

type ReportRunDayResponse struct {
	Day    string                  `json:"day"`
	Values advertising.ReportCells `json:"values"`
}

type ReportRunResponse struct {
	Currency    string                                              `json:"currency"`
	View        advertising.ReportView                              `json:"view"`
	Breakdowns  []advertising.Breakdown                             `json:"breakdowns"`
	Metrics     []advertising.ReportMetric                          `json:"metrics"`
	MetricKinds map[advertising.ReportMetric]advertising.MetricKind `json:"metricKinds"`
	Rows        []ReportRunRowResponse                              `json:"rows"`
	Totals      advertising.ReportCells                             `json:"totals"`
	Series      []ReportRunDayResponse                              `json:"series"`
}

type ReportExportResponse struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	AdAccountID string    `json:"adAccountId"`
	ReportID    string    `json:"reportId,omitempty"`
	Since       string    `json:"since"`
	Until       string    `json:"until"`
	Rows        int       `json:"rows"`
	SizeBytes   int       `json:"sizeBytes"`
	CreatedBy   string    `json:"createdBy"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (req ReportRunRequest) input(r *http.Request, accountID string) (adsuc.ReportRunInput, error) {
	dates, err := advertising.NewDateRange(req.Range.Since, req.Range.Until)
	if err != nil {
		return adsuc.ReportRunInput{}, err
	}
	return adsuc.ReportRunInput{
		WorkspaceID: workspaceOf(r), AccountID: accountID, Definition: req.Definition, Range: dates,
		ObjectIDs: req.ObjectIDs, Windows: req.Windows,
	}, nil
}

// @Summary		Rodar relatório
// @Description	Monta a tabela de Relatórios de Anúncios no servidor, com as mesmas fórmulas do gerenciador: tabela dinâmica (por item e quebras), barras (por quebras, ou por item sem quebras, do maior para o menor na primeira métrica) ou tendência (por dia). metricKinds diz o tipo de cada métrica (money em micros da moeda da conta, count, percent, decimal); alcance e frequência só valem para uma linha da Meta. Até 100 objectIds.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"ID da conta de anúncios"
// @Param			body	body		ReportRunRequest	true	"definição e período"
// @Success		200		{object}	ReportRunResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/report-runs [post]
func (h *Handler) RunReport(w http.ResponseWriter, r *http.Request) {
	var req ReportRunRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in, err := req.input(r, mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "Failed to run the report")
		return
	}
	run, err := h.d.Runs.Run(r.Context(), in)
	if err != nil {
		writeError(w, err, "Failed to run the report")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentRun(run))
}

// @Summary		Exportar relatório
// @Description	Roda o relatório, grava o CSV (separado por ponto e vírgula, com BOM, dinheiro em unidades da moeda) com os rótulos enviados no idioma da pessoa e guarda em Exportações. Ficam as 100 exportações mais recentes do workspace; até 10 MB por arquivo.
// @Tags			Anúncios
// @Accept			json
// @Produce		json
// @Param			body	body		ReportExportRequest	true	"relatório, período e rótulos"
// @Success		201		{object}	ReportExportResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/report-exports [post]
func (h *Handler) CreateReportExport(w http.ResponseWriter, r *http.Request) {
	var req ReportExportRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	in, err := req.ReportRunRequest.input(r, req.AdAccountID)
	if err != nil {
		writeError(w, err, "Failed to export the report")
		return
	}
	export, err := h.d.Runs.Export(r.Context(), adsuc.ReportExportInput{
		ReportRunInput: in, UserID: personOf(r).UserID, Name: req.Name, ReportID: req.ReportID, Labels: req.Labels,
	})
	if err != nil {
		writeError(w, err, "Failed to export the report")
		return
	}
	response.WriteSuccess(w, http.StatusCreated, presentExport(export))
}

// @Summary		Listar exportações
// @Description	Histórico de exportações do workspace, da mais recente para a mais antiga.
// @Tags			Anúncios
// @Produce		json
// @Success		200	{array}	ReportExportResponse
// @Security		BearerAuth
// @Router			/ads/report-exports [get]
func (h *Handler) ReportExports(w http.ResponseWriter, r *http.Request) {
	exports, err := h.d.Runs.Exports(r.Context(), workspaceOf(r))
	if err != nil {
		writeError(w, err, "Failed to list the report exports")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentAll(exports, presentExport))
}

// @Summary		Baixar exportação
// @Tags			Anúncios
// @Produce		text/csv
// @Param			id	path	string	true	"ID da exportação"
// @Success		200	{file}	file
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/report-exports/{id}/file [get]
func (h *Handler) ReportExportFile(w http.ResponseWriter, r *http.Request) {
	export, err := h.d.Runs.ExportFile(r.Context(), workspaceOf(r), mux.Vars(r)["id"])
	if err != nil {
		writeError(w, err, "Failed to download the report export")
		return
	}
	filename := report.Filename("csv", export.Name, export.Range.Since.Format(advertising.DayLayout), export.Range.Until.Format(advertising.DayLayout))
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("Content-Length", strconv.Itoa(len(export.Content)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(export.Content)
}

// @Summary		Excluir exportação
// @Tags			Anúncios
// @Param			id	path	string	true	"ID da exportação"
// @Success		204
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/report-exports/{id} [delete]
func (h *Handler) DeleteReportExport(w http.ResponseWriter, r *http.Request) {
	if err := h.d.Runs.DeleteExport(r.Context(), workspaceOf(r), mux.Vars(r)["id"]); err != nil {
		writeError(w, err, "Failed to delete the report export")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func presentRun(run *adsuc.ReportRun) ReportRunResponse {
	t := run.Table
	out := ReportRunResponse{
		Currency: run.Currency, View: t.View, Breakdowns: append([]advertising.Breakdown{}, t.Breakdowns...),
		Metrics: append([]advertising.ReportMetric{}, t.Metrics...), MetricKinds: advertising.KindsOf(t.Metrics), Rows: []ReportRunRowResponse{}, Totals: t.Totals,
		Series: []ReportRunDayResponse{},
	}
	if out.Totals == nil {
		out.Totals = advertising.ReportCells{}
	}
	for _, row := range t.Rows {
		out.Rows = append(out.Rows, ReportRunRowResponse{
			Key: row.Key, ObjectID: row.ObjectID, Name: row.Name, Dimensions: append([]string{}, row.Dimensions...), Share: row.Share, Values: row.Values,
		})
	}
	for _, day := range run.Series {
		out.Series = append(out.Series, ReportRunDayResponse{Day: day.Day.Format(advertising.DayLayout), Values: day.Values})
	}
	return out
}

func presentExport(e *advertising.ReportExport) ReportExportResponse {
	return ReportExportResponse{
		ID: e.ID, Name: e.Name, AdAccountID: e.AdAccountID, ReportID: e.ReportID,
		Since: e.Range.Since.Format(advertising.DayLayout), Until: e.Range.Until.Format(advertising.DayLayout),
		Rows: e.Rows, SizeBytes: e.SizeBytes, CreatedBy: e.CreatedBy, CreatedAt: e.CreatedAt,
	}
}
