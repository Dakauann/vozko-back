package node_executors

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"vozko/domain/calendar"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type checkCalendarAvailabilityExecutor struct {
	repo   calendar.Repository
	google calendar.GoogleCalendarAPI
	access calendar.AccessProvider
}

func NewCheckCalendarAvailabilityExecutor(repo calendar.Repository, google calendar.GoogleCalendarAPI, access calendar.AccessProvider) workflow.NodeExecutor {
	return &checkCalendarAvailabilityExecutor{repo: repo, google: google, access: access}
}

func (e *checkCalendarAvailabilityExecutor) Definition() workflow.NodeDefinition {
	return workflow.NodeDefinition{
		Type:         workflow.NodeTypeActionCheckCalendarAvailability,
		TestBehavior: workflow.NodeTestExternalCall,
		Category:     workflow.NodeCategoryAction,
		Scopes:       []workflow.NodeScope{workflow.NodeScopeShared},
		Label:        "Verificar Disponibilidade",
		Description:  "Consulta o Google Calendar do workspace e retorna horários livres no período informado.",
		Icon:         "CalendarBlank",
		Guidance: workflow.NodeGuidance{
			When: "Para consultar horários livres no Google Calendar.",
		},
		Outputs: []workflow.HandleDefinition{
			{ID: "sucesso", Label: "Sucesso"},
			{ID: "erro", Label: "Erro", Optional: true},
		},
		OutputKeys: []workflow.OutputKeyDefinition{
			{Key: "success", Description: "true quando a consulta foi bem-sucedida", Label: "Deu certo", Type: workflow.VariableTypeBoolean},
			{Key: "available_slots", Description: "Lista de horários livres formatados", Label: "Horários livres", Type: workflow.VariableTypeText},
			{Key: "available_slots_json", Description: "JSON array com objetos {start, end} dos slots livres", Label: "Horários livres (JSON)", Type: workflow.VariableTypeText},
			{Key: "busy_count", Description: "Quantidade de eventos ocupados encontrados", Label: "Eventos ocupados", Type: workflow.VariableTypeNumber},
			{Key: "date_from", Description: "Início do período consultado (RFC3339)", Label: "Início do período", Type: workflow.VariableTypeDate},
			{Key: "date_to", Description: "Fim do período consultado (RFC3339)", Label: "Fim do período", Type: workflow.VariableTypeDate},
			{Key: "timezone", Description: "Timezone efetivamente usada", Label: "Fuso horário", Type: workflow.VariableTypeText},
			{Key: "calendar_email", Description: "Conta Google conectada ao workspace", Label: "Conta Google", Type: workflow.VariableTypeText},
			{Key: "error", Description: "Descrição do erro quando a consulta falha", Label: "Erro", Type: workflow.VariableTypeText},
		},
		DefaultConfig: map[string]interface{}{
			"date_from":     "{{var.date_from}}",
			"date_to":       "",
			"timezone":      "America/Sao_Paulo",
			"slot_duration": 30,
			"working_start": "08:00",
			"working_end":   "18:00",
			"work_days":     "weekdays",
		},
		ConfigSchema: []workflow.ConfigField{
			{Key: "date_from", Label: "Data/hora início", Type: "text", Placeholder: "2026-04-10T08:00:00-03:00 ou {{var.date_from}}", Required: true},
			{Key: "date_to", Label: "Data/hora fim", Type: "text", Placeholder: "Opcional. Se vazio, consulta até o fim do horário comercial do dia"},
			{Key: "timezone", Label: "Timezone", Type: "text", Placeholder: "America/Sao_Paulo", Required: true},
			{Key: "slot_duration", Label: "Duração do slot (minutos)", Type: "number", Placeholder: "30"},
			{Key: "working_start", Label: "Início do expediente", Type: "text", Placeholder: "08:00"},
			{Key: "working_end", Label: "Fim do expediente", Type: "text", Placeholder: "18:00"},
			{
				Key:   "work_days",
				Label: "Dias de trabalho",
				Type:  "select",
				Options: []workflow.ConfigFieldOption{
					{Value: "weekdays", Label: "Segunda a Sexta"},
					{Value: "weekdays_saturday", Label: "Segunda a Sábado"},
					{Value: "all_week", Label: "Todos os dias"},
				},
			},
		},
	}
}

func (e *checkCalendarAvailabilityExecutor) Execute(ctx *workflow.NodeContext) (*workflow.NodeResult, error) {
	if e.repo == nil || e.google == nil || e.access == nil {
		return calAvailFailure(ctx, "integração Google Calendar indisponível no servidor"), nil
	}

	workspaceID := calendarWorkspaceID(ctx)
	if workspaceID == "" {
		return calAvailFailure(ctx, "workspace do fluxo não encontrado"), nil
	}

	googleAccess, err := e.access.CalendarAccess(context.Background(), workspaceID)
	if err != nil {
		return calAvailFailure(ctx, calendarAccessFailure(err)), nil
	}

	timezoneName := strings.TrimSpace(calendarConfigText(ctx, "timezone"))
	if timezoneName == "" {
		timezoneName = "America/Sao_Paulo"
	}
	loc, err := time.LoadLocation(timezoneName)
	if err != nil {
		return calAvailFailure(ctx, fmt.Sprintf("timezone inválida: %s", timezoneName)), nil
	}

	fromRaw := strings.TrimSpace(calendarConfigText(ctx, "date_from"))
	if fromRaw == "" {
		return calAvailFailure(ctx, "o campo Data/hora início é obrigatório"), nil
	}
	dateFrom, err := parseCalendarTime(fromRaw, loc)
	if err != nil {
		return calAvailFailure(ctx, fmt.Sprintf("data/hora início inválida: %v", err)), nil
	}

	toRaw := strings.TrimSpace(calendarConfigText(ctx, "date_to"))
	var dateTo time.Time
	if toRaw != "" {
		dateTo, err = parseCalendarTime(toRaw, loc)
		if err != nil {
			return calAvailFailure(ctx, fmt.Sprintf("data/hora fim inválida: %v", err)), nil
		}
	} else {

		workEnd := parseHHMM(calendarConfigText(ctx, "working_end"), 18, 0)
		dateTo = time.Date(dateFrom.Year(), dateFrom.Month(), dateFrom.Day(), workEnd.hour, workEnd.minute, 0, 0, loc)
		if !dateTo.After(dateFrom) {
			dateTo = dateFrom.Add(8 * time.Hour)
		}
	}

	if !dateTo.After(dateFrom) {
		return calAvailFailure(ctx, "a data de fim precisa ser depois da data de início"), nil
	}

	slotMinutes := 30
	if raw, ok := ctx.Node.Config["slot_duration"]; ok {
		switch v := raw.(type) {
		case float64:
			if v > 0 {
				slotMinutes = int(v)
			}
		case int:
			if v > 0 {
				slotMinutes = v
			}
		}
	}

	workStart := parseHHMM(calendarConfigText(ctx, "working_start"), 8, 0)
	workEnd := parseHHMM(calendarConfigText(ctx, "working_end"), 18, 0)

	workDays := parseWorkDays(calendarConfigText(ctx, "work_days"))

	accessToken := googleAccess.Token

	events, err := e.google.ListGoogleEvents(accessToken, dateFrom, dateTo, "", 250)
	if err != nil {
		return calAvailFailure(ctx, fmt.Sprintf("falha ao consultar eventos: %v", err)), nil
	}

	events = mergeLocalEvents(e.repo, workspaceID, dateFrom, dateTo, events)

	slots := computeFreeSlots(dateFrom, dateTo, events, slotMinutes, workStart, workEnd, loc, workDays)

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

	log.Printf("[workflow][check_calendar_availability][run:%s] workspace=%s found %d busy events, %d free slots in [%s..%s]",
		calendarRunID(ctx), workspaceID, len(events), len(slots),
		dateFrom.Format(time.RFC3339), dateTo.Format(time.RFC3339))

	availableText := strings.Join(textParts, "\n")
	if len(textParts) == 0 {
		availableText = "Nenhum horário disponível no período."
	}

	return routeOutput(&workflow.NodeResult{
		Output: map[string]interface{}{
			"success":              true,
			"available_slots":      availableText,
			"available_slots_json": jsonSlots,
			"busy_count":           len(events),
			"date_from":            dateFrom.Format(time.RFC3339),
			"date_to":              dateTo.Format(time.RFC3339),
			"timezone":             timezoneName,
			"calendar_email":       googleAccess.Email,
		},
	}, outgoingEdges(ctx), "sucesso"), nil
}

type timeSlot struct {
	Start time.Time
	End   time.Time
}

func computeFreeSlots(from, to time.Time, events []*calendar.CalendarEvent, slotMinutes int, workStart, workEnd hhMM, loc *time.Location, workDays map[time.Weekday]bool) []timeSlot {
	slotDuration := time.Duration(slotMinutes) * time.Minute

	busy := make([]timeSlot, 0, len(events))
	for _, ev := range events {
		if ev == nil || ev.Transparency == "transparent" {
			continue
		}
		if ev.EndTime.After(from) && ev.StartTime.Before(to) {
			busy = append(busy, timeSlot{Start: ev.StartTime, End: ev.EndTime})
		}
	}
	sort.Slice(busy, func(i, j int) bool { return busy[i].Start.Before(busy[j].Start) })

	merged := make([]timeSlot, 0, len(busy))
	for _, b := range busy {
		if len(merged) > 0 && !b.Start.After(merged[len(merged)-1].End) {
			if b.End.After(merged[len(merged)-1].End) {
				merged[len(merged)-1].End = b.End
			}
		} else {
			merged = append(merged, b)
		}
	}

	var slots []timeSlot
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
			slots = append(slots, findSlotsInWindow(dayStart, dayEnd, merged, slotDuration)...)
		}

		current = time.Date(dayInLoc.Year(), dayInLoc.Month(), dayInLoc.Day()+1, 0, 0, 0, 0, loc)
	}

	return slots
}

func findSlotsInWindow(windowStart, windowEnd time.Time, busy []timeSlot, slotDuration time.Duration) []timeSlot {
	var slots []timeSlot
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
		slots = append(slots, generateSlots(cursor, gapEnd, slotDuration)...)
		if b.End.After(cursor) {
			cursor = b.End
		}
	}

	if cursor.Before(windowEnd) {
		slots = append(slots, generateSlots(cursor, windowEnd, slotDuration)...)
	}

	return slots
}

func generateSlots(from, to time.Time, duration time.Duration) []timeSlot {
	var slots []timeSlot
	cursor := from
	for cursor.Add(duration).Before(to) || cursor.Add(duration).Equal(to) {
		slots = append(slots, timeSlot{Start: cursor, End: cursor.Add(duration)})
		cursor = cursor.Add(duration)
	}
	return slots
}

type hhMM struct {
	hour   int
	minute int
}

func parseHHMM(raw string, defaultH, defaultM int) hhMM {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return hhMM{hour: defaultH, minute: defaultM}
	}
	var h, m int
	if _, err := fmt.Sscanf(raw, "%d:%d", &h, &m); err == nil && h >= 0 && h <= 23 && m >= 0 && m <= 59 {
		return hhMM{hour: h, minute: m}
	}
	return hhMM{hour: defaultH, minute: defaultM}
}

func calAvailFailure(ctx *workflow.NodeContext, message string) *workflow.NodeResult {
	return routeOutput(&workflow.NodeResult{
		Output: map[string]interface{}{
			"success": false,
			"error":   message,
		},
	}, outgoingEdges(ctx), "erro")
}

func parseWorkDays(raw string) map[time.Weekday]bool {
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

func mergeLocalEvents(repo calendar.Repository, workspaceID string, from, to time.Time, googleEvents []*calendar.CalendarEvent) []*calendar.CalendarEvent {
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
