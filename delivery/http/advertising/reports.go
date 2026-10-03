package advertisinghttp

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/advertising"
	adsuc "vozko/usecases/advertising"
)

type PeriodResponse struct {
	Range   RangeResponse   `json:"range"`
	Totals  MetricsResponse `json:"totals"`
	Outcome OutcomeResponse `json:"outcome"`
}

type ReportResponse struct {
	Account  AccountResponse `json:"account"`
	Range    RangeResponse   `json:"range"`
	Level    string          `json:"level"`
	Totals   MetricsResponse `json:"totals"`
	Outcome  OutcomeResponse `json:"outcome"`
	Rows     []RowResponse   `json:"rows"`
	Previous *PeriodResponse `json:"previous,omitempty"`
}

type TrendPointResponse struct {
	Day           string `json:"day"`
	Spend         int64  `json:"spend"`
	Impressions   int64  `json:"impressions"`
	LinkClicks    int64  `json:"linkClicks"`
	Results       int64  `json:"results"`
	Conversations int64  `json:"conversations"`
}

type TrendResponse struct {
	Currency string               `json:"currency"`
	Range    RangeResponse        `json:"range"`
	Points   []TrendPointResponse `json:"points"`
}

type LiveRowResponse struct {
	ObjectID        string                   `json:"objectId"`
	Dimensions      map[string]string        `json:"dimensions"`
	Metrics         MetricsResponse          `json:"metrics"`
	Reach           int64                    `json:"reach"`
	Frequency       float64                  `json:"frequency"`
	Video           advertising.VideoMetrics `json:"video"`
	CostPerThruPlay *int64                   `json:"costPerThruPlay"`
}

type LiveReportResponse struct {
	Currency string            `json:"currency"`
	Range    RangeResponse     `json:"range"`
	Rows     []LiveRowResponse `json:"rows"`
}

func dateRangeQuery(q url.Values) (advertising.DateRange, error) {
	if q.Get("since") == "" && q.Get("until") == "" {
		return advertising.DateRange{}, nil
	}
	return advertising.NewDateRange(q.Get("since"), q.Get("until"))
}

func reportQuery(r *http.Request) (adsuc.ReportQuery, error) {
	q := r.URL.Query()
	dates, err := dateRangeQuery(q)
	if err != nil {
		return adsuc.ReportQuery{}, err
	}
	compare, err := flagQuery(r, "compare")
	if err != nil {
		return adsuc.ReportQuery{}, err
	}
	return adsuc.ReportQuery{
		WorkspaceID: workspaceOf(r), AccountID: mux.Vars(r)["id"], Level: advertising.Level(q.Get("level")), Range: dates,
		CampaignIDs: listParam(q.Get("campaignIds")), AdSetIDs: listParam(q.Get("adSetIds")), Search: strings.TrimSpace(q.Get("search")),
		Compare: compare,
	}, nil
}

func liveQuery(r *http.Request) (advertising.LiveQuery, error) {
	q := r.URL.Query()
	dates, err := dateRangeQuery(q)
	if err != nil {
		return advertising.LiveQuery{}, err
	}
	return advertising.LiveQuery{
		WorkspaceID: workspaceOf(r), AccountID: mux.Vars(r)["id"], Level: advertising.Level(q.Get("level")), Range: dates,
		ObjectIDs:  listParam(q.Get("objectIds")),
		Breakdowns: typedList[advertising.Breakdown](q.Get("breakdowns")),
		Windows:    typedList[advertising.AttributionWindow](q.Get("windows")),
	}, nil
}

func (h *Handler) buildReport(r *http.Request) (*adsuc.Report, error) {
	q, err := reportQuery(r)
	if err != nil {
		return nil, err
	}
	return h.d.Report.Report(r.Context(), q)
}

// @Summary		Relatório da conta de anúncios
// @Description	Campanhas, conjuntos ou anúncios com resultados da Meta (gasto, resultados, custo por resultado, CTR, CPM) e do CRM (conversas, leads, vendas, ROAS). Dias no fuso da conta de anúncios; sem período, os últimos 30 dias. Com compare=1, inclui o período anterior de mesmo tamanho.
// @Tags			Anúncios
// @Produce		json
// @Param			id			path		string	true	"ID da conta de anúncios"
// @Param			level		query		string	false	"campaign, adset ou ad"
// @Param			since		query		string	false	"YYYY-MM-DD"
// @Param			until		query		string	false	"YYYY-MM-DD"
// @Param			campaignIds	query		string	false	"IDs de campanha separados por vírgula"
// @Param			adSetIds	query		string	false	"IDs de conjunto separados por vírgula"
// @Param			search		query		string	false	"busca por nome"
// @Param			compare		query		string	false	"1 para comparar com o período anterior"
// @Success		200			{object}	ReportResponse
// @Failure		400			{object}	response.ErrorResponse
// @Failure		404			{object}	response.ErrorResponse
// @Failure		422			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/report [get]
func (h *Handler) Report(w http.ResponseWriter, r *http.Request) {
	report, err := h.buildReport(r)
	if err != nil {
		writeError(w, err, "Failed to build the ads report")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentReport(report, h.now()))
}

// @Summary		Exportar relatório da conta de anúncios em CSV
// @Description	As mesmas linhas do relatório, em CSV (UTF-8). Valores em dinheiro em unidades da moeda da conta.
// @Tags			Anúncios
// @Produce		text/csv
// @Param			id			path		string	true	"ID da conta de anúncios"
// @Param			level		query		string	false	"campaign, adset ou ad"
// @Param			since		query		string	false	"YYYY-MM-DD"
// @Param			until		query		string	false	"YYYY-MM-DD"
// @Param			campaignIds	query		string	false	"IDs de campanha separados por vírgula"
// @Param			adSetIds	query		string	false	"IDs de conjunto separados por vírgula"
// @Param			search		query		string	false	"busca por nome"
// @Success		200			{string}	string	"arquivo CSV"
// @Failure		400			{object}	response.ErrorResponse
// @Failure		404			{object}	response.ErrorResponse
// @Failure		422			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/report.csv [get]
func (h *Handler) ReportCSV(w http.ResponseWriter, r *http.Request) {
	report, err := h.buildReport(r)
	if err != nil {
		writeError(w, err, "Failed to export the ads report")
		return
	}
	presented := presentReport(report, h.now())
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="anuncios-%s-%s.csv"`, presented.Range.Since, presented.Range.Until))
	if err := writeReportCSV(w, presented); err != nil {
		log.Printf("[ads] report csv not fully written: %v", err)
	}
}

// @Summary		Evolução diária da conta de anúncios
// @Tags			Anúncios
// @Produce		json
// @Param			id			path		string	true	"ID da conta de anúncios"
// @Param			since		query		string	false	"YYYY-MM-DD"
// @Param			until		query		string	false	"YYYY-MM-DD"
// @Param			campaignIds	query		string	false	"IDs de campanha separados por vírgula"
// @Param			adSetIds	query		string	false	"IDs de conjunto separados por vírgula"
// @Success		200			{object}	TrendResponse
// @Failure		400			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/trend [get]
func (h *Handler) Trend(w http.ResponseWriter, r *http.Request) {
	q, err := reportQuery(r)
	if err != nil {
		writeError(w, err, "Failed to build the ads trend")
		return
	}
	trend, err := h.d.Report.Trend(r.Context(), q)
	if err != nil {
		writeError(w, err, "Failed to build the ads trend")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentTrend(trend))
}

// @Summary		Resultados ao vivo da Meta
// @Description	Consulta a Meta na hora, com quebras (idade, gênero, país, região, plataforma, posicionamento, dispositivo, hora do dia), janelas de atribuição, alcance, frequência e métricas de vídeo. Sem período, os últimos 30 dias.
// @Tags			Anúncios
// @Produce		json
// @Param			id			path		string	true	"ID da conta de anúncios"
// @Param			level		query		string	false	"campaign, adset ou ad"
// @Param			since		query		string	false	"YYYY-MM-DD"
// @Param			until		query		string	false	"YYYY-MM-DD"
// @Param			objectIds	query		string	false	"IDs na Meta separados por vírgula"
// @Param			breakdowns	query		string	false	"quebras separadas por vírgula, por exemplo age,gender"
// @Param			windows		query		string	false	"janelas de atribuição separadas por vírgula, por exemplo 7d_click,1d_view"
// @Success		200			{object}	LiveReportResponse
// @Failure		400			{object}	response.ErrorResponse
// @Failure		422			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/ads/accounts/{id}/insights [get]
func (h *Handler) Insights(w http.ResponseWriter, r *http.Request) {
	q, err := liveQuery(r)
	if err != nil {
		writeError(w, err, "Failed to load live insights")
		return
	}
	live, err := h.d.Live.Insights(r.Context(), q)
	if err != nil {
		writeError(w, err, "Failed to load live insights")
		return
	}
	response.WriteSuccess(w, http.StatusOK, presentLive(live))
}

func presentPeriod(p *adsuc.Period, currency string) *PeriodResponse {
	if p == nil {
		return nil
	}
	return &PeriodResponse{Range: presentRange(p.Range), Totals: presentMetrics(p.Totals, currency), Outcome: presentOutcome(p.Outcome)}
}

func presentReport(r *adsuc.Report, now time.Time) ReportResponse {
	currency := r.Account.Currency
	return ReportResponse{
		Account: presentAccount(r.Account),
		Range:   presentRange(r.Range),
		Level:   string(r.Level),
		Totals:  presentMetrics(r.Totals, currency),
		Outcome: presentOutcome(r.Outcome),
		Rows: presentAll(r.Rows, func(row adsuc.ReportRow) RowResponse {
			return presentRow(row, currency, now)
		}),
		Previous: presentPeriod(r.Previous, currency),
	}
}

func presentTrend(t *adsuc.Trend) TrendResponse {
	return TrendResponse{
		Currency: t.Account.Currency,
		Range:    presentRange(t.Range),
		Points: presentAll(t.Points, func(p adsuc.TrendPoint) TrendPointResponse {
			return TrendPointResponse{
				Day: p.Day.Format(advertising.DayLayout), Spend: p.Metrics.SpendMicros, Impressions: p.Metrics.Impressions,
				LinkClicks: p.Metrics.LinkClicks, Results: p.Metrics.Results, Conversations: p.Metrics.Conversations,
			}
		}),
	}
}

func presentLive(l *adsuc.LiveReport) LiveReportResponse {
	currency := l.Account.Currency
	return LiveReportResponse{
		Currency: currency,
		Range:    presentRange(l.Query.Range),
		Rows: presentAll(l.Rows, func(row advertising.LiveRow) LiveRowResponse {
			dimensions := make(map[string]string, len(row.Dimensions))
			for breakdown, value := range row.Dimensions {
				dimensions[string(breakdown)] = value
			}
			return LiveRowResponse{
				ObjectID: row.ObjectID, Dimensions: dimensions, Metrics: presentMetrics(row.Values.Metrics, currency),
				Reach: row.Values.Reach, Frequency: row.Values.Frequency, Video: row.Values.Video,
				CostPerThruPlay: row.Values.CostPerThruPlay(),
			}
		}),
	}
}

func presentRow(row adsuc.ReportRow, currency string, now time.Time) RowResponse {
	item := presentObject(row.Object, now)
	item.Metrics = presentMetrics(row.Metrics, currency)
	item.Outcome = presentOutcome(row.Outcome)
	return item
}
