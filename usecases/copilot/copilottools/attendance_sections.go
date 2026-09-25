package copilottools

import (
	"context"

	"vozko/domain/attendance"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

func attendanceReadMeta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceAttendance, Action: workspace.ActionRead}
}

type attendanceStagesTool struct{ deps AttendanceDeps }

func NewAttendanceStagesTool(deps AttendanceDeps) copilot.Tool {
	return &attendanceStagesTool{deps: deps}
}

func (t *attendanceStagesTool) Meta() copilot.Meta { return attendanceReadMeta() }

func (t *attendanceStagesTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "attendance_stages",
		Description: "Distribuição das conversas pelas etapas de cada funil no período: total por funil, conversas travadas " +
			"(paradas na mesma etapa além do limite) e, no dataset, cada etapa com total, finalizadas, em andamento, " +
			"pendentes, % do funil, dias médios e máximos na etapa e travadas. Use para gargalos de pipeline.",
		Parameters: attendanceParams(),
	}
}

func (t *attendanceStagesTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	q, err := t.deps.resolve(ctx, cc, args)
	if err != nil {
		return analyticsFailure("attendance_stages", err)
	}
	section, err := t.deps.Sections.Stages(ctx, cc.WorkspaceID, q.filter)
	if err != nil {
		return analyticsFailure("attendance_stages", err)
	}
	dataset, err := stagesDataset(section.Stages)
	if err != nil {
		return analyticsFailure("attendance_stages", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"query":   q.describe(),
		"stages":  attendance.DigestStages(section.Stages),
		"dataset": keep(cc, dataset).Handle(),
	}}
}

func stagesDataset(st attendance.OverviewStages) (*copilot.Dataset, error) {
	d := copilot.NewDataset("stages", []copilot.Column{
		{Key: "funnel", Label: "funnel", Kind: copilot.ColumnText},
		{Key: "stage", Label: "stage", Kind: copilot.ColumnText},
		{Key: "total", Label: "total", Kind: copilot.ColumnNumber},
		{Key: "finished", Label: "finished", Kind: copilot.ColumnNumber},
		{Key: "ongoing", Label: "ongoing", Kind: copilot.ColumnNumber},
		{Key: "pending", Label: "pending", Kind: copilot.ColumnNumber},
		{Key: "pct_of_funnel", Label: "pct_of_funnel", Kind: copilot.ColumnPercent},
		{Key: "avg_days_in_stage", Label: "avg_days_in_stage", Kind: copilot.ColumnNumber},
		{Key: "oldest_days_in_stage", Label: "oldest_days_in_stage", Kind: copilot.ColumnNumber},
		{Key: "stuck", Label: "stuck", Kind: copilot.ColumnNumber},
	})
	for _, f := range st.Funnels {
		for _, s := range f.Stages {
			if err := d.AddRow(f.FunnelName, s.StageName, s.Total, s.Finished, s.Ongoing, s.Pending, s.PctOfFunnel, s.AvgDaysInStage, s.OldestDaysInStage, s.Stuck); err != nil {
				return nil, err
			}
		}
	}
	return d, nil
}

type attendanceReworkTool struct{ deps AttendanceDeps }

func NewAttendanceReworkTool(deps AttendanceDeps) copilot.Tool {
	return &attendanceReworkTool{deps: deps}
}

func (t *attendanceReworkTool) Meta() copilot.Meta { return attendanceReadMeta() }

func (t *attendanceReworkTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "attendance_rework",
		Description: "Retrabalho no período: quem encerrou conversas que depois reabriram, taxa de reabertura por pessoa, " +
			"templates reenviados e o custo disso quando disponível (cost_micros em milionésimos da moeda). Traz a equipe, " +
			"os sem responsável e as pessoas com mais reaberturas; a lista completa fica no dataset.",
		Parameters: attendanceParams(),
	}
}

func (t *attendanceReworkTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	q, err := t.deps.resolve(ctx, cc, args)
	if err != nil {
		return analyticsFailure("attendance_rework", err)
	}
	section, err := t.deps.Sections.Rework(ctx, cc.WorkspaceID, q.filter)
	if err != nil {
		return analyticsFailure("attendance_rework", err)
	}
	dataset, err := reworkDataset(section.Rework)
	if err != nil {
		return analyticsFailure("attendance_rework", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"query":   q.describe(),
		"rework":  attendance.DigestRework(section.Rework),
		"dataset": keep(cc, dataset).Handle(),
	}}
}

func reworkDataset(r attendance.OverviewRework) (*copilot.Dataset, error) {
	d := copilot.NewDataset("rework", []copilot.Column{
		{Key: "name", Label: "name", Kind: copilot.ColumnText},
		{Key: "kind", Label: "kind", Kind: copilot.ColumnText},
		{Key: "finished", Label: "finished", Kind: copilot.ColumnNumber},
		{Key: "reopened", Label: "reopened", Kind: copilot.ColumnNumber},
		{Key: "reopen_rate", Label: "reopen_rate", Kind: copilot.ColumnPercent},
		{Key: "templates", Label: "templates", Kind: copilot.ColumnNumber},
	})
	for _, row := range r.Rows {
		if err := d.AddRow(row.DisplayName, row.ActorKind, row.Finished, row.Reopened, row.ReopenRate, row.Templates); err != nil {
			return nil, err
		}
	}
	return d, nil
}

type attendanceLiveTool struct{ deps AttendanceDeps }

func NewAttendanceLiveTool(deps AttendanceDeps) copilot.Tool {
	return &attendanceLiveTool{deps: deps}
}

func (t *attendanceLiveTool) Meta() copilot.Meta { return attendanceReadMeta() }

func (t *attendanceLiveTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "attendance_live",
		Description: "Operação AGORA: fila (entradas, atendidas, abandonos, espera média), ocupação da equipe e quantos estão " +
			"online, em atendimento e livres. As datas não se aplicam; departamento e canal sim.",
		Parameters: attendanceParams(),
	}
}

func (t *attendanceLiveTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	q, err := t.deps.resolve(ctx, cc, args)
	if err != nil {
		return analyticsFailure("attendance_live", err)
	}
	section, err := t.deps.Sections.Live(ctx, cc.WorkspaceID, q.filter)
	if err != nil {
		return analyticsFailure("attendance_live", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"department_id": orAll(q.filter.DepartmentID),
		"channel":       orAll(q.filter.Channel),
		"live":          attendance.DigestLive(*section),
	}}
}
