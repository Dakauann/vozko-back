package node_executors

import (
	"fmt"
	"log"
	"net/mail"
	"strings"
	"time"

	"vozko/brand"
	"vozko/domain/calendar"
	"vozko/domain/workflow"
)

type scheduleMeetingExecutor struct {
	repo   calendar.Repository
	google calendar.GoogleOAuthService
}

func NewScheduleMeetingExecutor(repo calendar.Repository, google calendar.GoogleOAuthService) workflow.NodeExecutor {
	return &scheduleMeetingExecutor{repo: repo, google: google}
}

func (s *scheduleMeetingExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:        workflow.NodeTypeActionScheduleMeeting,
		Category:    workflow.NodeCategoryAction,
		Scopes:      []workflow.NodeScope{workflow.NodeScopeShared},
		Label:       "Agendar Reunião",
		Description: "Cria um evento no Google Calendar do workspace e retorna o link da reunião.",
		Icon:        "CalendarCheck",
		Guidance: workflow.NodeGuidance{
			When:     "Para criar um evento no Google Calendar e retornar o link.",
			Behavior: "Requer uma conexão ativa com o Google Calendar do workspace.",
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "sucesso", Label: "Sucesso"},
			{ID: "erro", Label: "Erro", Optional: true},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "success", Description: "true quando o evento é criado com sucesso"},
			{Key: "title", Description: "Título final enviado ao Google Calendar"},
			{Key: "google_event_id", Description: "ID do evento no Google Calendar"},
			{Key: "meeting_link", Description: "Link do Google Meet criado para a reunião"},
			{Key: "start_time", Description: "Início da reunião em RFC3339"},
			{Key: "end_time", Description: "Fim da reunião em RFC3339"},
			{Key: "timezone", Description: "Timezone efetivamente usada"},
			{Key: "attendees", Description: "Lista final de participantes convidados"},
			{Key: "calendar_email", Description: "Conta Google conectada ao workspace"},
			{Key: "error", Description: "Descrição do erro quando a criação falha"},
		},
		DefaultConfig: map[string]interface{}{
			"title":              "Reunião Agendada",
			"description":        "Reunião agendada via " + brand.Active().Name,
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
			{Key: "title", Label: "Título", Type: "text", Placeholder: "Ex: Reunião de alinhamento", Required: true},
			{Key: "description", Label: "Descrição", Type: "textarea", Placeholder: "Contexto da reunião, pauta, observações..."},
			{Key: "location", Label: "Local", Type: "text", Placeholder: "Opcional. Ex: Escritório ou link externo"},
			{Key: "start_time", Label: "Início", Type: "text", Placeholder: "2026-03-31T14:00:00-03:00 ou {{var.start_time}}", Required: true},
			{Key: "end_time", Label: "Fim", Type: "text", Placeholder: "Opcional. Se vazio, usa a duração abaixo"},
			{Key: "duration", Label: "Duração (minutos)", Type: "number", Placeholder: "30"},
			{Key: "timezone", Label: "Timezone", Type: "text", Placeholder: "America/Sao_Paulo", Required: true},
			{Key: "attendees", Label: "Convidados", Type: "textarea", Placeholder: "email1@empresa.com, email2@empresa.com"},
			{
				Key:   "create_google_meet",
				Label: "Criar link do Google Meet",
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

func (s *scheduleMeetingExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	logPrefix := fmt.Sprintf("[workflow][schedule_meeting][node:%s][run:%s]",
		scheduleMeetingNodeID(ctx), scheduleMeetingRunID(ctx))
	log.Printf("%s config: start_time=%q end_time=%q title=%q duration=%v timezone=%q attendees=%q",
		logPrefix,
		scheduleMeetingInterpolatedString(ctx, "start_time"),
		scheduleMeetingInterpolatedString(ctx, "end_time"),
		scheduleMeetingInterpolatedString(ctx, "title"),
		ctx.Node.Config["duration"],
		scheduleMeetingInterpolatedString(ctx, "timezone"),
		scheduleMeetingInterpolatedString(ctx, "attendees"),
	)

	if s.repo == nil || s.google == nil {
		return scheduleMeetingFailure(ctx, "integração Google Calendar indisponível no servidor"), nil
	}

	workspaceID := scheduleMeetingWorkspaceID(ctx)
	if workspaceID == "" {
		return scheduleMeetingFailure(ctx, "workspace do fluxo não encontrado"), nil
	}

	conn, err := s.repo.GetConnection(workspaceID)
	if err != nil {
		return scheduleMeetingFailure(ctx, fmt.Sprintf("falha ao consultar integração do workspace: %v", err)), nil
	}
	if conn == nil {
		return scheduleMeetingFailure(ctx, "nenhuma conta Google Calendar conectada neste workspace"), nil
	}

	title := strings.TrimSpace(scheduleMeetingInterpolatedString(ctx, "title"))
	if title == "" {
		return scheduleMeetingFailure(ctx, "o campo Título é obrigatório"), nil
	}

	timezoneName := strings.TrimSpace(scheduleMeetingInterpolatedString(ctx, "timezone"))
	if timezoneName == "" {
		timezoneName = "America/Sao_Paulo"
	}
	location, err := time.LoadLocation(timezoneName)
	if err != nil {
		return scheduleMeetingFailure(ctx, fmt.Sprintf("timezone inválida: %s", timezoneName)), nil
	}

	startRaw := strings.TrimSpace(scheduleMeetingInterpolatedString(ctx, "start_time"))
	if startRaw == "" {
		return scheduleMeetingFailure(ctx, "o campo Início é obrigatório"), nil
	}
	startTime, err := parseScheduleMeetingTime(startRaw, location)
	if err != nil {
		return scheduleMeetingFailure(ctx, fmt.Sprintf("data/hora de início inválida: %v", err)), nil
	}

	endRaw := strings.TrimSpace(scheduleMeetingInterpolatedString(ctx, "end_time"))
	endTime := time.Time{}
	if endRaw != "" {
		endTime, err = parseScheduleMeetingTime(endRaw, location)
		if err != nil {
			return scheduleMeetingFailure(ctx, fmt.Sprintf("data/hora de fim inválida: %v", err)), nil
		}
	} else {
		durationMinutes := scheduleMeetingDuration(ctx)
		if durationMinutes <= 0 {
			// Neither an explicit end nor a positive duration was provided (e.g.
			// the AI scheduled with only start_time + title). Fall back to the
			// node's DefaultConfig 30-minute duration instead of failing the run.
			durationMinutes = 30
		}
		endTime = startTime.Add(time.Duration(durationMinutes) * time.Minute)
	}

	if !endTime.After(startTime) {
		return scheduleMeetingFailure(ctx, "o fim da reunião precisa ser depois do início"), nil
	}

	attendees, err := parseScheduleMeetingAttendees(ctx.Node.Config["attendees"], ctx.State)
	if err != nil {
		return scheduleMeetingFailure(ctx, err.Error()), nil
	}

	accessToken, err := ensureValidScheduleMeetingToken(s.google, s.repo, conn)
	if err != nil {
		return scheduleMeetingFailure(ctx, fmt.Sprintf("falha ao autenticar no Google Calendar: %v", err)), nil
	}

	conflictEvents, conflictErr := s.google.ListGoogleEvents(accessToken, startTime, endTime, "", 10)
	if conflictErr == nil {

		conflictEvents = mergeLocalEvents(s.repo, workspaceID, startTime, endTime, conflictEvents)
		for _, ev := range conflictEvents {
			if ev == nil || ev.Transparency == "transparent" {
				continue
			}
			if ev.StartTime.Before(endTime) && ev.EndTime.After(startTime) {
				log.Printf("[workflow][schedule_meeting][run:%s] CONFLICT: event %q (%s–%s) overlaps requested slot %s–%s",
					scheduleMeetingRunID(ctx), ev.Title,
					ev.StartTime.Format(time.RFC3339), ev.EndTime.Format(time.RFC3339),
					startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))
				return scheduleMeetingFailure(ctx, fmt.Sprintf(
					"O horário %s – %s já está ocupado (conflito com: %s). Escolha outro horário.",
					startTime.In(location).Format("02/01 15:04"),
					endTime.In(location).Format("15:04"),
					ev.Title,
				)), nil
			}
		}
	}

	event := &calendar.CalendarEvent{
		WorkspaceID:             workspaceID,
		Title:                   title,
		Description:             scheduleMeetingInterpolatedString(ctx, "description"),
		Location:                scheduleMeetingInterpolatedString(ctx, "location"),
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

	createGoogleMeet := scheduleMeetingBool(ctx.Node.Config["create_google_meet"], true)
	sendUpdates := strings.TrimSpace(scheduleMeetingInterpolatedString(ctx, "send_updates"))
	if sendUpdates == "" {
		sendUpdates = "all"
	}

	googleEventID, meetingLink, err := s.google.CreateGoogleEvent(accessToken, event, createGoogleMeet, sendUpdates)
	if err != nil {
		return scheduleMeetingFailure(ctx, fmt.Sprintf("falha ao criar evento no Google Calendar: %v", err)), nil
	}

	log.Printf("[workflow][node:%s][run:%s] action_schedule_meeting: created google event=%s workspace=%s meet=%t",
		ctx.Node.ID,
		scheduleMeetingRunID(ctx),
		googleEventID,
		workspaceID,
		meetingLink != "",
	)

	return &workflow.NodeResult{
		NextNodeID: scheduleMeetingResolveEdge(ctx, "sucesso"),
		Output: map[string]interface{}{
			"success":         true,
			"title":           title,
			"google_event_id": googleEventID,
			"meeting_link":    meetingLink,
			"start_time":      startTime.Format(time.RFC3339),
			"end_time":        endTime.Format(time.RFC3339),
			"timezone":        timezoneName,
			"attendees":       attendees,
			"calendar_email":  conn.Email,
		},
	}, nil
}

func scheduleMeetingFailure(ctx *workflow.NodeContext, message string) *workflow.NodeResult {
	log.Printf("[workflow][schedule_meeting][node:%s][run:%s] FAILED: %s",
		scheduleMeetingNodeID(ctx), scheduleMeetingRunID(ctx), message)
	return &workflow.NodeResult{
		// STRICT: route to "erro" only if that edge is actually wired. Never fall
		// back to the first edge, a failed schedule must not flow down the
		// success path. With no "erro" edge wired the run ends here.
		NextNodeID: scheduleMeetingResolveEdgeStrict(ctx, "erro"),
		Output: map[string]interface{}{
			"success": false,
			"error":   message,
		},
	}
}

func scheduleMeetingNodeID(ctx *workflow.NodeContext) string {
	if ctx == nil || ctx.Node == nil {
		return ""
	}
	return ctx.Node.ID
}

func scheduleMeetingResolveEdge(ctx *workflow.NodeContext, label string) string {
	if ctx == nil || ctx.Graph == nil || ctx.Node == nil {
		return ""
	}
	return resolveEdgeByLabel(ctx.Graph.OutgoingEdges(ctx.Node.ID), label)
}

// scheduleMeetingResolveEdgeStrict resolves an edge by label WITHOUT the
// fall-back-to-first-edge behaviour, so the error path can never be mistaken for
// the success path when no "erro" edge is wired.
func scheduleMeetingResolveEdgeStrict(ctx *workflow.NodeContext, label string) string {
	if ctx == nil || ctx.Graph == nil || ctx.Node == nil {
		return ""
	}
	return resolveEdgeByLabelStrict(ctx.Graph.OutgoingEdges(ctx.Node.ID), label)
}

func scheduleMeetingWorkspaceID(ctx *workflow.NodeContext) string {
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

func scheduleMeetingRunID(ctx *workflow.NodeContext) string {
	if ctx == nil || ctx.Run == nil {
		return ""
	}
	return ctx.Run.ID
}

func scheduleMeetingInterpolatedString(ctx *workflow.NodeContext, key string) string {
	if ctx == nil || ctx.Node == nil || ctx.Node.Config == nil {
		return ""
	}
	val, ok := ctx.Node.Config[key]
	if !ok || val == nil {
		return ""
	}
	str, ok := val.(string)
	if !ok {
		return fmt.Sprintf("%v", val)
	}
	return strings.TrimSpace(workflow.Interpolate(str, ctx.State, nil))
}

func scheduleMeetingDuration(ctx *workflow.NodeContext) int {
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

func scheduleMeetingBool(raw interface{}, fallback bool) bool {
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

func parseScheduleMeetingTime(raw string, location *time.Location) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("valor vazio")
	}

	for _, layout := range []string{
		time.RFC3339,
		time.RFC3339Nano,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
	} {
		var (
			parsed time.Time
			err    error
		)
		if layout == time.RFC3339 || layout == time.RFC3339Nano {
			parsed, err = time.Parse(layout, raw)
		} else {
			parsed, err = time.ParseInLocation(layout, raw, location)
		}
		if err == nil {
			return parsed, nil
		}
	}

	return time.Time{}, fmt.Errorf("use RFC3339 ou formatos como 2026-03-31 14:00")
}

func parseScheduleMeetingAttendees(raw interface{}, state *workflow.RunState) ([]calendar.Attendee, error) {
	if raw == nil {
		return nil, nil
	}

	values := make([]string, 0)
	switch v := raw.(type) {
	case string:
		interpolated := strings.TrimSpace(workflow.Interpolate(v, state, nil))
		if interpolated == "" {
			return nil, nil
		}
		values = splitAttendeeTokens(interpolated)
	case []string:
		values = append(values, v...)
	case []interface{}:
		for _, item := range v {
			values = append(values, fmt.Sprintf("%v", item))
		}
	default:
		values = append(values, fmt.Sprintf("%v", v))
	}

	attendees := make([]calendar.Attendee, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, token := range values {
		candidate := strings.TrimSpace(workflow.Interpolate(token, state, nil))
		if candidate == "" {
			continue
		}
		parsed, err := mail.ParseAddress(candidate)
		if err != nil {
			parsed, err = mail.ParseAddress("<" + candidate + ">")
			if err != nil {
				return nil, fmt.Errorf("convidado inválido: %s", candidate)
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

	return attendees, nil
}

func splitAttendeeTokens(raw string) []string {
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

func ensureValidScheduleMeetingToken(google calendar.GoogleOAuthService, repo calendar.Repository, conn *calendar.GoogleCalendarConnection) (string, error) {
	if time.Now().Before(conn.TokenExpiry.Add(-time.Minute)) {
		return conn.AccessToken, nil
	}

	refreshed, err := google.RefreshAccessToken(conn.RefreshToken)
	if err != nil {
		return "", err
	}

	expiry := time.Now().Add(time.Duration(refreshed.ExpiresIn) * time.Second)
	refreshToken := refreshed.RefreshToken
	if refreshToken == "" {
		refreshToken = conn.RefreshToken
	}

	if err := repo.UpdateConnectionTokens(conn.ID, refreshed.AccessToken, refreshToken, expiry); err != nil {
		log.Printf("[workflow][schedule_meeting] failed to persist refreshed token for workspace=%s: %v", conn.WorkspaceID, err)
	}

	return refreshed.AccessToken, nil
}
