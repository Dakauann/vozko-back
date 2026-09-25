package copilottools

import (
	"context"
	"sort"
	"strings"

	"vozko/domain/attendance"
	"vozko/domain/copilot"
	"vozko/domain/tools"
)

const maxTrendMetrics = 4

type attendanceTrendTool struct{ deps AttendanceDeps }

func NewAttendanceTrendTool(deps AttendanceDeps) copilot.Tool {
	return &attendanceTrendTool{deps: deps}
}

func (t *attendanceTrendTool) Meta() copilot.Meta { return attendanceReadMeta() }

func (t *attendanceTrendTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "attendance_trend",
		Description: "Série MENSAL de até 24 meses, terminando no mês de date_to, para as séries que a página desenha (concluídas, conversas com mensagem, novas entradas, fila acumulada e receita). Outras métricas não têm série mensal: compare períodos com attendance_metrics. Use para evolução, " +
			"sazonalidade e comparações longas (trimestres, anos). Retorna um resumo por métrica (melhor mês, variação contra o " +
			"mês anterior, mês parcial) e um dataset com a tabela mês a mês para render_chart ou query_dataset.",
		Parameters: withParams(attendanceParams(), map[string]tools.Parameter{
			"metrics": {Type: "array", Description: "de 1 a 4 séries: " + strings.Join(attendance.TrendMetricKeys(), ", ") + " (pending_stock é a fila acumulada no fim de cada mês)", Items: &tools.ParameterItems{Type: "string"}},
			"months":  {Type: "integer", Description: "quantidade de meses (1 a 24, padrão 13)"},
		}),
		Required: []string{"metrics"},
	}
}

func (t *attendanceTrendTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	keys := argStringList(args, "metrics")
	if len(keys) > maxTrendMetrics {
		return copilot.Result{Status: copilot.StatusError, Message: "no máximo 4 métricas por série; faça outra chamada para as demais"}
	}
	q, err := t.deps.resolve(ctx, cc, args)
	if err != nil {
		return analyticsFailure("attendance_trend", err)
	}
	q.filter.TrendBuckets = attendance.ClampTrendBuckets(argInt(args, "months"))
	section, err := t.deps.Sections.Trend(ctx, cc.WorkspaceID, q.filter)
	if err != nil {
		return analyticsFailure("attendance_trend", err)
	}
	series, err := attendance.DigestTrend(section.Trend, keys)
	if err != nil {
		return analyticsFailure("attendance_trend", err)
	}
	dataset, err := trendDataset(series)
	if err != nil {
		return analyticsFailure("attendance_trend", err)
	}
	summaries := make([]map[string]interface{}, 0, len(series))
	for _, s := range series {
		summaries = append(summaries, map[string]interface{}{
			"metric":                      s.Metric,
			"kind":                        s.Kind,
			"direction":                   s.Direction,
			"best_month":                  s.BestMonth,
			"delta_pct_vs_previous_month": s.DeltaPct,
			"partial_month":               partialMonth(s),
			"available":                   s.Available,
			"reason":                      s.Reason,
		})
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"query":   q.describe(),
		"months":  q.filter.TrendBuckets,
		"series":  summaries,
		"dataset": keep(cc, dataset).Preview(),
	}}
}

func partialMonth(s attendance.TrendDigest) string {
	for _, p := range s.Points {
		if p.Partial {
			return p.Month
		}
	}
	return ""
}

func trendDataset(series []attendance.TrendDigest) (*copilot.Dataset, error) {
	columns := []copilot.Column{{Key: "month", Label: "month", Kind: copilot.ColumnDate}}
	byMonth := map[string]map[string]float64{}
	for _, s := range series {
		columns = append(columns, copilot.Column{Key: s.Metric, Label: s.Metric, Kind: columnKindOf(s.Kind)})
		for _, p := range s.Points {
			if byMonth[p.Month] == nil {
				byMonth[p.Month] = map[string]float64{}
			}
			byMonth[p.Month][s.Metric] = p.Value
		}
	}
	months := make([]string, 0, len(byMonth))
	for m := range byMonth {
		months = append(months, m)
	}
	sort.Strings(months)
	d := copilot.NewDataset("attendance trend", columns)
	for _, m := range months {
		row := []any{m}
		for _, s := range series {
			if v, found := byMonth[m][s.Metric]; found {
				row = append(row, v)
			} else {
				row = append(row, nil)
			}
		}
		if err := d.AddRow(row...); err != nil {
			return nil, err
		}
	}
	return d, nil
}
