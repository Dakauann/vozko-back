package copilottools

import (
	"context"
	"strings"

	"vozko/domain/attendance"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type attendanceMetricsTool struct{ deps AttendanceDeps }

func NewAttendanceMetricsTool(deps AttendanceDeps) copilot.Tool {
	return &attendanceMetricsTool{deps: deps}
}

func (t *attendanceMetricsTool) Meta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceAttendance, Action: workspace.ActionRead}
}

func (t *attendanceMetricsTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "attendance_metrics",
		Description: "Indicadores de atendimento de um período (os mesmos números da página Métricas · Atendimento). " +
			"Retorna só as métricas pedidas, com meta e projeção quando existir meta. Com compare_previous=true traz também o " +
			"período anterior de mesma duração, a variação absoluta e percentual e se melhorou ou piorou (já considerando se " +
			"maior ou menor é melhor). value=null significa sem dado, não zero. Métricas: " + strings.Join(attendance.MetricKeys(), ", ") +
			". revenue_cents está em centavos.",
		Parameters: withParams(attendanceParams(), map[string]tools.Parameter{
			"metrics":          {Type: "array", Description: "chaves das métricas; omita para todas", Items: &tools.ParameterItems{Type: "string"}},
			"compare_previous": {Type: "boolean", Description: "compara com o período anterior de mesma duração"},
		}),
	}
}

func (t *attendanceMetricsTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	q, err := t.deps.resolve(ctx, cc, args)
	if err != nil {
		return analyticsFailure("attendance_metrics", err)
	}
	keys := argStringList(args, "metrics")
	current, err := t.read(ctx, cc, q, keys)
	if err != nil {
		return analyticsFailure("attendance_metrics", err)
	}
	data := map[string]interface{}{"query": q.describe()}
	if compare := argBoolPtr(args, "compare_previous"); compare == nil || !*compare {
		data["metrics"] = current
		return copilot.Result{Status: copilot.StatusOK, Data: data}
	}
	previousQuery := q.forWindow(q.window.Previous())
	previous, err := t.read(ctx, cc, previousQuery, keys)
	if err != nil {
		return analyticsFailure("attendance_metrics", err)
	}
	data["previous_period"] = previousQuery.describe()
	data["metrics"] = attendance.CompareReadings(current, previous)
	return copilot.Result{Status: copilot.StatusOK, Data: data}
}

func (t *attendanceMetricsTool) read(ctx context.Context, cc copilot.Context, q attendanceQuery, keys []string) ([]attendance.MetricReading, error) {
	summary, err := t.deps.Sections.Summary(ctx, cc.WorkspaceID, q.filter)
	if err != nil {
		return nil, err
	}
	return attendance.ReadMetrics(summary, keys)
}
