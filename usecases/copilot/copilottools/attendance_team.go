package copilottools

import (
	"context"

	"vozko/domain/attendance"
	"vozko/domain/copilot"
	"vozko/domain/tools"
)

type attendanceTeamTool struct{ deps AttendanceDeps }

func NewAttendanceTeamTool(deps AttendanceDeps) copilot.Tool {
	return &attendanceTeamTool{deps: deps}
}

func (t *attendanceTeamTool) Meta() copilot.Meta { return attendanceReadMeta() }

func (t *attendanceTeamTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "attendance_team",
		Description: "Ranking da equipe (pessoas e, opcionalmente, agentes de IA e automações) no período: os N primeiros ou " +
			"últimos pela métrica de ranking, a média da equipe e a classe de cada um. A equipe inteira fica em um dataset " +
			"(query_dataset para paginar ou ordenar por outra coluna, render_chart para gráficos); não tente listar todos.",
		Parameters: withParams(attendanceParams(), map[string]tools.Parameter{
			"rank_metric": {Type: "string", Description: "métrica de ranking", Enum: []string{attendance.RankByResolved, attendance.RankByVolume, attendance.RankByRevenue}},
			"include_ai":  {Type: "boolean", Description: "inclui agentes de IA e automações; omita para seguir o \"Mostrar IA\" da tela"},
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
	section, err := t.deps.Sections.Team(ctx, cc.WorkspaceID, q.filter)
	if err != nil {
		return analyticsFailure("attendance_team", err)
	}
	dataset, err := teamDataset(section.TeamRanking)
	if err != nil {
		return analyticsFailure("attendance_team", err)
	}
	departments, err := departmentsDataset(section.ByDepartment)
	if err != nil {
		return analyticsFailure("attendance_team", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"query":   q.describe(),
		"team":    attendance.DigestTeam(section.TeamRanking, argInt(args, "limit"), argString(args, "order") == "bottom"),
		"dataset": keep(cc, dataset).Handle(),
		"departments": map[string]interface{}{
			"top":     attendance.DigestDepartments(section.ByDepartment),
			"dataset": keep(cc, departments).Handle(),
		},
	}}
}

func departmentsDataset(rows []attendance.DepartmentRow) (*copilot.Dataset, error) {
	d := copilot.NewDataset("departments", []copilot.Column{
		{Key: "department", Label: "department", Kind: copilot.ColumnText},
		{Key: "finished", Label: "finished", Kind: copilot.ColumnNumber},
		{Key: "finished_human", Label: "finished_human", Kind: copilot.ColumnNumber},
		{Key: "finished_ai", Label: "finished_ai", Kind: copilot.ColumnNumber},
		{Key: "ongoing", Label: "ongoing", Kind: copilot.ColumnNumber},
		{Key: "pending", Label: "pending", Kind: copilot.ColumnNumber},
		{Key: "avg_wait_mins", Label: "avg_wait_mins", Kind: copilot.ColumnMinutes},
		{Key: "avg_handle_mins", Label: "avg_handle_mins", Kind: copilot.ColumnMinutes},
	})
	for _, r := range rows {
		if err := d.AddRow(r.DepartmentName, r.Finished, r.FinishedHuman, r.FinishedAI, r.Ongoing, r.Pending, r.AvgWaitMins, r.AvgHandleMins); err != nil {
			return nil, err
		}
	}
	return d, nil
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
		{Key: "revenue_cents", Label: "revenue_cents", Kind: copilot.ColumnMoney},
		{Key: "won_count", Label: "won_count", Kind: copilot.ColumnNumber},
		{Key: "avg_ticket_cents", Label: "avg_ticket_cents", Kind: copilot.ColumnMoney},
		{Key: "per_open_day", Label: "per_open_day", Kind: copilot.ColumnNumber},
		{Key: "per_online_hour", Label: "per_online_hour", Kind: copilot.ColumnNumber},
		{Key: "total_messages", Label: "total_messages", Kind: copilot.ColumnNumber},
		{Key: "avg_messages", Label: "avg_messages", Kind: copilot.ColumnNumber},
		{Key: "presence", Label: "presence", Kind: copilot.ColumnText},
	})
	for _, m := range r.Members {
		var revenue any
		if m.RevenueCents != nil {
			revenue = *m.RevenueCents
		}
		if err := d.AddRow(m.DisplayName, m.ActorKind, m.RankMetricValue, m.Resolved, m.Open, m.Pending, m.ResolutionPct, m.AvgResponseMins, m.Rating,
			revenue, m.WonCount, m.AvgTicketCents, m.PerOpenDay, m.PerOnlineHour, m.TotalMessages, m.AvgMessages, m.Presence); err != nil {
			return nil, err
		}
	}
	return d, nil
}
