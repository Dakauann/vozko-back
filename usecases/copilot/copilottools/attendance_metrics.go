package copilottools

import (
	"context"
	"fmt"
	"strings"

	"vozko/domain/attendance"
	"vozko/domain/copilot"
	"vozko/domain/tools"
)

type attendanceMetricsTool struct{ deps AttendanceDeps }

func NewAttendanceMetricsTool(deps AttendanceDeps) copilot.Tool {
	return &attendanceMetricsTool{deps: deps}
}

func (t *attendanceMetricsTool) Meta() copilot.Meta { return attendanceReadMeta() }

func (t *attendanceMetricsTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "attendance_metrics",
		Description: "Indicadores de atendimento de um período (os mesmos números da página Métricas · Atendimento). " +
			"Retorna só as métricas pedidas, com meta e projeção quando existir meta. Com compare_previous=true traz também o " +
			"período anterior de mesma duração, a variação absoluta e percentual e se melhorou ou piorou (já considerando se " +
			"maior ou menor é melhor). value=null significa sem dado, não zero. Métricas: " + strings.Join(attendance.MetricKeys(), ", ") +
			". revenue_cents está em centavos. Em details peça blocos extras da página: timing (primeira resposta média, mediana, " +
			"humano x IA; espera; manuseio), service_levels (SLA, CSAT, situação, sem responsável), ai (sessões, contenção, " +
			"transbordo), messaging (mensagens por conversa, templates), closing (quem encerrou, reaberturas), quality " +
			"(durabilidade por pessoa), revenue (por moeda, origem e responsável), goals (todas as metas e quantas estão no " +
			"rumo), hourly e channels (também viram dataset para gráfico) e definitions (como a própria página define cada indicador; use para explicar um número com as palavras da página).",
		Parameters: withParams(attendanceParams(), map[string]tools.Parameter{
			"metrics":          {Type: "array", Description: "chaves das métricas; omita para todas", Items: &tools.ParameterItems{Type: "string"}},
			"compare_previous": {Type: "boolean", Description: "compara com o período anterior de mesma duração"},
			"details":          {Type: "array", Description: "blocos extras da página", Items: &tools.ParameterItems{Type: "string"}},
		}),
	}
}

func (t *attendanceMetricsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	q, err := t.deps.resolve(ctx, cc, args)
	if err != nil {
		return analyticsFailure("attendance_metrics", err)
	}
	keys := argStringList(args, "metrics")
	summary, err := t.deps.Sections.Summary(ctx, cc.WorkspaceID, q.filter)
	if err != nil {
		return analyticsFailure("attendance_metrics", err)
	}
	current, err := attendance.ReadMetrics(summary, keys)
	if err != nil {
		return analyticsFailure("attendance_metrics", err)
	}
	data := map[string]interface{}{"query": q.describe(), "metrics": current}
	if blocks := argStringList(args, "details"); len(blocks) > 0 {
		details, err := summaryDetails(cc, summary, blocks)
		if err != nil {
			return analyticsFailure("attendance_metrics", err)
		}
		data["details"] = details
	}
	if compare := argBoolPtr(args, "compare_previous"); compare == nil || !*compare {
		return copilot.Result{Status: copilot.StatusOK, Data: data}
	}
	previousQuery := q.forWindow(q.window.Previous())
	previousSummary, err := t.deps.Sections.Summary(ctx, cc.WorkspaceID, previousQuery.filter)
	if err != nil {
		return analyticsFailure("attendance_metrics", err)
	}
	previous, err := attendance.ReadMetrics(previousSummary, keys)
	if err != nil {
		return analyticsFailure("attendance_metrics", err)
	}
	data["previous_period"] = previousQuery.describe()
	data["metrics"] = attendance.CompareReadings(current, previous)
	return copilot.Result{Status: copilot.StatusOK, Data: data}
}

func summaryDetails(cc copilot.Context, summary *attendance.SummarySection, blocks []string) (map[string]interface{}, error) {
	out := make(map[string]interface{}, len(blocks))
	for _, raw := range blocks {
		block := attendance.SummaryBlock(raw)
		digest, err := attendance.DigestSummaryBlock(summary, block)
		if err != nil {
			return nil, err
		}
		switch block {
		case attendance.BlockHourly:
			d, err := hourlyDataset(summary.Hourly)
			if err != nil {
				return nil, err
			}
			out[raw] = keep(cc, d).Preview()
		case attendance.BlockChannels:
			d, err := channelsDataset(summary.ChannelMix)
			if err != nil {
				return nil, err
			}
			out[raw] = keep(cc, d).Preview()
		default:
			out[raw] = digest
		}
	}
	return out, nil
}

func hourlyDataset(points []attendance.HourlyPoint) (*copilot.Dataset, error) {
	d := copilot.NewDataset("conversations by hour", []copilot.Column{
		{Key: "hour", Label: "hour", Kind: copilot.ColumnText},
		{Key: "conversations", Label: "conversations", Kind: copilot.ColumnNumber},
	})
	for _, p := range points {
		if err := d.AddRow(fmt.Sprintf("%02dh", p.Hour), p.Count); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func channelsDataset(slices []attendance.ChannelSlice) (*copilot.Dataset, error) {
	d := copilot.NewDataset("conversations by channel", []copilot.Column{
		{Key: "channel", Label: "channel", Kind: copilot.ColumnText},
		{Key: "conversations", Label: "conversations", Kind: copilot.ColumnNumber},
		{Key: "share_pct", Label: "share_pct", Kind: copilot.ColumnPercent},
	})
	for _, s := range slices {
		if err := d.AddRow(s.Channel, s.Count, s.Pct); err != nil {
			return nil, err
		}
	}
	return d, nil
}
