package node_executors

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"vozko/domain/workflow"
)

type formatDateExecutor struct{}

func NewFormatDateExecutor() workflow.NodeExecutor {
	return &formatDateExecutor{}
}

func (e *formatDateExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeActionFormatDate,
		Category:    workflow.NodeCategoryLogic,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Formatar Data/Hora",
		Description: "Formata, converte ou calcula datas e horários.",
		Icon:        "CalendarBlank",
		Guidance: workflow.NodeGuidance{
			When: "Para formatar/converter/calcular datas e horas.",
		},
		DefaultConfig: map[string]interface{}{
			"input":     "now",
			"format":    "2006-01-02 15:04:05",
			"timezone":  "America/Sao_Paulo",
			"operation": "none",
			"amount":    float64(0),
			"unit":      "hours",
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "formatted", Description: "Data formatada como string"},
			{Key: "timestamp", Description: "Timestamp Unix (segundos)"},
			{Key: "iso", Description: "Data em formato ISO 8601"},
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "input", Label: "Entrada", Type: "text", Placeholder: "now ou {{var.data}}", Required: true},
			{Key: "format", Label: "Formato de saída", Type: "select", Options: []workflow.ConfigFieldOption{
				{Value: "2006-01-02 15:04:05", Label: "YYYY-MM-DD HH:mm:ss"},
				{Value: "2006-01-02", Label: "YYYY-MM-DD"},
				{Value: "02/01/2006", Label: "DD/MM/YYYY"},
				{Value: "02/01/2006 15:04", Label: "DD/MM/YYYY HH:mm"},
				{Value: "15:04:05", Label: "HH:mm:ss"},
				{Value: "15:04", Label: "HH:mm"},
				{Value: "Monday", Label: "Dia da semana"},
				{Value: "January", Label: "Mês por extenso"},
				{Value: "unix", Label: "Timestamp Unix"},
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
			{Key: "operation", Label: "Operação", Type: "select", Options: []workflow.ConfigFieldOption{
				{Value: "none", Label: "Nenhuma"},
				{Value: "add", Label: "Adicionar tempo"},
				{Value: "subtract", Label: "Subtrair tempo"},
			}},
			{Key: "amount", Label: "Quantidade", Type: "number", Placeholder: "0"},
			{Key: "unit", Label: "Unidade", Type: "select", Options: []workflow.ConfigFieldOption{
				{Value: "minutes", Label: "Minutos"},
				{Value: "hours", Label: "Horas"},
				{Value: "days", Label: "Dias"},
				{Value: "weeks", Label: "Semanas"},
				{Value: "months", Label: "Meses"},
			}},
		},
	}
}

func parseInputDate(input string) (time.Time, error) {
	input = strings.TrimSpace(input)
	if input == "" || strings.EqualFold(input, "now") {
		return time.Now().UTC(), nil
	}

	if ts, err := strconv.ParseInt(input, 10, 64); err == nil {
		if ts > 1e12 {
			return time.UnixMilli(ts).UTC(), nil
		}
		return time.Unix(ts, 0).UTC(), nil
	}

	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
		"02/01/2006 15:04:05",
		"02/01/2006 15:04",
		"02/01/2006",
		"01/02/2006",
		"15:04:05",
		"15:04",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, input); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse date: %s", input)
}

func (e *formatDateExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	input, _ := ctx.Node.Config["input"].(string)
	format, _ := ctx.Node.Config["format"].(string)
	tz, _ := ctx.Node.Config["timezone"].(string)
	operation, _ := ctx.Node.Config["operation"].(string)
	amount, _ := ctx.Node.Config["amount"].(float64)
	unit, _ := ctx.Node.Config["unit"].(string)

	if input == "" {
		input = "now"
	}
	if format == "" {
		format = "2006-01-02 15:04:05"
	}

	input = workflow.Interpolate(input, ctx.State, nil)

	t, err := parseInputDate(input)
	if err != nil {
		return nil, err
	}

	if operation == "add" || operation == "subtract" {
		dur := toDuration(amount, unit)
		if operation == "subtract" {
			dur = -dur
		}
		if unit == "months" {
			months := int(amount)
			if operation == "subtract" {
				months = -months
			}
			t = t.AddDate(0, months, 0)
		} else {
			t = t.Add(dur)
		}
	}

	if tz != "" {
		loc, err := time.LoadLocation(tz)
		if err == nil {
			t = t.In(loc)
		}
	}

	var formatted string
	if format == "unix" {
		formatted = strconv.FormatInt(t.Unix(), 10)
	} else {
		formatted = t.Format(format)
	}

	return &workflow.NodeResult{
		Output: map[string]interface{}{
			"formatted": formatted,
			"timestamp": t.Unix(),
			"iso":       t.Format(time.RFC3339),
		},
	}, nil
}

func toDuration(amount float64, unit string) time.Duration {
	switch unit {
	case "minutes":
		return time.Duration(amount) * time.Minute
	case "hours":
		return time.Duration(amount) * time.Hour
	case "days":
		return time.Duration(amount) * 24 * time.Hour
	case "weeks":
		return time.Duration(amount) * 7 * 24 * time.Hour
	default:
		return time.Duration(amount) * time.Hour
	}
}
