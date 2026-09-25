package copilottools

import (
	"context"

	"vozko/domain/attendance"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type attendanceTeamTool struct{ deps AttendanceDeps }

func NewAttendanceTeamTool(deps AttendanceDeps) copilot.Tool {
	return &attendanceTeamTool{deps: deps}
}

func (t *attendanceTeamTool) Meta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceAttendance, Action: workspace.ActionRead}
}

func (t *attendanceTeamTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "attendance_team",
		Description: "Ranking da equipe (pessoas e, opcionalmente, agentes de IA e automações) no período: os N primeiros ou " +
			"últimos pela métrica de ranking, a média da equipe e a classe de cada um. A equipe inteira fica em um dataset " +
			"(query_dataset para paginar ou ordenar por outra coluna, render_chart para gráficos); não tente listar todos.",
		Parameters: withParams(attendanceParams(), map[string]tools.Parameter{
			"rank_metric": {Type: "string", Description: "métrica de ranking", Enum: []string{attendance.RankByResolved, attendance.RankByVolume, attendance.RankByRevenue}},
			"include_ai":  {Type: "boolean", Description: "inclui agentes de IA e automações (padrão true)"},
			"order":       {Type: "string", Description: "top (melhores) ou bottom (piores)", Enum: []string{"top", "bottom"}},
			"limit":       {Type: "integer", Description: "quantos membros listar (1 a 10, padrão 5)"},
		}),
	}
}

func (t *attendanceTeamTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	q, err := t.deps.resolve(ctx, cc, args)
	if err != nil {
		return analyticsFailure("attendance_team", err)
	}
	q.filter.RankMetric = attendance.NormalizeRankMetric(argString(args, "rank_metric"))
	if includeAI := argBoolPtr(args, "include_ai"); includeAI != nil {
		q.filter.IncludeAI = *includeAI
	}
	section, err := t.deps.Sections.Team(ctx, cc.WorkspaceID, q.filter)
	if err != nil {
		return analyticsFailure("attendance_team", err)
	}
	dataset, err := teamDataset(section.TeamRanking)
	if err != nil {
		return analyticsFailure("attendance_team", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"query":   q.describe(),
		"team":    attendance.DigestTeam(section.TeamRanking, argInt(args, "limit"), argString(args, "order") == "bottom"),
		"dataset": keep(cc, dataset).Handle(),
	}}
}

func teamDataset(r attendance.TeamRanking) (*copilot.Dataset, error) {
	d := copilot.NewDataset("team ranking", []copilot.Column{
		{Key: "name", Label: "name", Kind: copilot.ColumnText},
		{Key: "kind", Label: "kind", Kind: copilot.ColumnText},
		{Key: "rank_value", Label: r.RankMetricKey, Kind: copilot.ColumnNumber},
		{Key: "resolved", Label: "resolved", Kind: copilot.ColumnNumber},
		{Key: "open", Label: "open", Kind: copilot.ColumnNumber},
		{Key: "pending", Label: "pending", Kind: copilot.ColumnNumber},
		{Key: "resolution_pct", Label: "resolution_pct", Kind: copilot.ColumnPercent},
		{Key: "avg_response_mins", Label: "avg_response_mins", Kind: copilot.ColumnMinutes},
		{Key: "rating", Label: "rating", Kind: copilot.ColumnNumber},
	})
	for _, m := range r.Members {
		if err := d.AddRow(m.DisplayName, m.ActorKind, m.RankMetricValue, m.Resolved, m.Open, m.Pending, m.ResolutionPct, m.AvgResponseMins, m.Rating); err != nil {
			return nil, err
		}
	}
	return d, nil
}
