package tools_usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/mail"
	"strings"
	"time"

	"vozko/brand"
	"vozko/domain/calendar"
	"vozko/domain/tools"
)

const ScheduleMeetingToolName = "schedule_meeting"

type scheduleMeetingTool struct {
	repo   calendar.Repository
	google calendar.GoogleOAuthService
}

func NewScheduleMeetingToolUseCase(repo calendar.Repository, google calendar.GoogleOAuthService) tools.Handler {
	if repo == nil || google == nil {
		return nil
	}
	return &scheduleMeetingTool{repo: repo, google: google}
}

func (t *scheduleMeetingTool) Definition() tools.Definition {
	return tools.Definition{
		Name:        ScheduleMeetingToolName,
		DisplayName: "Agendar Reunião",
		Description: `Cria um evento no Google Calendar do workspace e retorna o link da reunião.

QUANDO USAR:
- Cliente confirmou um horário e quer agendar a reunião
- Após verificar disponibilidade com check_calendar_availability, o cliente escolheu um slot
- Cliente diz "agenda para amanhã às 14h" ou similar

Cria o evento no Google Calendar, opcionalmente gera link do Google Meet, e envia convites aos participantes.`,
		DisplayDescription: "Cria eventos no Google Calendar usando as regras configuradas do agente.",
		Parameters: map[string]tools.Parameter{
			"title": {
				Type:               "string",
				Description:        "Título do evento/reunião.",
				DisplayName:        "Título",
				DisplayDescription: "Título do evento no calendário",
			},
			"start_time": {
				Type:               "string",
				Description:        "Data/hora de início no formato RFC3339 (ex: 2026-04-10T14:00:00-03:00).",
				DisplayName:        "Início",
				DisplayDescription: "Data e hora de início da reunião",
			},
			"end_time": {
				Type:               "string",
				Description:        "Data/hora de fim no formato RFC3339. Opcional, se vazio, usa a duração para calcular.",
				DisplayName:        "Fim",
				DisplayDescription: "Data e hora de fim da reunião",
			},
			"duration": {
				Type:               "number",
				Description:        "Duração em minutos. Usado quando end_time não é informado. Padrão: 30.",
				DisplayName:        "Duração (minutos)",
				DisplayDescription: "Duração da reunião em minutos",
			},
			"attendees": {
				Type:               "string",
				Description:        "E-mails dos convidados separados por vírgula (ex: joao@empresa.com, maria@empresa.com). Opcional.",
				DisplayName:        "Convidados",
				DisplayDescription: "Lista de e-mails dos participantes",
			},
			"description": {
				Type:               "string",
				Description:        "Descrição ou pauta da reunião. Opcional.",
				DisplayName:        "Descrição",
				DisplayDescription: "Descrição do evento",
			},
			"location": {
				Type:               "string",
				Description:        "Local da reunião. Opcional.",
				DisplayName:        "Local",
				DisplayDescription: "Local do evento",
			},
			"timezone": {
				Type:               "string",
				Description:        "Timezone IANA (ex: America/Sao_Paulo). Padrão: America/Sao_Paulo.",
				DisplayName:        "Timezone",
				DisplayDescription: "Fuso horário do evento",
			},
			"create_google_meet": {
				Type:               "boolean",
				Description:        "Se true, cria um link do Google Meet. Padrão: true.",
				DisplayName:        "Criar Google Meet",
				DisplayDescription: "Criar link do Google Meet automaticamente",
			},
		},
		Required: []string{"title", "start_time"},
		ConfigSchema: map[string]tools.ConfigParameter{
			"title": {
				Type:               "string",
				Description:        "Título padrão do evento quando a conversa não fornecer um título específico.",
				DisplayName:        "Título padrão da reunião",
				DisplayDescription: "Nome padrão da reunião criada no calendário.",
				Default:            "Reunião Agendada",
				Required:           true,
			},
			"description": {
				Type:               "string",
				Description:        "Descrição padrão adicionada aos eventos criados pelo agente.",
				DisplayName:        "Descrição padrão",
				DisplayDescription: "Pauta ou contexto padrão da reunião.",
				Default:            "Reunião agendada via " + brand.Active().Name,
				Required:           false,
			},
			"location": {
				Type:               "string",
				Description:        "Local padrão do evento, caso não seja gerado link do Google Meet.",
				DisplayName:        "Local padrão",
				DisplayDescription: "Local físico ou link externo usado por padrão.",
				Default:            "",
				Required:           false,
			},
			"duration": {
				Type:               "number",
				Description:        "Duração padrão da reunião em minutos quando end_time não for informado.",
				DisplayName:        "Duração padrão da reunião",
				DisplayDescription: "Tempo padrão da reunião em minutos.",
				Default:            30,
				Required:           true,
			},
			"timezone": {
				Type:               "string",
				Description:        "Timezone IANA padrão do evento (ex: America/Sao_Paulo).",
				DisplayName:        "Fuso horário padrão",
				DisplayDescription: "Fuso horário usado por padrão para criar eventos.",
				Default:            "America/Sao_Paulo",
				Required:           true,
			},
			"attendees": {
				Type:               "string",
				Description:        "E-mails padrão dos convidados separados por vírgula, ponto e vírgula ou quebra de linha.",
				DisplayName:        "Convidados fixos",
				DisplayDescription: "Participantes adicionados quando a conversa não informar convidados.",
				Default:            "",
				Required:           false,
			},
			"create_google_meet": {
				Type:               "boolean",
				Description:        "Define se o agente cria um link do Google Meet por padrão.",
				DisplayName:        "Criar link do Google Meet",
				DisplayDescription: "Gerar automaticamente um link de reunião do Google Meet.",
				Default:            true,
				Required:           true,
			},
			"send_updates": {
				Type:               "string",
				Description:        "Como enviar convites do Google Calendar: all, externalOnly ou none.",
				DisplayName:        "Envio de convites",
				DisplayDescription: "Controla se o Google Calendar envia convites aos participantes.",
				Default:            "all",
				Options: []tools.ConfigParameterOption{
					{Value: "all", Label: "Enviar para todos"},
					{Value: "externalOnly", Label: "Somente convidados externos"},
					{Value: "none", Label: "Não enviar convites"},
				},
				Required: true,
			},
		},
		RequiredConfig: []string{"title", "duration", "timezone", "create_google_meet", "send_updates"},
		RequiresConfig: true,
		Visibility:     []tools.ToolVisibility{tools.VisibilityMessaging},
		Category:       tools.CategoryAgentUtility,
	}
}

func (t *scheduleMeetingTool) Execute(ctx context.Context, params map[string]interface{}) (tools.ExecutionResult, error) {
	return t.ExecuteWithConfig(ctx, nil, params)
}

func (t *scheduleMeetingTool) ExecuteWithConfig(ctx context.Context, config map[string]interface{}, params map[string]interface{}) (tools.ExecutionResult, error) {
	workspaceID, _ := config["__workspace_id"].(string)
	if workspaceID == "" {
		log.Printf("[ScheduleMeeting] missing __workspace_id in config")
		return tools.ExecutionResult{
			Result:  "Não foi possível identificar o workspace. Esta ferramenta requer contexto de workspace.",
			IsError: true,
		}, nil
	}

	conn, err := t.repo.GetConnection(workspaceID)
	if err != nil {
		log.Printf("[ScheduleMeeting] failed to query connection for workspace %s: %v", workspaceID, err)
		return tools.ExecutionResult{
			Result:  fmt.Sprintf("Falha ao consultar integração do workspace: %v", err),
			IsError: true,
		}, nil
	}
	if conn == nil {
		return tools.ExecutionResult{
			Result:  "Nenhuma conta Google Calendar conectada neste workspace.",
			IsError: true,
		}, nil
	}

	title := toolStringParamOrConfig(params, config, "title", "")
	if title == "" {
		return tools.ExecutionResult{
			Result:  "O parâmetro 'title' é obrigatório.",
			IsError: true,
		}, nil
	}

	tzRaw := toolStringParamOrConfig(params, config, "timezone", "America/Sao_Paulo")
	loc, err := time.LoadLocation(tzRaw)
	if err != nil {
		return tools.ExecutionResult{
			Result:  fmt.Sprintf("Timezone inválida: %s", tzRaw),
			IsError: true,
		}, nil
	}

	startRaw, _ := params["start_time"].(string)
	startRaw = strings.TrimSpace(startRaw)
	if startRaw == "" {
		return tools.ExecutionResult{
			Result:  "O parâmetro 'start_time' é obrigatório.",
			IsError: true,
		}, nil
	}
	startTime, err := parseToolTime(startRaw, loc)
	if err != nil {
		return tools.ExecutionResult{
			Result:  fmt.Sprintf("Data/hora de início inválida: %v", err),
			IsError: true,
		}, nil
	}

	endRaw := toolStringParamOrConfig(params, config, "end_time", "")
	var endTime time.Time
	if endRaw != "" {
		endTime, err = parseToolTime(endRaw, loc)
		if err != nil {
			return tools.ExecutionResult{
				Result:  fmt.Sprintf("Data/hora de fim inválida: %v", err),
				IsError: true,
			}, nil
		}
	} else {
		durationMinutes := toolIntParamOrConfig(params, config, "duration", 30)
		endTime = startTime.Add(time.Duration(durationMinutes) * time.Minute)
	}

	if !endTime.After(startTime) {
		return tools.ExecutionResult{
			Result:  "O fim da reunião precisa ser depois do início.",
			IsError: true,
		}, nil
	}

	attendeesRaw := toolParamOrConfig(params, config, "attendees")
	attendees, skippedAttendees := parseToolAttendees(attendeesRaw)

	description := toolStringParamOrConfig(params, config, "description", "")
	location := toolStringParamOrConfig(params, config, "location", "")

	createGoogleMeet := toolBoolParamOrConfig(params, config, "create_google_meet", true)
	sendUpdates := toolStringParamOrConfig(params, config, "send_updates", "all")

	accessToken, err := ensureValidCalendarToken(t.google, t.repo, conn)
	if err != nil {
		log.Printf("[ScheduleMeeting] token refresh failed for workspace %s: %v", workspaceID, err)
		return tools.ExecutionResult{
			Result:  fmt.Sprintf("Falha ao autenticar no Google Calendar: %v", err),
			IsError: true,
		}, nil
	}

	conflictEvents, conflictErr := t.google.ListGoogleEvents(accessToken, startTime, endTime, "", 10)
	if conflictErr == nil {
		conflictEvents = mergeLocalCalToolEvents(t.repo, workspaceID, startTime, endTime, conflictEvents)
		for _, ev := range conflictEvents {
			if ev == nil || ev.Transparency == "transparent" {
				continue
			}
			if ev.StartTime.Before(endTime) && ev.EndTime.After(startTime) {
				log.Printf("[ScheduleMeeting] CONFLICT: event %q (%s–%s) overlaps requested slot %s–%s workspace=%s",
					ev.Title, ev.StartTime.Format(time.RFC3339), ev.EndTime.Format(time.RFC3339),
					startTime.Format(time.RFC3339), endTime.Format(time.RFC3339), workspaceID)
				return tools.ExecutionResult{
					Result: fmt.Sprintf(
						"O horário %s – %s já está ocupado (conflito com: %s). Escolha outro horário.",
						startTime.In(loc).Format("02/01 15:04"),
						endTime.In(loc).Format("15:04"),
						ev.Title,
					),
					IsError: true,
				}, nil
			}
		}
	}

	event := &calendar.CalendarEvent{
		WorkspaceID:             workspaceID,
		Title:                   title,
		Description:             strings.TrimSpace(description),
		Location:                strings.TrimSpace(location),
		StartTime:               startTime,
		EndTime:                 endTime,
		TimeZone:                tzRaw,
		Attendees:               attendees,
		Status:                  "confirmed",
		GuestsCanModify:         false,
		GuestsCanInviteOthers:   true,
		GuestsCanSeeOtherGuests: true,
		Visibility:              "default",
		Transparency:            "opaque",
		RemindersUseDefault:     true,
	}

	googleEventID, meetingLink, err := t.google.CreateGoogleEvent(accessToken, event, createGoogleMeet, sendUpdates)
	if err != nil {
		log.Printf("[ScheduleMeeting] failed to create event for workspace %s: %v", workspaceID, err)
		return tools.ExecutionResult{
			Result:  fmt.Sprintf("Falha ao criar evento no Google Calendar: %v", err),
			IsError: true,
		}, nil
	}

	log.Printf("[ScheduleMeeting] created event=%s workspace=%s meet=%t", googleEventID, workspaceID, meetingLink != "")

	attendeeEmails := make([]string, 0, len(attendees))
	for _, a := range attendees {
		attendeeEmails = append(attendeeEmails, a.Email)
	}

	resultMap := map[string]interface{}{
		"google_event_id": googleEventID,
		"meeting_link":    meetingLink,
		"title":           title,
		"start_time":      startTime.Format(time.RFC3339),
		"end_time":        endTime.Format(time.RFC3339),
		"timezone":        tzRaw,
		"attendees":       strings.Join(attendeeEmails, ", "),
		"calendar_email":  conn.Email,
	}

	// Tell the AI about contacts that couldn't be invited (no valid e-mail, e.g. a
	// phone number) so it informs the user the link was shared in the chat rather
	// than claiming an invite was sent.
	if len(skippedAttendees) > 0 {
		resultMap["skipped_attendees"] = strings.Join(skippedAttendees, ", ")
		resultMap["note"] = fmt.Sprintf(
			"Estes contatos não têm e-mail válido e NÃO foram convidados no Google Calendar: %s. "+
				"A reunião foi criada normalmente; compartilhe o link com eles na conversa.",
			strings.Join(skippedAttendees, ", "),
		)
	}

	resultJSON, _ := json.Marshal(resultMap)
	return tools.ExecutionResult{
		Result: string(resultJSON),
	}, nil
}

// parseToolAttendees parses the attendees param into valid Google Calendar
// attendees. Non-email entries (e.g. a WhatsApp lead's phone number the AI passes
// through) are returned in `skipped` instead of aborting the whole meeting, so
// the meeting is still created and the caller can tell the AI which contacts
// weren't invited (and to share the link in the conversation instead).
func parseToolAttendees(raw interface{}) (attendees []calendar.Attendee, skipped []string) {
	if raw == nil {
		return nil, nil
	}

	var tokens []string
	switch v := raw.(type) {
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return nil, nil
		}
		tokens = splitToolAttendeeTokens(v)
	case []interface{}:
		for _, item := range v {
			tokens = append(tokens, fmt.Sprintf("%v", item))
		}
	default:
		tokens = append(tokens, fmt.Sprintf("%v", v))
	}

	attendees = make([]calendar.Attendee, 0, len(tokens))
	seen := make(map[string]struct{}, len(tokens))
	for _, token := range tokens {
		candidate := strings.TrimSpace(token)
		if candidate == "" {
			continue
		}
		parsed, err := mail.ParseAddress(candidate)
		if err != nil {
			parsed, err = mail.ParseAddress("<" + candidate + ">")
			if err != nil {
				skipped = append(skipped, candidate)
				continue
			}
		}
		email := strings.ToLower(strings.TrimSpace(parsed.Address))
		if email == "" {
			continue
		}
		if _, exists := seen[email]; exists {
			continue
		}
		seen[email] = struct{}{}
		attendees = append(attendees, calendar.Attendee{Email: email, Name: strings.TrimSpace(parsed.Name)})
	}

	return attendees, skipped
}

func splitToolAttendeeTokens(raw string) []string {
	replacer := strings.NewReplacer(";", "\n", ",", "\n")
	parts := strings.Split(replacer.Replace(raw), "\n")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

var _ tools.Handler = (*scheduleMeetingTool)(nil)
