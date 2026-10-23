package node_executors

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/domain/calendar"
	"vozko/domain/workflow"
)

type rescheduleCalendarEventExecutor struct {
	reschedule calendar.RescheduleEventUseCase
}

func NewRescheduleCalendarEventExecutor(reschedule calendar.RescheduleEventUseCase) workflow.NodeExecutor {
	return &rescheduleCalendarEventExecutor{reschedule: reschedule}
}

func (s *rescheduleCalendarEventExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:         workflow.NodeTypeActionRescheduleCalendarEvent,
		TestBehavior: workflow.NodeTestExternalCall,
		Category:     workflow.NodeCategoryAction,
		Scopes:       []workflow.NodeScope{workflow.NodeScopeShared},
		Label:        "Reagendar evento na agenda",
		Description:  "Muda a data/horário de um evento já existente no Google Calendar, mantendo os participantes e o link do Google Meet, quando houver.",
		Icon:         "ArrowsClockwise",
		Guidance: workflow.NodeGuidance{
			When:     "Para mudar a data/horário de um evento existente na agenda (reagendamento).",
			Behavior: "Requer o ID do evento (google_event_id do nó Criar evento na agenda) e uma conexão ativa com o Google Calendar.",
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "sucesso", Label: "Sucesso"},
			{ID: "erro", Label: "Erro", Optional: true},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "success", Description: "true quando o evento é reagendado com sucesso", Label: "Deu certo", Type: workflow.VariableTypeBoolean},
			{Key: "google_event_id", Description: "ID do evento no Google Calendar", Label: "ID do evento no Google", Type: workflow.VariableTypeText},
			{Key: "meeting_link", Description: "Link do Google Meet do evento, vazio quando o evento não tem Meet", Label: "Link do Google Meet", Type: workflow.VariableTypeText},
			{Key: "start_time", Description: "Novo início em RFC3339", Label: "Início", Type: workflow.VariableTypeDate},
			{Key: "end_time", Description: "Novo fim em RFC3339", Label: "Fim", Type: workflow.VariableTypeDate},
			{Key: "timezone", Description: "Timezone efetivamente usada", Label: "Fuso horário", Type: workflow.VariableTypeText},
			{Key: "error", Description: "Descrição do erro quando o reagendamento falha", Label: "Erro", Type: workflow.VariableTypeText},
		},
		DefaultConfig: map[string]interface{}{
			"event_id":     "{{var.google_event_id}}",
			"start_time":   "{{var.start_time}}",
			"end_time":     "",
			"duration":     0,
			"timezone":     "America/Sao_Paulo",
			"send_updates": "all",
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "event_id", Label: "ID do evento", Type: "text", Placeholder: "{{var.google_event_id}}", Required: true},
			{Key: "start_time", Label: "Novo início", Type: "text", Placeholder: "2026-03-31T15:00:00-03:00 ou {{var.start_time}}", Required: true},
			{Key: "end_time", Label: "Novo fim", Type: "text", Placeholder: "Opcional. Se vazio, mantém a duração original"},
			{Key: "duration", Label: "Duração (minutos)", Type: "number", Placeholder: "Opcional. Ex: 30"},
			{Key: "timezone", Label: "Timezone", Type: "text", Placeholder: "America/Sao_Paulo", Required: true},
			{
				Key:   "send_updates",
				Label: "Notificar participantes",
				Type:  "select",
				Options: []workflow.ConfigFieldOption{
					{Value: "all", Label: "Todos"},
					{Value: "externalOnly", Label: "Somente externos"},
					{Value: "none", Label: "Não notificar"},
				},
			},
		},
	}
}

func (s *rescheduleCalendarEventExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	logPrefix := fmt.Sprintf("[workflow][reschedule_calendar_event][node:%s][run:%s]",
		calendarNodeID(ctx), calendarRunID(ctx))

	if s.reschedule == nil {
		return rescheduleCalendarEventFailure(ctx, "integração Google Calendar indisponível no servidor"), nil
	}

	workspaceID := calendarWorkspaceID(ctx)
	if workspaceID == "" {
		return rescheduleCalendarEventFailure(ctx, "workspace do fluxo não encontrado"), nil
	}

	eventID := strings.TrimSpace(calendarConfigText(ctx, "event_id"))
	if eventID == "" {
		return rescheduleCalendarEventFailure(ctx, "o ID do evento a reagendar é obrigatório"), nil
	}

	timezoneName := strings.TrimSpace(calendarConfigText(ctx, "timezone"))
	if timezoneName == "" {
		timezoneName = "America/Sao_Paulo"
	}
	location, err := time.LoadLocation(timezoneName)
	if err != nil {
		return rescheduleCalendarEventFailure(ctx, fmt.Sprintf("timezone inválida: %s", timezoneName)), nil
	}

	startRaw := strings.TrimSpace(calendarConfigText(ctx, "start_time"))
	if startRaw == "" {
		return rescheduleCalendarEventFailure(ctx, "o novo horário de início é obrigatório"), nil
	}
	newStart, err := parseCalendarTime(startRaw, location)
	if err != nil {
		return rescheduleCalendarEventFailure(ctx, fmt.Sprintf("nova data/hora de início inválida: %v", err)), nil
	}

	input := calendar.RescheduleEventInput{
		EventID:      eventID,
		WorkspaceID:  workspaceID,
		NewStartTime: newStart,
		SendUpdates:  strings.TrimSpace(calendarConfigText(ctx, "send_updates")),
	}
	if endRaw := strings.TrimSpace(calendarConfigText(ctx, "end_time")); endRaw != "" {
		newEnd, endErr := parseCalendarTime(endRaw, location)
		if endErr != nil {
			return rescheduleCalendarEventFailure(ctx, fmt.Sprintf("nova data/hora de fim inválida: %v", endErr)), nil
		}
		input.NewEndTime = &newEnd
	} else if d := calendarDurationMinutes(ctx); d > 0 {
		input.DurationMinutes = d
	}

	event, err := s.reschedule.Execute(input)
	if err != nil {
		switch {
		case errors.Is(err, calendar.ErrSlotConflict):
			return rescheduleCalendarEventFailure(ctx, fmt.Sprintf(
				"O horário %s já está ocupado. Escolha outro horário.",
				newStart.In(location).Format("02/01 15:04"))), nil
		case errors.Is(err, calendar.ErrEventNotFound):
			return rescheduleCalendarEventFailure(ctx, "evento não encontrado para reagendar"), nil
		case errors.Is(err, calendar.ErrGoogleNotConnected):
			return rescheduleCalendarEventFailure(ctx, "nenhuma conta Google Calendar conectada neste workspace"), nil
		case errors.Is(err, calendar.ErrInvalidTimeRange):
			return rescheduleCalendarEventFailure(ctx, "o horário de fim precisa ser depois do início"), nil
		default:
			return rescheduleCalendarEventFailure(ctx, fmt.Sprintf("falha ao reagendar o evento: %v", err)), nil
		}
	}

	log.Printf("%s moved google event=%s workspace=%s to %s",
		logPrefix, event.GoogleEventID, workspaceID, newStart.Format(time.RFC3339))

	return routeOutput(&workflow.NodeResult{
		Output: map[string]interface{}{
			"success":         true,
			"google_event_id": event.GoogleEventID,
			"meeting_link":    event.MeetingLink,
			"start_time":      event.StartTime.Format(time.RFC3339),
			"end_time":        event.EndTime.Format(time.RFC3339),
			"timezone":        timezoneName,
		},
	}, outgoingEdges(ctx), "sucesso"), nil
}

func rescheduleCalendarEventFailure(ctx *workflow.NodeContext, message string) *workflow.NodeResult {
	log.Printf("[workflow][reschedule_calendar_event][node:%s][run:%s] FAILED: %s",
		calendarNodeID(ctx), calendarRunID(ctx), message)
	return routeFailure(&workflow.NodeResult{
		Output: map[string]interface{}{
			"success": false,
			"error":   message,
		},
	}, outgoingEdges(ctx), "erro")
}
