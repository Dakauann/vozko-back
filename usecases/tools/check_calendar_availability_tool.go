package tools_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/calendar"
	"vozko/domain/tools"
)

const CheckCalendarAvailabilityToolName = "check_calendar_availability"

type checkCalendarAvailabilityTool struct {
	repo   calendar.Repository
	google calendar.GoogleCalendarAPI
	access calendar.AccessProvider
}

func NewCheckCalendarAvailabilityToolUseCase(repo calendar.Repository, google calendar.GoogleCalendarAPI, access calendar.AccessProvider) tools.Handler {
	if repo == nil || google == nil || access == nil {
		return nil
	}
	return &checkCalendarAvailabilityTool{repo: repo, google: google, access: access}
}

func (t *checkCalendarAvailabilityTool) Definition() tools.Definition {
	return tools.Definition{
		Name:        CheckCalendarAvailabilityToolName,
		DisplayName: "Verificar Disponibilidade no Calendário",
		Description: `Consulta o Google Calendar do workspace e retorna os horários livres no período informado.

QUANDO USAR:
- Cliente quer agendar uma reunião e precisa saber os horários disponíveis
- Precisa verificar a disponibilidade antes de propor um horário
- Cliente pergunta "quando tem horário livre?" ou "qual o próximo horário disponível?"

Retorna os slots livres em formato texto (para mostrar ao cliente) e JSON (para uso programático).`,
		DisplayDescription: "Consulta o Google Calendar e retorna horários livres usando a agenda configurada do agente.",
		Parameters: map[string]tools.Parameter{
			"date_from": {
				Type:               "string",
				Description:        "Data/hora de início da busca no formato RFC3339 (ex: 2026-04-10T08:00:00-03:00) ou YYYY-MM-DD para o dia inteiro.",
				DisplayName:        "Data/hora início",
				DisplayDescription: "Início do período para verificar disponibilidade",
			},
			"date_to": {
				Type:               "string",
				Description:        "Data/hora de fim da busca no formato RFC3339 ou YYYY-MM-DD para ir até o fim do expediente desse dia. Opcional, se vazio, consulta até o fim do horário comercial do dia de início.",
				DisplayName:        "Data/hora fim",
				DisplayDescription: "Fim do período para verificar disponibilidade",
			},
			"timezone": {
				Type:               "string",
				Description:        "Timezone IANA (ex: America/Sao_Paulo). Padrão: America/Sao_Paulo.",
				DisplayName:        "Timezone",
				DisplayDescription: "Fuso horário para a consulta",
			},
			"slot_duration": {
				Type:               "number",
				Description:        "Duração de cada slot em minutos. Padrão: 30.",
				DisplayName:        "Duração do slot (minutos)",
				DisplayDescription: "Duração de cada slot livre em minutos",
			},
			"working_start": {
				Type:               "string",
				Description:        "Horário de início do expediente no formato HH:MM (ex: 08:00). Padrão: 08:00.",
				DisplayName:        "Início do expediente",
				DisplayDescription: "Hora de início do horário de trabalho",
			},
			"working_end": {
				Type:               "string",
				Description:        "Horário de fim do expediente no formato HH:MM (ex: 18:00). Padrão: 18:00.",
				DisplayName:        "Fim do expediente",
				DisplayDescription: "Hora de fim do horário de trabalho",
			},
			"work_days": {
				Type:               "string",
				Description:        "Dias de trabalho: 'weekdays' (seg-sex, padrão), 'weekdays_saturday' (seg-sab), ou 'all_week' (todos).",
				DisplayName:        "Dias de trabalho",
				DisplayDescription: "Quais dias da semana são dias úteis",
			},
		},
		Required: []string{"date_from"},
		ConfigSchema: map[string]tools.ConfigParameter{
			"timezone": {
				Type:               "string",
				Description:        "Timezone IANA padrão para a agenda (ex: America/Sao_Paulo).",
				DisplayName:        "Fuso horário padrão",
				DisplayDescription: "Fuso horário usado por padrão nas consultas de disponibilidade.",
				Default:            "America/Sao_Paulo",
				Required:           true,
			},
			"slot_duration": {
				Type:               "number",
				Description:        "Duração padrão de cada horário livre em minutos.",
				DisplayName:        "Duração de cada horário",
				DisplayDescription: "Tamanho de cada bloco livre retornado ao cliente, em minutos.",
				Default:            30,
				Required:           true,
			},
			"working_start": {
				Type:               "string",
				Description:        "Início do expediente no formato HH:MM.",
				DisplayName:        "Horário inicial",
				DisplayDescription: "Primeiro horário do dia em que o agente pode oferecer agendamentos.",
				Default:            "08:00",
				Required:           true,
			},
			"working_end": {
				Type:               "string",
				Description:        "Fim do expediente no formato HH:MM.",
				DisplayName:        "Horário final",
				DisplayDescription: "Último horário do dia em que o agente pode oferecer agendamentos.",
				Default:            "18:00",
				Required:           true,
			},
			"work_days": {
				Type:               "string",
				Description:        "Dias de trabalho: weekdays, weekdays_saturday ou all_week.",
				DisplayName:        "Dias disponíveis",
				DisplayDescription: "Define se o agente agenda de segunda a sexta, segunda a sábado ou todos os dias.",
				Default:            "weekdays",
				Options: []tools.ConfigParameterOption{
					{Value: "weekdays", Label: "Segunda a sexta"},
					{Value: "weekdays_saturday", Label: "Segunda a sábado"},
					{Value: "all_week", Label: "Todos os dias"},
				},
				Required: true,
			},
		},
		RequiredConfig: []string{"timezone", "slot_duration", "working_start", "working_end", "work_days"},
		RequiresConfig: true,
		Visibility:     []tools.ToolVisibility{tools.VisibilityMessaging},
		Category:       tools.CategoryAgentUtility,
	}
}

func (t *checkCalendarAvailabilityTool) Execute(ctx context.Context, params map[string]interface{}) (tools.ExecutionResult, error) {
	return t.ExecuteWithConfig(ctx, nil, params)
}

func (t *checkCalendarAvailabilityTool) ExecuteWithConfig(ctx context.Context, config map[string]interface{}, params map[string]interface{}) (tools.ExecutionResult, error) {
	workspaceID, _ := config["__workspace_id"].(string)
	if workspaceID == "" {
		log.Printf("[CheckCalendarAvailability] missing __workspace_id in config")
		return tools.ExecutionResult{
			Result:  "Não foi possível identificar o workspace. Esta ferramenta requer contexto de workspace.",
			IsError: true,
		}, nil
	}

	googleAccess, err := t.access.CalendarAccess(ctx, workspaceID)
	if err != nil {
		return calendarAccessRefusal(workspaceID, err), nil
	}

	tzRaw := toolStringParamOrConfig(params, config, "timezone", "America/Sao_Paulo")
	loc, err := time.LoadLocation(tzRaw)
	if err != nil {
		return tools.ExecutionResult{
			Result:  fmt.Sprintf("Timezone inválida: %s", tzRaw),
			IsError: true,
		}, nil
	}

	fromRaw, _ := params["date_from"].(string)
	fromRaw = strings.TrimSpace(fromRaw)
	if fromRaw == "" {
		return tools.ExecutionResult{
			Result:  "O parâmetro 'date_from' é obrigatório.",
			IsError: true,
		}, nil
	}
	hours := calendar.WorkingHours{
		Start:    calendar.ParseClock(toolStringParamOrConfig(params, config, "working_start", ""), calendar.DefaultWorkStart),
		End:      calendar.ParseClock(toolStringParamOrConfig(params, config, "working_end", ""), calendar.DefaultWorkEnd),
		Days:     calendar.ParseWorkDays(toolStringParamOrConfig(params, config, "work_days", "weekdays")),
		Location: loc,
	}
	toRaw, _ := params["date_to"].(string)
	dateFrom, dateTo, err := calendar.AvailabilityRange(fromRaw, toRaw, hours.Start, hours.End, loc)
	if err != nil {
		return tools.ExecutionResult{
			Result:  err.Error(),
			IsError: true,
		}, nil
	}

	slotMinutes := toolIntParamOrConfig(params, config, "slot_duration", 30)

	accessToken := googleAccess.Token

	events, err := t.google.ListGoogleEvents(accessToken, dateFrom, dateTo, "", 250)
	if err != nil {
		log.Printf("[CheckCalendarAvailability] failed to list events for workspace %s: %v", workspaceID, err)
		return tools.ExecutionResult{
			Result:  fmt.Sprintf("Falha ao consultar eventos: %v", err),
			IsError: true,
		}, nil
	}

	events, err = calendar.WithLocalEvents(t.repo, workspaceID, dateFrom, dateTo, events)
	if err != nil {
		return tools.ExecutionResult{
			Result:  err.Error(),
			IsError: true,
		}, nil
	}

	slots := calendar.FreeSlots(dateFrom, dateTo, events, time.Duration(slotMinutes)*time.Minute, hours)

	var textParts []string
	var jsonSlots []map[string]string
	for _, s := range slots {
		textParts = append(textParts, fmt.Sprintf("%s a %s",
			s.Start.In(loc).Format("02/01 15:04"),
			s.End.In(loc).Format("15:04"),
		))
		jsonSlots = append(jsonSlots, map[string]string{
			"start": s.Start.Format(time.RFC3339),
			"end":   s.End.Format(time.RFC3339),
		})
	}

	availableText := strings.Join(textParts, "\n")
	if len(textParts) == 0 {
		availableText = "Nenhum horário disponível no período."
	}

	log.Printf("[CheckCalendarAvailability] workspace=%s found %d busy events, %d free slots in [%s..%s]",
		workspaceID, len(events), len(slots), dateFrom.Format(time.RFC3339), dateTo.Format(time.RFC3339))

	resultMap := map[string]interface{}{
		"available_slots":      availableText,
		"available_slots_json": jsonSlots,
		"busy_count":           len(events),
		"date_from":            dateFrom.Format(time.RFC3339),
		"date_to":              dateTo.Format(time.RFC3339),
		"timezone":             tzRaw,
		"calendar_email":       googleAccess.Email,
	}

	resultJSON, _ := json.Marshal(resultMap)
	return tools.ExecutionResult{
		Result: string(resultJSON),
	}, nil
}

func toolParamOrConfig(params map[string]interface{}, config map[string]interface{}, key string) interface{} {
	if params != nil {
		if value, ok := params[key]; ok {
			return value
		}
	}
	if config != nil {
		if value, ok := config[key]; ok {
			return value
		}
	}
	return nil
}

func toolStringParamOrConfig(params map[string]interface{}, config map[string]interface{}, key string, fallback string) string {
	value := toolParamOrConfig(params, config, key)
	if value == nil {
		return fallback
	}
	text := strings.TrimSpace(fmt.Sprintf("%v", value))
	if text == "" {
		return fallback
	}
	return text
}

func toolIntParamOrConfig(params map[string]interface{}, config map[string]interface{}, key string, fallback int) int {
	value := toolParamOrConfig(params, config, key)
	switch typed := value.(type) {
	case int:
		if typed > 0 {
			return typed
		}
	case int32:
		if typed > 0 {
			return int(typed)
		}
	case int64:
		if typed > 0 {
			return int(typed)
		}
	case float32:
		if typed > 0 {
			return int(typed)
		}
	case float64:
		if typed > 0 {
			return int(typed)
		}
	case json.Number:
		if parsed, err := typed.Int64(); err == nil && parsed > 0 {
			return int(parsed)
		}
	case string:
		var parsed int
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%d", &parsed); err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

func toolBoolParamOrConfig(params map[string]interface{}, config map[string]interface{}, key string, fallback bool) bool {
	value := toolParamOrConfig(params, config, key)
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes", "sim":
			return true
		case "false", "0", "no", "nao", "não":
			return false
		}
	}
	return fallback
}

var _ tools.Handler = (*checkCalendarAvailabilityTool)(nil)

func calendarAccessRefusal(workspaceID string, err error) tools.ExecutionResult {
	if errors.Is(err, calendar.ErrGoogleNotConnected) {
		return tools.ExecutionResult{Result: "Nenhuma conta Google Calendar conectada neste workspace.", IsError: true}
	}
	log.Printf("[calendar_tool] google access failed for workspace %s: %v", workspaceID, err)
	return tools.ExecutionResult{Result: fmt.Sprintf("Falha ao autenticar no Google Calendar: %v", err), IsError: true}
}
