package node_executors

import (
	"fmt"
	"time"

	"vozko/domain/workflow"
)

type getCurrentTimeExecutor struct{}

func NewGetCurrentTimeExecutor() workflow.NodeExecutor {
	return &getCurrentTimeExecutor{}
}

func (e *getCurrentTimeExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeActionGetCurrentTime,
		Category:    workflow.NodeCategoryLogic,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Hora Atual",
		Description: "Obtém a data e hora atual e salva em variáveis.",
		Icon:        "Clock",
		Guidance: workflow.NodeGuidance{
			When: "Para obter a data/hora atual em um fuso e usar adiante.",
		},
		DefaultConfig: map[string]interface{}{
			"timezone": "America/Sao_Paulo",
			"variable": "current_time",
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "datetime", Description: "Data e hora completa (YYYY-MM-DD HH:mm:ss)"},
			{Key: "date", Description: "Apenas a data (YYYY-MM-DD)"},
			{Key: "time", Description: "Apenas a hora (HH:mm:ss)"},
			{Key: "hour", Description: "Hora (0-23)"},
			{Key: "minute", Description: "Minuto (0-59)"},
			{Key: "weekday", Description: "Dia da semana (Monday, Tuesday, etc.)"},
			{Key: "timestamp", Description: "Timestamp Unix (segundos)"},
			{Key: "iso", Description: "Data em formato ISO 8601"},
		},
		ConfigSchema: []workflow.ConfigField{
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
			{Key: "variable", Label: "Salvar como variável", Type: "text", Placeholder: "current_time"},
		},
	}
}

func (e *getCurrentTimeExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	tz, _ := ctx.Node.Config["timezone"].(string)
	variable, _ := ctx.Node.Config["variable"].(string)

	now := time.Now().UTC()
	if tz != "" {
		loc, err := time.LoadLocation(tz)
		if err == nil {
			now = now.In(loc)
		}
	}

	output := map[string]interface{}{
		"datetime":  now.Format("2006-01-02 15:04:05"),
		"date":      now.Format("2006-01-02"),
		"time":      now.Format("15:04:05"),
		"hour":      now.Hour(),
		"minute":    now.Minute(),
		"weekday":   now.Weekday().String(),
		"timestamp": now.Unix(),
		"iso":       now.Format(time.RFC3339),
	}

	if variable != "" {
		ctx.State.Set(variable, output)
		ctx.State.Set(variable+"_datetime", now.Format("2006-01-02 15:04:05"))
		ctx.State.Set(variable+"_hour", fmt.Sprintf("%d", now.Hour()))
		ctx.State.Set(variable+"_weekday", now.Weekday().String())
	}

	return &workflow.NodeResult{Output: output}, nil
}
