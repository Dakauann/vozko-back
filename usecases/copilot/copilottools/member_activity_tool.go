package copilottools

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"

	"vozko/domain/attendance"
	"vozko/domain/copilot"
	"vozko/domain/tools"
	attendance_usecase "vozko/usecases/attendance"
)

const activityClock = "15:04"

type MemberActivityReader interface {
	Execute(ctx context.Context, q attendance_usecase.MemberActivityQuery) (*attendance_usecase.MemberActivityReport, error)
}

type memberActivityTool struct{ reader MemberActivityReader }

func NewMemberActivityTool(reader MemberActivityReader) copilot.Tool {
	return &memberActivityTool{reader: reader}
}

func (t *memberActivityTool) Meta() copilot.Meta { return attendanceReadMeta() }

func (t *memberActivityTool) Definition() tools.Definition {
	return tools.Definition{
		Name: "member_activity",
		Description: "Atividade de uma pessoa da equipe: quando esteve conectada ao painel (sessões por dia no fuso local, horas conectada e em ligação, " +
			"horário habitual de início), sinais por dia (late_start: entrou bem depois do habitual; no_presence: não conectou num dia da semana em que costuma " +
			"trabalhar; possible_forgotten_tab: uma sessão de 14h ou mais, provavelmente aba esquecida aberta, não é jornada), conversas recebidas por gatilho, " +
			"recebidas pela roleta estando offline (deve ser zero) e os números de atendimento do período. Conectado significa painel aberto, não trabalho " +
			"comprovado. Os dias e o mapa de calor (minutos por dia da semana e hora) ficam em datasets para render_chart. Máximo de 92 dias.",
		Parameters: map[string]tools.Parameter{
			"member_id": {Type: "string", Description: "user_id do membro (de list_workspace_members ou attendance_team); omita para usar o da tela ou, sem ele, o próprio usuário"},
			"date_from": {Type: "string", Description: "primeiro dia YYYY-MM-DD; omita para usar o da tela ou os últimos 7 dias"},
			"date_to":   {Type: "string", Description: "último dia YYYY-MM-DD (inclusivo); omita para usar o da tela ou hoje"},
		},
	}
}

func (t *memberActivityTool) Execute(ctx context.Context, cc copilot.Context, args map[string]interface{}) copilot.Result {
	member := onScreen(argString(args, "member_id"), cc.View.MemberID)
	if member == "" {
		member = cc.UserID
	}
	if _, err := uuid.Parse(member); err != nil {
		return copilot.Result{Status: copilot.StatusError, Message: errUnknownMember.Error()}
	}
	report, err := t.reader.Execute(ctx, attendance_usecase.MemberActivityQuery{
		WorkspaceID: cc.WorkspaceID,
		MemberID:    member,
		FromDay:     onScreen(argString(args, "date_from"), cc.View.DateFrom),
		ToDay:       onScreen(argString(args, "date_to"), cc.View.DateTo),
		Timezone:    cc.Timezone,
		Self:        member == cc.UserID,
		Departments: cc.Departments,
	})
	switch {
	case errors.Is(err, attendance_usecase.ErrMemberOutOfScope):
		return copilot.Result{Status: copilot.StatusDenied, Message: "este membro não está nos departamentos do usuário"}
	case errors.Is(err, attendance_usecase.ErrActivityPeriod):
		return copilot.Result{Status: copilot.StatusError, Message: "período inválido: use datas YYYY-MM-DD, início antes do fim, no máximo 92 dias"}
	case err != nil:
		return analyticsFailure("member_activity", err)
	}
	loc, err := time.LoadLocation(report.Timezone)
	if err != nil {
		return analyticsFailure("member_activity", err)
	}
	days, err := activityDaysDataset(report.Days, loc)
	if err != nil {
		return analyticsFailure("member_activity", err)
	}
	heat, err := activityHeatmapDataset(report.Heatmap)
	if err != nil {
		return analyticsFailure("member_activity", err)
	}
	return copilot.Result{Status: copilot.StatusOK, Data: map[string]interface{}{
		"member_id":              member,
		"timezone":               report.Timezone,
		"usual_start":            report.UsualStart,
		"connected_hours":        hours(report.ConnectedMS),
		"on_call_hours":          hours(report.OnCallMS),
		"flagged_days":           flaggedDays(report.Days),
		"received":               report.Received,
		"received_while_offline": report.ReceivedWhileOffline,
		"work":                   report.Work,
		"days":                   keep(cc, days).Handle(),
		"heatmap":                keep(cc, heat).Handle(),
	}}
}

func hours(ms int64) float64 {
	return math.Round(float64(ms)/float64(time.Hour.Milliseconds())*10) / 10
}

func flaggedDays(days []attendance.ActivityDay) map[string][]string {
	out := map[string][]string{}
	for _, d := range days {
		if len(d.Flags) > 0 {
			out[d.Date] = d.Flags
		}
	}
	return out
}

func activityDaysDataset(days []attendance.ActivityDay, loc *time.Location) (*copilot.Dataset, error) {
	d := copilot.NewDataset("member activity by day", []copilot.Column{
		{Key: "date", Label: "date", Kind: copilot.ColumnDate},
		{Key: "first_connect", Label: "first_connect", Kind: copilot.ColumnText},
		{Key: "last_disconnect", Label: "last_disconnect", Kind: copilot.ColumnText},
		{Key: "connected_hours", Label: "connected_hours", Kind: copilot.ColumnNumber},
		{Key: "on_call_hours", Label: "on_call_hours", Kind: copilot.ColumnNumber},
		{Key: "sessions", Label: "sessions", Kind: copilot.ColumnNumber},
		{Key: "flags", Label: "flags", Kind: copilot.ColumnText},
	})
	for _, day := range days {
		first, last := sessionBounds(day.Sessions, loc)
		if err := d.AddRow(day.Date, first, last, hours(day.ConnectedMS), hours(day.OnCallMS), len(day.Sessions), strings.Join(day.Flags, ",")); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func sessionBounds(sessions []attendance.ActivitySession, loc *time.Location) (string, string) {
	if len(sessions) == 0 {
		return "", ""
	}
	last := sessions[len(sessions)-1]
	end := last.End.In(loc).Format(activityClock)
	if last.Open {
		end = "conectado agora"
	}
	return sessions[0].Start.In(loc).Format(activityClock), end
}

func activityHeatmapDataset(grid [7][24]int) (*copilot.Dataset, error) {
	d := copilot.NewDataset("member connected minutes by weekday and hour", []copilot.Column{
		{Key: "weekday", Label: "weekday", Kind: copilot.ColumnText},
		{Key: "hour", Label: "hour", Kind: copilot.ColumnNumber},
		{Key: "minutes", Label: "minutes", Kind: copilot.ColumnNumber},
	})
	for weekday, row := range grid {
		for hour, minutes := range row {
			if minutes == 0 {
				continue
			}
			if err := d.AddRow(time.Weekday(weekday).String(), hour, minutes); err != nil {
				return nil, err
			}
		}
	}
	return d, nil
}
