package copilottools

import (
	"context"

	"vozko/domain/attendance"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	"vozko/domain/workspace"
)

type attendanceBacklogTool struct{ deps AttendanceDeps }

func NewAttendanceBacklogTool(deps AttendanceDeps) copilot.Tool {
	return &attendanceBacklogTool{deps: deps}
}

func (t *attendanceBacklogTool) Meta() copilot.Meta {
	return copilot.Meta{Resource: workspace.ResourceAttendance, Action: workspace.ActionRead}
}

func (t *attendanceBacklogTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "attendance_backlog",
		Description: "Raio X do que está parado AGORA (conversas em aberto e pendentes): total e distribuição por idade, origem, " +
			"responsável, tempo de casa do contato e retorno. Use para perguntas sobre acúmulo e gargalos.",
		Parameters: attendanceParams(),
	}
}

func (t *attendanceBacklogTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	q, err := t.deps.resolve(ctx, cc, args)
	if err != nil {
		return analyticsFailure("attendance_backlog", err)
	}
	section, err := t.deps.Sections.Backlog(ctx, cc.WorkspaceID, q.filter)
	if err != nil {
		return analyticsFailure("attendance_backlog", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"query":   q.describe(),
		"backlog": attendance.DigestBacklog(section.BacklogXray),
	}}
}
