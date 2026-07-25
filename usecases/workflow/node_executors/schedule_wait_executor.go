package node_executors

import (
	"fmt"
	"strings"
	"time"

	"vozko/domain/workflow"
)

type scheduleWaitExecutor struct{}

func NewScheduleWaitExecutor() workflow.NodeExecutor {
	return &scheduleWaitExecutor{}
}

func (e *scheduleWaitExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:     workflow.NodeTypeWaitSchedule,
		Category: workflow.NodeCategoryWait,
		Scopes: []workflow.NodeScope{
			workflow.NodeScopeShared,
		},
		Label:       "Esperar Horário",
		Description: "Pausa o fluxo até uma data/hora específica. Ideal para enviar mensagens em horários programados.",
		Icon:        "CalendarCheck",
		Guidance: workflow.NodeGuidance{
			When: "Para pausar até uma data/horário específico (envios agendados).",
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "completed", Label: "Horário atingido"},
			{ID: "message_received", Label: "Mensagem recebida", Optional: true},
		},
		DefaultConfig: map[string]interface{}{
			"mode":     "time",
			"time":     "09:00",
			"date":     "",
			"timezone": "America/Sao_Paulo",
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "scheduled_at", Description: "Data/hora agendada (ISO 8601)"},
			{Key: "wait_minutes", Description: "Minutos de espera calculados"},
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "mode", Label: "Modo", Type: "select", Required: true, Options: []workflow.ConfigFieldOption{
				{Value: "time", Label: "Horário específico (ex: 16:00)"},
				{Value: "datetime", Label: "Data e hora exata"},
				{Value: "next_weekday", Label: "Próximo dia da semana"},
			}},
			{Key: "time", Label: "Horário (HH:mm)", Type: "text", Placeholder: "16:00"},
			{Key: "date", Label: "Data (YYYY-MM-DD)", Type: "text", Placeholder: "2025-01-15"},
			{Key: "weekday", Label: "Dia da semana", Type: "select", Options: []workflow.ConfigFieldOption{
				{Value: "Monday", Label: "Segunda-feira"},
				{Value: "Tuesday", Label: "Terça-feira"},
				{Value: "Wednesday", Label: "Quarta-feira"},
				{Value: "Thursday", Label: "Quinta-feira"},
				{Value: "Friday", Label: "Sexta-feira"},
				{Value: "Saturday", Label: "Sábado"},
				{Value: "Sunday", Label: "Domingo"},
			}},
			{Key: "timezone", Label: "Fuso horário", Type: "select", Options: []workflow.ConfigFieldOption{
				{Value: "America/Sao_Paulo", Label: "São Paulo (BRT)"},
				{Value: "America/New_York", Label: "Nova York (EST)"},
				{Value: "America/Chicago", Label: "Chicago (CST)"},
				{Value: "America/Los_Angeles", Label: "Los Angeles (PST)"},
				{Value: "Europe/London", Label: "Londres (GMT)"},
				{Value: "Europe/Paris", Label: "Paris (CET)"},
				{Value: "Asia/Tokyo", Label: "Tóquio (JST)"},
				{Value: "UTC", Label: "UTC"},
			}},
		},
	}
}

func (e *scheduleWaitExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	mode, _ := ctx.Node.Config["mode"].(string)
	timeStr, _ := ctx.Node.Config["time"].(string)
	dateStr, _ := ctx.Node.Config["date"].(string)
	weekday, _ := ctx.Node.Config["weekday"].(string)
	tz, _ := ctx.Node.Config["timezone"].(string)

	if mode == "" {
		mode = "time"
	}

	timeStr = workflow.Interpolate(timeStr, ctx.State, nil)
	dateStr = workflow.Interpolate(dateStr, ctx.State, nil)

	loc := time.UTC
	if tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}

	now := time.Now().In(loc)
	var targetTime time.Time

	switch mode {
	case "time":

		targetTime = parseTimeOfDay(now, timeStr, loc)
	case "datetime":

		targetTime = parseDatetime(dateStr, timeStr, loc)
	case "next_weekday":

		targetTime = parseNextWeekday(now, weekday, timeStr, loc)
	default:
		return nil, fmt.Errorf("unknown schedule mode: %s", mode)
	}

	if targetTime.IsZero() {
		return nil, fmt.Errorf("could not parse target time (mode=%s, time=%s, date=%s)", mode, timeStr, dateStr)
	}

	if targetTime.Before(now) {
		return &workflow.NodeResult{
			Output: map[string]interface{}{
				"scheduled_at": targetTime.Format(time.RFC3339),
				"wait_minutes": 0,
			},
		}, nil
	}

	waitMinutes := int(targetTime.Sub(now).Minutes())
	wakeAt := targetTime.UTC().UnixMilli()

	return &workflow.NodeResult{
		Wait: &workflow.WaitInstruction{
			WakeAt: wakeAt,
			Reason: workflow.WaitReasonDuration,
		},
		Output: map[string]interface{}{
			"scheduled_at": targetTime.Format(time.RFC3339),
			"wait_minutes": waitMinutes,
		},
	}, nil
}

func parseTimeOfDay(now time.Time, timeStr string, loc *time.Location) time.Time {
	parts := strings.SplitN(timeStr, ":", 2)
	if len(parts) != 2 {
		return time.Time{}
	}

	hour, minute := 0, 0
	fmt.Sscanf(parts[0], "%d", &hour)
	fmt.Sscanf(parts[1], "%d", &minute)

	target := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, loc)
	if target.Before(now) {
		target = target.AddDate(0, 0, 1)
	}
	return target
}

func parseDatetime(dateStr, timeStr string, loc *time.Location) time.Time {
	combined := strings.TrimSpace(dateStr)
	if timeStr != "" {
		combined += " " + strings.TrimSpace(timeStr)
	}

	formats := []string{
		"2006-01-02 15:04",
		"2006-01-02 15:04:05",
		"2006-01-02",
		"02/01/2006 15:04",
		"02/01/2006",
	}
	for _, f := range formats {
		if t, err := time.ParseInLocation(f, combined, loc); err == nil {
			return t
		}
	}
	return time.Time{}
}

func parseNextWeekday(now time.Time, weekday, timeStr string, loc *time.Location) time.Time {
	targetDay := parseWeekday(weekday)
	if targetDay < 0 {
		return time.Time{}
	}

	parts := strings.SplitN(timeStr, ":", 2)
	hour, minute := 9, 0
	if len(parts) == 2 {
		fmt.Sscanf(parts[0], "%d", &hour)
		fmt.Sscanf(parts[1], "%d", &minute)
	}

	daysUntil := (int(targetDay) - int(now.Weekday()) + 7) % 7
	if daysUntil == 0 {

		candidate := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, loc)
		if candidate.Before(now) {
			daysUntil = 7
		}
	}

	target := time.Date(now.Year(), now.Month(), now.Day()+daysUntil, hour, minute, 0, 0, loc)
	return target
}

func parseWeekday(s string) time.Weekday {
	switch strings.ToLower(s) {
	case "sunday", "domingo":
		return time.Sunday
	case "monday", "segunda", "segunda-feira":
		return time.Monday
	case "tuesday", "terça", "terca", "terça-feira":
		return time.Tuesday
	case "wednesday", "quarta", "quarta-feira":
		return time.Wednesday
	case "thursday", "quinta", "quinta-feira":
		return time.Thursday
	case "friday", "sexta", "sexta-feira":
		return time.Friday
	case "saturday", "sábado", "sabado":
		return time.Saturday
	default:
		return -1
	}
}
