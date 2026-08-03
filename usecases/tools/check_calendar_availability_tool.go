package tools_usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"vozko/domain/calendar"
	"vozko/domain/shared"
	"vozko/domain/tools"
)

const CheckCalendarAvailabilityToolName = "check_calendar_availability"

type checkCalendarAvailabilityTool struct {
	repo   calendar.Repository
	google calendar.GoogleOAuthService
}

func NewCheckCalendarAvailabilityToolUseCase(repo calendar.Repository, google calendar.GoogleOAuthService) tools.Handler {
	if repo == nil || google == nil {
		return nil
	}
	return &checkCalendarAvailabilityTool{repo: repo, google: google}
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
				Description:        "Data/hora de fim da busca no formato RFC3339. Opcional, se vazio, consulta até o fim do horário comercial do dia.",
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

	conn, err := t.repo.GetConnection(workspaceID)
	if err != nil {
		log.Printf("[CheckCalendarAvailability] failed to query connection for workspace %s: %v", workspaceID, err)
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
	dateFrom, err := parseToolTime(fromRaw, loc)
	if err != nil {
		return tools.ExecutionResult{
			Result:  fmt.Sprintf("Data/hora início inválida: %v", err),
			IsError: true,
		}, nil
	}

	toRaw, _ := params["date_to"].(string)
	toRaw = strings.TrimSpace(toRaw)
	var dateTo time.Time
	if toRaw != "" {
		dateTo, err = parseToolTime(toRaw, loc)
		if err != nil {
			return tools.ExecutionResult{
				Result:  fmt.Sprintf("Data/hora fim inválida: %v", err),
				IsError: true,
			}, nil
		}
	} else {

		workingEndRaw := toolStringParamOrConfig(params, config, "working_end", "18:00")
		we := parseCalToolHHMM(workingEndRaw, 18, 0)
		dateTo = time.Date(dateFrom.Year(), dateFrom.Month(), dateFrom.Day(), we.hour, we.minute, 0, 0, loc)
		if !dateTo.After(dateFrom) {
			dateTo = dateFrom.Add(8 * time.Hour)
		}
	}

	if !dateTo.After(dateFrom) {
		return tools.ExecutionResult{
			Result:  "A data de fim precisa ser depois da data de início.",
			IsError: true,
		}, nil
	}

	slotMinutes := toolIntParamOrConfig(params, config, "slot_duration", 30)

	accessToken, err := ensureValidCalendarToken(t.google, t.repo, conn)
	if err != nil {
		log.Printf("[CheckCalendarAvailability] token refresh failed for workspace %s: %v", workspaceID, err)
		return tools.ExecutionResult{
			Result:  fmt.Sprintf("Falha ao autenticar no Google Calendar: %v", err),
			IsError: true,
		}, nil
	}

	events, err := t.google.ListGoogleEvents(accessToken, dateFrom, dateTo, "", 250)
	if err != nil {
		log.Printf("[CheckCalendarAvailability] failed to list events for workspace %s: %v", workspaceID, err)
		return tools.ExecutionResult{
			Result:  fmt.Sprintf("Falha ao consultar eventos: %v", err),
			IsError: true,
		}, nil
	}

	events = mergeLocalCalToolEvents(t.repo, workspaceID, dateFrom, dateTo, events)

	workStartRaw := toolStringParamOrConfig(params, config, "working_start", "08:00")
	workEndRaw := toolStringParamOrConfig(params, config, "working_end", "18:00")
	workStart := parseCalToolHHMM(workStartRaw, 8, 0)
	workEnd := parseCalToolHHMM(workEndRaw, 18, 0)

	workDaysRaw := toolStringParamOrConfig(params, config, "work_days", "weekdays")
	workDays := parseCalToolWorkDays(workDaysRaw)

	slots := computeToolFreeSlots(dateFrom, dateTo, events, slotMinutes, workStart, workEnd, loc, workDays)

	var textParts []string
	var jsonSlots []map[string]string
	for _, s := range slots {
		textParts = append(textParts, fmt.Sprintf("%s – %s",
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
		"calendar_email":       conn.Email,
	}

	resultJSON, _ := json.Marshal(resultMap)
	return tools.ExecutionResult{
		Result: string(resultJSON),
	}, nil
}

func ensureValidCalendarToken(google calendar.GoogleOAuthService, repo calendar.Repository, conn *calendar.GoogleCalendarConnection) (string, error) {
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
		log.Printf("[calendar_tool] failed to persist refreshed token for workspace=%s: %v", conn.WorkspaceID, err)
	}

	return refreshed.AccessToken, nil
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

func parseToolTime(raw string, location *time.Location) (time.Time, error) {
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
		"2006-01-02",
	} {
		var parsed time.Time
		var err error
		if layout == time.RFC3339 || layout == time.RFC3339Nano {
			parsed, err = time.Parse(layout, raw)
		} else {
			parsed, err = time.ParseInLocation(layout, raw, location)
		}
		if err == nil {
			return parsed, nil
		}
	}

	return time.Time{}, fmt.Errorf("formato não reconhecido: %s", raw)
}

type calToolTimeSlot struct {
	Start time.Time
	End   time.Time
}

type calToolHHMM struct {
	hour   int
	minute int
}

func computeToolFreeSlots(from, to time.Time, events []*calendar.CalendarEvent, slotMinutes int, workStart, workEnd calToolHHMM, loc *time.Location, workDays map[time.Weekday]bool) []calToolTimeSlot {
	slotDuration := time.Duration(slotMinutes) * time.Minute

	busy := make([]calToolTimeSlot, 0, len(events))
	for _, ev := range events {
		if ev == nil || ev.Transparency == "transparent" {
			continue
		}
		if ev.EndTime.After(from) && ev.StartTime.Before(to) {
			busy = append(busy, calToolTimeSlot{Start: ev.StartTime, End: ev.EndTime})
		}
	}
	sort.Slice(busy, func(i, j int) bool { return busy[i].Start.Before(busy[j].Start) })

	merged := make([]calToolTimeSlot, 0, len(busy))
	for _, b := range busy {
		if len(merged) > 0 && !b.Start.After(merged[len(merged)-1].End) {
			if b.End.After(merged[len(merged)-1].End) {
				merged[len(merged)-1].End = b.End
			}
		} else {
			merged = append(merged, b)
		}
	}

	var slots []calToolTimeSlot
	current := from
	for current.Before(to) {
		dayInLoc := current.In(loc)
		weekday := dayInLoc.Weekday()

		if workDays != nil && !workDays[weekday] {
			current = time.Date(dayInLoc.Year(), dayInLoc.Month(), dayInLoc.Day()+1, 0, 0, 0, 0, loc)
			continue
		}

		dayStart := time.Date(dayInLoc.Year(), dayInLoc.Month(), dayInLoc.Day(), workStart.hour, workStart.minute, 0, 0, loc)
		dayEnd := time.Date(dayInLoc.Year(), dayInLoc.Month(), dayInLoc.Day(), workEnd.hour, workEnd.minute, 0, 0, loc)

		if dayStart.Before(from) {
			dayStart = from
		}
		if dayEnd.After(to) {
			dayEnd = to
		}

		if dayEnd.After(dayStart) {
			slots = append(slots, findToolSlotsInWindow(dayStart, dayEnd, merged, slotDuration)...)
		}

		current = time.Date(dayInLoc.Year(), dayInLoc.Month(), dayInLoc.Day()+1, 0, 0, 0, 0, loc)
	}

	return slots
}

func findToolSlotsInWindow(windowStart, windowEnd time.Time, busy []calToolTimeSlot, slotDuration time.Duration) []calToolTimeSlot {
	var slots []calToolTimeSlot
	cursor := windowStart

	for _, b := range busy {
		if b.End.Before(cursor) || b.End.Equal(cursor) {
			continue
		}
		if b.Start.After(windowEnd) || b.Start.Equal(windowEnd) {
			break
		}
		gapEnd := b.Start
		if gapEnd.After(windowEnd) {
			gapEnd = windowEnd
		}
		slots = append(slots, generateToolSlots(cursor, gapEnd, slotDuration)...)
		if b.End.After(cursor) {
			cursor = b.End
		}
	}

	if cursor.Before(windowEnd) {
		slots = append(slots, generateToolSlots(cursor, windowEnd, slotDuration)...)
	}

	return slots
}

func generateToolSlots(from, to time.Time, duration time.Duration) []calToolTimeSlot {
	var slots []calToolTimeSlot
	cursor := from
	for cursor.Add(duration).Before(to) || cursor.Add(duration).Equal(to) {
		slots = append(slots, calToolTimeSlot{Start: cursor, End: cursor.Add(duration)})
		cursor = cursor.Add(duration)
	}
	return slots
}

func parseCalToolHHMM(raw string, defaultH, defaultM int) calToolHHMM {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return calToolHHMM{hour: defaultH, minute: defaultM}
	}
	var h, m int
	if _, err := fmt.Sscanf(raw, "%d:%d", &h, &m); err == nil && h >= 0 && h <= 23 && m >= 0 && m <= 59 {
		return calToolHHMM{hour: h, minute: m}
	}
	return calToolHHMM{hour: defaultH, minute: defaultM}
}

func parseCalToolWorkDays(raw string) map[time.Weekday]bool {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "all_week":
		return map[time.Weekday]bool{
			time.Sunday: true, time.Monday: true, time.Tuesday: true,
			time.Wednesday: true, time.Thursday: true, time.Friday: true,
			time.Saturday: true,
		}
	case "weekdays_saturday":
		return map[time.Weekday]bool{
			time.Monday: true, time.Tuesday: true, time.Wednesday: true,
			time.Thursday: true, time.Friday: true, time.Saturday: true,
		}
	default:
		return map[time.Weekday]bool{
			time.Monday: true, time.Tuesday: true, time.Wednesday: true,
			time.Thursday: true, time.Friday: true,
		}
	}
}

func mergeLocalCalToolEvents(repo calendar.Repository, workspaceID string, from, to time.Time, googleEvents []*calendar.CalendarEvent) []*calendar.CalendarEvent {
	if repo == nil {
		return googleEvents
	}
	localResult, err := repo.ListEvents(calendar.ListEventsInput{
		WorkspaceID: workspaceID,
		From:        &from,
		To:          &to,
		Pagination:  shared.Pagination{Page: 1, PageSize: 500},
	})
	if err != nil || localResult == nil || len(localResult.Items) == 0 {
		return googleEvents
	}

	seen := make(map[string]bool, len(googleEvents))
	for _, ev := range googleEvents {
		if ev != nil && ev.GoogleEventID != "" {
			seen[ev.GoogleEventID] = true
		}
	}

	merged := make([]*calendar.CalendarEvent, len(googleEvents))
	copy(merged, googleEvents)
	for _, ev := range localResult.Items {
		if ev == nil {
			continue
		}
		if ev.GoogleEventID != "" && seen[ev.GoogleEventID] {
			continue
		}
		merged = append(merged, ev)
	}

	return merged
}

var _ tools.Handler = (*checkCalendarAvailabilityTool)(nil)
