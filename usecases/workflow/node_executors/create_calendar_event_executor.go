package node_executors

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"vozko/brand"
	"vozko/domain/calendar"
	"vozko/domain/workflow"
)

type createCalendarEventExecutor struct {
	repo   calendar.Repository
	google calendar.GoogleCalendarAPI
	access calendar.AccessProvider
}

func NewCreateCalendarEventExecutor(repo calendar.Repository, google calendar.GoogleCalendarAPI, access calendar.AccessProvider) workflow.NodeExecutor {
	return &createCalendarEventExecutor{repo: repo, google: google, access: access}
}

func (s *createCalendarEventExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:         workflow.NodeTypeActionCreateCalendarEvent,
		TestBehavior: workflow.NodeTestExternalCall,
		Category:     workflow.NodeCategoryAction,
		Scopes:       []workflow.NodeScope{workflow.NodeScopeShared},
		Label:        "Criar evento na agenda",
		Description:  "Cria um evento no Google Calendar do workspace. Gerar um link do Google Meet para o evento é opcional.",
		Icon:         "CalendarCheck",
		Guidance: workflow.NodeGuidance{
			When:     "Para criar um evento no Google Calendar (visita, consulta, chamada ou qualquer compromisso).",
			Behavior: "Requer uma conexão ativa com o Google Calendar do workspace.",
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "sucesso", Label: "Sucesso"},
			{ID: "erro", Label: "Erro", Optional: true},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "success", Description: "true quando o evento é criado com sucesso", Label: "Deu certo", Type: workflow.VariableTypeBoolean},
			{Key: "title", Description: "Título final enviado ao Google Calendar", Label: "Título", Type: workflow.VariableTypeText},
			{Key: "google_event_id", Description: "ID do evento no Google Calendar", Label: "ID do evento no Google", Type: workflow.VariableTypeText},
			{Key: "meeting_link", Description: "Link do Google Meet do evento, vazio quando o Meet não foi pedido", Label: "Link do Google Meet", Type: workflow.VariableTypeText},
			{Key: "start_time", Description: "Início do evento em RFC3339", Label: "Início", Type: workflow.VariableTypeDate},
			{Key: "end_time", Description: "Fim do evento em RFC3339", Label: "Fim", Type: workflow.VariableTypeDate},
			{Key: "timezone", Description: "Timezone efetivamente usada", Label: "Fuso horário", Type: workflow.VariableTypeText},
			{Key: "attendees", Description: "Lista final de participantes convidados", Label: "Participantes", Type: workflow.VariableTypeList},
			{Key: "calendar_email", Description: "Conta Google conectada ao workspace", Label: "Conta Google", Type: workflow.VariableTypeText},
			{Key: "error", Description: "Descrição do erro quando a criação falha", Label: "Erro", Type: workflow.VariableTypeText},
		},
		DefaultConfig: map[string]interface{}{
			"title":              "Evento agendado",
			"description":        "Evento agendado via " + brand.Active().Name,
			"location":           "",
			"duration":           30,
			"attendees":          "",
			"start_time":         "{{var.start_time}}",
			"end_time":           "",
			"timezone":           "America/Sao_Paulo",
			"create_google_meet": "true",
			"send_updates":       "all",
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "title", Label: "Título", Type: "text", Placeholder: "Ex: Visita técnica", Required: true},
			{Key: "description", Label: "Descrição", Type: "textarea", Placeholder: "Contexto do evento, pauta, observações..."},
			{Key: "location", Label: "Local", Type: "text", Placeholder: "Opcional. Ex: Escritório ou link externo"},
			{Key: "start_time", Label: "Início", Type: "text", Placeholder: "2026-03-31T14:00:00-03:00 ou {{var.start_time}}", Required: true},
			{Key: "end_time", Label: "Fim", Type: "text", Placeholder: "Opcional. Se vazio, usa a duração abaixo"},
			{Key: "duration", Label: "Duração (minutos)", Type: "number", Placeholder: "30"},
			{Key: "timezone", Label: "Timezone", Type: "text", Placeholder: "America/Sao_Paulo", Required: true},
			{Key: "attendees", Label: "Convidados", Type: "textarea", Placeholder: "email1@empresa.com, email2@empresa.com"},
			{
				Key:   "create_google_meet",
				Label: "Criar link do Google Meet (opcional)",
				Type:  "select",
				Options: []workflow.ConfigFieldOption{
					{Value: "true", Label: "Sim"},
					{Value: "false", Label: "Não"},
				},
			},
			{
				Key:   "send_updates",
				Label: "Enviar convites",
				Type:  "select",
				Options: []workflow.ConfigFieldOption{
					{Value: "all", Label: "Todos"},
					{Value: "externalOnly", Label: "Somente externos"},
					{Value: "none", Label: "Não enviar"},
				},
			},
		},
	}
}

func (s *createCalendarEventExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	logPrefix := fmt.Sprintf("[workflow][create_calendar_event][node:%s][run:%s]",
		calendarNodeID(ctx), calendarRunID(ctx))
	log.Printf("%s config: start_time=%q end_time=%q title=%q duration=%v timezone=%q attendees=%q",
		logPrefix,
		calendarConfigText(ctx, "start_time"),
		calendarConfigText(ctx, "end_time"),
		calendarConfigText(ctx, "title"),
		ctx.Node.Config["duration"],
		calendarConfigText(ctx, "timezone"),
		calendarConfigText(ctx, "attendees"),
	)

	if s.repo == nil || s.google == nil || s.access == nil {
		return createCalendarEventFailure(ctx, "integração Google Calendar indisponível no servidor"), nil
	}

	workspaceID := calendarWorkspaceID(ctx)
	if workspaceID == "" {
		return createCalendarEventFailure(ctx, "workspace do fluxo não encontrado"), nil
	}

	googleAccess, err := s.access.CalendarAccess(context.Background(), workspaceID)
	if err != nil {
		return createCalendarEventFailure(ctx, calendarAccessFailure(err)), nil
	}

	title := strings.TrimSpace(calendarConfigText(ctx, "title"))
	if title == "" {
		return createCalendarEventFailure(ctx, "o campo Título é obrigatório"), nil
	}

	timezoneName := strings.TrimSpace(calendarConfigText(ctx, "timezone"))
	if timezoneName == "" {
		timezoneName = "America/Sao_Paulo"
	}
	location, err := time.LoadLocation(timezoneName)
	if err != nil {
		return createCalendarEventFailure(ctx, fmt.Sprintf("timezone inválida: %s", timezoneName)), nil
	}

	startRaw := strings.TrimSpace(calendarConfigText(ctx, "start_time"))
	if startRaw == "" {
		return createCalendarEventFailure(ctx, "o campo Início é obrigatório"), nil
	}
	startTime, err := parseCalendarTime(startRaw, location)
	if err != nil {
		return createCalendarEventFailure(ctx, fmt.Sprintf("data/hora de início inválida: %v", err)), nil
	}

	endRaw := strings.TrimSpace(calendarConfigText(ctx, "end_time"))
	endTime := time.Time{}
	if endRaw != "" {
		endTime, err = parseCalendarTime(endRaw, location)
		if err != nil {
			return createCalendarEventFailure(ctx, fmt.Sprintf("data/hora de fim inválida: %v", err)), nil
		}
	} else {
		durationMinutes := calendarDurationMinutes(ctx)
		if durationMinutes <= 0 {
			durationMinutes = 30
		}
		endTime = startTime.Add(time.Duration(durationMinutes) * time.Minute)
	}

	if !endTime.After(startTime) {
		return createCalendarEventFailure(ctx, "o fim do evento precisa ser depois do início"), nil
	}

	attendees, err := parseCalendarAttendees(ctx.Node.Config["attendees"], ctx.State)
	if err != nil {
		return createCalendarEventFailure(ctx, err.Error()), nil
	}

	accessToken := googleAccess.Token

	conflict, err := calendar.ConflictCheck{Google: s.google, AccessToken: accessToken, Local: s.repo}.Conflict(workspaceID, startTime, endTime)
	if err != nil {
		return createCalendarEventRefused(ctx, err, err.Error()), nil
	}
	if conflict != nil {
		log.Printf("[workflow][create_calendar_event][run:%s] CONFLICT: event %q (%s to %s) overlaps requested slot %s to %s",
			calendarRunID(ctx), conflict.Title,
			conflict.StartTime.Format(time.RFC3339), conflict.EndTime.Format(time.RFC3339),
			startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))
		return createCalendarEventRefused(ctx, calendar.ErrSlotConflict, fmt.Sprintf(
			"O horário %s a %s já está ocupado (conflito com: %s). Escolha outro horário.",
			startTime.In(location).Format("02/01 15:04"),
			endTime.In(location).Format("15:04"),
			conflict.Title,
		)), nil
	}

	event := &calendar.CalendarEvent{
		WorkspaceID:             workspaceID,
		Title:                   title,
		Description:             calendarConfigText(ctx, "description"),
		Location:                calendarConfigText(ctx, "location"),
		StartTime:               startTime,
		EndTime:                 endTime,
		TimeZone:                timezoneName,
		Attendees:               attendees,
		Status:                  "confirmed",
		GuestsCanModify:         false,
		GuestsCanInviteOthers:   true,
		GuestsCanSeeOtherGuests: true,
		Visibility:              "default",
		Transparency:            "opaque",
		RemindersUseDefault:     true,
	}

	createGoogleMeet := calendarConfigBool(ctx.Node.Config["create_google_meet"], true)
	sendUpdates := strings.TrimSpace(calendarConfigText(ctx, "send_updates"))
	if sendUpdates == "" {
		sendUpdates = "all"
	}

	googleEventID, meetingLink, err := s.google.CreateGoogleEvent(accessToken, event, createGoogleMeet, sendUpdates)
	if err != nil {
		return createCalendarEventFailure(ctx, fmt.Sprintf("falha ao criar evento no Google Calendar: %v", err)), nil
	}

	log.Printf("[workflow][node:%s][run:%s] action_create_calendar_event: created google event=%s workspace=%s meet=%t",
		ctx.Node.ID,
		calendarRunID(ctx),
		googleEventID,
		workspaceID,
		meetingLink != "",
	)

	return routeOutput(&workflow.NodeResult{
		Output: map[string]interface{}{
			"success":         true,
			"title":           title,
			"google_event_id": googleEventID,
			"meeting_link":    meetingLink,
			"start_time":      startTime.Format(time.RFC3339),
			"end_time":        endTime.Format(time.RFC3339),
			"timezone":        timezoneName,
			"attendees":       attendees,
			"calendar_email":  googleAccess.Email,
		},
	}, outgoingEdges(ctx), "sucesso"), nil
}

func createCalendarEventFailure(ctx *workflow.NodeContext, message string) *workflow.NodeResult {
	log.Printf("[workflow][create_calendar_event][node:%s][run:%s] FAILED: %s",
		calendarNodeID(ctx), calendarRunID(ctx), message)
	return routeFailure(&workflow.NodeResult{
		Output: map[string]interface{}{
			"success": false,
			"error":   message,
		},
	}, outgoingEdges(ctx), "erro")
}

func calendarNodeID(ctx *workflow.NodeContext) string {
	if ctx == nil || ctx.Node == nil {
		return ""
	}
	return ctx.Node.ID
}

func calendarWorkspaceID(ctx *workflow.NodeContext) string {
	if ctx == nil {
		return ""
	}
	if ctx.Workflow != nil && strings.TrimSpace(ctx.Workflow.WorkspaceID) != "" {
		return strings.TrimSpace(ctx.Workflow.WorkspaceID)
	}
	if ctx.Run != nil {
		return strings.TrimSpace(ctx.Run.WorkspaceID)
	}
	return ""
}

func calendarRunID(ctx *workflow.NodeContext) string {
	if ctx == nil || ctx.Run == nil {
		return ""
	}
	return ctx.Run.ID
}

func calendarConfigText(ctx *workflow.NodeContext, key string) string {
	if ctx == nil || ctx.Node == nil || ctx.Node.Config == nil {
		return ""
	}
	val, ok := ctx.Node.Config[key]
	if !ok || val == nil {
		return ""
	}
	str, ok := val.(string)
	if !ok {
		return workflow.TemplateText(val)
	}
	return strings.TrimSpace(workflow.Interpolate(str, ctx.State, nil))
}

func calendarDurationMinutes(ctx *workflow.NodeContext) int {
	if ctx == nil || ctx.Node == nil || ctx.Node.Config == nil {
		return 0
	}
	if raw, ok := ctx.Node.Config["duration"]; ok {
		switch v := raw.(type) {
		case int:
			return v
		case int32:
			return int(v)
		case int64:
			return int(v)
		case float32:
			return int(v)
		case float64:
			return int(v)
		case string:
			parsed, err := time.ParseDuration(strings.TrimSpace(v) + "m")
			if err == nil {
				return int(parsed / time.Minute)
			}
		}
	}
	return 0
}

func calendarConfigBool(raw interface{}, fallback bool) bool {
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "sim":
			return true
		case "false", "0", "no", "nao", "não":
			return false
		}
	}
	return fallback
}

func parseCalendarTime(raw string, location *time.Location) (time.Time, error) {
	return calendar.ParseDateTime(raw, location)
}

func parseCalendarAttendees(raw interface{}, state *workflow.RunState) ([]calendar.Attendee, error) {
	attendees, rejected := calendar.ParseAttendees(interpolatedAttendees(raw, state))
	if len(rejected) > 0 {
		return nil, fmt.Errorf("convidado inválido: %s", rejected[0])
	}
	return attendees, nil
}

func interpolatedAttendees(raw interface{}, state *workflow.RunState) interface{} {
	switch v := raw.(type) {
	case nil:
		return nil
	case string:
		return workflow.Interpolate(v, state, nil)
	case []string:
		items := make([]string, len(v))
		for i, item := range v {
			items[i] = workflow.Interpolate(item, state, nil)
		}
		return items
	case []interface{}:
		items := make([]string, len(v))
		for i, item := range v {
			items[i] = workflow.Interpolate(workflow.TemplateText(item), state, nil)
		}
		return items
	default:
		return []string{workflow.Interpolate(workflow.TemplateText(v), state, nil)}
	}
}

func calendarAccessFailure(err error) string {
	if errors.Is(err, calendar.ErrGoogleNotConnected) {
		return "nenhuma conta Google Calendar conectada neste workspace"
	}
	return fmt.Sprintf("falha ao autenticar no Google Calendar: %v", err)
}

func createCalendarEventRefused(ctx *workflow.NodeContext, cause error, message string) *workflow.NodeResult {
	result := createCalendarEventFailure(ctx, message)
	result.Output["error_code"] = calendar.ErrorCode(cause)
	return result
}
