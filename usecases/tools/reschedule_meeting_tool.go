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

const RescheduleMeetingToolName = "reschedule_meeting"

type rescheduleMeetingTool struct {
	reschedule calendar.RescheduleEventUseCase
}

func NewRescheduleMeetingToolUseCase(reschedule calendar.RescheduleEventUseCase) tools.Handler {
	if reschedule == nil {
		return nil
	}
	return &rescheduleMeetingTool{reschedule: reschedule}
}

func (t *rescheduleMeetingTool) Definition() tools.Definition {
	return tools.Definition{
		Name:        RescheduleMeetingToolName,
		DisplayName: "Reagendar Reunião",
		Description: `Reagenda (muda a data/horário) de um evento já existente no Google Calendar, mantendo o mesmo link do Google Meet e os mesmos participantes.

QUANDO USAR:
- Cliente já tem uma reunião agendada e quer mudar para outra data ou horário ("pode remarcar para amanhã às 15h?", "consigo mudar minha consulta?").
- Após um agendamento (schedule_meeting), use o "google_event_id" retornado como "event_id" aqui.

IMPORTANTE: informe o "event_id" do agendamento existente. Se o novo horário já estiver ocupado, a ferramenta avisa e o evento NÃO é movido — ofereça outro horário.`,
		DisplayDescription: "Muda a data/horário de um evento existente no Google Calendar.",
		Parameters: map[string]tools.Parameter{
			"event_id": {
				Type:               "string",
				Description:        "Identificador do agendamento a reagendar (o 'google_event_id' retornado por schedule_meeting).",
				DisplayName:        "ID do agendamento",
				DisplayDescription: "Evento existente que será reagendado",
			},
			"start_time": {
				Type:               "string",
				Description:        "Nova data/hora de início no formato RFC3339 (ex: 2026-04-10T15:00:00-03:00).",
				DisplayName:        "Novo início",
				DisplayDescription: "Nova data e hora de início",
			},
			"end_time": {
				Type:               "string",
				Description:        "Nova data/hora de fim no formato RFC3339. Opcional — se vazio, mantém a mesma duração do evento original.",
				DisplayName:        "Novo fim",
				DisplayDescription: "Nova data e hora de fim",
			},
			"duration": {
				Type:               "number",
				Description:        "Nova duração em minutos. Opcional — usada quando end_time não é informado. Se ambos vazios, mantém a duração original.",
				DisplayName:        "Duração (minutos)",
				DisplayDescription: "Nova duração em minutos",
			},
			"timezone": {
				Type:               "string",
				Description:        "Timezone IANA (ex: America/Sao_Paulo). Padrão: America/Sao_Paulo.",
				DisplayName:        "Timezone",
				DisplayDescription: "Fuso horário do novo horário",
			},
		},
		Required: []string{"event_id", "start_time"},
		ConfigSchema: map[string]tools.ConfigParameter{
			"timezone": {
				Type:               "string",
				Description:        "Timezone IANA padrão do evento (ex: America/Sao_Paulo).",
				DisplayName:        "Fuso horário padrão",
				DisplayDescription: "Fuso horário usado por padrão ao reagendar.",
				Default:            "America/Sao_Paulo",
				Required:           true,
			},
			"send_updates": {
				Type:               "string",
				Description:        "Como notificar os participantes sobre a mudança: all, externalOnly ou none.",
				DisplayName:        "Notificar participantes",
				DisplayDescription: "Controla se o Google Calendar avisa os participantes do reagendamento.",
				Default:            "all",
				Options: []tools.ConfigParameterOption{
					{Value: "all", Label: "Notificar todos"},
					{Value: "externalOnly", Label: "Somente convidados externos"},
					{Value: "none", Label: "Não notificar"},
				},
				Required: true,
			},
		},
		RequiredConfig: []string{"timezone", "send_updates"},
		RequiresConfig: true,
		Visibility:     []tools.ToolVisibility{tools.VisibilityMessaging},
		Category:       tools.CategoryAgentUtility,
	}
}

func (t *rescheduleMeetingTool) Execute(ctx context.Context, params map[string]interface{}) (tools.ExecutionResult, error) {
	return t.ExecuteWithConfig(ctx, nil, params)
}

func (t *rescheduleMeetingTool) ExecuteWithConfig(ctx context.Context, config map[string]interface{}, params map[string]interface{}) (tools.ExecutionResult, error) {
	workspaceID, _ := config["__workspace_id"].(string)
	if workspaceID == "" {
		log.Printf("[RescheduleMeeting] missing __workspace_id in config")
		return toolError("Não foi possível identificar o workspace. Esta ferramenta requer contexto de workspace."), nil
	}

	eventID := strings.TrimSpace(toolStringParamOrConfig(params, config, "event_id", ""))
	if eventID == "" {
		return toolError("Informe o 'event_id' do agendamento que deseja reagendar."), nil
	}

	tzRaw := toolStringParamOrConfig(params, config, "timezone", "America/Sao_Paulo")
	loc, err := time.LoadLocation(tzRaw)
	if err != nil {
		return toolError(fmt.Sprintf("Timezone inválida: %s", tzRaw)), nil
	}

	startRaw := strings.TrimSpace(toolStringParamOrConfig(params, config, "start_time", ""))
	if startRaw == "" {
		return toolError("O parâmetro 'start_time' (novo horário) é obrigatório."), nil
	}
	newStart, err := parseToolTime(startRaw, loc)
	if err != nil {
		return toolError(fmt.Sprintf("Nova data/hora de início inválida: %v", err)), nil
	}

	input := calendar.RescheduleEventInput{
		EventID:      eventID,
		WorkspaceID:  workspaceID,
		NewStartTime: newStart,
		SendUpdates:  toolStringParamOrConfig(params, config, "send_updates", "all"),
	}
	if endRaw := strings.TrimSpace(toolStringParamOrConfig(params, config, "end_time", "")); endRaw != "" {
		newEnd, endErr := parseToolTime(endRaw, loc)
		if endErr != nil {
			return toolError(fmt.Sprintf("Nova data/hora de fim inválida: %v", endErr)), nil
		}
		input.NewEndTime = &newEnd
	} else if d := toolIntParamOrConfig(params, config, "duration", 0); d > 0 {
		input.DurationMinutes = d
	}

	event, err := t.reschedule.Execute(input)
	if err != nil {
		switch {
		case errors.Is(err, calendar.ErrSlotConflict):
			return toolError(fmt.Sprintf(
				"O horário %s já está ocupado. Ofereça outro horário ao cliente.",
				newStart.In(loc).Format("02/01 15:04"),
			)), nil
		case errors.Is(err, calendar.ErrEventNotFound):
			return toolError("Não encontrei esse agendamento para reagendar. Confirme o 'event_id'."), nil
		case errors.Is(err, calendar.ErrGoogleNotConnected):
			return toolError("Nenhuma conta Google Calendar conectada neste workspace."), nil
		case errors.Is(err, calendar.ErrInvalidTimeRange):
			return toolError("O horário de fim precisa ser depois do início."), nil
		default:
			log.Printf("[RescheduleMeeting] failed for workspace %s event %s: %v", workspaceID, eventID, err)
			return toolError(fmt.Sprintf("Falha ao reagendar o evento: %v", err)), nil
		}
	}

	log.Printf("[RescheduleMeeting] moved event=%s workspace=%s to %s", event.GoogleEventID, workspaceID, newStart.Format(time.RFC3339))

	resultJSON, _ := json.Marshal(map[string]interface{}{
		"google_event_id": event.GoogleEventID,
		"title":           event.Title,
		"start_time":      event.StartTime.Format(time.RFC3339),
		"end_time":        event.EndTime.Format(time.RFC3339),
		"meeting_link":    event.MeetingLink,
		"timezone":        tzRaw,
		"rescheduled":     true,
	})
	return tools.ExecutionResult{Result: string(resultJSON)}, nil
}

func toolError(msg string) tools.ExecutionResult {
	return tools.ExecutionResult{Result: msg, IsError: true}
}

var _ tools.Handler = (*rescheduleMeetingTool)(nil)
