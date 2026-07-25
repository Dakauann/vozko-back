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

// rescheduleMeetingExecutor moves an existing Google Calendar event to a new time. It
// delegates the actual move (provider update, persistence, conflict check) to the
// reschedule use case and only maps node config → input and result → success/error
// branches, reusing the schedule-meeting node helpers for interpolation/time parsing.
type rescheduleMeetingExecutor struct {
	reschedule calendar.RescheduleEventUseCase
}

func NewRescheduleMeetingExecutor(reschedule calendar.RescheduleEventUseCase) workflow.NodeExecutor {
	return &rescheduleMeetingExecutor{reschedule: reschedule}
}

func (s *rescheduleMeetingExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeActionRescheduleMeeting,
		Category:    workflow.NodeCategoryAction,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Reagendar Reunião",
		Description: "Muda a data/horário de um evento já existente no Google Calendar, mantendo o mesmo link e participantes.",
		Icon:        "ArrowsClockwise",
		Guidance: workflow.NodeGuidance{
			When:     "Para mudar a data/horário de um agendamento existente (reagendamento).",
			Behavior: "Requer o ID do evento (google_event_id do agendamento) e uma conexão ativa com o Google Calendar.",
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "sucesso", Label: "Sucesso"},
			{ID: "erro", Label: "Erro", Optional: true},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "success", Description: "true quando o evento é reagendado com sucesso"},
			{Key: "google_event_id", Description: "ID do evento no Google Calendar"},
			{Key: "meeting_link", Description: "Link do Google Meet do evento"},
			{Key: "start_time", Description: "Novo início em RFC3339"},
			{Key: "end_time", Description: "Novo fim em RFC3339"},
			{Key: "timezone", Description: "Timezone efetivamente usada"},
			{Key: "error", Description: "Descrição do erro quando o reagendamento falha"},
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
			{Key: "event_id", Label: "ID do agendamento", Type: "text", Placeholder: "{{var.google_event_id}}", Required: true},
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

func (s *rescheduleMeetingExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	logPrefix := fmt.Sprintf("[workflow][reschedule_meeting][node:%s][run:%s]",
		scheduleMeetingNodeID(ctx), scheduleMeetingRunID(ctx))

	if s.reschedule == nil {
		return rescheduleMeetingFailure(ctx, "integração Google Calendar indisponível no servidor"), nil
	}

	workspaceID := scheduleMeetingWorkspaceID(ctx)
	if workspaceID == "" {
		return rescheduleMeetingFailure(ctx, "workspace do fluxo não encontrado"), nil
	}

	eventID := strings.TrimSpace(scheduleMeetingInterpolatedString(ctx, "event_id"))
	if eventID == "" {
		return rescheduleMeetingFailure(ctx, "o ID do agendamento a reagendar é obrigatório"), nil
	}

	timezoneName := strings.TrimSpace(scheduleMeetingInterpolatedString(ctx, "timezone"))
	if timezoneName == "" {
		timezoneName = "America/Sao_Paulo"
	}
	location, err := time.LoadLocation(timezoneName)
	if err != nil {
		return rescheduleMeetingFailure(ctx, fmt.Sprintf("timezone inválida: %s", timezoneName)), nil
	}

	startRaw := strings.TrimSpace(scheduleMeetingInterpolatedString(ctx, "start_time"))
	if startRaw == "" {
		return rescheduleMeetingFailure(ctx, "o novo horário de início é obrigatório"), nil
	}
	newStart, err := parseScheduleMeetingTime(startRaw, location)
	if err != nil {
		return rescheduleMeetingFailure(ctx, fmt.Sprintf("nova data/hora de início inválida: %v", err)), nil
	}

	input := calendar.RescheduleEventInput{
		EventID:      eventID,
		WorkspaceID:  workspaceID,
		NewStartTime: newStart,
		SendUpdates:  strings.TrimSpace(scheduleMeetingInterpolatedString(ctx, "send_updates")),
	}
	if endRaw := strings.TrimSpace(scheduleMeetingInterpolatedString(ctx, "end_time")); endRaw != "" {
		newEnd, endErr := parseScheduleMeetingTime(endRaw, location)
		if endErr != nil {
			return rescheduleMeetingFailure(ctx, fmt.Sprintf("nova data/hora de fim inválida: %v", endErr)), nil
		}
		input.NewEndTime = &newEnd
	} else if d := scheduleMeetingDuration(ctx); d > 0 {
		input.DurationMinutes = d
	}

	event, err := s.reschedule.Execute(input)
	if err != nil {
		switch {
		case errors.Is(err, calendar.ErrSlotConflict):
			return rescheduleMeetingFailure(ctx, fmt.Sprintf(
				"O horário %s já está ocupado. Escolha outro horário.",
				newStart.In(location).Format("02/01 15:04"))), nil
		case errors.Is(err, calendar.ErrEventNotFound):
			return rescheduleMeetingFailure(ctx, "agendamento não encontrado para reagendar"), nil
		case errors.Is(err, calendar.ErrGoogleNotConnected):
			return rescheduleMeetingFailure(ctx, "nenhuma conta Google Calendar conectada neste workspace"), nil
		case errors.Is(err, calendar.ErrInvalidTimeRange):
			return rescheduleMeetingFailure(ctx, "o horário de fim precisa ser depois do início"), nil
		default:
			return rescheduleMeetingFailure(ctx, fmt.Sprintf("falha ao reagendar o evento: %v", err)), nil
		}
	}

	log.Printf("%s moved google event=%s workspace=%s to %s",
		logPrefix, event.GoogleEventID, workspaceID, newStart.Format(time.RFC3339))

	return &workflow.NodeResult{
		NextNodeID: scheduleMeetingResolveEdge(ctx, "sucesso"),
		Output: map[string]interface{}{
			"success":         true,
			"google_event_id": event.GoogleEventID,
			"meeting_link":    event.MeetingLink,
			"start_time":      event.StartTime.Format(time.RFC3339),
			"end_time":        event.EndTime.Format(time.RFC3339),
			"timezone":        timezoneName,
		},
	}, nil
}

func rescheduleMeetingFailure(ctx *workflow.NodeContext, message string) *workflow.NodeResult {
	log.Printf("[workflow][reschedule_meeting][node:%s][run:%s] FAILED: %s",
		scheduleMeetingNodeID(ctx), scheduleMeetingRunID(ctx), message)
	return &workflow.NodeResult{
		// STRICT "erro" routing: a failed reschedule must never flow down the success
		// path; with no "erro" edge wired the run ends here.
		NextNodeID: scheduleMeetingResolveEdgeStrict(ctx, "erro"),
		Output: map[string]interface{}{
			"success": false,
			"error":   message,
		},
	}
}
